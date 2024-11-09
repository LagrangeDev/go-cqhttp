package coolq

import (
	"strconv"
	"strings"

	"github.com/LagrangeDev/LagrangeGo/client/entity"

	"github.com/LagrangeDev/LagrangeGo/message"

	log "github.com/sirupsen/logrus"

	"github.com/Mrs4s/go-cqhttp/global"
)

func convertGroupMemberInfo(groupID int64, m *entity.GroupMember) global.MSG {
	// TODO nt 协议依然是获取不到
	sex := "unknown"
	role := "member"
	switch m.Permission { // nolint:exhaustive
	case entity.Owner:
		role = "owner"
	case entity.Admin:
		role = "admin"
	case entity.Member:
		role = "member"
	}
	return global.MSG{
		"group_id":          groupID,
		"user_id":           m.Uin,
		"nickname":          m.MemberName,
		"card":              m.MemberCard,
		"sex":               sex,
		"age":               0,
		"area":              "",
		"join_time":         m.JoinTime,
		"last_sent_time":    m.LastMsgTime,
		"shut_up_timestamp": m.ShutUpTime,
		"level":             strconv.Itoa(int(m.GroupLevel)),
		"role":              role,
		"unfriendly":        false,
		"title":             m.SpecialTitle,
		"title_expire_time": 0,
		"card_changeable":   false,
	}
}

func (bot *CQBot) formatGroupMessage(m *message.GroupMessage) *event {
	source := message.Source{
		SourceType: message.SourceGroup,
		PrimaryID:  int64(m.GroupUin),
	}
	cqm := toStringMessage(m.Elements, source)
	typ := "message/group/normal"
	if m.Sender.Uin == bot.Client.Uin {
		typ = "message_sent/group/normal"
	}
	gm := global.MSG{
		"anonymous":   nil,
		"font":        0,
		"group_id":    m.GroupUin,
		"message":     ToFormattedMessage(m.Elements, source),
		"message_seq": m.ID,
		"raw_message": cqm,
		"sender": global.MSG{
			"age":     0,
			"area":    "",
			"level":   "",
			"sex":     "unknown",
			"user_id": m.Sender.Uin,
		},
		"user_id": m.Sender.Uin,
	}
	if m.Sender.IsAnonymous() {
		gm["anonymous"] = global.MSG{
			"flag": m.Sender.AnonymousInfo.AnonymousID + "|" + m.Sender.AnonymousInfo.AnonymousNick,
			"id":   m.Sender.Uin,
			"name": m.Sender.AnonymousInfo.AnonymousNick,
		}
		gm["sender"].(global.MSG)["nickname"] = "匿名消息"
		typ = "message/group/anonymous"
	} else {
		mem := bot.Client.GetCachedMemberInfo(m.Sender.Uin, m.GroupUin)
		if mem == nil {
			log.Warnf("获取 %v 成员信息失败，尝试刷新成员列表", m.Sender.Uin)
			err := bot.Client.RefreshGroupMembersCache(m.GroupUin)
			if err != nil {
				log.Warnf("刷新群 %v 成员列表失败: %v", m.GroupUin, err)
				return nil
			}
			mem = bot.Client.GetCachedMemberInfo(m.Sender.Uin, m.GroupUin)
			if mem == nil {
				return nil
			}
		}
		ms := gm["sender"].(global.MSG)
		role := "member"
		switch mem.Permission { // nolint:exhaustive
		case entity.Owner:
			role = "owner"
		case entity.Admin:
			role = "admin"
		case entity.Member:
			role = "member"
		}
		ms["role"] = role
		ms["nickname"] = m.Sender.Nickname
		ms["card"] = m.Sender.CardName
		// TODO 获取专属头衔
		ms["title"] = ""
	}
	ev := bot.event(typ, gm)
	ev.Time = int64(m.Time)
	return ev
}

func toStringMessage(m []message.IMessageElement, source message.Source) string {
	elems := toElements(m, source)
	var sb strings.Builder
	for _, elem := range elems {
		elem.WriteCQCodeTo(&sb)
	}
	return sb.String()
}

func fU64(v uint64) string {
	return strconv.FormatUint(v, 10)
}
