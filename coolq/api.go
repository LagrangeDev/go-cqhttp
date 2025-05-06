package coolq

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/LagrangeDev/LagrangeGo/client/entity"
	"github.com/LagrangeDev/LagrangeGo/client/sign"
	"github.com/LagrangeDev/LagrangeGo/message"
	"github.com/LagrangeDev/LagrangeGo/utils"
	"github.com/LagrangeDev/LagrangeGo/utils/binary"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"

	"github.com/Mrs4s/go-cqhttp/db"
	"github.com/Mrs4s/go-cqhttp/global"
	"github.com/Mrs4s/go-cqhttp/internal/base"
	"github.com/Mrs4s/go-cqhttp/internal/cache"
	"github.com/Mrs4s/go-cqhttp/internal/download"
	"github.com/Mrs4s/go-cqhttp/internal/msg"
	"github.com/Mrs4s/go-cqhttp/internal/param"
	"github.com/Mrs4s/go-cqhttp/modules/filter"
	"github.com/Mrs4s/go-cqhttp/pkg/onebot"
)

// CQGetLoginInfo 获取登录号信息
//
// https://git.io/Jtz1I
// @route11(get_login_info)
// @route12(get_self_info)
func (bot *CQBot) CQGetLoginInfo() global.MSG {
	return OK(global.MSG{"user_id": bot.Client.Uin, "nickname": bot.Client.NickName()})
}

// CQGetFriendList 获取好友列表
//
// https://git.io/Jtz1L
// @route(get_friend_list)
func (bot *CQBot) CQGetFriendList(spec *onebot.Spec) global.MSG {
	friendList, _ := bot.Client.GetFriendsData()
	fs := make([]global.MSG, 0, len(friendList))
	for _, f := range friendList {
		fs = append(fs, global.MSG{
			"nickname": f.Nickname,
			"remark":   f.Remarks,
			"user_id":  spec.ConvertID(f.Uin),
		})
	}
	return OK(fs)
}

// CQGetUnidirectionalFriendList 获取单向好友列表
//
// @route(get_unidirectional_friend_list)
func (bot *CQBot) CQGetUnidirectionalFriendList() global.MSG {
	list, err := bot.Client.GetUnidirectionalFriendList()
	if err != nil {
		log.Warnf("获取单向好友列表时出现错误: %v", err)
		return Failed(100, "API_ERROR", err.Error())
	}
	fs := make([]global.MSG, 0, len(list))
	for _, f := range list {
		fs = append(fs, global.MSG{
			"nickname": f.Nickname,
			"user_id":  f.Uin,
			"source":   f.Source,
		})
	}
	return OK(fs)
}

// CQDeleteUnidirectionalFriend 删除单向好友
//
// @route(delete_unidirectional_friend)
// @rename(uin->user_id)
func (bot *CQBot) CQDeleteUnidirectionalFriend(uin int64) global.MSG {
	list, err := bot.Client.GetUnidirectionalFriendList()
	if err != nil {
		log.Warnf("获取单向好友列表时出现错误: %v", err)
		return Failed(100, "API_ERROR", err.Error())
	}
	for _, f := range list {
		if f.Uin == uint32(uin) {
			if err = bot.Client.DeleteUnidirectionalFriend(uint32(uin)); err != nil {
				log.Warnf("删除单向好友时出现错误: %v", err)
				return Failed(100, "API_ERROR", err.Error())
			}
			return OK(nil)
		}
	}
	return Failed(100, "FRIEND_NOT_FOUND", "好友不存在")
}

// CQDeleteFriend 删除好友
// @route(delete_friend)
// @rename(uin->"[user_id\x2Cid].0")
func (bot *CQBot) CQDeleteFriend(uin int64, block bool) global.MSG {
	if bot.Client.GetCachedFriendInfo(uint32(uin)) == nil {
		return Failed(100, "FRIEND_NOT_FOUND", "好友不存在")
	}
	if err := bot.Client.DeleteFriend(uint32(uin), block); err != nil {
		log.Warnf("删除好友时出现错误: %v", err)
		return Failed(100, "DELETE_API_ERROR", err.Error())
	}
	return OK(nil)
}

// CQGetGroupList 获取群列表
//
// https://git.io/Jtz1t
// @route(get_group_list)
func (bot *CQBot) CQGetGroupList(noCache bool, spec *onebot.Spec) global.MSG {
	if noCache {
		_ = bot.Client.RefreshAllGroupsInfo()
	}
	grpInfos := bot.Client.GetCachedAllGroupsInfo()
	gs := make([]global.MSG, 0, len(grpInfos))
	for _, g := range grpInfos {
		gs = append(gs, global.MSG{
			"group_id":          spec.ConvertID(g.GroupUin),
			"group_name":        g.GroupName,
			"group_create_time": g.GroupCreateTime,
			"group_level":       g.GroupLevel,
			"max_member_count":  g.MaxMember,
			"member_count":      g.MemberCount,
		})
	}
	return OK(gs)
}

// CQGetGroupInfo 获取群信息
//
// https://git.io/Jtz1O
// @route(get_group_info)
func (bot *CQBot) CQGetGroupInfo(groupID int64, noCache bool, spec *onebot.Spec) global.MSG {
	if noCache {
		_ = bot.Client.RefreshAllGroupsInfo()
	}
	group := bot.Client.GetCachedGroupInfo(uint32(groupID))
	if group != nil {
		return OK(global.MSG{
			"group_id":          spec.ConvertID(group.GroupUin),
			"group_name":        group.GroupName,
			"group_create_time": group.GroupCreateTime,
			"group_level":       group.GroupLevel,
			"max_member_count":  group.MaxMember,
			"member_count":      group.MemberCount,
		})
	}
	return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
}

// CQGetGroupMemberList 获取群成员列表
//
// https://git.io/Jtz13
// @route(get_group_member_list)
func (bot *CQBot) CQGetGroupMemberList(groupID int64, noCache bool) global.MSG {
	if noCache {
		_ = bot.Client.RefreshAllGroupsInfo()
	}
	groupMembers := bot.Client.GetCachedMembersInfo(uint32(groupID))
	if groupMembers == nil {
		return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
	}
	members := make([]global.MSG, 0, len(groupMembers))
	for _, m := range groupMembers {
		members = append(members, convertGroupMemberInfo(groupID, m))
	}
	return OK(members)
}

// CQGetGroupMemberInfo 获取群成员信息
//
// https://git.io/Jtz1s
// @route(get_group_member_info)
func (bot *CQBot) CQGetGroupMemberInfo(groupID, userID int64, noCache bool) global.MSG {
	group := bot.Client.GetCachedGroupInfo(uint32(groupID))
	if group == nil {
		return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
	}
	if noCache {
		err := bot.Client.RefreshGroupMembersCache(group.GroupUin)
		if err != nil {
			log.Warnf("刷新群 %v 中成员 %v 失败: %v", groupID, userID, err)
			return Failed(100, "GET_MEMBER_INFO_API_ERROR", err.Error())
		}
	}
	member := bot.Client.GetCachedMemberInfo(uint32(userID), uint32(groupID))
	if member == nil {
		return Failed(100, "MEMBER_NOT_FOUND", "群员不存在")
	}
	return OK(convertGroupMemberInfo(groupID, member))
}

// CQGetGroupFileSystemInfo 扩展API-获取群文件系统信息
//
// https://docs.go-cqhttp.org/api/#%E8%8E%B7%E5%8F%96%E7%BE%A4%E6%96%87%E4%BB%B6%E7%B3%BB%E7%BB%9F%E4%BF%A1%E6%81%AF
// @route(get_group_file_system_info)
func (bot *CQBot) CQGetGroupFileSystemInfo(groupID int64) global.MSG {
	fs, err := bot.Client.GetGroupFileSystemInfo(uint32(groupID))
	if err != nil {
		log.Warnf("获取群 %v 文件系统信息失败: %v", groupID, err)
		return Failed(100, "FILE_SYSTEM_API_ERROR", err.Error())
	}
	return OK(fs)
}

// CQGetGroupRootFiles 扩展API-获取群根目录文件列表
//
// https://docs.go-cqhttp.org/api/#%E8%8E%B7%E5%8F%96%E7%BE%A4%E6%A0%B9%E7%9B%AE%E5%BD%95%E6%96%87%E4%BB%B6%E5%88%97%E8%A1%A8
// @route(get_group_root_files)
func (bot *CQBot) CQGetGroupRootFiles(groupID int64) global.MSG {
	files, folders, err := bot.Client.ListGroupRootFiles(uint32(groupID))
	if err != nil {
		log.Warnf("获取群 %v 根目录文件失败: %v", groupID, err)
		return Failed(100, "FILE_SYSTEM_API_ERROR", err.Error())
	}
	return OK(global.MSG{
		"files":   files,
		"folders": folders,
	})
}

