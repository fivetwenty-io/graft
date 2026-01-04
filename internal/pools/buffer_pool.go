// Package pools provides memory pooling utilities for graft to reduce allocations.
// It includes pools for bytes.Buffer, string slices, and a string interner for deduplication.
package pools

import (
	"bytes"
	"sync"
)

const (
	// DefaultMaxBufferSize is the maximum buffer capacity that will be returned to the pool.
	// Buffers larger than this are discarded to prevent memory bloat.
	DefaultMaxBufferSize = 64 * 1024 // 64KB

	// defaultBufferInitialSize is the initial capacity for new buffers.
	defaultBufferInitialSize = 512
)

// BufferPool manages a pool of reusable bytes.Buffer instances.
// It uses sync.Pool internally for efficient allocation and reuse.
type BufferPool struct {
	pool    sync.Pool
	maxSize int
}

// NewBufferPool creates a new BufferPool with the specified maximum buffer size.
// Buffers exceeding this size will be discarded rather than returned to the pool.
func NewBufferPool(maxSize int) *BufferPool {
	if maxSize <= 0 {
		maxSize = DefaultMaxBufferSize
	}

	bp := &BufferPool{
		maxSize: maxSize,
	}

	bp.pool = sync.Pool{
		New: func() interface{} {
			return bytes.NewBuffer(make([]byte, 0, defaultBufferInitialSize))
		},
	}

	return bp
}

// Get retrieves a buffer from the pool, reset and ready for use.
// The returned buffer is guaranteed to be empty (length 0).
func (bp *BufferPool) Get() *bytes.Buffer {
	buf, ok := bp.pool.Get().(*bytes.Buffer)
	if !ok {
		return bytes.NewBuffer(make([]byte, 0, bp.maxSize))
	}
	buf.Reset()
	return buf
}

// Put returns a buffer to the pool for reuse.
// Buffers with capacity exceeding the maximum size are discarded.
// It is safe to call Put with a nil buffer.
func (bp *BufferPool) Put(buf *bytes.Buffer) {
	if buf == nil {
		return
	}

	// Only return buffers within the size limit to avoid memory bloat
	if buf.Cap() <= bp.maxSize {
		bp.pool.Put(buf)
	}
	// Oversized buffers are left for garbage collection
}

// MaxSize returns the maximum buffer capacity that will be pooled.
func (bp *BufferPool) MaxSize() int {
	return bp.maxSize
}

// Buffers is the global buffer pool instance.
// It uses the default maximum buffer size of 64KB.
var Buffers = NewBufferPool(DefaultMaxBufferSize)

// GetBuffer retrieves a buffer from the global pool.
// This is a convenience function equivalent to Buffers.Get().
func GetBuffer() *bytes.Buffer {
	return Buffers.Get()
}

// PutBuffer returns a buffer to the global pool.
// This is a convenience function equivalent to Buffers.Put(buf).
func PutBuffer(buf *bytes.Buffer) {
	Buffers.Put(buf)
}
