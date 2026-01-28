package policy

import (
	"math"
	"math/bits"
	"sync/atomic"
)

const (
	// blockSizeBits is the size of each block in bits (cache line = 64 bytes = 512 bits)
	blockSizeBits = 512
	// blockSizeWords is the number of 64-bit words per block
	blockSizeWords = blockSizeBits / 64
)

// block is a cache-line sized bloom filter block.
// All hash probes for an element stay within one block, ensuring only 1 cache miss.
type block [blockSizeWords]atomic.Uint64

// BlockedBloomFilter is a cache-efficient bloom filter variant.
// Instead of spreading hash probes across the entire filter,
// it confines all probes for an element to a single cache-line sized block.
// This provides much better cache locality at the cost of slightly higher false positive rate.
type BlockedBloomFilter struct {
	blocks    []block
	numBlocks uint64
	numHash   int // Number of hash functions per block (typically 8)
}

// NewBlockedBloomFilter creates a new blocked bloom filter.
// n is the expected number of items, fpRate is the desired false positive rate.
func NewBlockedBloomFilter(n int64, fpRate float64) *BlockedBloomFilter {
	if n <= 0 {
		n = 10000
	}
	if fpRate <= 0 || fpRate >= 1 {
		fpRate = 0.01
	}

	// Calculate total bits needed
	// For blocked filters, we need slightly more bits due to reduced entropy
	// Increase by ~20% compared to standard bloom filter
	ln2Sq := math.Ln2 * math.Ln2
	totalBits := uint64(1.2 * (-float64(n) * math.Log(fpRate) / ln2Sq))

	// Round up to whole blocks
	numBlocks := (totalBits + blockSizeBits - 1) / blockSizeBits
	if numBlocks < 1 {
		numBlocks = 1
	}

	// Calculate optimal number of hash functions for block size
	// k = (blockSize / avgItemsPerBlock) * ln(2)
	avgItemsPerBlock := float64(n) / float64(numBlocks)
	numHash := int(float64(blockSizeBits) / avgItemsPerBlock * math.Ln2)
	if numHash < 2 {
		numHash = 2
	}
	if numHash > 8 {
		numHash = 8 // Cap at 8 for performance (fits in cache line)
	}

	return &BlockedBloomFilter{
		blocks:    make([]block, numBlocks),
		numBlocks: numBlocks,
		numHash:   numHash,
	}
}

// Add adds a key hash to the filter.
// Returns true if the key was already present (may be false positive).
func (bf *BlockedBloomFilter) Add(keyHash uint64) bool {
	blockIdx := bf.blockIndex(keyHash)
	blk := &bf.blocks[blockIdx]

	alreadyPresent := true

	// All probes stay within this single block
	for i := 0; i < bf.numHash; i++ {
		// Compute bit position within block
		bitPos := bf.bitIndex(keyHash, i)
		wordIdx := bitPos / 64
		mask := uint64(1) << (bitPos % 64)

		for {
			old := blk[wordIdx].Load()
			if old&mask == 0 {
				alreadyPresent = false
			}
			if old&mask != 0 {
				break
			}
			if blk[wordIdx].CompareAndSwap(old, old|mask) {
				break
			}
		}
	}

	return alreadyPresent
}

// Contains checks if a key hash might be in the filter.
func (bf *BlockedBloomFilter) Contains(keyHash uint64) bool {
	blockIdx := bf.blockIndex(keyHash)
	blk := &bf.blocks[blockIdx]

	// All probes within one block = 1 cache miss max
	for i := 0; i < bf.numHash; i++ {
		bitPos := bf.bitIndex(keyHash, i)
		wordIdx := bitPos / 64
		mask := uint64(1) << (bitPos % 64)

		if blk[wordIdx].Load()&mask == 0 {
			return false
		}
	}
	return true
}

// blockIndex determines which block a key belongs to.
func (bf *BlockedBloomFilter) blockIndex(keyHash uint64) uint64 {
	// Use upper bits for block selection (they're usually well-mixed)
	return (keyHash >> 32) % bf.numBlocks
}

// bitIndex computes the bit position within a block for hash function i.
func (bf *BlockedBloomFilter) bitIndex(keyHash uint64, i int) uint64 {
	// Use lower bits with double hashing within block
	h1 := keyHash & 0xFFFFFFFF
	h2 := ((keyHash >> 16) ^ keyHash) & 0xFFFFFFFF
	h := h1 + uint64(i)*h2
	return h % blockSizeBits
}

// Reset clears the bloom filter.
func (bf *BlockedBloomFilter) Reset() {
	for i := range bf.blocks {
		for j := range bf.blocks[i] {
			bf.blocks[i][j].Store(0)
		}
	}
}

// FillRatio returns the ratio of set bits to total bits.
func (bf *BlockedBloomFilter) FillRatio() float64 {
	count := 0
	for i := range bf.blocks {
		for j := range bf.blocks[i] {
			word := bf.blocks[i][j].Load()
			count += bits.OnesCount64(word)
		}
	}
	totalBits := bf.numBlocks * blockSizeBits
	return float64(count) / float64(totalBits)
}

// BlockFillRatios returns fill ratios per block.
// Useful for detecting hot spots.
func (bf *BlockedBloomFilter) BlockFillRatios() []float64 {
	ratios := make([]float64, bf.numBlocks)
	for i := range bf.blocks {
		count := 0
		for j := range bf.blocks[i] {
			word := bf.blocks[i][j].Load()
			count += bits.OnesCount64(word)
		}
		ratios[i] = float64(count) / float64(blockSizeBits)
	}
	return ratios
}

// AddBatch adds multiple key hashes to the filter.
// Groups keys by block for better cache efficiency.
func (bf *BlockedBloomFilter) AddBatch(keyHashes []uint64) int {
	alreadyPresent := 0

	// Sort by block for better cache locality (optional optimization)
	// For simplicity, process in order
	for _, keyHash := range keyHashes {
		if bf.Add(keyHash) {
			alreadyPresent++
		}
	}

	return alreadyPresent
}

// ContainsBatch checks multiple key hashes.
func (bf *BlockedBloomFilter) ContainsBatch(keyHashes []uint64, results []bool) {
	n := len(keyHashes)
	if n > len(results) {
		n = len(results)
	}

	for i := 0; i < n; i++ {
		results[i] = bf.Contains(keyHashes[i])
	}
}

// NumBlocks returns the number of blocks.
func (bf *BlockedBloomFilter) NumBlocks() uint64 {
	return bf.numBlocks
}

// NumHash returns the number of hash functions.
func (bf *BlockedBloomFilter) NumHash() int {
	return bf.numHash
}

// TotalBits returns the total number of bits in the filter.
func (bf *BlockedBloomFilter) TotalBits() uint64 {
	return bf.numBlocks * blockSizeBits
}

// EstimatedFPRate returns the estimated false positive rate based on current fill.
func (bf *BlockedBloomFilter) EstimatedFPRate() float64 {
	fill := bf.FillRatio()
	// FP rate ≈ (fill ratio)^k
	return math.Pow(fill, float64(bf.numHash))
}
