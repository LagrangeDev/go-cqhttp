package coolq

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/LagrangeDev/LagrangeGo/utils/binary"

	"github.com/LagrangeDev/LagrangeGo/client/entity"

	event2 "github.com/LagrangeDev/LagrangeGo/client/event"

	"github.com/LagrangeDev/LagrangeGo/message"

	"github.com/LagrangeDev/LagrangeGo/client"
	log "github.com/sirupsen/logrus"

	"github.com/Mrs4s/go-cqhttp/db"
	"github.com/Mrs4s/go-cqhttp/global"
	"github.com/Mrs4s/go-cqhttp/internal/base"
	"github.com/Mrs4s/go-cqhttp/internal/cache"
	"github.com/Mrs4s/go-cqhttp/internal/download"
)

// ToFormattedMessage 将给定[]message.IMessageElement转换为通过coolq.SetMessageFormat所定义的消息上报格式
func ToFormattedMessage(e []message.IMessageElement, source message.Source) (r any) {
	if base.PostFormat == "string" {
		r = toStringMessage(e, source)
	} else if base.PostFormat == "array" {
		r = toElements(e, source)
	}
	return
}

type event struct {
	PostType   string
	DetailType string
	SubType    string
	Time       int64
	SelfID     int64
	Others     global.MSG
}

func (ev *event) MarshalJSON() ([]byte, error) {
	buf := global.NewBuffer()
	defer global.PutBuffer(buf)

	detail := ""
	switch ev.PostType {
	case "message", "message_sent":
		detail = "message_type"
	case "notice":
		detail = "notice_type"
	case "request":
		detail = "request_type"
	case "meta_event":
		detail = "meta_event_type"
	default:
		panic("unknown post type: " + ev.PostType)
	}
	fmt.Fprintf(buf, `{"post_type":"%s","%s":"%s","time":%d,"self_id":%d`, ev.PostType, detail, ev.DetailType, ev.Time, ev.SelfID)
	if ev.SubType != "" {
		fmt.Fprintf(buf, `,"sub_type":"%s"`, ev.SubType)
	}
	for k, v := range ev.Others {
		v, err := json.Marshal(v)
		if err != nil {
			log.Warnf("marshal message payload error: %v", err)
			return nil, err
		}
		fmt.Fprintf(buf, `,"%s":%s`, k, v)
	}
	buf.WriteByte('}')
	return append([]byte(nil), buf.Bytes()...), nil
}

func (bot *CQBot) privateMessageEvent(_ *client.QQClient, m *message.PrivateMessage) {
	source := message.Source{
		SourceType: message.SourcePrivate,
		PrimaryID:  int64(m.Sender.Uin),
	}
	bot.checkMedia(m.Elements, source)
	cqm := toStringMessage(m.Elements, source)
	id := bot.InsertPrivateMessage(m, source)
	log.Infof("收到好友 %v(%v) 的消息: %v (%v)", m.Sender.Nickname, m.Sender.Uin, cqm, id)
	typ := "message/private/friend"
	if m.Sender.Uin == bot.Client.Uin {
		typ = "message_sent/private/friend"
	}
	fm := global.MSG{
		"message_id":  id,
		"user_id":     m.Sender.Uin,
		"target_id":   m.Target,
		"message":     ToFormattedMessage(m.Elements, source),
		"raw_message": cqm,
		"font":        0,
		"sender": global.MSG{
			"user_id":  m.Sender.Uin,
			"nickname": m.Sender.Nickname,
			"sex":      "unknown",
			"age":      0,
		},
	}
	bot.dispatchEvent(typ, fm)
}

