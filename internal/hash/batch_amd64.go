//go:build amd64

package hash

import (
	"unsafe"
)

// HashStringBatch computes hashes for multiple strings.
// Uses optimized memory access patterns for better cache utilization.
func HashStringBatch(keys []string, hashes []uint64) {
	n := len(keys)
	if n > len(hashes) {
		n = len(hashes)
	}

	// Process in groups for better instruction pipelining
	i := 0

	// Main loop: process 4 strings at a time
	for ; i+4 <= n; i += 4 {
		hashes[i] = String(keys[i])
		hashes[i+1] = String(keys[i+1])
		hashes[i+2] = String(keys[i+2])
		hashes[i+3] = String(keys[i+3])
	}

	// Handle remaining
	for ; i < n; i++ {
		hashes[i] = String(keys[i])
	}
}

// HashInt64Batch computes hashes for multiple int64 values.
// Uses splitmix64 algorithm with SIMD-friendly unrolling.
func HashInt64Batch(keys []int64, hashes []uint64) {
	n := len(keys)
	if n > len(hashes) {
		n = len(hashes)
	}

	// Process 4 at a time for instruction-level parallelism
	i := 0
	for ; i+4 <= n; i += 4 {
		hashes[i] = Int64(keys[i])
		hashes[i+1] = Int64(keys[i+1])
		hashes[i+2] = Int64(keys[i+2])
		hashes[i+3] = Int64(keys[i+3])
	}

	for ; i < n; i++ {
		hashes[i] = Int64(keys[i])
	}
}

// HashUint64Batch computes hashes for multiple uint64 values.
func HashUint64Batch(keys []uint64, hashes []uint64) {
	n := len(keys)
	if n > len(hashes) {
		n = len(hashes)
	}

	i := 0
	for ; i+4 <= n; i += 4 {
		hashes[i] = Uint64(keys[i])
		hashes[i+1] = Uint64(keys[i+1])
		hashes[i+2] = Uint64(keys[i+2])
		hashes[i+3] = Uint64(keys[i+3])
	}

	for ; i < n; i++ {
		hashes[i] = Uint64(keys[i])
	}
}

// HashBytesBatch computes hashes for multiple byte slices.
func HashBytesBatch(keys [][]byte, hashes []uint64) {
	n := len(keys)
	if n > len(hashes) {
		n = len(hashes)
	}

	for i := 0; i < n; i++ {
		hashes[i] = Bytes(keys[i])
	}
}

// MixBatch applies splitmix64 mixing to multiple values in place.
// This is useful for converting raw keys to well-distributed hashes.
func MixBatch(values []uint64) {
	n := len(values)

	i := 0
	for ; i+4 <= n; i += 4 {
		values[i] = mix64(values[i])
		values[i+1] = mix64(values[i+1])
		values[i+2] = mix64(values[i+2])
		values[i+3] = mix64(values[i+3])
	}

	for ; i < n; i++ {
		values[i] = mix64(values[i])
	}
}

// mix64 is the splitmix64 finalizer.
func mix64(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

// ShardIndices computes shard indices for multiple hashes.
// shardMask should be (numShards - 1) where numShards is a power of 2.
func ShardIndices(hashes []uint64, shardMask uint64, indices []uint64) {
	n := len(hashes)
	if n > len(indices) {
		n = len(indices)
	}

	i := 0
	for ; i+4 <= n; i += 4 {
		indices[i] = hashes[i] & shardMask
		indices[i+1] = hashes[i+1] & shardMask
		indices[i+2] = hashes[i+2] & shardMask
		indices[i+3] = hashes[i+3] & shardMask
	}

	for ; i < n; i++ {
		indices[i] = hashes[i] & shardMask
	}
}

// HashInterfaceBatch computes hashes for a batch of interface values.
// Falls back to individual hashing for each element.
func HashInterfaceBatch[K comparable](keys []K, hashes []uint64, hasher func(K) uint64) {
	n := len(keys)
	if n > len(hashes) {
		n = len(hashes)
	}

	for i := 0; i < n; i++ {
		hashes[i] = hasher(keys[i])
	}
}

// Precomputed FNV constants for batch processing
const (
	fnvOffset64Batch = offset64
	fnvPrime64Batch  = prime64
)

// StringLengthBatch returns the total length of strings in a batch.
// Useful for pre-allocating buffers.
func StringLengthBatch(keys []string) int {
	total := 0
	for _, k := range keys {
		total += len(k)
	}
	return total
}

// HashStringFastBatch uses the fast string hash for longer strings.
func HashStringFastBatch(keys []string, hashes []uint64) {
	n := len(keys)
	if n > len(hashes) {
		n = len(hashes)
	}

	for i := 0; i < n; i++ {
		hashes[i] = StringFast(keys[i])
	}
}

// HashPtr computes a hash from a pointer value.
// Useful for hashing arbitrary pointer types.
func HashPtr(p unsafe.Pointer) uint64 {
	return Uint64(uint64(uintptr(p)))
}
