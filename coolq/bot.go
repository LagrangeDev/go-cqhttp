package coolq

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/LagrangeDev/LagrangeGo/utils/binary"

	"github.com/Mrs4s/go-cqhttp/internal/mime"
	"golang.org/x/image/webp"

	"github.com/LagrangeDev/LagrangeGo/client/entity"
	event2 "github.com/LagrangeDev/LagrangeGo/client/event"
	"github.com/Mrs4s/go-cqhttp/utils"

	"github.com/LagrangeDev/LagrangeGo/client"
	"github.com/LagrangeDev/LagrangeGo/message"
	"github.com/RomiChan/syncx"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/Mrs4s/go-cqhttp/db"
	"github.com/Mrs4s/go-cqhttp/global"
	"github.com/Mrs4s/go-cqhttp/internal/base"
	"github.com/Mrs4s/go-cqhttp/internal/msg"
	"github.com/Mrs4s/go-cqhttp/pkg/onebot"
)

// CQBot CQBot结构体,存储Bot实例相关配置
type CQBot struct {
	Client *client.QQClient

	lock   sync.RWMutex
	events []func(*Event)

	friendReqCache syncx.Map[string, *event2.NewFriendRequest]
	// tempSessionCache syncx.Map[int64, *event2.]
}

// Event 事件
type Event struct {
	once   sync.Once
	Raw    *event
	buffer *bytes.Buffer
}

func (e *Event) marshal() {
	if e.buffer == nil {
		e.buffer = global.NewBuffer()
	}
	_ = json.NewEncoder(e.buffer).Encode(e.Raw)
}

// JSONBytes return byes of json by lazy marshalling.
func (e *Event) JSONBytes() []byte {
	e.once.Do(e.marshal)
	return e.buffer.Bytes()
}

// JSONString return string of json without extra allocation
// by lazy marshalling.
func (e *Event) JSONString() string {
	e.once.Do(e.marshal)
	return utils.B2S(e.buffer.Bytes())
}

// NewQQBot 初始化一个QQBot实例
func NewQQBot(cli *client.QQClient) *CQBot {
	bot := &CQBot{
		Client: cli,
	}
	bot.Client.PrivateMessageEvent.Subscribe(bot.privateMessageEvent)
	bot.Client.GroupMessageEvent.Subscribe(bot.groupMessageEvent)
	if base.ReportSelfMessage {
		bot.Client.SelfPrivateMessageEvent.Subscribe(bot.privateMessageEvent)
		bot.Client.SelfGroupMessageEvent.Subscribe(bot.groupMessageEvent)
	}
	// bot.Client.TempMessageEvent.Subscribe(bot.tempMessageEvent)
	bot.Client.GroupMuteEvent.Subscribe(bot.groupMutedEvent)
	bot.Client.GroupRecallEvent.Subscribe(bot.groupRecallEvent)
	bot.Client.GroupNotifyEvent.Subscribe(bot.groupNotifyEvent)
	bot.Client.FriendNotifyEvent.Subscribe(bot.friendNotifyEvent)
	bot.Client.MemberSpecialTitleUpdatedEvent.Subscribe(bot.memberTitleUpdatedEvent)
	bot.Client.FriendRecallEvent.Subscribe(bot.friendRecallEvent)
	// TODO 离线文件
	// bot.Client.OfflineFileEvent.Subscribe(bot.offlineFileEvent)
	// TODO bot加群
	bot.Client.GroupJoinEvent.Subscribe(bot.joinGroupEvent)
	// TODO bot退群
	bot.Client.GroupLeaveEvent.Subscribe(bot.leaveGroupEvent)
	bot.Client.GroupMemberJoinEvent.Subscribe(bot.memberJoinEvent)
	bot.Client.GroupMemberLeaveEvent.Subscribe(bot.memberLeaveEvent)
	// TODO 群成员权限变更
	// bot.Client.GroupMemberPermissionChangedEvent.Subscribe(bot.memberPermissionChangedEvent)
	// TODO 群成员名片更新
	// bot.Client.MemberCardUpdatedEvent.Subscribe(bot.memberCardUpdatedEvent)
	// bot.Client.FriendRequestEvent.Subscribe(bot.friendRequestEvent)
	// TODO 成为好友
	// bot.Client.NewFriendEvent.Subscribe(bot.friendAddedEvent)
	// bot.Client.GroupInvitedEvent.Subscribe(bot.groupInvitedEvent)
	// bot.Client.GroupMemberJoinRequestEvent.Subscribe(bot.groupJoinReqEvent)
	// TODO 客户端变更
	// bot.Client.OtherClientStatusChangedEvent.Subscribe(bot.otherClientStatusChangedEvent)
	// TODO 精华消息
	bot.Client.GroupDigestEvent.Subscribe(bot.groupEssenceMsg)
	go func() {
		if base.HeartbeatInterval == 0 {
			log.Warn("警告: 心跳功能已关闭，若非预期，请检查配置文件。")
			return
		}
		t := time.NewTicker(base.HeartbeatInterval)
		for {
			<-t.C
			bot.dispatchEvent("meta_event/heartbeat", global.MSG{
				"status":   bot.CQGetStatus(onebot.V11)["data"],
				"interval": base.HeartbeatInterval.Milliseconds(),
			})
		}
	}()
	return bot
}

