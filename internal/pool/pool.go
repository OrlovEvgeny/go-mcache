// Package pool provides sync.Pool utilities for reducing allocations.
package pool

import "sync"

// BufferPool is a pool of byte buffers.
var BufferPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 0, 4096)
		return &b
	},
}

// GetBuffer retrieves a byte buffer from the pool.
func GetBuffer() *[]byte {
	return BufferPool.Get().(*[]byte)
}

// PutBuffer returns a byte buffer to the pool.
func PutBuffer(b *[]byte) {
	if b == nil {
		return
	}
	// Reset the buffer
	*b = (*b)[:0]
	BufferPool.Put(b)
}

// SmallBufferPool is a pool of smaller byte buffers (256 bytes).
var SmallBufferPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 0, 256)
		return &b
	},
}

// GetSmallBuffer retrieves a small byte buffer from the pool.
func GetSmallBuffer() *[]byte {
	return SmallBufferPool.Get().(*[]byte)
}

// PutSmallBuffer returns a small byte buffer to the pool.
func PutSmallBuffer(b *[]byte) {
	if b == nil {
		return
	}
	*b = (*b)[:0]
	SmallBufferPool.Put(b)
}

// StringSlicePool is a pool of string slices.
var StringSlicePool = sync.Pool{
	New: func() interface{} {
		s := make([]string, 0, 64)
		return &s
	},
}

// GetStringSlice retrieves a string slice from the pool.
func GetStringSlice() *[]string {
	return StringSlicePool.Get().(*[]string)
}

// PutStringSlice returns a string slice to the pool.
func PutStringSlice(s *[]string) {
	if s == nil {
		return
	}
	*s = (*s)[:0]
	StringSlicePool.Put(s)
}

// Uint64SlicePool is a pool of uint64 slices.
var Uint64SlicePool = sync.Pool{
	New: func() interface{} {
		s := make([]uint64, 0, 64)
		return &s
	},
}

// GetUint64Slice retrieves a uint64 slice from the pool.
func GetUint64Slice() *[]uint64 {
	return Uint64SlicePool.Get().(*[]uint64)
}

// PutUint64Slice returns a uint64 slice to the pool.
func PutUint64Slice(s *[]uint64) {
	if s == nil {
		return
	}
	*s = (*s)[:0]
	Uint64SlicePool.Put(s)
}