// CQGetGroupFilesByFolderID 扩展API-获取群子目录文件列表
//
// https://docs.go-cqhttp.org/api/#%E8%8E%B7%E5%8F%96%E7%BE%A4%E5%AD%90%E7%9B%AE%E5%BD%95%E6%96%87%E4%BB%B6%E5%88%97%E8%A1%A8
// @route(get_group_files_by_folder)
func (bot *CQBot) CQGetGroupFilesByFolderID(groupID int64, folderID string) global.MSG {
	files, folders, err := bot.Client.ListGroupFilesByFolder(uint32(groupID), folderID)
	if err != nil {
		log.Warnf("获取群 %v 根目录 %v 子文件失败: %v", groupID, folderID, err)
		return Failed(100, "FILE_SYSTEM_API_ERROR", err.Error())
	}
	return OK(global.MSG{
		"files":   files,
		"folders": folders,
	})
}

// CQGetGroupFileURL 扩展API-获取群文件资源链接
//
// https://docs.go-cqhttp.org/api/#%E8%8E%B7%E5%8F%96%E7%BE%A4%E6%96%87%E4%BB%B6%E8%B5%84%E6%BA%90%E9%93%BE%E6%8E%A5
// @route(get_group_file_url)
func (bot *CQBot) CQGetGroupFileURL(groupID int64, fileID string) global.MSG {
	url, err := bot.Client.GetGroupFileURL(uint32(groupID), fileID)
	if err != nil {
		return Failed(100, "FILE_SYSTEM_API_ERROR")
	}
	return OK(global.MSG{
		"url": url,
	})
}

// CQUploadGroupFile 扩展API-上传群文件
//
// https://docs.go-cqhttp.org/api/#%E4%B8%8A%E4%BC%A0%E7%BE%A4%E6%96%87%E4%BB%B6
// @route(upload_group_file)
func (bot *CQBot) CQUploadGroupFile(groupID int64, file, name, folder string) global.MSG {
	if !global.FileExists(file) {
		log.Warnf("上传群文件 %v 失败: 文件不存在", file)
		return Failed(100, "FILE_NOT_FOUND", "文件不存在")
	}
	if err := bot.Client.SendGroupFile(uint32(groupID), file, name, utils.Ternary(folder == "", "/", folder)); err != nil {
		log.Warnf("上传群 %v 文件 %v 失败: %v", groupID, file, err)
		return Failed(100, "FILE_SYSTEM_UPLOAD_API_ERROR", err.Error())
	}
	return OK(nil)
}

// CQUploadPrivateFile 扩展API-上传私聊文件
//
// @route(upload_private_file)
func (bot *CQBot) CQUploadPrivateFile(userID int64, file, name string) global.MSG {
	if !global.FileExists(file) {
		log.Warnf("上传群文件 %v 失败: 文件不存在", file)
		return Failed(100, "FILE_NOT_FOUND", "文件不存在")
	}
	if err := bot.Client.SendPrivateFile(uint32(userID), file, name); err != nil {
		log.Warnf("上传私聊 %v 文件 %v 失败: %+v", userID, file, err)
		return Failed(100, "FILE_SYSTEM_UPLOAD_API_ERROR", err.Error())
	}
	return OK(nil)
}

// CQGroupFileCreateFolder 拓展API-创建群文件文件夹
//
// @route(create_group_file_folder)
func (bot *CQBot) CQGroupFileCreateFolder(groupID int64, parentID, name string) global.MSG {
	if err := bot.Client.CreateGroupFolder(uint32(groupID), parentID, name); err != nil {
		log.Warnf("创建群 %v 文件夹失败: %v", groupID, err)
		return Failed(100, "FILE_SYSTEM_API_ERROR", err.Error())
	}
	return OK(nil)
}

// CQGroupFileDeleteFolder 拓展API-删除群文件文件夹
//
// @route(delete_group_folder)
// @rename(id->folder_id)
func (bot *CQBot) CQGroupFileDeleteFolder(groupID int64, id string) global.MSG {
	if err := bot.Client.DeleteGroupFolder(uint32(groupID), id); err != nil {
		log.Warnf("删除群 %v 文件夹 %v 时出现错误: %v", groupID, id, err)
		return Failed(200, "FILE_SYSTEM_API_ERROR", err.Error())
	}
	return OK(nil)
}

// CQGroupFileDeleteFile 拓展API-删除群文件
//
// @route(delete_group_file)
func (bot *CQBot) CQGroupFileDeleteFile(groupID int64, id string) global.MSG {
	if err := bot.Client.DeleteGroupFile(uint32(groupID), id); err != nil {
		log.Warnf("删除群 %v 文件 %v 时出现错误: %v", groupID, id, err)
		return Failed(200, "FILE_SYSTEM_API_ERROR", err.Error())
	}
	return OK(nil)
}

// CQSendMessage 发送消息
//
// @route11(send_msg)
// @rename(m->message)
func (bot *CQBot) CQSendMessage(groupID, userID int64, m gjson.Result, messageType string, autoEscape bool) global.MSG {
	switch {
	case messageType == "group":
		return bot.CQSendGroupMessage(groupID, m, autoEscape)
	case messageType == "private":
		fallthrough
	case userID != 0:
		return bot.CQSendPrivateMessage(userID, groupID, m, autoEscape)
	case groupID != 0:
		return bot.CQSendGroupMessage(groupID, m, autoEscape)
	}
	return global.MSG{}
}

// CQSendForwardMessage 发送合并转发消息
//
// @route11(send_forward_msg)
// @rename(m->messages)
func (bot *CQBot) CQSendForwardMessage(groupID, userID int64, m gjson.Result, messageType string) global.MSG {
	switch {
	case messageType == "group":
		return bot.CQSendGroupForwardMessage(groupID, m)
	case messageType == "private":
		fallthrough
	case userID != 0:
		return bot.CQSendPrivateForwardMessage(userID, m)
	case groupID != 0:
		return bot.CQSendGroupForwardMessage(groupID, m)
	}
	return global.MSG{}
}

// CQSendGroupMessage 发送群消息
//
// https://git.io/Jtz1c
// @route11(send_group_msg)
// @rename(m->message)
func (bot *CQBot) CQSendGroupMessage(groupID int64, m gjson.Result, autoEscape bool) global.MSG {
	group := bot.Client.GetCachedGroupInfo(uint32(groupID))
	if group == nil {
		return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
	}
	var elem []message.IMessageElement
	if m.Type == gjson.JSON {
		elem = bot.ConvertObjectMessage(onebot.V11, m, message.SourceGroup)
	} else {
		str := m.String()
		if str == "" {
			log.Warnf("群 %v 消息发送失败: 信息为空.", groupID)
			return Failed(100, "EMPTY_MSG_ERROR", "消息为空")
		}
		if autoEscape {
			elem = []message.IMessageElement{message.NewText(str)}
		} else {
			elem = bot.ConvertStringMessage(onebot.V11, str, message.SourceGroup)
		}
	}
	mid, err := bot.SendGroupMessage(groupID, &message.SendingMessage{Elements: elem})
	if err != nil {
		return Failed(100, "SEND_MSG_API_ERROR", err.Error())
	}
	log.Infof("发送群 %v(%v) 的消息: %v (%v)", group.GroupName, groupID, limitedString(m.String()), mid)
	return OK(global.MSG{"message_id": mid})
}