// OnEventPush 注册事件上报函数
func (bot *CQBot) OnEventPush(f func(e *Event)) {
	bot.lock.Lock()
	bot.events = append(bot.events, f)
	bot.lock.Unlock()
}

type worker struct {
	wg sync.WaitGroup
}

func (w *worker) do(f func()) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		f()
	}()
}

func (w *worker) wait() {
	w.wg.Wait()
}

// uploadLocalVoice 上传语音
func (bot *CQBot) uploadLocalVoice(target message.Source, voice *message.VoiceElement) (message.IMessageElement, error) {
	switch target.SourceType {
	case message.SourceGroup:
		i, err := bot.Client.RecordUploadGroup(uint32(target.PrimaryID), voice)
		if err != nil {
			return nil, err
		}
		return i, nil
	case message.SourcePrivate:
		i, err := bot.Client.RecordUploadPrivate(bot.Client.GetUid(uint32(target.PrimaryID)), voice)
		if err != nil {
			return nil, err
		}
		return i, nil
	default:
		return nil, errors.New("unknown target source type")
	}
}

// uploadLocalImage 上传本地图片
func (bot *CQBot) uploadLocalImage(target message.Source, img *msg.LocalImage) (message.IMessageElement, error) {
	if img.File != "" {
		f, err := os.Open(img.File)
		if err != nil {
			return nil, errors.Wrap(err, "open image error")
		}
		defer func() { _ = f.Close() }()
		img.Stream = f
	}
	mt, ok := mime.CheckImage(img.Stream)
	if !ok {
		return nil, errors.New("image type error: " + mt)
	}
	if mt == "image/webp" && base.ConvertWebpImage {
		img0, err := webp.Decode(img.Stream)
		if err != nil {
			return nil, errors.Wrap(err, "decode webp error")
		}
		stream := bytes.NewBuffer(nil)
		err = png.Encode(stream, img0)
		if err != nil {
			return nil, errors.Wrap(err, "encode png error")
		}
		img.Stream = bytes.NewReader(stream.Bytes())
	}
	switch target.SourceType {
	case message.SourceGroup:
		i, err := bot.Client.ImageUploadGroup(uint32(target.PrimaryID), message.NewStreamImage(img.Stream))
		if err != nil {
			return nil, errors.Wrap(err, "upload group error")
		}
		return i, nil
	case message.SourcePrivate:
		i, err := bot.Client.ImageUploadPrivate(bot.Client.GetUid(uint32(target.PrimaryID)), message.NewStreamImage(img.Stream))
		if err != nil {
			return nil, errors.Wrap(err, "upload group error")
		}
		return i, nil
	default:
		return nil, errors.New("unknown target source type")
	}
}

// TODO 短视频上传
//// uploadLocalVideo 上传本地短视频至群聊
//func (bot *CQBot) uploadLocalVideo(target message.Source, v *msg.LocalVideo) (*message.ShortVideoElement, error) {
//	video, err := os.Open(v.File)
//	if err != nil {
//		return nil, err
//	}
//	defer func() { _ = video.Close() }()
//	return bot.Client.UploadShortVideo(target, video, v.Thumb)
//}

func removeLocalElement(elements []message.IMessageElement) []message.IMessageElement {
	var j int
	for i, e := range elements {
		switch e.(type) {
		case *msg.LocalImage, *msg.LocalVideo:
		case *message.VoiceElement: // 未上传的语音消息， 也删除
		case nil:
		default:
			if j < i {
				elements[j] = e
			}
			j++
		}
	}
	return elements[:j]
}

