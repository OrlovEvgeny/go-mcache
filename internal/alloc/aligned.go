// Package alloc provides aligned memory allocation utilities for SIMD operations.
package alloc

import (
	"unsafe"
)

const (
	// CacheLineSize is the typical CPU cache line size in bytes.
	CacheLineSize = 64

	// AVX2Alignment is the required alignment for AVX2 operations.
	AVX2Alignment = 32

	// AVX512Alignment is the required alignment for AVX-512 operations.
	AVX512Alignment = 64
)

// AllocAligned32 allocates a byte slice aligned to 32-byte boundary (AVX2).
// The returned slice is guaranteed to have its first element aligned.
func AllocAligned32(size int) []byte {
	return allocAligned(size, AVX2Alignment)
}

// AllocAligned64 allocates a byte slice aligned to 64-byte boundary (cache line/AVX-512).
// The returned slice is guaranteed to have its first element aligned.
func AllocAligned64(size int) []byte {
	return allocAligned(size, CacheLineSize)
}

// AllocAlignedUint32 allocates a uint32 slice aligned to 32-byte boundary.
func AllocAlignedUint32(count int) []uint32 {
	bytes := allocAligned(count*4, AVX2Alignment)
	return unsafe.Slice((*uint32)(unsafe.Pointer(&bytes[0])), count)
}

// AllocAlignedUint64 allocates a uint64 slice aligned to 64-byte boundary.
func AllocAlignedUint64(count int) []uint64 {
	bytes := allocAligned(count*8, CacheLineSize)
	return unsafe.Slice((*uint64)(unsafe.Pointer(&bytes[0])), count)
}

// allocAligned allocates memory with the specified alignment.
// This works by over-allocating and returning a slice starting at an aligned offset.
func allocAligned(size, alignment int) []byte {
	if size <= 0 {
		return nil
	}

	// Allocate extra space for alignment
	raw := make([]byte, size+alignment-1)

	// Find the aligned offset
	addr := uintptr(unsafe.Pointer(&raw[0]))
	alignedAddr := (addr + uintptr(alignment-1)) &^ uintptr(alignment-1)
	offset := int(alignedAddr - addr)

	// Return aligned slice
	// Note: This slice shares underlying array with raw, so raw won't be GC'd
	// until the returned slice is no longer referenced
	return raw[offset : offset+size]
}

// IsAligned checks if a pointer is aligned to the given boundary.
func IsAligned(ptr unsafe.Pointer, alignment uintptr) bool {
	return uintptr(ptr)&(alignment-1) == 0
}

// IsAligned32 checks if a pointer is 32-byte aligned.
func IsAligned32(ptr unsafe.Pointer) bool {
	return IsAligned(ptr, AVX2Alignment)
}

// IsAligned64 checks if a pointer is 64-byte aligned.
func IsAligned64(ptr unsafe.Pointer) bool {
	return IsAligned(ptr, CacheLineSize)
}

// PadToCacheLine pads a size to the next cache line boundary.
func PadToCacheLine(size int) int {
	return (size + CacheLineSize - 1) &^ (CacheLineSize - 1)
}

// Pad represents a cache line padding type.
// Use in structs to prevent false sharing.
type Pad [CacheLineSize]byte

// AlignedBuffer is a buffer with guaranteed alignment.
type AlignedBuffer struct {
	data      []byte
	alignment int
}

// NewAlignedBuffer creates a new aligned buffer.
func NewAlignedBuffer(size, alignment int) *AlignedBuffer {
	return &AlignedBuffer{
		data:      allocAligned(size, alignment),
		alignment: alignment,
	}
}

// Bytes returns the underlying byte slice.
func (b *AlignedBuffer) Bytes() []byte {
	return b.data
}

// Ptr returns an unsafe pointer to the first element.
func (b *AlignedBuffer) Ptr() unsafe.Pointer {
	if len(b.data) == 0 {
		return nil
	}
	return unsafe.Pointer(&b.data[0])
}

// Len returns the buffer length.
func (b *AlignedBuffer) Len() int {
	return len(b.data)
}

// AsUint32Slice reinterprets the buffer as a uint32 slice.
func (b *AlignedBuffer) AsUint32Slice() []uint32 {
	if len(b.data) < 4 {
		return nil
	}
	count := len(b.data) / 4
	return unsafe.Slice((*uint32)(unsafe.Pointer(&b.data[0])), count)
}

// AsUint64Slice reinterprets the buffer as a uint64 slice.
func (b *AlignedBuffer) AsUint64Slice() []uint64 {
	if len(b.data) < 8 {
		return nil
	}
	count := len(b.data) / 8
	return unsafe.Slice((*uint64)(unsafe.Pointer(&b.data[0])), count)
}