func (bot *CQBot) uploadForwardElement(m gjson.Result, target int64, sourceType message.SourceType) (*message.ForwardMessage, error) {
	ts := time.Now().Add(-time.Minute * 5)
	source := message.Source{SourceType: sourceType, PrimaryID: target}
	var convertMessage func(m gjson.Result) *message.ForwardMessage
	convertMessage = func(m gjson.Result) *message.ForwardMessage {
		fm := message.NewForwardWithNodes(nil)
		var w worker
		resolveElement := func(elems []message.IMessageElement) []message.IMessageElement {
			for i, elem := range elems {
				p := &elems[i]
				switch o := elem.(type) {
				case *msg.LocalVideo:
					w.do(func() {
						gm, err := bot.uploadLocalVideo(source, o)
						if err != nil {
							log.Warnf(uploadFailedTemplate, "合并转发", target, "视频", err)
						} else {
							*p = gm
						}
					})
				case *msg.LocalImage:
					w.do(func() {
						gm, err := bot.uploadLocalImage(source, o)
						if err != nil {
							log.Warnf(uploadFailedTemplate, "合并转发", target, "图片", err)
						} else {
							*p = gm
						}
					})
				}
			}
			return elems
		}

		convert := func(e gjson.Result) *message.ForwardNode {
			if e.Get("type").Str != "node" {
				return nil
			}
			if e.Get("data.id").Exists() {
				i := e.Get("data.id").Int()
				m, _ := db.GetMessageByGlobalID(int32(i))
				if m != nil {
					mSource := message.SourcePrivate
					if m.GetType() == "group" {
						mSource = message.SourceGroup
					}
					msgTime := m.GetAttribute().Timestamp
					if msgTime == 0 {
						msgTime = ts.Unix()
					}
					return &message.ForwardNode{
						SenderID:   uint32(m.GetAttribute().SenderUin),
						SenderName: m.GetAttribute().SenderName,
						Time:       uint32(msgTime),
						Message:    resolveElement(bot.ConvertContentMessage(m.GetContent(), mSource, false)),
					}
				}
				log.Warnf("警告: 引用消息 %v 错误或数据库未开启.", e.Get("data.id").Str)
				return nil
			}
			uin := e.Get("data.[user_id,uin].0").Int()
			msgTime := e.Get("data.time").Int()
			if msgTime == 0 {
				msgTime = ts.Unix()
			}
			name := e.Get("data.[name,nickname].0").Str
			c := e.Get("data.content")
			if c.IsArray() {
				nested := false
				c.ForEach(func(_, value gjson.Result) bool {
					if value.Get("type").Str == "node" {
						nested = true
						return false
					}
					return true
				})
				if nested { // 处理嵌套
					return &message.ForwardNode{
						SenderID:   uint32(uin),
						SenderName: name,
						Time:       uint32(msgTime),
						Message:    []message.IMessageElement{convertMessage(c)},
					}
				}
			}
			content := bot.ConvertObjectMessage(onebot.V11, c, sourceType)
			if uin != 0 && name != "" && len(content) > 0 {
				return &message.ForwardNode{
					SenderID:   uint32(uin),
					SenderName: name,
					Time:       uint32(msgTime),
					Message:    resolveElement(content),
				}
			}
			log.Warnf("警告: 非法 Forward node 将跳过. uin: %v name: %v content count: %v", uin, name, len(content))
			return nil
		}

		if m.IsArray() {
			for _, item := range m.Array() {
				node := convert(item)
				if node != nil {
					fm.Nodes = append(fm.Nodes, node)
				}
			}
		} else {
			node := convert(m)
			if node != nil {
				fm.Nodes = append(fm.Nodes, node)
			}
		}

		w.wait()
		return fm
	}
	return bot.Client.UploadForwardMsg(convertMessage(m), uint32(target))
}

// CQSendGroupForwardMessage 扩展API-发送合并转发(群)
//
// https://docs.go-cqhttp.org/api/#%E5%8F%91%E9%80%81%E5%90%88%E5%B9%B6%E8%BD%AC%E5%8F%91-%E7%BE%A4
// @route11(send_group_forward_msg)
// @rename(m->messages)
func (bot *CQBot) CQSendGroupForwardMessage(groupID int64, m gjson.Result) global.MSG {
	if m.Type != gjson.JSON {
		return Failed(100)
	}
	source := message.Source{
		SourceType: message.SourceGroup,
		PrimaryID:  groupID,
	}
	fe, err := bot.uploadForwardElement(m, groupID, message.SourceGroup)
	if err != nil {
		return Failed(100, "UPLOAD_FAILED", "上传合并转发消息失败")
	}
	if fe == nil {
		return Failed(100, "EMPTY_NODES", "未找到任何可发送的合并转发信息")
	}
	ret, err := bot.Client.SendGroupMessage(uint32(groupID), []message.IMessageElement{fe})
	if err != nil || ret == nil {
		if errors.Is(err, sign.ErrVersionMismatch) {
			log.Warnf("群 %v 发送消息失败: 签名与当前协议版本不对应.", groupID)
			return Failed(100, "SIGN_ERRPR", "签名与当前协议版本不对应")
		}
		log.Warnf("合并转发(群 %v)消息发送失败: 账号可能被风控.", groupID)
		return Failed(100, "SEND_MSG_API_ERROR", "请参考 go-cqhttp 端输出")
	}
	mid := bot.InsertGroupMessage(ret, source)
	log.Infof("发送群 %v(%v)  的合并转发消息: %v (%v)", groupID, groupID, limitedString(m.String()), mid)
	return OK(global.MSG{
		"message_id": mid,
		"forward_id": fe.ResID,
	})
}

// CQSendPrivateForwardMessage 扩展API-发送合并转发(好友)
//
// https://docs.go-cqhttp.org/api/#%E5%8F%91%E9%80%81%E5%90%88%E5%B9%B6%E8%BD%AC%E5%8F%91-%E7%BE%A4
// @route11(send_private_forward_msg)
// @rename(m->messages)
func (bot *CQBot) CQSendPrivateForwardMessage(userID int64, m gjson.Result) global.MSG {
	if m.Type != gjson.JSON {
		return Failed(100)
	}
	fe, err := bot.uploadForwardElement(m, 0, message.SourcePrivate)
	if err != nil {
		return Failed(100, "UPLOAD_FAILED", "上传合并转发消息失败")
	}
	mid := bot.SendPrivateMessage(userID, 0, &message.SendingMessage{Elements: []message.IMessageElement{fe}})
	if mid == -1 {
		log.Warnf("合并转发(好友 %v)消息发送失败: 账号可能被风控.", userID)
		return Failed(100, "SEND_MSG_API_ERROR", "请参考 go-cqhttp 端输出")
	}
	log.Infof("发送好友 %v(%v)  的合并转发消息: %v (%v)", userID, userID, limitedString(m.String()), mid)
	return OK(global.MSG{
		"message_id": mid,
		"forward_id": fe.ResID,
	})
}

// CQSendPrivateMessage 发送私聊消息
//
// https://git.io/Jtz1l
// @route11(send_private_msg)
// @rename(m->message)
func (bot *CQBot) CQSendPrivateMessage(userID int64, groupID int64, m gjson.Result, autoEscape bool) global.MSG {
	var elem []message.IMessageElement
	if m.Type == gjson.JSON {
		elem = bot.ConvertObjectMessage(onebot.V11, m, message.SourcePrivate)
	} else {
		str := m.String()
		if str == "" {
			return Failed(100, "EMPTY_MSG_ERROR", "消息为空")
		}
		if autoEscape {
			elem = []message.IMessageElement{message.NewText(str)}
		} else {
			elem = bot.ConvertStringMessage(onebot.V11, str, message.SourcePrivate)
		}
	}
	mid := bot.SendPrivateMessage(userID, groupID, &message.SendingMessage{Elements: elem})
	if mid == -1 {
		return Failed(100, "SEND_MSG_API_ERROR", "请参考 go-cqhttp 端输出")
	}
	log.Infof("发送好友 %v(%v)  的消息: %v (%v)", userID, userID, limitedString(m.String()), mid)
	return OK(global.MSG{"message_id": mid})
}

// CQSetGroupCard 设置群名片(群备注)
//
// https://git.io/Jtz1B
// @route(set_group_card)
func (bot *CQBot) CQSetGroupCard(groupID, userID int64, card string) global.MSG {
	if g := bot.Client.GetCachedGroupInfo(uint32(groupID)); g != nil {
		if m := bot.Client.GetCachedMemberInfo(uint32(userID), uint32(groupID)); m != nil {
			if err := bot.Client.SetGroupMemberName(uint32(groupID), uint32(userID), card); err != nil {
				return Failed(100, "SET_CARD_FAILED", err.Error())
			}
			return OK(nil)
		}
	}
	return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
}

// CQSetGroupSpecialTitle 设置群组专属头衔
//
// https://git.io/Jtz10
// @route(set_group_special_title)
// @rename(title->special_title)
func (bot *CQBot) CQSetGroupSpecialTitle(groupID, userID int64, title string) global.MSG {
	if g := bot.Client.GetCachedGroupInfo(uint32(groupID)); g != nil {
		if m := bot.Client.GetCachedMemberInfo(uint32(userID), uint32(groupID)); m != nil {
			if err := bot.Client.SetGroupMemberSpecialTitle(uint32(groupID), uint32(userID), title); err != nil {
				return Failed(100, "SET_Title_FAILED", err.Error())
			}
			return OK(nil)
		}
	}
	return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
}