const uploadFailedTemplate = "警告: %s %d %s上传失败: %v"

func (bot *CQBot) uploadMedia(target message.Source, elements []message.IMessageElement) []message.IMessageElement {
	var w worker
	var source string
	switch target.SourceType { // nolint:exhaustive
	case message.SourceGroup:
		source = "群"
	case message.SourcePrivate:
		source = "私聊"
	}

	for i, m := range elements {
		p := &elements[i]
		switch e := m.(type) {
		case *msg.LocalImage:
			w.do(func() {
				m, err := bot.uploadLocalImage(target, e)
				if err != nil {
					log.Warnf(uploadFailedTemplate, source, target.PrimaryID, "图片", err)
				} else {
					*p = m
				}
			})
		case *message.VoiceElement:
			w.do(func() {
				m, err := bot.uploadLocalVoice(target, e)
				if err != nil {
					log.Warnf(uploadFailedTemplate, source, target.PrimaryID, "语音", err)
				} else {
					*p = m
				}
			})
			// TODO 短视频上传
			// case *msg.LocalVideo:
			//	w.do(func() {
			//		m, err := bot.uploadLocalVideo(target, e)
			//		if err != nil {
			//			log.Warnf(uploadFailedTemplate, source, target.PrimaryID, "视频", err)
			//		} else {
			//			*p = m
			//		}
			//	})
		}
	}
	w.wait()
	return removeLocalElement(elements)
}

// SendGroupMessage 发送群消息
func (bot *CQBot) SendGroupMessage(groupID int64, m *message.SendingMessage) (int32, error) {
	newElem := make([]message.IMessageElement, 0, len(m.Elements))
	source := message.Source{
		SourceType: message.SourceGroup,
		PrimaryID:  groupID,
	}
	m.Elements = bot.uploadMedia(source, m.Elements)
	for _, e := range m.Elements {
		switch i := e.(type) {
		case *msg.Poke:
			return 0, bot.Client.GroupPoke(uint32(groupID), uint32(i.Target))
		// TODO 发送音乐卡片
		// case *message.MusicShareElement:
		//	ret, err := bot.Client.SendGroupMusicShare(groupID, i)
		//	if err != nil {
		//		log.Warnf("警告: 群 %v 富文本消息发送失败: %v", groupID, err)
		//		return -1, errors.Wrap(err, "send group music share error")
		//	}
		//	return bot.InsertGroupMessage(ret, source), nil
		case *message.AtElement:
			self := bot.Client.GetCachedMemberInfo(bot.Client.Uin, uint32(groupID))
			if i.TargetUin == 0 && self.Permission == entity.Member {
				e = message.NewText("@全体成员")
			}
		}
		newElem = append(newElem, e)
	}
	if len(newElem) == 0 {
		log.Warnf("群 %v 消息发送失败: 消息为空.", groupID)
		return -1, errors.New("empty message")
	}
	m.Elements = newElem
	bot.checkMedia(newElem, groupID)
	ret, err := bot.Client.SendGroupMessage(uint32(groupID), m.Elements)
	if err != nil || ret == nil {
		log.Warnf("群 %v 发送消息失败: 账号可能被风控.", groupID)
		return -1, errors.New("send group message failed: blocked by server")
	}
	return bot.InsertGroupMessage(ret, source), nil
}

