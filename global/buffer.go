package global

import (
	"bytes"
	"sync"
)

var bufferPool = sync.Pool{New: func() interface{} { return &bytes.Buffer{} }}

// NewBuffer 从池中获取新 bytes.Buffer
func NewBuffer() *bytes.Buffer {
	return bufferPool.Get().(*bytes.Buffer)
}

// PutBuffer 将 Buffer放入池中
func PutBuffer(buf *bytes.Buffer) {
	buf.Reset()
	bufferPool.Put(buf)
}