// CQSetGroupName 设置群名
//
// https://git.io/Jtz12
// @route(set_group_name)
// @rename(name->group_name)
func (bot *CQBot) CQSetGroupName(groupID int64, name string) global.MSG {
	if g := bot.Client.GetCachedGroupInfo(uint32(groupID)); g != nil {
		if err := bot.Client.SetGroupName(uint32(groupID), name); err != nil {
			return Failed(100, "SET_NAME_FAILED", err.Error())
		}
		return OK(nil)
	}
	return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
}

// CQGetGroupMemo 扩展API-获取群公告
// @route(_get_group_notice)
func (bot *CQBot) CQGetGroupMemo(groupID int64) global.MSG {
	r, err := bot.Client.GetGroupNotice(uint32(groupID))
	if err != nil {
		return Failed(100, "获取群公告失败", err.Error())
	}

	return OK(r)
}

// CQSetGroupMemo 扩展API-发送群公告
//
// https://docs.go-cqhttp.org/api/#%E5%8F%91%E9%80%81%E7%BE%A4%E5%85%AC%E5%91%8A
// @route(_send_group_notice)
// @rename(msg->content, img->image)
func (bot *CQBot) CQSetGroupMemo(groupID int64, msg, img string) global.MSG {
	if g := bot.Client.GetCachedGroupInfo(uint32(groupID)); g != nil {
		if s := bot.Client.GetCachedMemberInfo(uint32(groupID), uint32(groupID)); s != nil && s.Permission == entity.Member {
			return Failed(100, "PERMISSION_DENIED", "权限不足")
		}
		if img != "" {
			data, err := global.FindFile(img, "", global.ImagePath)
			if err != nil {
				return Failed(100, "IMAGE_NOT_FOUND", "图片未找到")
			}
			noticeID, err := bot.Client.AddGroupNoticeWithPic(uint32(groupID), msg, data)
			if err != nil {
				return Failed(100, "SEND_NOTICE_ERROR", err.Error())
			}
			return OK(global.MSG{"notice_id": noticeID})
		}
		noticeID, err := bot.Client.AddGroupNoticeSimple(uint32(groupID), msg)
		if err != nil {
			return Failed(100, "SEND_NOTICE_ERROR", err.Error())
		}
		return OK(global.MSG{"notice_id": noticeID})
	}
	return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
}

// CQDelGroupMemo 扩展API-删除群公告
// @route(_del_group_notice)
// @rename(fid->notice_id)
func (bot *CQBot) CQDelGroupMemo(groupID int64, fid string) global.MSG {
	if g := bot.Client.GetCachedGroupInfo(uint32(groupID)); g != nil {
		if s := bot.Client.GetCachedMemberInfo(uint32(groupID), uint32(groupID)); s != nil && s.Permission == entity.Member {
			return Failed(100, "PERMISSION_DENIED", "权限不足")
		}
		err := bot.Client.DelGroupNotice(uint32(groupID), fid)
		if err != nil {
			return Failed(100, "DELETE_NOTICE_ERROR", err.Error())
		}
		return OK(nil)
	}
	return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
}

// CQSetGroupKick 群组踢人
//
// https://git.io/Jtz1V
// @route(set_group_kick)
// @rename(block->reject_add_request)
func (bot *CQBot) CQSetGroupKick(groupID int64, userID int64, block bool) global.MSG {
	if g := bot.Client.GetCachedGroupInfo(uint32(groupID)); g != nil {
		m := bot.Client.GetCachedMemberInfo(uint32(userID), uint32(groupID))
		if m == nil {
			return Failed(100, "MEMBER_NOT_FOUND", "人员不存在")
		}
		if err := bot.Client.KickGroupMember(uint32(groupID), uint32(userID), block); err != nil {
			return Failed(100, "NOT_MANAGEABLE", "机器人权限不足")
		}
		return OK(nil)
	}
	return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
}

// CQSetGroupBan 群组单人禁言
//
// https://git.io/Jtz1w
// @route(set_group_ban)
// @default(duration=1800)
func (bot *CQBot) CQSetGroupBan(groupID, userID int64, duration uint32) global.MSG {
	if g := bot.Client.GetCachedGroupInfo(uint32(groupID)); g != nil {
		if m := bot.Client.GetCachedMemberInfo(uint32(userID), uint32(groupID)); m != nil {
			if duration >= 2592000 {
				return Failed(100, "DURATION_IS_NOT_IN_RANGE", "非法的禁言时长")
			}
			if err := bot.Client.SetGroupMemberMute(uint32(groupID), uint32(userID), duration); err != nil {
				return Failed(100, "NOT_MANAGEABLE", "机器人权限不足")
			}
			return OK(nil)
		}
	}
	return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
}

// CQSetGroupWholeBan 群组全员禁言
//
// https://git.io/Jtz1o
// @route(set_group_whole_ban)
// @default(enable=true)
func (bot *CQBot) CQSetGroupWholeBan(groupID int64, enable bool) global.MSG {
	if g := bot.Client.GetCachedGroupInfo(uint32(groupID)); g != nil {
		if err := bot.Client.SetGroupGlobalMute(uint32(groupID), enable); err != nil {
			return Failed(100, "NOT_MANAGEABLE", "机器人权限不足")
		}
		return OK(nil)
	}
	return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
}

// CQSetGroupLeave 退出群组
//
// https://git.io/Jtz1K
// @route(set_group_leave)
func (bot *CQBot) CQSetGroupLeave(groupID int64) global.MSG {
	if g := bot.Client.GetCachedGroupInfo(uint32(groupID)); g != nil {
		if err := bot.Client.SetGroupLeave(uint32(groupID)); err != nil {
			return Failed(100, "反正是失败了.png", err.Error())
		}
		return OK(nil)
	}
	return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
}

// CQGetAtAllRemain 扩展API-获取群 @全体成员 剩余次数
//
// https://docs.go-cqhttp.org/api/#%E8%8E%B7%E5%8F%96%E7%BE%A4-%E5%85%A8%E4%BD%93%E6%88%90%E5%91%98-%E5%89%A9%E4%BD%99%E6%AC%A1%E6%95%B0
// @route(get_group_at_all_remain)
func (bot *CQBot) CQGetAtAllRemain(groupID int64) global.MSG {
	if g := bot.Client.GetCachedGroupInfo(uint32(groupID)); g != nil {
		i, err := bot.Client.GetAtAllRemain(bot.Client.Uin, uint32(groupID))
		if err != nil {
			return Failed(100, "GROUP_REMAIN_API_ERROR", err.Error())
		}
		return OK(i)
	}
	return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
}

// CQProcessFriendRequest 处理加好友请求
//
// https://git.io/Jtz11
// @route(set_friend_add_request)
// @default(approve=true)
func (bot *CQBot) CQProcessFriendRequest(flag string, approve bool) global.MSG {
	req, ok := bot.friendReqCache.Load(flag)
	if !ok {
		return Failed(100, "FLAG_NOT_FOUND", "FLAG不存在")
	}
	if approve {
		_ = bot.Client.SetFriendRequest(true, req.SourceUID)
	} else {
		_ = bot.Client.SetFriendRequest(false, req.SourceUID)
	}
	return OK(nil)
}

// CQProcessGroupRequest 处理加群请求／邀请
//
// https://git.io/Jtz1D
// @route(set_group_add_request)
// @rename(sub_type->"[sub_type\x2Ctype].0")
// @default(approve=true)
func (bot *CQBot) CQProcessGroupRequest(flag, subType, reason string, approve bool) global.MSG {
	msgs, err := bot.Client.GetGroupSystemMessages(false, 20)
	if err != nil {
		log.Warnf("获取群系统消息失败: %v", err)
		return Failed(100, "SYSTEM_MSG_API_ERROR", err.Error())
	}
	filteredmsgs, err := bot.Client.GetGroupSystemMessages(true, 20)
	if err != nil {
		log.Warnf("获取群系统消息失败: %v", err)
		return Failed(100, "SYSTEM_MSG_API_ERROR", err.Error())
	}
	if subType == "add" {
		for _, req := range append(msgs.JoinRequests, filteredmsgs.JoinRequests...) {
			if strconv.FormatInt(int64(req.Sequence), 10) == flag {
				if req.Checked {
					log.Warnf("处理群系统消息失败: 无法操作已处理的消息.")
					return Failed(100, "FLAG_HAS_BEEN_CHECKED", "消息已被处理")
				}
				if approve {
					_ = bot.Client.SetGroupRequest(req.IsFiltered, true, req.Sequence, uint32(req.EventType), req.GroupUin, "")
				} else {
					_ = bot.Client.SetGroupRequest(req.IsFiltered, false, req.Sequence, uint32(req.EventType), req.GroupUin, reason)
				}
				return OK(nil)
			}
		}
	} else {
		for _, req := range append(msgs.InvitedRequests, filteredmsgs.InvitedRequests...) {
			if strconv.FormatInt(int64(req.Sequence), 10) == flag {
				if req.Checked {
					log.Warnf("处理群系统消息失败: 无法操作已处理的消息.")
					return Failed(100, "FLAG_HAS_BEEN_CHECKED", "消息已被处理")
				}
				if approve {
					_ = bot.Client.SetGroupRequest(req.IsFiltered, true, req.Sequence, uint32(req.EventType), req.GroupUin, "")
				} else {
					_ = bot.Client.SetGroupRequest(req.IsFiltered, false, req.Sequence, uint32(req.EventType), req.GroupUin, reason)
				}
				return OK(nil)
			}
		}
	}
	log.Warnf("处理群系统消息失败: 消息 %v 不存在.", flag)
	return Failed(100, "FLAG_NOT_FOUND", "FLAG不存在")
}