func (bot *CQBot) groupMessageEvent(c *client.QQClient, m *message.GroupMessage) {
	source := message.Source{
		SourceType: message.SourceGroup,
		PrimaryID:  int64(m.GroupUin),
	}
	bot.checkMedia(m.Elements, source)
	// TODO 群聊文件上传
	//for _, elem := range m.Elements {
	//	if file, ok := elem.(*message.GroupFileElement); ok {
	//		log.Infof("群 %v(%v) 内 %v(%v) 上传了文件: %v", m.GroupName, m.GroupCode, m.Sender.CardName, m.Sender.Uin, file.Name)
	//		bot.dispatchEvent("notice/group_upload", global.MSG{
	//			"group_id": m.GroupCode,
	//			"user_id":  m.Sender.Uin,
	//			"file": global.MSG{
	//				"id":    file.Path,
	//				"name":  file.Name,
	//				"size":  file.Size,
	//				"busid": file.Busid,
	//				"url":   c.GetGroupFileUrl(m.GroupCode, file.Path, file.Busid),
	//			},
	//		})
	//		// return
	//	}
	//}
	cqm := toStringMessage(m.Elements, source)
	id := bot.InsertGroupMessage(m, source)
	log.Infof("收到群 %v(%v) 内 %v(%v) 的消息: %v (%v)", m.GroupName, m.GroupUin, m.Sender.CardName, m.Sender.Uin, cqm, id)
	gm := bot.formatGroupMessage(m)
	if gm == nil {
		return
	}
	gm.Others["message_id"] = id
	bot.dispatch(gm)
}

// TODO 暂不支持临时会话
/*
func (bot *CQBot) tempMessageEvent(_ *client.QQClient, e *client.TempMessageEvent) {
	m := e.Message
	bot.checkMedia(m.Elements, m.Sender.Uin)
	source := message.Source{
		SourceType: message.SourcePrivate,
		PrimaryID:  e.Session.Sender,
	}
	cqm := toStringMessage(m.Elements, source)
	if base.AllowTempSession {
		bot.tempSessionCache.Store(m.Sender.Uin, e.Session)
	}

	id := m.Id
	// todo(Mrs4s)
	// if bot.db != nil { // nolint
	// 		id = bot.InsertTempMessage(m.Sender.Uin, m)
	// }
	log.Infof("收到来自群 %v(%v) 内 %v(%v) 的临时会话消息: %v", m.GroupName, m.GroupCode, m.Sender.DisplayName(), m.Sender.Uin, cqm)
	tm := global.MSG{
		"temp_source": e.Session.Source,
		"message_id":  id,
		"user_id":     m.Sender.Uin,
		"message":     ToFormattedMessage(m.Elements, source),
		"raw_message": cqm,
		"font":        0,
		"sender": global.MSG{
			"user_id":  m.Sender.Uin,
			"group_id": m.GroupCode,
			"nickname": m.Sender.Nickname,
			"sex":      "unknown",
			"age":      0,
		},
	}
	bot.dispatchEvent("message/private/group", tm)
}*/

func (bot *CQBot) groupMutedEvent(c *client.QQClient, e *event2.GroupMute) {
	g := c.GetCachedGroupInfo(e.GroupUin)
	operator := c.GetCachedMemberInfo(c.GetUin(e.OperatorUID, e.GroupUin), e.GroupUin)
	target := c.GetCachedMemberInfo(c.GetUin(e.TargetUID, e.GroupUin), e.GroupUin)
	if e.TargetUID == "" {
		if e.Duration != 0 {
			log.Infof("群 %v 被 %v 开启全员禁言.",
				formatGroupName(g), formatMemberName(operator))
		} else {
			log.Infof("群 %v 被 %v 解除全员禁言.",
				formatGroupName(g), formatMemberName(operator))
		}
	} else {
		if e.Duration > 0 {
			log.Infof("群 %v 内 %v 被 %v 禁言了 %v 秒.",
				formatGroupName(g), formatMemberName(target), formatMemberName(operator), e.Duration)
		} else {
			log.Infof("群 %v 内 %v 被 %v 解除禁言.",
				formatGroupName(g), formatMemberName(target), formatMemberName(operator))
		}
	}
	typ := "notice/group_ban/ban"
	if e.Duration == 0 {
		typ = "notice/group_ban/lift_ban"
	}
	var userID uint32
	if target != nil {
		userID = target.Uin
	} else {
		userID = 0
	}
	bot.dispatchEvent(typ, global.MSG{
		"duration":    e.Duration,
		"group_id":    e.GroupUin,
		"operator_id": operator.Uin,
		"user_id":     userID,
	})
}

