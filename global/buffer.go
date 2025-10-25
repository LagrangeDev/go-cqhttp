package global

import (
	"bytes"

	"github.com/LagrangeDev/LagrangeGo/utils/binary"
	"github.com/RomiChan/syncx"
)

var bufferTable syncx.Map[*bytes.Buffer, *binary.Builder]

// NewBuffer 从池中获取新 bytes.Buffer
func NewBuffer() *bytes.Buffer {
	builder := binary.NewBuilder()
	bufferTable.Store(builder.Buffer(), builder)
	return builder.Buffer()
}

// PutBuffer 将 Buffer放入池中
func PutBuffer(buf *bytes.Buffer) {
	if v, ok := bufferTable.LoadAndDelete(buf); ok {
		binary.PutBuilder(v)
	}
}
