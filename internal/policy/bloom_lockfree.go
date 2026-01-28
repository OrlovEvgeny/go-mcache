package policy

import (
	"math"
	"math/bits"
	"sync/atomic"
)

// bloomFilterLockFree is a lock-free bloom filter used as a doorkeeper for TinyLFU.
// It prevents incrementing counters for items that haven't been seen before.
type bloomFilterLockFree struct {
	bits    []atomic.Uint64
	numBits uint64
	numHash int
}

// newBloomFilterLockFree creates a new lock-free bloom filter with approximately n items capacity
// and the given false positive rate.
func newBloomFilterLockFree(n int64, fpRate float64) *bloomFilterLockFree {
	if n <= 0 {
		n = 10000
	}
	if fpRate <= 0 || fpRate >= 1 {
		fpRate = 0.01
	}

	// Calculate optimal number of bits: m = -n*ln(p) / (ln(2)^2)
	ln2Sq := math.Ln2 * math.Ln2
	numBits := uint64(-float64(n) * math.Log(fpRate) / ln2Sq)
	if numBits < 64 {
		numBits = 64
	}

	// Round up to multiple of 64
	numBits = ((numBits + 63) / 64) * 64

	// Calculate optimal number of hash functions: k = (m/n) * ln(2)
	numHash := int(float64(numBits) / float64(n) * math.Ln2)
	if numHash < 1 {
		numHash = 1
	}
	if numHash > 16 {
		numHash = 16 // Cap at 16 for performance
	}

	return &bloomFilterLockFree{
		bits:    make([]atomic.Uint64, numBits/64),
		numBits: numBits,
		numHash: numHash,
	}
}

// Add adds a key hash to the filter using lock-free atomic operations.
// Returns true if the key was already present (may be false positive).
func (bf *bloomFilterLockFree) Add(keyHash uint64) bool {
	alreadyPresent := true

	for i := 0; i < bf.numHash; i++ {
		idx := bf.index(keyHash, i)
		wordIdx := idx / 64
		mask := uint64(1) << (idx % 64)

		for {
			old := bf.bits[wordIdx].Load()
			if old&mask == 0 {
				alreadyPresent = false
			}

			// If bit is already set, no need to CAS
			if old&mask != 0 {
				break
			}

			// Try to set the bit
			if bf.bits[wordIdx].CompareAndSwap(old, old|mask) {
				break
			}
			// CAS failed, retry
		}
	}

	return alreadyPresent
}

// Contains checks if a key hash might be in the filter.
// This is naturally lock-free as it only reads.
func (bf *bloomFilterLockFree) Contains(keyHash uint64) bool {
	for i := 0; i < bf.numHash; i++ {
		idx := bf.index(keyHash, i)
		wordIdx := idx / 64
		mask := uint64(1) << (idx % 64)

		if bf.bits[wordIdx].Load()&mask == 0 {
			return false
		}
	}
	return true
}

// index computes the bit index for hash function i.
// Uses double hashing: h(i) = h1 + i*h2 + i^2 (enhanced double hashing)
func (bf *bloomFilterLockFree) index(keyHash uint64, i int) uint64 {
	h1 := keyHash
	h2 := (keyHash >> 32) | (keyHash << 32)
	// Enhanced double hashing with quadratic term for better distribution
	h := h1 + uint64(i)*h2 + uint64(i*i)
	return h % bf.numBits
}

// Reset clears the bloom filter.
func (bf *bloomFilterLockFree) Reset() {
	for i := range bf.bits {
		bf.bits[i].Store(0)
	}
}

// FillRatio returns the ratio of set bits to total bits.
func (bf *bloomFilterLockFree) FillRatio() float64 {
	count := 0
	for i := range bf.bits {
		word := bf.bits[i].Load()
		count += bits.OnesCount64(word)
	}
	return float64(count) / float64(bf.numBits)
}

// EstimatedCount returns an estimate of the number of items in the filter.
// Uses the formula: n* = -(m/k) * ln(1 - X/m) where X is set bits count
func (bf *bloomFilterLockFree) EstimatedCount() int64 {
	setBits := 0
	for i := range bf.bits {
		word := bf.bits[i].Load()
		setBits += bits.OnesCount64(word)
	}

	if setBits == 0 {
		return 0
	}
	if setBits >= int(bf.numBits) {
		// Filter is full
		return int64(bf.numBits) / int64(bf.numHash)
	}

	ratio := float64(setBits) / float64(bf.numBits)
	estimate := -float64(bf.numBits) / float64(bf.numHash) * math.Log(1-ratio)
	return int64(estimate)
}

// AddBatch adds multiple key hashes to the filter.
// Returns the count of keys that were already present.
func (bf *bloomFilterLockFree) AddBatch(keyHashes []uint64) int {
	alreadyPresent := 0
	for _, keyHash := range keyHashes {
		if bf.Add(keyHash) {
			alreadyPresent++
		}
	}
	return alreadyPresent
}

// ContainsBatch checks multiple keys and stores results in the provided slice.
func (bf *bloomFilterLockFree) ContainsBatch(keyHashes []uint64, results []bool) {
	for i, keyHash := range keyHashes {
		if i < len(results) {
			results[i] = bf.Contains(keyHash)
		}
	}
}