func (bot *CQBot) groupRecallEvent(c *client.QQClient, e *event2.GroupRecall) {
	g := c.GetCachedGroupInfo(e.GroupUin)
	gid := db.ToGlobalID(int64(e.GroupUin), int32(e.Sequence))
	operator := c.GetCachedMemberInfo(c.GetUin(e.OperatorUID, e.GroupUin), e.GroupUin)
	Author := c.GetCachedMemberInfo(c.GetUin(e.AuthorUID, e.GroupUin), e.GroupUin)
	log.Infof("群 %v 内 %v 撤回了 %v 的消息: %v.",
		formatGroupName(g), formatMemberName(operator), formatMemberName(Author), gid)

	ev := bot.event("notice/group_recall", global.MSG{
		"group_id":    e.GroupUin,
		"user_id":     Author.Uin,
		"operator_id": operator.Uin,
		"message_id":  gid,
	})
	ev.Time = int64(e.Time)
	bot.dispatch(ev)
}

//TODO 群通知

func (bot *CQBot) groupNotifyEvent(c *client.QQClient, e event2.INotifyEvent) {
	group := c.GetCachedGroupInfo(e.From())
	switch notify := e.(type) {
	case *event2.GroupPokeEvent:
		sender := c.GetCachedMemberInfo(notify.Sender, e.From())
		receiver := c.GetCachedMemberInfo(notify.Receiver, e.From())
		log.Infof("群 %v 内 %v 戳了戳 %v", formatGroupName(group), formatMemberName(sender), formatMemberName(receiver))
		bot.dispatchEvent("notice/notify/poke", global.MSG{
			"group_id":  group.GroupUin,
			"user_id":   notify.Sender,
			"sender_id": notify.Sender,
			"target_id": notify.Receiver,
		})
		//case *client.GroupRedBagLuckyKingNotifyEvent:
		//	sender := group.FindMember(notify.Sender)
		//	luckyKing := group.FindMember(notify.LuckyKing)
		//	log.Infof("群 %v 内 %v 的红包被抢完, %v 是运气王", formatGroupName(group), formatMemberName(sender), formatMemberName(luckyKing))
		//	bot.dispatchEvent("notice/notify/lucky_king", global.MSG{
		//		"group_id":  group.Code,
		//		"user_id":   notify.Sender,
		//		"sender_id": notify.Sender,
		//		"target_id": notify.LuckyKing,
		//	})
		//case *client.MemberHonorChangedNotifyEvent:
		//	log.Info(notify.Content())
		//	bot.dispatchEvent("notice/notify/honor", global.MSG{
		//		"group_id": group.Code,
		//		"user_id":  notify.Uin,
		//		"honor_type": func() string {
		//			switch notify.Honor {
		//			case client.Talkative:
		//				return "talkative"
		//			case client.Performer:
		//				return "performer"
		//			case client.Emotion:
		//				return "emotion"
		//			case client.Legend:
		//				return "legend"
		//			case client.StrongNewbie:
		//				return "strong_newbie"
		//			default:
		//				return "ERROR"
		//			}
		//		}(),
		//	})
	}
}

func (bot *CQBot) friendNotifyEvent(c *client.QQClient, e event2.INotifyEvent) {
	friend := c.GetCachedFriendInfo(e.From())
	if notify, ok := e.(*event2.FriendPokeEvent); ok {
		if notify.Receiver == notify.Sender {
			log.Infof("好友 %v 戳了戳自己.", friend.Nickname)
		} else {
			log.Infof("好友 %v 戳了戳你.", friend.Nickname)
		}
		bot.dispatchEvent("notice/notify/poke", global.MSG{
			"user_id":   notify.Sender,
			"sender_id": notify.Sender,
			"target_id": notify.Receiver,
		})
	}
}

func (bot *CQBot) memberTitleUpdatedEvent(c *client.QQClient, e *event2.MemberSpecialTitleUpdated) {
	group := c.GetCachedGroupInfo(e.GroupUin)
	mem := c.GetCachedMemberInfo(e.Uin, e.GroupUin)
	log.Infof("群 %v(%v) 内成员 %v(%v) 获得了新的头衔: %v", group.GroupName, group.GroupUin, mem.MemberCard, mem.Uin, e.NewTitle)
	bot.dispatchEvent("notice/notify/title", global.MSG{
		"group_id": group.GroupUin,
		"user_id":  e.Uin,
		"title":    e.NewTitle,
	})
}

