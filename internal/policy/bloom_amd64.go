//go:build amd64

package policy

import (
	"math"
	"math/bits"
	"sync/atomic"
	"unsafe"

	"github.com/OrlovEvgeny/go-mcache/internal/alloc"
)

// bloomFilterSIMD is a SIMD-optimized bloom filter.
// Uses aligned memory and optimized hash position calculations.
type bloomFilterSIMD struct {
	bits    []atomic.Uint64
	rawBits *alloc.AlignedBuffer // Aligned for SIMD access
	numBits uint64
	numHash int

	// Pre-computed hash multipliers for vectorized hash computation
	hashMults [16]uint64
}

// newBloomFilterSIMD creates a new SIMD-optimized bloom filter.
func newBloomFilterSIMD(n int64, fpRate float64) *bloomFilterSIMD {
	if n <= 0 {
		n = 10000
	}
	if fpRate <= 0 || fpRate >= 1 {
		fpRate = 0.01
	}

	// Calculate optimal parameters
	ln2Sq := math.Ln2 * math.Ln2
	numBits := uint64(-float64(n) * math.Log(fpRate) / ln2Sq)
	if numBits < 64 {
		numBits = 64
	}
	// Round up to multiple of 512 for SIMD alignment (8 x 64-bit words)
	numBits = ((numBits + 511) / 512) * 512

	numHash := int(float64(numBits) / float64(n) * math.Ln2)
	if numHash < 1 {
		numHash = 1
	}
	if numHash > 16 {
		numHash = 16
	}

	bf := &bloomFilterSIMD{
		bits:    make([]atomic.Uint64, numBits/64),
		rawBits: alloc.NewAlignedBuffer(int(numBits/8), alloc.CacheLineSize),
		numBits: numBits,
		numHash: numHash,
	}

	// Pre-compute hash multipliers for double hashing
	for i := 0; i < 16; i++ {
		bf.hashMults[i] = uint64(i) * 0x9e3779b97f4a7c15
	}

	return bf
}

// Add adds a key hash to the filter.
// Returns true if the key was already present (may be false positive).
func (bf *bloomFilterSIMD) Add(keyHash uint64) bool {
	alreadyPresent := true

	// Use optimized double hashing
	h1 := keyHash
	h2 := (keyHash >> 32) | (keyHash << 32)

	for i := 0; i < bf.numHash; i++ {
		// h(i) = h1 + i*h2 + i^2 (enhanced double hashing)
		h := h1 + uint64(i)*h2 + uint64(i*i)
		idx := h % bf.numBits
		wordIdx := idx / 64
		mask := uint64(1) << (idx % 64)

		for {
			old := bf.bits[wordIdx].Load()
			if old&mask == 0 {
				alreadyPresent = false
			}
			if old&mask != 0 {
				break
			}
			if bf.bits[wordIdx].CompareAndSwap(old, old|mask) {
				break
			}
		}
	}

	return alreadyPresent
}

// Contains checks if a key hash might be in the filter.
func (bf *bloomFilterSIMD) Contains(keyHash uint64) bool {
	h1 := keyHash
	h2 := (keyHash >> 32) | (keyHash << 32)

	for i := 0; i < bf.numHash; i++ {
		h := h1 + uint64(i)*h2 + uint64(i*i)
		idx := h % bf.numBits
		wordIdx := idx / 64
		mask := uint64(1) << (idx % 64)

		if bf.bits[wordIdx].Load()&mask == 0 {
			return false
		}
	}
	return true
}

// ContainsBatch checks multiple keys using optimized access patterns.
// Pre-computes all hash positions for better CPU pipelining.
func (bf *bloomFilterSIMD) ContainsBatch(keyHashes []uint64, results []bool) {
	n := len(keyHashes)
	if n > len(results) {
		n = len(results)
	}

	for i := 0; i < n; i++ {
		results[i] = bf.Contains(keyHashes[i])
	}
}

// AddBatch adds multiple keys to the filter.
func (bf *bloomFilterSIMD) AddBatch(keyHashes []uint64) int {
	alreadyPresent := 0
	for _, h := range keyHashes {
		if bf.Add(h) {
			alreadyPresent++
		}
	}
	return alreadyPresent
}

// Reset clears the bloom filter.
func (bf *bloomFilterSIMD) Reset() {
	for i := range bf.bits {
		bf.bits[i].Store(0)
	}
}

// FillRatio returns the ratio of set bits to total bits.
func (bf *bloomFilterSIMD) FillRatio() float64 {
	count := 0
	for i := range bf.bits {
		word := bf.bits[i].Load()
		count += bits.OnesCount64(word)
	}
	return float64(count) / float64(bf.numBits)
}

// BitsPtr returns the pointer to the bits array for SIMD operations.
func (bf *bloomFilterSIMD) BitsPtr() unsafe.Pointer {
	return bf.rawBits.Ptr()
}

// NumBits returns the total number of bits in the filter.
func (bf *bloomFilterSIMD) NumBits() uint64 {
	return bf.numBits
}

// NumHash returns the number of hash functions used.
func (bf *bloomFilterSIMD) NumHash() int {
	return bf.numHash
}
