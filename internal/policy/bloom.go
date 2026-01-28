package policy

import (
	"sync"
)

// bloomFilter is a simple bloom filter used as a doorkeeper for TinyLFU.
// It prevents incrementing counters for items that haven't been seen before.
type bloomFilter struct {
	bits    []uint64
	numBits uint64
	numHash int
	mu      sync.RWMutex
}

// newBloomFilter creates a new bloom filter with approximately n items capacity
// and the given false positive rate.
func newBloomFilter(n int64, fpRate float64) *bloomFilter {
	if n <= 0 {
		n = 10000
	}
	if fpRate <= 0 || fpRate >= 1 {
		fpRate = 0.01
	}

	// Calculate optimal number of bits: m = -n*ln(p) / (ln(2)^2)
	// Simplified: m ≈ n * 10 for 1% false positive
	numBits := uint64(float64(n) * 10)
	if numBits < 64 {
		numBits = 64
	}

	// Round up to multiple of 64
	numBits = ((numBits + 63) / 64) * 64

	// Calculate optimal number of hash functions: k = (m/n) * ln(2)
	// Simplified: k ≈ 7 for 1% false positive
	numHash := 7

	return &bloomFilter{
		bits:    make([]uint64, numBits/64),
		numBits: numBits,
		numHash: numHash,
	}
}

// Add adds a key hash to the filter.
// Returns true if the key was already present (may be false positive).
func (bf *bloomFilter) Add(keyHash uint64) bool {
	bf.mu.Lock()
	defer bf.mu.Unlock()

	alreadyPresent := true
	for i := 0; i < bf.numHash; i++ {
		idx := bf.index(keyHash, i)
		wordIdx := idx / 64
		bitIdx := idx % 64
		mask := uint64(1) << bitIdx

		if bf.bits[wordIdx]&mask == 0 {
			alreadyPresent = false
			bf.bits[wordIdx] |= mask
		}
	}
	return alreadyPresent
}

// Contains checks if a key hash might be in the filter.
func (bf *bloomFilter) Contains(keyHash uint64) bool {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	for i := 0; i < bf.numHash; i++ {
		idx := bf.index(keyHash, i)
		wordIdx := idx / 64
		bitIdx := idx % 64
		mask := uint64(1) << bitIdx

		if bf.bits[wordIdx]&mask == 0 {
			return false
		}
	}
	return true
}

// index computes the bit index for hash function i.
func (bf *bloomFilter) index(keyHash uint64, i int) uint64 {
	// Use double hashing: h(i) = h1 + i*h2
	h1 := keyHash
	h2 := (keyHash >> 32) | (keyHash << 32)
	h := h1 + uint64(i)*h2
	return h % bf.numBits
}

// Reset clears the bloom filter.
func (bf *bloomFilter) Reset() {
	bf.mu.Lock()
	defer bf.mu.Unlock()

	for i := range bf.bits {
		bf.bits[i] = 0
	}
}

// FillRatio returns the ratio of set bits to total bits.
func (bf *bloomFilter) FillRatio() float64 {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	count := 0
	for _, word := range bf.bits {
		count += popcount(word)
	}
	return float64(count) / float64(bf.numBits)
}

// popcount returns the number of set bits in x.
func popcount(x uint64) int {
	// SWAR algorithm
	x = x - ((x >> 1) & 0x5555555555555555)
	x = (x & 0x3333333333333333) + ((x >> 2) & 0x3333333333333333)
	x = (x + (x >> 4)) & 0x0f0f0f0f0f0f0f0f
	x = x + (x >> 8)
	x = x + (x >> 16)
	x = x + (x >> 32)
	return int(x & 0x7f)
}