func (bot *CQBot) friendRecallEvent(c *client.QQClient, e *event2.FriendRecall) {
	f := c.GetCachedFriendInfo(c.GetUin(e.FromUID))
	gid := db.ToGlobalID(int64(f.Uin), int32(e.Sequence))
	//if f != nil {
	log.Infof("好友 %v(%v) 撤回了消息: %v", f.Nickname, f.Uin, gid)
	//} else {
	//	log.Infof("好友 %v 撤回了消息: %v", e.FriendUin, gid)
	//}
	ev := bot.event("notice/friend_recall", global.MSG{
		"user_id":    f.Uin,
		"message_id": gid,
	})
	ev.Time = int64(e.Time)
	bot.dispatch(ev)
}

// TODO 好友离线文件
/*func (bot *CQBot) offlineFileEvent(c *client.QQClient, e *client.OfflineFileEvent) {
	f := c.FindFriend(e.Sender)
	if f == nil {
		return
	}
	log.Infof("好友 %v(%v) 发送了离线文件 %v", f.Nickname, f.Uin, e.FileName)
	bot.dispatchEvent("notice/offline_file", global.MSG{
		"user_id": e.Sender,
		"file": global.MSG{
			"name": e.FileName,
			"size": e.FileSize,
			"url":  e.DownloadUrl,
		},
	})
}*/

// TODO bot自身进群退群
func (bot *CQBot) joinGroupEvent(c *client.QQClient, event *event2.GroupMemberIncrease) {
	log.Infof("Bot进入了群 %v.", formatGroupName(c.GetCachedGroupInfo(event.GroupUin)))
	bot.dispatch(bot.groupIncrease(int64(event.GroupUin), 0, int64(c.Uin)))
}

func (bot *CQBot) leaveGroupEvent(c *client.QQClient, e *event2.GroupMemberDecrease) {
	if e.IsKicked() {
		log.Infof("Bot被 %v T出了群 %v.", formatMemberName(c.GetCachedMemberInfo(e.OperatorUin, e.GroupUin)), formatGroupName(c.GetCachedGroupInfo(e.GroupUin)))
	} else {
		log.Infof("Bot退出了群 %v.", formatGroupName(c.GetCachedGroupInfo(e.GroupUin)))
	}
	bot.dispatch(bot.groupDecrease(int64(e.GroupUin), int64(c.Uin), c.GetCachedMemberInfo(e.OperatorUin, e.GroupUin)))
}

func (bot *CQBot) memberPermissionChangedEvent(_ *client.QQClient, e *event2.GroupMemberPermissionChanged) {
	st := "unset"
	if e.IsAdmin {
		st = "set"
	}
	bot.dispatchEvent("notice/group_admin/"+st, global.MSG{
		"group_id": e.GroupUin,
		"user_id":  e.TargetUin,
	})
}

// TODO 群名片变更
//func (bot *CQBot) memberCardUpdatedEvent(_ *client.QQClient, e *client.MemberCardUpdatedEvent) {
//	log.Infof("群 %v 的 %v 更新了名片 %v -> %v", formatGroupName(e.Group), formatMemberName(e.Member), e.OldCard, e.Member.CardName)
//	bot.dispatchEvent("notice/group_card", global.MSG{
//		"group_id": e.Group.Code,
//		"user_id":  e.Member.Uin,
//		"card_new": e.Member.CardName,
//		"card_old": e.OldCard,
//	})
//}

func (bot *CQBot) memberJoinEvent(c *client.QQClient, e *event2.GroupMemberIncrease) {
	log.Infof("新成员 %v 进入了群 %v.", formatMemberName(c.GetCachedMemberInfo(e.MemberUin, e.GroupUin)), formatGroupName(c.GetCachedGroupInfo(e.GroupUin)))
	bot.dispatch(bot.groupIncrease(int64(e.GroupUin), 0, int64(e.MemberUin)))
}

