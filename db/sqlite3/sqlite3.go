package sqlite3

import (
	"hash/crc64"
	"os"
	"path"
	"strconv"
	"sync"
	"time"

	sql "github.com/FloatTech/sqlite"
	"github.com/LagrangeDev/LagrangeGo/utils/binary"
	lgrio "github.com/LagrangeDev/LagrangeGo/utils/io"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"github.com/Mrs4s/go-cqhttp/db"
)

type database struct {
	sync.RWMutex
	db  *sql.Sqlite
	ttl time.Duration
}

// config mongodb 相关配置
type config struct {
	Enable   bool   `yaml:"enable"`
	CacheTTL string `yaml:"cachettl"`
}

func init() {
	sql.DriverName = "sqlite"
	db.Register("sqlite3", func(node yaml.Node) db.Database {
		conf := new(config)
		_ = node.Decode(conf)
		if !conf.Enable {
			return nil
		}
		duration, err := time.ParseDuration(conf.CacheTTL)
		if err != nil {
			log.Fatalf("illegal ttl config: %v", err)
		}
		return &database{db: new(sql.Sqlite), ttl: duration}
	})
}

func (s *database) Open() error {
	s.db.DBPath = path.Join("data", "sqlite3")
	_ = os.MkdirAll(s.db.DBPath, 0755)
	s.db.DBPath += "/msg.db"
	err := s.db.Open(s.ttl)
	if err != nil {
		return errors.Wrap(err, "open sqlite3 error")
	}
	_, err = s.db.DB.Exec("PRAGMA foreign_keys = ON;")
	if err != nil {
		return errors.Wrap(err, "enable foreign_keys error")
	}
	err = s.db.Create(Sqlite3UinInfoTableName, &UinInfo{})
	if err != nil {
		return errors.Wrap(err, "create sqlite3 table error")
	}
	err = s.db.Insert(Sqlite3UinInfoTableName, &UinInfo{Name: "null"})
	if err != nil {
		return errors.Wrap(err, "insert into sqlite3 table "+Sqlite3UinInfoTableName+" error")
	}
	err = s.db.Create(Sqlite3TinyInfoTableName, &TinyInfo{})
	if err != nil {
		return errors.Wrap(err, "create sqlite3 table error")
	}
	err = s.db.Insert(Sqlite3TinyInfoTableName, &TinyInfo{Name: "null"})
	if err != nil {
		return errors.Wrap(err, "insert into sqlite3 table "+Sqlite3TinyInfoTableName+" error")
	}
	err = s.db.Create(Sqlite3MessageAttributeTableName, &StoredMessageAttribute{},
		"FOREIGN KEY(SenderUin) REFERENCES "+Sqlite3UinInfoTableName+"(Uin)",
	)
	if err != nil {
		return errors.Wrap(err, "create sqlite3 table error")
	}
	err = s.db.Insert(Sqlite3MessageAttributeTableName, &StoredMessageAttribute{})
	if err != nil {
		return errors.Wrap(err, "insert into sqlite3 table "+Sqlite3MessageAttributeTableName+" error")
	}
	err = s.db.Create(Sqlite3QuotedInfoTableName, &QuotedInfo{})
	if err != nil {
		return errors.Wrap(err, "create sqlite3 table error")
	}
	err = s.db.Insert(Sqlite3QuotedInfoTableName, &QuotedInfo{QuotedContent: "null"})
	if err != nil {
		return errors.Wrap(err, "insert into sqlite3 table "+Sqlite3QuotedInfoTableName+" error")
	}
	err = s.db.Create(Sqlite3GroupMessageTableName, &StoredGroupMessage{},
		"FOREIGN KEY(AttributeID) REFERENCES "+Sqlite3MessageAttributeTableName+"(ID)",
		"FOREIGN KEY(QuotedInfoID) REFERENCES "+Sqlite3QuotedInfoTableName+"(ID)",
	)
	if err != nil {
		return errors.Wrap(err, "create sqlite3 table error")
	}
	err = s.db.Create(Sqlite3PrivateMessageTableName, &StoredPrivateMessage{},
		"FOREIGN KEY(AttributeID) REFERENCES "+Sqlite3MessageAttributeTableName+"(ID)",
		"FOREIGN KEY(QuotedInfoID) REFERENCES "+Sqlite3QuotedInfoTableName+"(ID)",
	)
	if err != nil {
		return errors.Wrap(err, "create sqlite3 table error")
	}
	return nil
}

func (s *database) GetMessageByGlobalID(id int32) (db.StoredMessage, error) {
	if r, err := s.GetGroupMessageByGlobalID(id); err == nil {
		return r, nil
	}
	return s.GetPrivateMessageByGlobalID(id)
}

