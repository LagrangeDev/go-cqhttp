package gocq

import (
	"bufio"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"time"

	"github.com/LagrangeDev/LagrangeGo/utils"

	"github.com/LagrangeDev/LagrangeGo/client/packets/wtlogin/qrcodestate"

	"github.com/LagrangeDev/LagrangeGo/client/auth"

	"github.com/LagrangeDev/LagrangeGo/client"
	"github.com/mattn/go-colorable"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"gopkg.ilharper.com/x/isatty"
)

var console = bufio.NewReader(os.Stdin)

func readLine() (str string) {
	str, _ = console.ReadString('\n')
	str = strings.TrimSpace(str)
	return
}

func readLineTimeout(t time.Duration) {
	r := make(chan string)
	go func() {
		select {
		case r <- readLine():
		case <-time.After(t):
		}
	}()
	select {
	case <-r:
	case <-time.After(t):
	}
}

func readIfTTY(de string) (str string) {
	if isatty.Isatty(os.Stdin.Fd()) {
		return readLine()
	}
	log.Warnf("未检测到输入终端，自动选择%s.", de)
	return de
}

var cli *client.QQClient
var device *auth.DeviceInfo

// ErrSMSRequestError SMS请求出错
var ErrSMSRequestError = errors.New("sms request error")

func printQRCode(imgData []byte) {
	// (".", "^", " ", "@") : ("▄", "▀", " ", "█")
	const (
		bb = "█"
		wb = "▄"
		bw = "▀"
		ww = " "
	)
	img, err := png.Decode(bytes.NewReader(imgData))
	if err != nil {
		log.Panic(err)
	}

	bound := img.Bounds().Max.X
	buf := make([]byte, 0, (bound+1)*(bound/2+utils.Ternary(bound%2 == 0, 0, 1)))

	padding := 0
	lastColor := img.At(padding, padding).(color.Gray).Y
	for padding++; padding < bound; padding++ {
		if img.At(padding, padding).(color.Gray).Y != lastColor {
			break
		}
	}

	for y := padding; y < bound-padding; y += 2 {
		for x := padding; x < bound-padding; x++ {
			isUpWhite := img.At(x, y).(color.Gray).Y == 255
			isDownWhite := utils.Ternary(y < bound-padding, img.At(x, y+1).(color.Gray).Y == 255, false)

			switch {
			case !isUpWhite && !isDownWhite:
				buf = append(buf, bb...)
			case isUpWhite && !isDownWhite:
				buf = append(buf, wb...)
			case !isUpWhite:
				buf = append(buf, bw...)
			default:
				buf = append(buf, ww...)
			}
		}
		buf = append(buf, '\n')
	}
	_, _ = colorable.NewColorableStdout().Write(buf)
}

func printQRCodeCommon(imgData []byte) {
	const (
		black = "\033[48;5;0m  \033[0m"
		white = "\033[48;5;7m  \033[0m"
	)
	img, err := png.Decode(bytes.NewReader(imgData))
	if err != nil {
		log.Panic(err)
	}
	data := img.(*image.Gray).Pix
	bound := img.Bounds().Max.X
	buf := make([]byte, 0, (bound*4+1)*(bound))
	i := 0
	for y := 0; y < bound; y++ {
		i = y * bound
		for x := 0; x < bound; x++ {
			if data[i] != 255 {
				buf = append(buf, white...)
			} else {
				buf = append(buf, black...)
			}
			i++
		}
		buf = append(buf, '\n')
	}
	_, _ = colorable.NewColorableStdout().Write(buf)
}

func qrcodeLogin() error {
	qrcodeData, _, err := cli.FetchQRCode(1, 2, 1)
	if err != nil {
		return err
	}
	_ = os.WriteFile("qrcode.png", qrcodeData, 0o644)
	defer func() { _ = os.Remove("qrcode.png") }()
	if cli.Uin != 0 {
		log.Infof("请使用账号 %v 登录手机QQ扫描二维码 (qrcode.png) : ", cli.Uin)
	} else {
		log.Infof("请使用手机QQ扫描二维码 (qrcode.png) : ")
	}
	time.Sleep(time.Second)
	printQRCode(qrcodeData)
	s, err := cli.GetQRCodeResult()
	if err != nil {
		return err
	}
	prevState := s
	for {
		time.Sleep(time.Second)
		s, _ = cli.GetQRCodeResult()
		if prevState == s {
			continue
		}
		prevState = s
		switch s {
		case qrcodestate.Canceled:
			log.Fatalf("扫码被用户取消.")
		case qrcodestate.Expired:
			log.Fatalf("二维码过期")
		case qrcodestate.WaitingForConfirm:
			log.Infof("扫码成功, 请在手机端确认登录.")
		case qrcodestate.Confirmed:
			err := cli.QRCodeLogin(1)
			if err != nil {
				return err
			}
			return cli.Register()
		case qrcodestate.WaitingForScan:
			// ignore
		}
	}
}