// SendPrivateMessage 发送私聊消息
func (bot *CQBot) SendPrivateMessage(target int64, groupID int64, m *message.SendingMessage) int32 {
	newElem := make([]message.IMessageElement, 0, len(m.Elements))
	source := message.Source{
		SourceType: message.SourcePrivate,
		PrimaryID:  target,
	}
	m.Elements = bot.uploadMedia(source, m.Elements)
	for _, e := range m.Elements {
		switch i := e.(type) {
		case *msg.Poke:
			_ = bot.Client.FriendPoke(uint32(i.Target))
			return 0

			// TODO 音乐卡片
			// case *message.MusicShareElement:
			//	bot.Client.SendFriendMusicShare(target, i)
			//	return 0
		}
		newElem = append(newElem, e)
	}
	if len(newElem) == 0 {
		log.Warnf("好友消息发送失败: 消息为空.")
		return -1
	}
	m.Elements = newElem
	bot.checkMedia(newElem, int64(bot.Client.Uin))

	// 单向好友是否存在
	//unidirectionalFriendExists := func() bool {
	//	list, err := bot.Client.GetUnidirectionalFriendList()
	//	if err != nil {
	//		return false
	//	}
	//	for _, f := range list {
	//		if f.Uin == target {
	//			return true
	//		}
	//	}
	//	return false
	//}

	// session, ok := bot.tempSessionCache.Load(target)
	var id int32 = -1
	ret, err := bot.Client.SendPrivateMessage(uint32(groupID), m.Elements)
	if ret != nil {
		id = bot.InsertPrivateMessage(ret, source)
	}
	if ret == nil && err == nil {
		err = fmt.Errorf("未知错误")
	}
	if err != nil {
		log.Warnf("好友消息发送失败: %v.", err)
	}

	//switch {
	//case bot.Client.FindFriend(target) != nil: // 双向好友
	//	msg := bot.Client.SendPrivateMessage(target, m)
	//	if msg != nil {
	//		id = bot.InsertPrivateMessage(msg, source)
	//	}
	// TODO 应该是不支持临时会话了
	//case ok || groupID != 0: // 临时会话
	//	if !base.AllowTempSession {
	//		log.Warnf("发送临时会话消息失败: 已关闭临时会话信息发送功能")
	//		return -1
	//	}
	//	switch {
	//	case groupID != 0 && bot.Client.FindGroup(groupID) == nil:
	//		log.Errorf("错误: 找不到群(%v)", groupID)
	//	case groupID != 0 && !bot.Client.FindGroup(groupID).AdministratorOrOwner():
	//		log.Errorf("错误: 机器人在群(%v) 为非管理员或群主, 无法主动发起临时会话", groupID)
	//	case groupID != 0 && bot.Client.FindGroup(groupID).FindMember(target) == nil:
	//		log.Errorf("错误: 群员(%v) 不在 群(%v), 无法发起临时会话", target, groupID)
	//	default:
	//		if session == nil && groupID != 0 {
	//			msg := bot.Client.SendGroupTempMessage(groupID, target, m)
	//			//lint:ignore SA9003 there is a todo
	//			if msg != nil { // nolint
	//				// todo(Mrs4s)
	//				// id = bot.InsertTempMessage(target, msg)
	//			}
	//			break
	//		}
	//		msg, err := session.SendMessage(m)
	//		if err != nil {
	//			log.Errorf("发送临时会话消息失败: %v", err)
	//			break
	//		}
	//		//lint:ignore SA9003 there is a todo
	//		if msg != nil { // nolint
	//			// todo(Mrs4s)
	//			// id = bot.InsertTempMessage(target, msg)
	//		}
	//	}
	//case unidirectionalFriendExists(): // 单向好友
	//	msg, err := bot.Client.SendPrivateMessage(target, m)
	//	if err == nil {
	//		id = bot.InsertPrivateMessage(msg, source)
	//	}
	// x获取陌生人信息x
	//default:
	//	nickname := "Unknown"
	//	if summaryInfo, _ := bot.Client.GetSummaryInfo(target); summaryInfo != nil {
	//		nickname = summaryInfo.Nickname
	//	}
	//	log.Errorf("错误: 请先添加 %v(%v) 为好友", nickname, target)
	//}
	return id
}

// InsertGroupMessage 群聊消息入数据库
func (bot *CQBot) InsertGroupMessage(m *message.GroupMessage, source message.Source) int32 {
	t := &message.SendingMessage{Elements: m.Elements}
	replyElem := t.FirstOrNil(func(e message.IMessageElement) bool {
		_, ok := e.(*message.ReplyElement)
		return ok
	})
	msg := &db.StoredGroupMessage{
		ID:       encodeMessageID(int64(m.GroupUin), m.Id),
		GlobalID: db.ToGlobalID(int64(m.GroupUin), m.Id),
		SubType:  "normal",
		Attribute: &db.StoredMessageAttribute{
			MessageSeq: m.Id,
			InternalID: m.InternalId,
			SenderUin:  int64(m.Sender.Uin),
			SenderName: m.Sender.CardName,
			Timestamp:  int64(m.Time),
		},
		GroupCode: int64(m.GroupUin),
		AnonymousID: func() string {
			if m.Sender.IsAnonymous() {
				return m.Sender.AnonymousInfo.AnonymousId
			}
			return ""
		}(),
		Content: ToMessageContent(m.Elements, source),
	}
	if replyElem != nil {
		reply := replyElem.(*message.ReplyElement)
		msg.SubType = "quote"
		msg.QuotedInfo = &db.QuotedInfo{
			PrevID:        encodeMessageID(int64(m.GroupUin), int32(reply.ReplySeq)),
			PrevGlobalID:  db.ToGlobalID(int64(m.GroupUin), int32(reply.ReplySeq)),
			QuotedContent: ToMessageContent(reply.Elements, source),
		}
	}
	if err := db.InsertGroupMessage(msg); err != nil {
		log.Warnf("记录聊天数据时出现错误: %v", err)
		return -1
	}
	return msg.GlobalID
}