func (s *database) GetGroupMessageByGlobalID(id int32) (*db.StoredGroupMessage, error) {
	var ret db.StoredGroupMessage
	var grpmsg StoredGroupMessage
	s.RLock()
	err := s.db.Find(Sqlite3GroupMessageTableName, &grpmsg, "WHERE GlobalID="+strconv.Itoa(int(id)))
	s.RUnlock()
	if err != nil {
		return nil, errors.Wrap(err, "query error")
	}
	ret.ID = grpmsg.ID
	ret.GlobalID = grpmsg.GlobalID
	ret.SubType = grpmsg.SubType
	ret.GroupCode = grpmsg.GroupCode
	ret.AnonymousID = grpmsg.AnonymousID
	_ = yaml.Unmarshal(lgrio.S2B(grpmsg.Content), &ret)
	if grpmsg.AttributeID != 0 {
		var attr StoredMessageAttribute
		s.RLock()
		err = s.db.Find(Sqlite3MessageAttributeTableName, &attr, "WHERE ID="+strconv.FormatInt(grpmsg.AttributeID, 10))
		s.RUnlock()
		if err == nil {
			var uin UinInfo
			s.RLock()
			err = s.db.Find(Sqlite3UinInfoTableName, &uin, "WHERE Uin="+strconv.FormatInt(attr.SenderUin, 10))
			s.RUnlock()
			if err == nil {
				ret.Attribute = &db.StoredMessageAttribute{
					MessageSeq: attr.MessageSeq,
					InternalID: attr.InternalID,
					SenderUin:  attr.SenderUin,
					SenderName: uin.Name,
					Timestamp:  attr.Timestamp,
				}
			}
		}
	}
	if grpmsg.QuotedInfoID != 0 {
		var quoinf QuotedInfo
		s.RLock()
		err = s.db.Find(Sqlite3QuotedInfoTableName, &quoinf, "WHERE ID="+strconv.FormatInt(grpmsg.QuotedInfoID, 10))
		s.RUnlock()
		if err == nil {
			ret.QuotedInfo = &db.QuotedInfo{
				PrevID:       quoinf.PrevID,
				PrevGlobalID: quoinf.PrevGlobalID,
			}
			_ = yaml.Unmarshal(lgrio.S2B(quoinf.QuotedContent), &ret.QuotedInfo)
		}
	}
	return &ret, nil
}

func (s *database) GetPrivateMessageByGlobalID(id int32) (*db.StoredPrivateMessage, error) {
	var ret db.StoredPrivateMessage
	var privmsg StoredPrivateMessage
	s.RLock()
	err := s.db.Find(Sqlite3PrivateMessageTableName, &privmsg, "WHERE GlobalID="+strconv.Itoa(int(id)))
	s.RUnlock()
	if err != nil {
		return nil, errors.Wrap(err, "query error")
	}
	ret.ID = privmsg.ID
	ret.GlobalID = privmsg.GlobalID
	ret.SubType = privmsg.SubType
	ret.SessionUin = privmsg.SessionUin
	ret.TargetUin = privmsg.TargetUin
	_ = yaml.Unmarshal(lgrio.S2B(privmsg.Content), &ret)
	if privmsg.AttributeID != 0 {
		var attr StoredMessageAttribute
		s.RLock()
		err = s.db.Find(Sqlite3MessageAttributeTableName, &attr, "WHERE ID="+strconv.FormatInt(privmsg.AttributeID, 10))
		s.RUnlock()
		if err == nil {
			var uin UinInfo
			s.RLock()
			err = s.db.Find(Sqlite3UinInfoTableName, &uin, "WHERE Uin="+strconv.FormatInt(attr.SenderUin, 10))
			s.RUnlock()
			if err == nil {
				ret.Attribute = &db.StoredMessageAttribute{
					MessageSeq: attr.MessageSeq,
					InternalID: attr.InternalID,
					SenderUin:  attr.SenderUin,
					SenderName: uin.Name,
					Timestamp:  attr.Timestamp,
				}
			}
		}
	}
	if privmsg.QuotedInfoID != 0 {
		var quoinf QuotedInfo
		s.RLock()
		err = s.db.Find(Sqlite3QuotedInfoTableName, &quoinf, "WHERE ID="+strconv.FormatInt(privmsg.QuotedInfoID, 10))
		s.RUnlock()
		if err == nil {
			ret.QuotedInfo = &db.QuotedInfo{
				PrevID:       quoinf.PrevID,
				PrevGlobalID: quoinf.PrevGlobalID,
			}
			_ = yaml.Unmarshal(lgrio.S2B(quoinf.QuotedContent), &ret.QuotedInfo)
		}
	}
	return &ret, nil
}

