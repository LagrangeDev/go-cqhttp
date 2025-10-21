package leveldb

import (
	"path"

	"github.com/LagrangeDev/LagrangeGo/utils/binary"
	lgrio "github.com/LagrangeDev/LagrangeGo/utils/io"
	"github.com/pkg/errors"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"gopkg.in/yaml.v3"

	"github.com/Mrs4s/go-cqhttp/db"
)

type database struct {
	db *leveldb.DB
}

// config leveldb 相关配置
type config struct {
	Enable bool `yaml:"enable"`
}

func init() {
	db.Register("leveldb", func(node yaml.Node) db.Database {
		conf := new(config)
		_ = node.Decode(conf)
		if !conf.Enable {
			return nil
		}
		return &database{}
	})
}

func (ldb *database) Open() error {
	p := path.Join("data", "leveldb-v3")
	d, err := leveldb.OpenFile(p, &opt.Options{
		WriteBuffer: 32 * opt.KiB,
	})
	if err != nil {
		return errors.Wrap(err, "open leveldb error")
	}
	ldb.db = d
	return nil
}

func (ldb *database) GetMessageByGlobalID(id int32) (_ db.StoredMessage, err error) {
	v, err := ldb.db.Get(binary.NewBuilder().WriteI32(id).ToBytes(), nil)
	if err != nil || len(v) == 0 {
		return nil, errors.Wrap(err, "get value error")
	}
	defer func() {
		if r := recover(); r != nil {
			err = errors.Errorf("%v", r)
		}
	}()
	r, err := newReader(lgrio.B2S(v))
	if err != nil {
		return nil, err
	}
	switch r.uvarint() {
	case group:
		return r.readStoredGroupMessage(), nil
	case private:
		return r.readStoredPrivateMessage(), nil
	default:
		return nil, errors.New("unknown message flag")
	}
}

func (ldb *database) GetGroupMessageByGlobalID(id int32) (*db.StoredGroupMessage, error) {
	i, err := ldb.GetMessageByGlobalID(id)
	if err != nil {
		return nil, err
	}
	g, ok := i.(*db.StoredGroupMessage)
	if !ok {
		return nil, errors.New("message type error")
	}
	return g, nil
}

func (ldb *database) GetPrivateMessageByGlobalID(id int32) (*db.StoredPrivateMessage, error) {
	i, err := ldb.GetMessageByGlobalID(id)
	if err != nil {
		return nil, err
	}
	p, ok := i.(*db.StoredPrivateMessage)
	if !ok {
		return nil, errors.New("message type error")
	}
	return p, nil
}

func (ldb *database) InsertGroupMessage(msg *db.StoredGroupMessage) error {
	w := newWriter()
	w.uvarint(group)
	w.writeStoredGroupMessage(msg)
	err := ldb.db.Put(binary.NewBuilder().WriteI32(msg.GlobalID).ToBytes(), w.bytes(), nil)
	return errors.Wrap(err, "put data error")
}

func (ldb *database) InsertPrivateMessage(msg *db.StoredPrivateMessage) error {
	w := newWriter()
	w.uvarint(private)
	w.writeStoredPrivateMessage(msg)
	err := ldb.db.Put(binary.NewBuilder().WriteI32(msg.GlobalID).ToBytes(), w.bytes(), nil)
	return errors.Wrap(err, "put data error")
}
