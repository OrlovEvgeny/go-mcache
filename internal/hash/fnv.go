// Package hash provides optimized hash functions for cache keys.
package hash

import (
	"math/bits"
	"unsafe"
)

const (
	// FNV-1a constants
	offset64 = 14695981039346656037
	prime64  = 1099511628211
)

// String computes FNV-1a hash of a string without allocations.
func String(s string) uint64 {
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

// Bytes computes FNV-1a hash of a byte slice.
func Bytes(b []byte) uint64 {
	h := uint64(offset64)
	for i := 0; i < len(b); i++ {
		h ^= uint64(b[i])
		h *= prime64
	}
	return h
}

// Int64 computes a hash for an int64 key.
func Int64(k int64) uint64 {
	// Use splitmix64 algorithm for good distribution
	x := uint64(k)
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

// Uint64 computes a hash for a uint64 key.
func Uint64(k uint64) uint64 {
	// Use splitmix64 algorithm for good distribution
	x := k
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

// Int computes a hash for an int key.
func Int(k int) uint64 {
	return Int64(int64(k))
}

// Uint computes a hash for a uint key.
func Uint(k uint) uint64 {
	return Uint64(uint64(k))
}

// Int32 computes a hash for an int32 key.
func Int32(k int32) uint64 {
	return Int64(int64(k))
}

// Uint32 computes a hash for a uint32 key.
func Uint32(k uint32) uint64 {
	return Uint64(uint64(k))
}

// Pointer computes a hash from a pointer value.
func Pointer(p unsafe.Pointer) uint64 {
	return Uint64(uint64(uintptr(p)))
}

// Combine combines two hashes into one.
func Combine(h1, h2 uint64) uint64 {
	// Use a variant of boost::hash_combine
	h1 ^= h2 + 0x9e3779b97f4a7c15 + (h1 << 12) + (h1 >> 4)
	return h1
}

// StringFast computes a fast hash for longer strings using parallel processing.
// Falls back to regular FNV-1a for short strings.
func StringFast(s string) uint64 {
	if len(s) < 32 {
		return String(s)
	}
	return stringFastLong(s)
}

// stringFastLong processes longer strings in 8-byte chunks.
func stringFastLong(s string) uint64 {
	h := uint64(offset64)

	// Process 8 bytes at a time
	for len(s) >= 8 {
		// Read 8 bytes as uint64 (little-endian)
		k := *(*uint64)(unsafe.Pointer(unsafe.StringData(s)))
		h ^= k
		h *= prime64
		h = bits.RotateLeft64(h, 31)
		s = s[8:]
	}

	// Process remaining bytes
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}

	return h
}

// BytesFast computes a fast hash for longer byte slices.
func BytesFast(b []byte) uint64 {
	if len(b) < 32 {
		return Bytes(b)
	}

	h := uint64(offset64)

	// Process 8 bytes at a time
	for len(b) >= 8 {
		k := *(*uint64)(unsafe.Pointer(&b[0]))
		h ^= k
		h *= prime64
		h = bits.RotateLeft64(h, 31)
		b = b[8:]
	}

	// Process remaining bytes
	for i := 0; i < len(b); i++ {
		h ^= uint64(b[i])
		h *= prime64
	}

	return h
}