// CQDeleteMessage 撤回消息
//
// https:// git.io/Jtz1y
// @route(delete_msg)
func (bot *CQBot) CQDeleteMessage(messageID int32) global.MSG {
	msg, err := db.GetMessageByGlobalID(messageID)
	if err != nil {
		log.Warnf("撤回消息时出现错误: %v", err)
		return Failed(100, "MESSAGE_NOT_FOUND", "消息不存在")
	}
	switch o := msg.(type) {
	case *db.StoredGroupMessage:
		if err = bot.Client.RecallGroupMessage(uint32(o.GroupCode), uint32(o.Attribute.MessageSeq)); err != nil {
			log.Warnf("撤回 %v 失败: %v", messageID, err)
			return Failed(100, "RECALL_API_ERROR", err.Error())
		}
	case *db.StoredPrivateMessage:
		if o.Attribute.SenderUin != int64(bot.Client.Uin) {
			log.Warnf("撤回 %v 失败: 好友会话无法撤回对方消息.", messageID)
			return Failed(100, "CANNOT_RECALL_FRIEND_MSG", "无法撤回对方消息")
		}
		if err = bot.Client.RecallFriendMessage(uint32(o.TargetUin), uint32(o.Attribute.MessageSeq), uint32(o.Attribute.InternalID), uint32(o.Attribute.ClientSeq), uint32(o.Attribute.Timestamp)); err != nil {
			log.Warnf("撤回 %v 失败: %v", messageID, err)
			return Failed(100, "RECALL_API_ERROR", err.Error())
		}
	default:
		return Failed(100, "UNKNOWN_ERROR")
	}
	return OK(nil)
}

// CQSetGroupAdmin 群组设置管理员
//
// https://git.io/Jtz1S
// @route(set_group_admin)
// @default(enable=true)
func (bot *CQBot) CQSetGroupAdmin(groupID, userID int64, enable bool) global.MSG {
	if g := bot.Client.GetCachedGroupInfo(uint32(groupID)); g == nil {
		return Failed(100, "GROUP_NOT_FOUND", "群不存在")
	}
	m := bot.Client.GetCachedMemberInfo(uint32(userID), uint32(groupID))
	if m == nil {
		return Failed(100, "GROUP_MEMBER_NOT_FOUND", "群成员不存在")
	}
	if m.Permission != entity.Owner {
		return Failed(100, "PERMISSION_DENIED", "或权限不足")
	}
	if err := bot.Client.SetGroupAdmin(uint32(groupID), uint32(userID), enable); err != nil {
		return Failed(100, "UNKNOWN_ERROR", err.Error())
	}
	return OK(nil)
}

// CQSetGroupAnonymous 群组匿名
//
// https://beautyyu.one
// @route(set_group_anonymous)
// @default(enable=true)
// func (bot *CQBot) CQSetGroupAnonymous(groupID int64, enable bool) global.MSG {
//	if g := bot.Client.FindGroup(groupID); g != nil {
//		g.SetAnonymous(enable)
//		return OK(nil)
//	}
//	return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
//}

// CQGetGroupHonorInfo 获取群荣誉信息
//
// https://git.io/Jtz1H
// @route(get_group_honor_info)
// @rename(t->type)
func (bot *CQBot) CQGetGroupHonorInfo(groupID int64, t string) global.MSG {
	msg := global.MSG{"group_id": groupID}
	convertMem := func(memList []entity.HonorMemberInfo) (ret []global.MSG) {
		for _, mem := range memList {
			ret = append(ret, global.MSG{
				"user_id":     mem.Uin,
				"nickname":    mem.Name,
				"avatar":      mem.Avatar,
				"description": mem.Desc,
			})
		}
		return
	}
	if t == "talkative" || t == "all" {
		if honor, err := bot.Client.GetGroupHonorInfo(uint32(groupID), entity.Talkative); err == nil {
			if honor.CurrentTalkative.Uin != 0 {
				msg["current_talkative"] = global.MSG{
					"user_id":   honor.CurrentTalkative.Uin,
					"nickname":  honor.CurrentTalkative.Name,
					"avatar":    honor.CurrentTalkative.Avatar,
					"day_count": honor.CurrentTalkative.DayCount,
				}
			}
			msg["talkative_list"] = convertMem(honor.TalkativeList)
		} else {
			log.Infof("获取群龙王出错：%v", err)
		}
	}

	if t == "performer" || t == "all" {
		if honor, err := bot.Client.GetGroupHonorInfo(uint32(groupID), entity.Performer); err == nil {
			msg["performer_list"] = convertMem(honor.ActorList)
		} else {
			log.Infof("获取群聊之火出错：%v", err)
		}
	}

	if t == "legend" || t == "all" {
		if honor, err := bot.Client.GetGroupHonorInfo(uint32(groupID), entity.Legend); err == nil {
			msg["legend_list"] = convertMem(honor.LegendList)
		} else {
			log.Infof("获取群聊炽焰出错：%v", err)
		}
	}

	if t == "strong_newbie" || t == "all" {
		if honor, err := bot.Client.GetGroupHonorInfo(uint32(groupID), entity.StrongNewbie); err == nil {
			msg["strong_newbie_list"] = convertMem(honor.StrongNewbieList)
		} else {
			log.Infof("获取冒尖小春笋出错：%v", err)
		}
	}

	if t == "emotion" || t == "all" {
		if honor, err := bot.Client.GetGroupHonorInfo(uint32(groupID), entity.Emotion); err == nil {
			msg["emotion_list"] = convertMem(honor.EmotionList)
		} else {
			log.Infof("获取快乐之源出错：%v", err)
		}
	}
	return OK(msg)
}

// CQGetStrangerInfo 获取陌生人信息
//
// https://git.io/Jtz17
// @route11(get_stranger_info)
// @route12(get_user_info)
func (bot *CQBot) CQGetStrangerInfo(userID int64) global.MSG {
	info, err := bot.Client.FetchUserInfoUin(uint32(userID))
	if err != nil {
		return Failed(100, "SUMMARY_API_ERROR", err.Error())
	}
	return OK(global.MSG{
		"user_id":  info.Uin,
		"nickname": info.Nickname,
		"qid":      info.QID,
		"sex": func() string {
			// if info.Sex == 1 {
			//	return "female"
			//} else if info.Sex == 0 {
			//	return "male"
			//}
			// unknown = 0x2
			return "unknown"
		}(),
		"sign":  info.PersonalSign,
		"age":   info.Age,
		"level": info.Level,
		// "login_days": info.LoginDays,
		"vip_level": info.VipLevel,
	})
}