func (bot *CQBot) memberLeaveEvent(c *client.QQClient, e *event2.GroupMemberDecrease) {
	member := c.GetCachedMemberInfo(c.GetUin(e.MemberUID), e.GroupUin)
	op := c.GetCachedMemberInfo(c.GetUin(e.OperatorUID), e.GroupUin)
	group := c.GetCachedGroupInfo(e.GroupUin)
	if e.IsKicked() {
		log.Infof("成员 %v 被 %v T出了群 %v.", formatMemberName(member), formatMemberName(op), formatGroupName(group))
	} else {
		log.Infof("成员 %v 离开了群 %v.", formatMemberName(member), formatGroupName(group))
	}
	bot.dispatch(bot.groupDecrease(int64(e.GroupUin), int64(member.Uin), op))
}

func (bot *CQBot) friendRequestEvent(_ *client.QQClient, e *event2.NewFriendRequest) {
	log.Infof("收到来自 %v(%v) 的好友请求: %v", e.Source, e.SourceUin, e.Msg)
	// 就用uin当flag吧
	flag := strconv.FormatInt(int64(e.SourceUin), 10)
	bot.friendReqCache.Store(flag, e)
	bot.dispatchEvent("request/friend", global.MSG{
		"user_id": e.SourceUin,
		"comment": e.Msg,
		"flag":    flag,
	})
}

//func (bot *CQBot) friendAddedEvent(_ *client.QQClient, e *client.NewFriendEvent) {
//	log.Infof("添加了新好友: %v(%v)", e.Friend.Nickname, e.Friend.Uin)
//	bot.tempSessionCache.Delete(e.Friend.Uin)
//	bot.dispatchEvent("notice/friend_add", global.MSG{
//		"user_id": e.Friend.Uin,
//	})
//}

func (bot *CQBot) groupInvitedEvent(_ *client.QQClient, e *event2.GroupInvite) {
	log.Infof("收到来自群 %v(%v) 内用户 %v(%v) 的加群邀请.", e.GroupName, e.GroupUin, e.InvitorNick, e.InvitorUin)
	flag := strconv.FormatInt(int64(e.RequestSeq), 10)
	bot.dispatchEvent("request/group/invite", global.MSG{
		"group_id":   e.GroupUin,
		"user_id":    e.InvitorUin,
		"invitor_id": 0,
		"comment":    "",
		"flag":       flag,
	})
}

func (bot *CQBot) groupJoinReqEvent(c *client.QQClient, e *event2.GroupMemberJoinRequest) {
	group := c.GetCachedGroupInfo(e.GroupUin)
	log.Infof("群 %v(%v) 收到来自用户 %v(%v) 的加群请求.", group.GroupName, e.GroupUin, e.TargetNick, e.TargetUin)
	flag := strconv.FormatInt(int64(e.RequestSeq), 10)
	bot.dispatchEvent("request/group/add", global.MSG{
		"group_id":   e.GroupUin,
		"user_id":    e.TargetUin,
		"invitor_id": e.InvitorUin,
		"comment":    e.Answer,
		"flag":       flag,
	})
}

//func (bot *CQBot) otherClientStatusChangedEvent(_ *client.QQClient, e *client.OtherClientStatusChangedEvent) {
//	if e.Online {
//		log.Infof("Bot 账号在客户端 %v (%v) 登录.", e.Client.DeviceName, e.Client.DeviceKind)
//	} else {
//		log.Infof("Bot 账号在客户端 %v (%v) 登出.", e.Client.DeviceName, e.Client.DeviceKind)
//	}
//	bot.dispatchEvent("notice/client_status", global.MSG{
//		"online": e.Online,
//		"client": global.MSG{
//			"app_id":      e.Client.AppId,
//			"device_name": e.Client.DeviceName,
//			"device_kind": e.Client.DeviceKind,
//		},
//	})
//}

