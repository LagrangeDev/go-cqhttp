package gocq

import (
	"bufio"
	"bytes"
	"image/color"
	"image/png"
	"os"
	"strings"
	"time"

	"github.com/LagrangeDev/LagrangeGo/client/packets/wtlogin/qrcodeState"
	"github.com/Mrs4s/go-cqhttp/utils/ternary"

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

func commonLogin() error {
	return errors.New("仅支持二维码登录")
	//res, err := cli.Login()
	//if err != nil {
	//	return err
	//}
	//return loginResponseProcessor(res)
}

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
	buf := make([]byte, 0, (bound+1)*(bound/2+ternary.BV(bound%2 == 0, 0, 1)))

	padding := 0
	lastColor := img.At(padding, padding).(color.Gray).Y
	for padding += 1; padding < bound; padding++ {
		if img.At(padding, padding).(color.Gray).Y != lastColor {
			break
		}
	}

	for y := padding; y < bound-padding; y += 2 {
		for x := padding; x < bound-padding; x++ {
			isUpWhite := img.At(x, y).(color.Gray).Y == 255
			isDownWhite := ternary.BV(y < bound-padding, img.At(x, y+1).(color.Gray).Y == 255, false)

			if !isUpWhite && !isDownWhite {
				buf = append(buf, bb...)
			} else if isUpWhite && !isDownWhite {
				buf = append(buf, wb...)
			} else if !isUpWhite && isDownWhite {
				buf = append(buf, bw...)
			} else if isUpWhite && isDownWhite {
				buf = append(buf, ww...)
			}
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
		case qrcodeState.Canceled:
			log.Fatalf("扫码被用户取消.")
		case qrcodeState.Expired:
			log.Fatalf("二维码过期")
		case qrcodeState.WaitingForConfirm:
			log.Infof("扫码成功, 请在手机端确认登录.")
		case qrcodeState.Confirmed:
			err := cli.QRCodeLogin(1)
			if err != nil {
				return err
			}
			return cli.Register()
		case qrcodeState.WaitingForScan:
			// ignore
		}
	}
}

//func loginResponseProcessor(res *client.LoginResponse) error {
//	var err error
//	for {
//		if err != nil {
//			return err
//		}
//		if res.Success {
//			return nil
//		}
//		var text string
//		switch res.Error {
//		case client.SliderNeededError:
//			log.Warnf("登录需要滑条验证码, 请验证后重试.")
//			ticket := getTicket(res.VerifyUrl)
//			if ticket == "" {
//				log.Infof("按 Enter 继续....")
//				readLine()
//				os.Exit(0)
//			}
//			res, err = cli.SubmitTicket(ticket)
//			continue
//		case client.NeedCaptcha:
//			log.Warnf("登录需要验证码.")
//			_ = os.WriteFile("captcha.jpg", res.CaptchaImage, 0o644)
//			log.Warnf("请输入验证码 (captcha.jpg)： (Enter 提交)")
//			text = readLine()
//			global.DelFile("captcha.jpg")
//			res, err = cli.SubmitCaptcha(text, res.CaptchaSign)
//			continue
//		case client.SMSNeededError:
//			log.Warnf("账号已开启设备锁, 按 Enter 向手机 %v 发送短信验证码.", res.SMSPhone)
//			readLine()
//			if !cli.RequestSMS() {
//				log.Warnf("发送验证码失败，可能是请求过于频繁.")
//				return errors.WithStack(ErrSMSRequestError)
//			}
//			log.Warn("请输入短信验证码： (Enter 提交)")
//			text = readLine()
//			res, err = cli.SubmitSMS(text)
//			continue
//		case client.SMSOrVerifyNeededError:
//			log.Warnf("账号已开启设备锁，请选择验证方式:")
//			log.Warnf("1. 向手机 %v 发送短信验证码", res.SMSPhone)
//			log.Warnf("2. 使用手机QQ扫码验证.")
//			log.Warn("请输入(1 - 2)：")
//			text = readIfTTY("2")
//			if strings.Contains(text, "1") {
//				if !cli.RequestSMS() {
//					log.Warnf("发送验证码失败，可能是请求过于频繁.")
//					return errors.WithStack(ErrSMSRequestError)
//				}
//				log.Warn("请输入短信验证码： (Enter 提交)")
//				text = readLine()
//				res, err = cli.SubmitSMS(text)
//				continue
//			}
//			fallthrough
//		case client.UnsafeDeviceError:
//			log.Warnf("账号已开启设备锁，请前往 -> %v <- 验证后重启Bot.", res.VerifyUrl)
//			log.Infof("按 Enter 或等待 5s 后继续....")
//			readLineTimeout(time.Second * 5)
//			os.Exit(0)
//		case client.OtherLoginError, client.UnknownLoginError, client.TooManySMSRequestError:
//			msg := res.ErrorMessage
//			log.Warnf("登录失败: %v Code: %v", msg, res.Code)
//			switch res.Code {
//			case 235:
//				log.Warnf("设备信息被封禁, 请删除 device.json 后重试.")
//			case 237:
//				log.Warnf("登录过于频繁, 请在手机QQ登录并根据提示完成认证后等一段时间重试")
//			case 45:
//				log.Warnf("你的账号被限制登录, 请配置 SignServer 后重试")
//			}
//			log.Infof("按 Enter 继续....")
//			readLine()
//			os.Exit(0)
//		}
//	}
//}

//func getTicket(u string) string {
//	log.Warnf("请选择提交滑块ticket方式:")
//	log.Warnf("1. 自动提交")
//	log.Warnf("2. 手动抓取提交")
//	log.Warn("请输入(1 - 2)：")
//	text := readLine()
//	id := utils.RandomString(8)
//	auto := !strings.Contains(text, "2")
//	if auto {
//		u = strings.ReplaceAll(u, "https://ssl.captcha.qq.com/template/wireless_mqq_captcha.html?", fmt.Sprintf("https://captcha.go-cqhttp.org/captcha?id=%v&", id))
//	}
//	log.Warnf("请前往该地址验证 -> %v ", u)
//	if !auto {
//		log.Warn("请输入ticket： (Enter 提交)")
//		return readLine()
//	}
//
//	for count := 120; count > 0; count-- {
//		str := fetchCaptcha(id)
//		if str != "" {
//			return str
//		}
//		time.Sleep(time.Second)
//	}
//	log.Warnf("验证超时")
//	return ""
//}
//
//func fetchCaptcha(id string) string {
//	g, err := download.Request{URL: "https://captcha.go-cqhttp.org/captcha/ticket?id=" + id}.JSON()
//	if err != nil {
//		log.Debugf("获取 Ticket 时出现错误: %v", err)
//		return ""
//	}
//	if g.Get("ticket").Exists() {
//		return g.Get("ticket").String()
//	}
//	return ""
//}