// CQHandleQuickOperation 隐藏API-对事件执行快速操作
//
// https://git.io/Jtz15
// @route11(".handle_quick_operation")
func (bot *CQBot) CQHandleQuickOperation(context, operation gjson.Result) global.MSG {
	postType := context.Get("post_type").Str

	switch postType {
	case "message":
		anonymous := context.Get("anonymous")
		isAnonymous := anonymous.Type != gjson.Null
		msgType := context.Get("message_type").Str
		reply := operation.Get("reply")

		if reply.Exists() {
			autoEscape := param.EnsureBool(operation.Get("auto_escape"), false)
			at := !isAnonymous && operation.Get("at_sender").Bool() && msgType == "group"
			if at && reply.IsArray() {
				// 在 reply 数组头部插入CQ码
				replySegments := make([]global.MSG, 0)
				segments := make([]global.MSG, 0)
				segments = append(segments, global.MSG{
					"type": "at",
					"data": global.MSG{
						"qq": context.Get("sender.user_id").Int(),
					},
				})

				err := json.Unmarshal(utils.S2B(reply.Raw), &replySegments)
				if err != nil {
					log.WithError(err).Warnf("处理 at_sender 过程中发生错误")
					return Failed(-1, "处理 at_sender 过程中发生错误", err.Error())
				}

				segments = append(segments, replySegments...)

				modified, err := json.Marshal(segments)
				if err != nil {
					log.WithError(err).Warnf("处理 at_sender 过程中发生错误")
					return Failed(-1, "处理 at_sender 过程中发生错误", err.Error())
				}

				reply = gjson.Parse(utils.B2S(modified))
			} else if at && reply.Type == gjson.String {
				reply = gjson.Parse(fmt.Sprintf(
					"\"[CQ:at,qq=%d]%s\"",
					context.Get("sender.user_id").Int(),
					reply.String(),
				))
			}

			if msgType == "group" {
				bot.CQSendGroupMessage(context.Get("group_id").Int(), reply, autoEscape)
			}
			if msgType == "private" {
				bot.CQSendPrivateMessage(context.Get("user_id").Int(), context.Get("group_id").Int(), reply, autoEscape)
			}
		}
		if msgType == "group" {
			if operation.Get("delete").Bool() {
				bot.CQDeleteMessage(int32(context.Get("message_id").Int()))
			}
			if !isAnonymous && operation.Get("kick").Bool() {
				bot.CQSetGroupKick(context.Get("group_id").Int(), context.Get("user_id").Int(), operation.Get("reject_add_request").Bool())
			}
			if operation.Get("ban").Bool() {
				var duration uint32 = 30 * 60
				if operation.Get("ban_duration").Exists() {
					duration = uint32(operation.Get("ban_duration").Uint())
				}
				// unsupported anonymous ban yet
				if !isAnonymous {
					bot.CQSetGroupBan(context.Get("group_id").Int(), context.Get("user_id").Int(), duration)
				}
			}
		}
	case "request":
		reqType := context.Get("request_type").Str
		if operation.Get("approve").Exists() {
			if reqType == "friend" {
				bot.CQProcessFriendRequest(context.Get("flag").String(), operation.Get("approve").Bool())
			}
			if reqType == "group" {
				bot.CQProcessGroupRequest(context.Get("flag").String(), context.Get("sub_type").Str, operation.Get("reason").Str, operation.Get("approve").Bool())
			}
		}
	}
	return OK(nil)
}

// CQGetImage 获取图片(修改自OneBot)
//
// https://docs.go-cqhttp.org/api/#%E8%8E%B7%E5%8F%96%E5%9B%BE%E7%89%87%E4%BF%A1%E6%81%AF
// @route(get_image)
func (bot *CQBot) CQGetImage(file string) global.MSG {
	var b []byte
	var err error
	if strings.HasSuffix(file, ".image") {
		var f []byte
		f, err = hex.DecodeString(strings.TrimSuffix(file, ".image"))
		b = cache.Image.Get(f)
	}

	if b == nil {
		if !global.FileExists(path.Join(global.ImagePath, file)) {
			return Failed(100)
		}
		b, err = os.ReadFile(path.Join(global.ImagePath, file))
	}

	if err == nil {
		r := binary.NewReader(b)
		r.ReadBytes(16)
		msg := global.MSG{
			"size":     r.ReadI32(),
			"filename": r.ReadStringWithLength("u32", true),
			"url":      r.ReadStringWithLength("u32", true),
		}
		local := path.Join(global.CachePath, file+path.Ext(msg["filename"].(string)))
		if !global.FileExists(local) {
			r := download.Request{URL: msg["url"].(string)}
			if err := r.WriteToFile(local); err != nil {
				log.Warnf("下载图片 %v 时出现错误: %v", msg["url"], err)
				return Failed(100, "DOWNLOAD_IMAGE_ERROR", err.Error())
			}
		}
		msg["file"] = local
		return OK(msg)
	}
	return Failed(100, "LOAD_FILE_ERROR", err.Error())
}

// CQDownloadFile 扩展API-下载文件到缓存目录
//
// https://docs.go-cqhttp.org/api/#%E4%B8%8B%E8%BD%BD%E6%96%87%E4%BB%B6%E5%88%B0%E7%BC%93%E5%AD%98%E7%9B%AE%E5%BD%95
// @route(download_file)
func (bot *CQBot) CQDownloadFile(url string, headers gjson.Result, threadCount int) global.MSG {
	h := map[string]string{}
	if headers.IsArray() {
		for _, sub := range headers.Array() {
			first, second, ok := strings.Cut(sub.String(), "=")
			if ok {
				h[first] = second
			}
		}
	}
	if headers.Type == gjson.String {
		lines := strings.Split(headers.String(), "\r\n")
		for _, sub := range lines {
			first, second, ok := strings.Cut(sub, "=")
			if ok {
				h[first] = second
			}
		}
	}

	hash := md5.Sum([]byte(url))
	file := path.Join(global.CachePath, hex.EncodeToString(hash[:])+".cache")
	if global.FileExists(file) {
		if err := os.Remove(file); err != nil {
			log.Warnf("删除缓存文件 %v 时出现错误: %v", file, err)
			return Failed(100, "DELETE_FILE_ERROR", err.Error())
		}
	}
	r := download.Request{URL: url, Header: h}
	if err := r.WriteToFileMultiThreading(file, threadCount); err != nil {
		log.Warnf("下载链接 %v 时出现错误: %v", url, err)
		return Failed(100, "DOWNLOAD_FILE_ERROR", err.Error())
	}
	abs, _ := filepath.Abs(file)
	return OK(global.MSG{
		"file": abs,
	})
}

// CQGetForwardMessage 获取合并转发消息
//
// https://git.io/Jtz1F
// @route(get_forward_msg)
// @rename(res_id->"[message_id\x2Cid].0")
func (bot *CQBot) CQGetForwardMessage(resID string) global.MSG {
	m, err := bot.Client.FetchForwardMsg(resID)
	if err != nil {
		return Failed(100, "MSG_NOT_FOUND", "消息不存在")
	}

	var transformNodes func(nodes []*message.ForwardNode) []global.MSG
	transformNodes = func(nodes []*message.ForwardNode) []global.MSG {
		r := make([]global.MSG, len(nodes))
		for i, n := range nodes {
			source := message.Source{SourceType: message.SourceGroup}
			bot.checkMedia(n.Message, source)
			content := ToFormattedMessage(n.Message, source)
			if len(n.Message) == 1 {
				if forward, ok := n.Message[0].(*message.ForwardMessage); ok {
					content = transformNodes(forward.Nodes)
				}
			}
			r[i] = global.MSG{
				"sender": global.MSG{
					"user_id":  n.SenderID,
					"nickname": n.SenderName,
				},
				"time":     n.Time,
				"content":  content,
				"group_id": n.GroupID,
			}
		}
		return r
	}
	return OK(global.MSG{
		"messages": transformNodes(m.Nodes),
	})
}

// CQGetMessage 获取消息
//
// https://git.io/Jtz1b
// @route(get_msg)
func (bot *CQBot) CQGetMessage(messageID int32) global.MSG {
	msg, err := db.GetMessageByGlobalID(messageID)
	if err != nil {
		log.Warnf("获取消息时出现错误: %v", err)
		return Failed(100, "MSG_NOT_FOUND", "消息不存在")
	}
	m := global.MSG{
		"message_id":    msg.GetGlobalID(),
		"message_id_v2": msg.GetID(),
		"message_type":  msg.GetType(),
		"real_id":       msg.GetAttribute().MessageSeq,
		"message_seq":   msg.GetAttribute().MessageSeq,
		"group":         msg.GetType() == "group",
		"sender": global.MSG{
			"user_id":  msg.GetAttribute().SenderUin,
			"nickname": msg.GetAttribute().SenderName,
		},
		"time": msg.GetAttribute().Timestamp,
	}
	switch o := msg.(type) {
	case *db.StoredGroupMessage:
		m["group_id"] = o.GroupCode
		m["message"] = ToFormattedMessage(bot.ConvertContentMessage(o.Content, message.SourceGroup, false), message.Source{SourceType: message.SourceGroup, PrimaryID: o.GroupCode})
	case *db.StoredPrivateMessage:
		m["message"] = ToFormattedMessage(bot.ConvertContentMessage(o.Content, message.SourcePrivate, false), message.Source{SourceType: message.SourcePrivate})
	}
	return OK(m)
}

// CQGetGroupSystemMessages 扩展API-获取群系统消息
//
// https://docs.go-cqhttp.org/api/#%E8%8E%B7%E5%8F%96%E7%BE%A4%E7%B3%BB%E7%BB%9F%E6%B6%88%E6%81%AF
// @route(get_group_system_msg)
func (bot *CQBot) CQGetGroupSystemMessages() global.MSG {
	msgs, err := bot.Client.GetGroupSystemMessages(false, 20)
	if err != nil {
		log.Warnf("获取群系统消息失败: %v", err)
		return Failed(100, "SYSTEM_MSG_API_ERROR", err.Error())
	}
	return OK(msgs)
}