func (s *database) InsertGroupMessage(msg *db.StoredGroupMessage) error {
	grpmsg := &StoredGroupMessage{
		GlobalID:    msg.GlobalID,
		ID:          msg.ID,
		SubType:     msg.SubType,
		GroupCode:   msg.GroupCode,
		AnonymousID: msg.AnonymousID,
	}
	h := crc64.New(crc64.MakeTable(crc64.ISO))
	if msg.Attribute != nil {
		h.Write(binary.NewBuilder().
			WriteI32(msg.Attribute.MessageSeq).
			WriteI32(msg.Attribute.InternalID).
			WriteI64(msg.Attribute.SenderUin).
			WriteI64(msg.Attribute.Timestamp).
			ToBytes())
		h.Write(lgrio.S2B(msg.Attribute.SenderName))
		id := int64(h.Sum64())
		if id == 0 {
			id++
		}
		s.Lock()
		err := s.db.Insert(Sqlite3UinInfoTableName, &UinInfo{
			Uin:  msg.Attribute.SenderUin,
			Name: msg.Attribute.SenderName,
		})
		if err == nil {
			err = s.db.Insert(Sqlite3MessageAttributeTableName, &StoredMessageAttribute{
				ID:         id,
				MessageSeq: msg.Attribute.MessageSeq,
				InternalID: msg.Attribute.InternalID,
				SenderUin:  msg.Attribute.SenderUin,
				Timestamp:  msg.Attribute.Timestamp,
			})
		}
		s.Unlock()
		if err == nil {
			grpmsg.AttributeID = id
		}
		h.Reset()
	}
	if msg.QuotedInfo != nil {
		h.Write(lgrio.S2B(msg.QuotedInfo.PrevID))
		h.Write(binary.NewBuilder().WriteI32(msg.QuotedInfo.PrevGlobalID).ToBytes())
		content, err := yaml.Marshal(&msg.QuotedInfo)
		if err != nil {
			return errors.Wrap(err, "insert marshal QuotedContent error")
		}
		h.Write(content)
		id := int64(h.Sum64())
		if id == 0 {
			id++
		}
		s.Lock()
		err = s.db.Insert(Sqlite3QuotedInfoTableName, &QuotedInfo{
			ID:            id,
			PrevID:        msg.QuotedInfo.PrevID,
			PrevGlobalID:  msg.QuotedInfo.PrevGlobalID,
			QuotedContent: lgrio.B2S(content),
		})
		s.Unlock()
		if err == nil {
			grpmsg.QuotedInfoID = id
		}
	}
	content, err := yaml.Marshal(&msg)
	if err != nil {
		return errors.Wrap(err, "insert marshal Content error")
	}
	grpmsg.Content = lgrio.B2S(content)
	s.Lock()
	err = s.db.Insert(Sqlite3GroupMessageTableName, grpmsg)
	s.Unlock()
	if err != nil {
		return errors.Wrap(err, "insert error")
	}
	return nil
}

func (s *database) InsertPrivateMessage(msg *db.StoredPrivateMessage) error {
	privmsg := &StoredPrivateMessage{
		GlobalID:   msg.GlobalID,
		ID:         msg.ID,
		SubType:    msg.SubType,
		SessionUin: msg.SessionUin,
		TargetUin:  msg.TargetUin,
	}
	h := crc64.New(crc64.MakeTable(crc64.ISO))
	if msg.Attribute != nil {
		h.Write(binary.NewBuilder().
			WriteI32(msg.Attribute.MessageSeq).
			WriteI32(msg.Attribute.InternalID).
			WriteI64(msg.Attribute.SenderUin).
			WriteI64(msg.Attribute.Timestamp).
			ToBytes())
		h.Write(lgrio.S2B(msg.Attribute.SenderName))
		id := int64(h.Sum64())
		if id == 0 {
			id++
		}
		s.Lock()
		err := s.db.Insert(Sqlite3UinInfoTableName, &UinInfo{
			Uin:  msg.Attribute.SenderUin,
			Name: msg.Attribute.SenderName,
		})
		if err == nil {
			err = s.db.Insert(Sqlite3MessageAttributeTableName, &StoredMessageAttribute{
				ID:         id,
				MessageSeq: msg.Attribute.MessageSeq,
				InternalID: msg.Attribute.InternalID,
				SenderUin:  msg.Attribute.SenderUin,
				Timestamp:  msg.Attribute.Timestamp,
			})
		}
		s.Unlock()
		if err == nil {
			privmsg.AttributeID = id
		}
		h.Reset()
	}
	if msg.QuotedInfo != nil {
		h.Write(lgrio.S2B(msg.QuotedInfo.PrevID))
		h.Write(binary.NewBuilder().WriteI32(msg.QuotedInfo.PrevGlobalID).ToBytes())
		content, err := yaml.Marshal(&msg.QuotedInfo)
		if err != nil {
			return errors.Wrap(err, "insert marshal QuotedContent error")
		}
		h.Write(content)
		id := int64(h.Sum64())
		if id == 0 {
			id++
		}
		s.Lock()
		err = s.db.Insert(Sqlite3QuotedInfoTableName, &QuotedInfo{
			ID:            id,
			PrevID:        msg.QuotedInfo.PrevID,
			PrevGlobalID:  msg.QuotedInfo.PrevGlobalID,
			QuotedContent: lgrio.B2S(content),
		})
		s.Unlock()
		if err == nil {
			privmsg.QuotedInfoID = id
		}
	}
	content, err := yaml.Marshal(&msg)
	if err != nil {
		return errors.Wrap(err, "insert marshal Content error")
	}
	privmsg.Content = lgrio.B2S(content)
	s.Lock()
	err = s.db.Insert(Sqlite3PrivateMessageTableName, privmsg)
	s.Unlock()
	if err != nil {
		return errors.Wrap(err, "insert error")
	}
	return nil
}