// InsertPrivateMessage 私聊消息入数据库
func (bot *CQBot) InsertPrivateMessage(m *message.PrivateMessage, source message.Source) int32 {
	t := &message.SendingMessage{Elements: m.Elements}
	replyElem := t.FirstOrNil(func(e message.IMessageElement) bool {
		_, ok := e.(*message.ReplyElement)
		return ok
	})
	msg := &db.StoredPrivateMessage{
		ID:       encodeMessageID(int64(m.Sender.Uin), m.Id),
		GlobalID: db.ToGlobalID(int64(m.Sender.Uin), m.Id),
		SubType:  "normal",
		Attribute: &db.StoredMessageAttribute{
			MessageSeq: m.Id,
			InternalID: m.InternalId,
			SenderUin:  int64(m.Sender.Uin),
			SenderName: m.Sender.Nickname,
			Timestamp:  int64(m.Time),
		},
		SessionUin: func() int64 {
			if int64(m.Sender.Uin) == m.Self {
				return m.Target
			}
			return int64(m.Sender.Uin)
		}(),
		TargetUin: m.Target,
		Content:   ToMessageContent(m.Elements, source),
	}
	if replyElem != nil {
		reply := replyElem.(*message.ReplyElement)
		msg.SubType = "quote"
		msg.QuotedInfo = &db.QuotedInfo{
			PrevID:        encodeMessageID(int64(reply.SenderUin), int32(reply.ReplySeq)),
			PrevGlobalID:  db.ToGlobalID(int64(reply.SenderUin), int32(reply.ReplySeq)),
			QuotedContent: ToMessageContent(reply.Elements, source),
		}
	}
	if err := db.InsertPrivateMessage(msg); err != nil {
		log.Warnf("记录聊天数据时出现错误: %v", err)
		return -1
	}
	return msg.GlobalID
}

func (bot *CQBot) event(typ string, others global.MSG) *event {
	ev := new(event)
	post, detail, ok := strings.Cut(typ, "/")
	ev.PostType = post
	ev.DetailType = detail
	if ok {
		detail, sub, _ := strings.Cut(detail, "/")
		ev.DetailType = detail
		ev.SubType = sub
	}
	ev.Time = time.Now().Unix()
	ev.SelfID = int64(bot.Client.Uin)
	ev.Others = others
	return ev
}

func (bot *CQBot) dispatchEvent(typ string, others global.MSG) {
	bot.dispatch(bot.event(typ, others))
}

func (bot *CQBot) dispatch(ev *event) {
	bot.lock.RLock()
	defer bot.lock.RUnlock()

	event := &Event{Raw: ev}
	wg := sync.WaitGroup{}
	wg.Add(len(bot.events))
	for _, f := range bot.events {
		go func(fn func(*Event)) {
			defer func() {
				if pan := recover(); pan != nil {
					log.Warnf("处理事件 %v 时出现错误: %v \n%s", event.JSONString(), pan, debug.Stack())
				}
				wg.Done()
			}()

			start := time.Now()
			fn(event)
			end := time.Now()
			if end.Sub(start) > time.Second*5 {
				log.Debugf("警告: 事件处理耗时超过 5 秒 (%v), 请检查应用是否有堵塞.", end.Sub(start))
			}
		}(f)
	}
	wg.Wait()
	if event.buffer != nil {
		global.PutBuffer(event.buffer)
	}
}

func formatGroupName(group *entity.Group) string {
	return fmt.Sprintf("%s(%d)", group.GroupName, group.GroupUin)
}

func formatMemberName(mem *entity.GroupMember) string {
	if mem == nil {
		return "未知"
	}
	return fmt.Sprintf("%s(%d)", mem.DisplayName(), mem.Uin)
}

// encodeMessageID 临时先这样, 暂时用不上
func encodeMessageID(target int64, seq int32) string {
	return hex.EncodeToString(binary.NewWriterF(func(w *binary.Builder) {
		w.WriteU64(uint64(target))
		w.WriteU32(uint32(seq))
	}))
}