// CQGetGroupMessageHistory 获取群消息历史记录
//
// https://docs.go-cqhttp.org/api/#%E8%8E%B7%E5%8F%96%E7%BE%A4%E6%B6%88%E6%81%AF%E5%8E%86%E5%8F%B2%E8%AE%B0%E5%BD%95
// @route(get_group_msg_history)
// @rename(seq->message_seq)
func (bot *CQBot) CQGetGroupMessageHistory(groupID int64, seq int64) global.MSG {
	if g := bot.Client.GetCachedGroupInfo(uint32(groupID)); g == nil {
		return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
	}
	if seq == 0 {
		g := bot.Client.GetCachedGroupInfo(uint32(groupID))
		seq = int64(g.LastMsgSeq)
	}
	msg, err := bot.Client.GetGroupMessages(uint32(groupID), uint32(math.Max(float64(seq-19), 1)), uint32(seq))
	if err != nil {
		log.Warnf("获取群历史消息失败: %v", err)
		return Failed(100, "MESSAGES_API_ERROR", err.Error())
	}
	source := message.Source{
		SourceType: message.SourcePrivate,
		PrimaryID:  0,
	}
	ms := make([]*event, 0, len(msg))
	for _, m := range msg {
		bot.checkMedia(m.Elements, source)
		id := bot.InsertGroupMessage(m, source)
		t := bot.formatGroupMessage(m)
		t.Others["message_id"] = id
		ms = append(ms, t)
	}
	return OK(global.MSG{
		"messages": ms,
	})
}

// CQGetOnlineClients 扩展API-获取当前账号在线客户端列表
//
// https://docs.go-cqhttp.org/api/#%E8%8E%B7%E5%8F%96%E5%BD%93%E5%89%8D%E8%B4%A6%E5%8F%B7%E5%9C%A8%E7%BA%BF%E5%AE%A2%E6%88%B7%E7%AB%AF%E5%88%97%E8%A1%A8
// @route(get_online_clients)
// func (bot *CQBot) CQGetOnlineClients(noCache bool) global.MSG {
//	if noCache {
//		if err := bot.Client.RefreshStatus(); err != nil {
//			log.Warnf("刷新客户端状态时出现问题 %v", err)
//			return Failed(100, "REFRESH_STATUS_ERROR", err.Error())
//		}
//	}
//	d := make([]global.MSG, 0, len(bot.Client.OnlineClients))
//	for _, oc := range bot.Client.OnlineClients {
//		d = append(d, global.MSG{
//			"app_id":      oc.AppId,
//			"device_name": oc.DeviceName,
//			"device_kind": oc.DeviceKind,
//		})
//	}
//	return OK(global.MSG{
//		"clients": d,
//	})
//}

// CQCanSendImage 检查是否可以发送图片(此处永远返回true)
//
// https://git.io/Jtz1N
// @route11(can_send_image)
func (bot *CQBot) CQCanSendImage() global.MSG {
	return OK(global.MSG{"yes": true})
}

// CQCanSendRecord 检查是否可以发送语音(此处永远返回true)
//
// https://git.io/Jtz1x
// @route11(can_send_record)
func (bot *CQBot) CQCanSendRecord() global.MSG {
	return OK(global.MSG{"yes": true})
}

// CQOcrImage 扩展API-图片OCR
//
// https://docs.go-cqhttp.org/api/#%E5%9B%BE%E7%89%87-ocr
// @route(ocr_image,".ocr_image")
// @rename(image_id->image)
func (bot *CQBot) CQOcrImage(imageID string) global.MSG {
	// TODO: fix this
	var elem msg.Element
	elem.Type = "image"
	elem.Data = []msg.Pair{{K: "file", V: imageID}}
	img, err := bot.makeImageOrVideoElem(elem, false, message.SourceGroup)
	if err != nil {
		log.Warnf("load image error: %v", err)
		return Failed(100, "LOAD_FILE_ERROR", err.Error())
	}
	image, ok := img.(*msg.LocalImage)
	if !ok {
		return Failed(100, "IMAGE_ERROR", "图片数据错误")
	}
	rsp, err := bot.Client.ImageOcr(image.URL)
	if err != nil {
		log.Warnf("ocr image error: %v", err)
		return Failed(100, "OCR_API_ERROR", err.Error())
	}
	return OK(rsp)
}

// CQSetGroupPortrait 扩展API-设置群头像
//
// https://docs.go-cqhttp.org/api/#%E8%AE%BE%E7%BD%AE%E7%BE%A4%E5%A4%B4%E5%83%8F
// @route(set_group_portrait)
func (bot *CQBot) CQSetGroupPortrait(groupID int64, file, cache string) global.MSG {
	if g := bot.Client.GetCachedGroupInfo(uint32(groupID)); g != nil {
		img, err := global.FindFile(file, cache, global.ImagePath)
		if err != nil {
			log.Warnf("set group portrait error: %v", err)
			return Failed(100, "LOAD_FILE_ERROR", err.Error())
		}
		if bot.Client.SetGroupAvatar(g.GroupUin, bytes.NewReader(img)) == nil {
			return OK(nil)
		}
		Failed(100, "PERMISSION_DENIED", "机器人权限不足")
	}
	return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
}

// CQSetGroupReaction 扩展API-设置群消息表态
//
// @route(set_group_reaction)
// @default(is_add=true)
func (bot *CQBot) CQSetGroupReaction(messageID int32, emojiID string, isAdd bool) global.MSG {
	msg, err := db.GetGroupMessageByGlobalID(messageID)
	if err != nil {
		return Failed(100, "MESSAGE_NOT_FOUND", "消息不存在")
	}
	if err := bot.Client.SetGroupReaction(uint32(msg.GroupCode), uint32(msg.Attribute.MessageSeq), emojiID, isAdd); err != nil {
		log.Warnf("发送群表态失败: %v", err)
		return Failed(100, "SEND_GROUP_REACTIONS_ERROR", err.Error())
	}
	return OK(nil)
}

// CQSetGroupAnonymousBan 群组匿名用户禁言
//
// https://git.io/Jtz1p
// @route(set_group_anonymous_ban)
// @rename(flag->"[anonymous_flag\x2Canonymous.flag].0")
// func (bot *CQBot) CQSetGroupAnonymousBan(groupID int64, flag string, duration int32) global.MSG {
//	if flag == "" {
//		return Failed(100, "INVALID_FLAG", "无效的flag")
//	}
//	if g := bot.Client.FindGroup(groupID); g != nil {
//		id, nick, ok := strings.Cut(flag, "|")
//		if !ok {
//			return Failed(100, "INVALID_FLAG", "无效的flag")
//		}
//		if err := g.MuteAnonymous(id, nick, duration); err != nil {
//			log.Warnf("anonymous ban error: %v", err)
//			return Failed(100, "CALL_API_ERROR", err.Error())
//		}
//		return OK(nil)
//	}
//	return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
//}

// CQGetStatus 获取运行状态
//
// https://git.io/JtzMe
// @route(get_status)
func (bot *CQBot) CQGetStatus(spec *onebot.Spec) global.MSG {
	if spec.Version == 11 {
		return OK(global.MSG{
			"app_initialized": true,
			"app_enabled":     true,
			"plugins_good":    nil,
			"app_good":        true,
			"online":          bot.Client.Online.Load(),
			"good":            bot.Client.Online.Load(),
			"stat":            bot.Client.GetStatistics(),
		})
	}
	return OK(global.MSG{
		"online": bot.Client.Online.Load(),
		"good":   bot.Client.Online.Load(),
		"stat":   bot.Client.GetStatistics(),
	})
}

// CQSetEssenceMessage 扩展API-设置精华消息
//
// https://docs.go-cqhttp.org/api/#%E8%AE%BE%E7%BD%AE%E7%B2%BE%E5%8D%8E%E6%B6%88%E6%81%AF
// @route(set_essence_msg)
func (bot *CQBot) CQSetEssenceMessage(messageID int32) global.MSG {
	msg, err := db.GetGroupMessageByGlobalID(messageID)
	if err != nil {
		return Failed(100, "MESSAGE_NOT_FOUND", "消息不存在")
	}
	if err := bot.Client.SetEssenceMessage(uint32(msg.GroupCode), uint32(msg.Attribute.MessageSeq), uint32(msg.Attribute.InternalID), true); err != nil {
		log.Warnf("设置精华消息 %v 失败: %v", messageID, err)
		return Failed(100, "SET_ESSENCE_MSG_ERROR", err.Error())
	}
	return OK(nil)
}