// TODO 精华消息
func (bot *CQBot) groupEssenceMsg(c *client.QQClient, e *event2.GroupDigestEvent) {
	g := c.GetCachedGroupInfo(e.GroupUin)
	gid := db.ToGlobalID(int64(e.GroupUin), int32(e.MessageID))
	if e.OperationType == 1 {
		log.Infof(
			"群 %v 内 %v 将 %v 的消息(%v)设为了精华消息.",
			formatGroupName(g),
			formatMemberName(c.GetCachedMemberInfo(e.OperatorUin, e.GroupUin)),
			formatMemberName(c.GetCachedMemberInfo(e.SenderUin, e.GroupUin)),
			gid,
		)
	} else {
		log.Infof(
			"群 %v 内 %v 将 %v 的消息(%v)移出了精华消息.",
			formatGroupName(g),
			formatMemberName(c.GetCachedMemberInfo(e.OperatorUin, e.GroupUin)),
			formatMemberName(c.GetCachedMemberInfo(e.SenderUin, e.GroupUin)),
			gid,
		)
	}
	if e.OperatorUin == bot.Client.Uin {
		return
	}
	subtype := "delete"
	if e.IsSet() {
		subtype = "add"
	}
	bot.dispatchEvent("notice/essence/"+subtype, global.MSG{
		"group_id":    e.GroupUin,
		"sender_id":   e.SenderUin,
		"operator_id": e.OperatorUin,
		"message_id":  gid,
	})
}

func (bot *CQBot) groupIncrease(groupCode, operatorUin, userUin int64) *event {
	return bot.event("notice/group_increase/approve", global.MSG{
		"group_id":    groupCode,
		"operator_id": operatorUin,
		"user_id":     userUin,
	})
}

func (bot *CQBot) groupDecrease(groupCode, userUin int64, operator *entity.GroupMember) *event {
	op := userUin
	if operator != nil {
		op = int64(operator.Uin)
	}
	subtype := "leave"
	if operator != nil {
		if userUin == int64(bot.Client.Uin) {
			subtype = "kick_me"
		} else {
			subtype = "kick"
		}
	}
	return bot.event("notice/group_decrease/"+subtype, global.MSG{
		"group_id":    groupCode,
		"operator_id": op,
		"user_id":     userUin,
	})
}

func (bot *CQBot) checkMedia(e []message.IMessageElement, source message.Source) {
	for _, elem := range e {
		switch i := elem.(type) {
		case *message.ImageElement:
			// 闪照已经4了(私聊还没)
			//if i.Flash && sourceID != 0 {
			//	u, err := bot.Client.GetGroupImageDownloadUrl(i.FileId, sourceID, i.Md5)
			//	if err != nil {
			//		log.Warnf("获取闪照地址时出现错误: %v", err)
			//	} else {
			//		i.Url = u
			//	}
			//}
			data := binary.NewWriterF(func(w *binary.Builder) {
				_, err := w.Write(i.Md5)
				if err != nil {
					log.Warnf("计算图片md5时出现错误: %v", err)
					return
				}

				w.WritePacketString(i.FileUUID, "u32", true)
				w.WritePacketString(i.ImageID, "u32", true)
			})
			cache.Image.Insert(i.Md5, data)

		case *message.VoiceElement:
			// todo: don't download original file?
			i.Name = strings.ReplaceAll(i.Name, "{", "")
			i.Name = strings.ReplaceAll(i.Name, "}", "")
			if !global.FileExists(path.Join(global.VoicePath, i.Name)) {
				err := download.Request{URL: i.URL}.WriteToFile(path.Join(global.VoicePath, i.Name))
				if err != nil {
					log.Warnf("语音文件 %v 下载失败: %v", i.Name, err)
					continue
				}
			}
		case *message.ShortVideoElement:
			data := binary.NewWriterF(func(w *binary.Builder) {
				_, err := w.Write(i.Md5)
				_, err = w.Write(i.Sha1)
				if err != nil {
					log.Warnf("计算视频sha1时出现错误: %v", err)
					return
				}
				w.WritePacketString(i.Name, "u32", true)
				w.WritePacketBytes(i.UUID, "u32", true)
			})
			filename := hex.EncodeToString(i.Md5) + ".video"
			cache.Video.Insert(i.Md5, data)
			i.URL, _ = bot.Client.GetVideoURL(source.SourceType == message.SourceGroup, i)
			i.Name = filename
		}
	}
}
