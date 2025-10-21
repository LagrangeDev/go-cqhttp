package gocq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LagrangeDev/LagrangeGo/client/auth"
	"github.com/LagrangeDev/LagrangeGo/client/sign"
	"github.com/LagrangeDev/LagrangeGo/utils/io"
	log "github.com/sirupsen/logrus"

	"github.com/Mrs4s/go-cqhttp/internal/base"
)

const serverLatencyDown = math.MaxUint32

// ErrAllSignDown 所有签名服务器都不可用
var ErrAllSignDown = errors.New("all sign down")

type (
	signer struct {
		lock         sync.RWMutex
		instances    []*remote
		app          *auth.AppInfo
		extraHeaders http.Header
		doneChan     chan struct{}
	}

	remote struct {
		server  string
		latency atomic.Uint32
	}
)

func newSigner() *signer {
	return &signer{
		extraHeaders: http.Header{},
		doneChan:     make(chan struct{}),
	}
}

func (c *signer) init() {
	go func() {
		c.check()
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		select {
		case <-c.doneChan:
			return
		case <-ticker.C:
			c.check()
		}
	}()
}

// Release 释放资源
func (c *signer) Release() {
	close(c.doneChan)
}

// Sign 对数据包签名
func (c *signer) Sign(cmd string, seq uint32, data []byte) (*sign.Response, error) {
	sortFlag := false
	defer func() {
		if sortFlag {
			c.sortByLatency()
		}
	}()
	// 防止死锁
	c.lock.RLock()
	defer c.lock.RUnlock()
	for _, instance := range c.instances {
		resp, err := instance.sign(cmd, seq, data, c.extraHeaders)
		if err == nil {
			return resp, nil
		}
		sortFlag = true
		instance.latency.Store(serverLatencyDown)
		log.Errorf("签名时出现错误：%v", err)
	}
	return nil, ErrAllSignDown
}

func (c *signer) sortByLatency() {
	c.lock.Lock()
	defer c.lock.Unlock()
	sort.Slice(c.instances, func(i, j int) bool {
		return c.instances[i].latency.Load() < c.instances[j].latency.Load()
	})
}

// AddRequestHeader 添加请求头
func (c *signer) AddRequestHeader(header map[string]string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	for k, v := range header {
		c.extraHeaders.Add(k, v)
	}
}

// AddSignServer 添加签名服务器
func (c *signer) AddSignServer(signServers ...string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.instances = append(c.instances, io.Map(signServers, func(s string) *remote {
		return &remote{server: s}
	})...)
}

// GetSignServer 获取签名服务器
func (c *signer) GetSignServer() []string {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return io.Map(c.instances, func(sign *remote) string {
		return sign.server
	})
}

// SetAppInfo 设置版本信息
func (c *signer) SetAppInfo(app *auth.AppInfo) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.app = app
	c.extraHeaders.Set("User-Agent", fmt.Sprintf("qq/%s (%s_%s) go-cqhttp/%s",
		app.CurrentVersion, runtime.GOOS, runtime.GOARCH, base.Version))
}

func (c *signer) check() {
	log.Infoln("开始签名服务器质量测试")
	availableQuantity := 0
	wg := sync.WaitGroup{}
	c.lock.RLock()
	for _, instance := range c.instances {
		wg.Add(1)
		go func(i *remote) {
			defer wg.Done()
			i.test()
		}(instance)
	}
	wg.Wait()
	for _, instance := range c.instances {
		if instance.latency.Load() < serverLatencyDown {
			availableQuantity++
		}
	}
	c.lock.RUnlock()
	c.sortByLatency()
	log.Infof("签名服务器质量测试完成，可用服务器数量: %d", availableQuantity)
}

func (i *remote) sign(cmd string, seq uint32, buf []byte, header http.Header) (signResp *sign.Response, err error) {
	if !(sign.ContainSignPKG(cmd) || cmd == "wtlogin.trans_emp") {
		return nil, nil
	}
	signReq := sign.Request{
		Cmd: cmd,
		Seq: int(seq),
		Src: buf,
	}
	u, err := url.Parse(i.server)
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(&signReq)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(&signResp)
	if err != nil {
		return nil, err
	}

	return signResp, nil
}

func (i *remote) test() {
	startTime := time.Now().UnixMilli()
	resp, err := i.sign("wtlogin.login", 1, []byte{11, 45, 14}, nil)
	if err != nil || len(resp.Value.Sign) == 0 {
		log.Warnf("测试签名服务器：%s时出现错误: %v", i.server, err)
		i.latency.Store(serverLatencyDown)
		return
	}
	// 有长连接的情况，取两次平均值
	resp, err = i.sign("wtlogin.login", 1, []byte{11, 45, 14}, nil)
	if err != nil || len(resp.Value.Sign) == 0 {
		log.Warnf("测试签名服务器：%s时出现错误: %v", i.server, err)
		i.latency.Store(serverLatencyDown)
		return
	}
	latency := (time.Now().UnixMilli() - startTime) / 2
	i.latency.Store(uint32(latency))
	log.Debugf("签名服务器：%s，延迟：%dms", i.server, latency)
}