// CQDeleteEssenceMessage 扩展API-移出精华消息
//
// https://docs.go-cqhttp.org/api/#%E7%A7%BB%E5%87%BA%E7%B2%BE%E5%8D%8E%E6%B6%88%E6%81%AF
// @route(delete_essence_msg)
func (bot *CQBot) CQDeleteEssenceMessage(messageID int32) global.MSG {
	msg, err := db.GetGroupMessageByGlobalID(messageID)
	if err != nil {
		return Failed(100, "MESSAGE_NOT_FOUND", "消息不存在")
	}
	if err := bot.Client.SetEssenceMessage(uint32(msg.GroupCode), uint32(msg.Attribute.MessageSeq), uint32(msg.Attribute.InternalID), false); err != nil {
		log.Warnf("删除精华消息 %v 失败: %v", messageID, err)
		return Failed(100, "SET_ESSENCE_MSG_ERROR", err.Error())
	}
	return OK(nil)
}

// CQGetEssenceMessageList 扩展API-获取精华消息列表
//
// https://docs.go-cqhttp.org/api/#%E8%8E%B7%E5%8F%96%E7%B2%BE%E5%8D%8E%E6%B6%88%E6%81%AF%E5%88%97%E8%A1%A8
// @route(get_essence_msg_list)
func (bot *CQBot) CQGetEssenceMessageList(groupID int64) global.MSG {
	g := bot.Client.GetCachedGroupInfo(uint32(groupID))
	if g == nil {
		return Failed(100, "GROUP_NOT_FOUND", "群聊不存在")
	}
	msgList, err := bot.Client.FetchEssenceMessage(uint32(groupID))
	if err != nil {
		return Failed(100, "GET_ESSENCE_LIST_FOUND", err.Error())
	}
	list := make([]global.MSG, 0, len(msgList))
	for _, m := range msgList {
		msg := global.MSG{
			"sender_nick":   m.Message.Sender.Nickname,
			"sender_time":   m.Message.Time,
			"operator_time": m.OperatorTime,
			"operator_nick": "",
			"sender_id":     m.Message.Sender.Nickname,
			"operator_id":   m.OperatorUin,
		}
		if operator := bot.Client.GetCachedMemberInfo(m.OperatorUin, uint32(groupID)); operator != nil {
			msg["operator_nick"] = operator.DisplayName()
		}
		msg["message_id"] = db.ToGlobalID(groupID, int32(m.Message.ID))
		list = append(list, msg)
	}
	return OK(list)
}

// CQCheckURLSafely 扩展API-检查链接安全性
//
// https://docs.go-cqhttp.org/api/#%E6%A3%80%E6%9F%A5%E9%93%BE%E6%8E%A5%E5%AE%89%E5%85%A8%E6%80%A7
// @route(check_url_safely)
func (bot *CQBot) CQCheckURLSafely(url string) global.MSG {
	level, _ := bot.Client.CheckURLSafely(url)
	return OK(global.MSG{
		"level": level,
	})
}

// CQGetVersionInfo 获取版本信息
//
// https://git.io/JtwUs
// @route11(get_version_info)
func (bot *CQBot) CQGetVersionInfo() global.MSG {
	wd, _ := os.Getwd()
	return OK(global.MSG{
		"app_name":                   "go-cqhttp",
		"app_version":                base.Version,
		"app_full_name":              fmt.Sprintf("go-cqhttp-%s_%s_%s-%s", base.Version, runtime.GOOS, runtime.GOARCH, runtime.Version()),
		"protocol_version":           "v11",
		"coolq_directory":            wd,
		"coolq_edition":              "pro",
		"go_cqhttp":                  true,
		"plugin_version":             "4.15.0",
		"plugin_build_number":        99,
		"plugin_build_configuration": "release",
		"runtime_version":            runtime.Version(),
		"runtime_os":                 runtime.GOOS,
		"version":                    base.Version,
		"protocol_name":              "nt",
		// "protocol_name":              bot.Client.Device().Protocol,
	})
}

// CQGetModelShow 获取在线机型
//
// https://club.vip.qq.com/onlinestatus/set
// @route(_get_model_show)
// func (bot *CQBot) CQGetModelShow(model string) global.MSG {
//	variants, err := bot.Client.GetModelShow(model)
//	if err != nil {
//		return Failed(100, "GET_MODEL_SHOW_API_ERROR", "无法获取在线机型")
//	}
//	a := make([]global.MSG, 0, len(variants))
//	for _, v := range variants {
//		a = append(a, global.MSG{
//			"model_show": v.ModelShow,
//			"need_pay":   v.NeedPay,
//		})
//	}
//	return OK(global.MSG{
//		"variants": a,
//	})
//}

// CQSendGroupSign 群打卡
//
// https://club.vip.qq.com/onlinestatus/set
// @route(send_group_sign)
func (bot *CQBot) CQSendGroupSign(groupID int64) global.MSG {
	_, err := bot.Client.SendGroupSign(uint32(groupID))
	if err == nil {
		return OK(nil)
	}
	return Failed(100, "SEND_GROUP_SIGN_ERROR", err.Error())
}

// CQSetModelShow 设置在线机型
//
// https://club.vip.qq.com/onlinestatus/set
// @route(_set_model_show)
// func (bot *CQBot) CQSetModelShow(model, modelShow string) global.MSG {
//	err := bot.Client.SetModelShow(model, modelShow)
//	if err != nil {
//		return Failed(100, "SET_MODEL_SHOW_API_ERROR", "无法设置在线机型")
//	}
//	return OK(nil)
//}

// CQMarkMessageAsRead 标记消息已读
// @route(mark_msg_as_read)
// @rename(msg_id->message_id)
func (bot *CQBot) CQMarkMessageAsRead(msgID int32) global.MSG {
	m, err := db.GetMessageByGlobalID(msgID)
	if err != nil {
		return Failed(100, "MSG_NOT_FOUND", "消息不存在")
	}
	switch o := m.(type) {
	case *db.StoredGroupMessage:
		if bot.Client.MarkGroupMessageReaded(uint32(o.GroupCode), uint32(o.Attribute.MessageSeq)) != nil {
			return Failed(100, "ACTION_FAILED", "标记群消息已读失败")
		}
	case *db.StoredPrivateMessage:
		if bot.Client.MarkPrivateMessageReaded(uint32(o.SessionUin), uint32(o.Attribute.Timestamp), uint32(o.Attribute.MessageSeq)) != nil {
			return Failed(100, "ACTION_FAILED", "标记私聊消息已读失败")
		}
	}
	return OK(nil)
}

// CQSetQQProfile 设置 QQ 资料
//
// @route(set_qq_profile)
// func (bot *CQBot) CQSetQQProfile(nickname, company, email, college, personalNote gjson.Result) global.MSG {
//	u := client.NewProfileDetailUpdate()
//
//	fi := func(f gjson.Result, do func(value string) client.ProfileDetailUpdate) {
//		if f.Exists() {
//			do(f.String())
//		}
//	}
//
//	fi(nickname, u.Nick)
//	fi(company, u.Company)
//	fi(email, u.Email)
//	fi(college, u.College)
//	fi(personalNote, u.PersonalNote)
//	bot.Client.UpdateProfile(u)
//	return OK(nil)
//}

// CQReloadEventFilter 重载事件过滤器
//
// @route(reload_event_filter)
func (bot *CQBot) CQReloadEventFilter(file string) global.MSG {
	filter.Add(file)
	return OK(nil)
}

// CQGetSupportedActions 获取支持的动作列表
//
// @route(get_supported_actions)
func (bot *CQBot) CQGetSupportedActions(spec *onebot.Spec) global.MSG {
	return OK(spec.SupportedActions)
}

// OK 生成成功返回值
func OK(data any) global.MSG {
	return global.MSG{"data": data, "retcode": 0, "status": "ok", "message": ""}
}

// Failed 生成失败返回值
func Failed(code int, msg ...string) global.MSG {
	m, w := "", ""
	if len(msg) > 0 {
		m = msg[0]
	}
	if len(msg) > 1 {
		w = msg[1]
	}
	return global.MSG{"data": nil, "retcode": code, "msg": m, "wording": w, "message": w, "status": "failed"}
}

func limitedString(str string) string {
	limited := [14]rune{10: ' ', 11: '.', 12: '.', 13: '.'}
	i := 0
	for _, r := range str {
		if i >= 10 {
			break
		}
		limited[i] = r
		i++
	}
	if i != 10 {
		return str
	}
	return string(limited[:])
}
