// Package policy provides cache eviction policies.
package policy

import (
	"math/rand"
	"sync/atomic"
)

const (
	// cmDepthLockFree is the number of rows in the Count-Min Sketch.
	cmDepthLockFree = 4
	// cmWidthLockFree determines the accuracy of frequency estimation.
	// Using 8-bit counters packed into uint32 (4 counters per word).
	cmWidthLockFree = 1 << 20 // ~1M counters per row
)

// cmSketchLockFree is a lock-free Count-Min Sketch for frequency estimation.
// Uses 8-bit counters (0-255) packed into atomic.Uint32 words.
// This provides higher accuracy than 4-bit counters at the cost of 2x memory.
type cmSketchLockFree struct {
	rows  [cmDepthLockFree][]atomic.Uint32
	seeds [cmDepthLockFree]uint64
	width uint64
}

// newCMSketchLockFree creates a new lock-free Count-Min Sketch with the given width.
// Width is the number of 8-bit counters per row.
func newCMSketchLockFree(width int64) *cmSketchLockFree {
	if width <= 0 {
		width = cmWidthLockFree
	}
	// Round up to power of 2 for efficient modulo
	w := int64(1)
	for w < width {
		w <<= 1
	}

	s := &cmSketchLockFree{
		width: uint64(w),
	}

	// Each uint32 holds 4 counters (8 bits each)
	wordWidth := (w + 3) / 4
	for i := 0; i < cmDepthLockFree; i++ {
		s.rows[i] = make([]atomic.Uint32, wordWidth)
		s.seeds[i] = rand.Uint64()
	}

	return s
}

// Increment increments the frequency counter for the given hash.
// This is lock-free using atomic CAS operations.
func (s *cmSketchLockFree) Increment(keyHash uint64) {
	for i := 0; i < cmDepthLockFree; i++ {
		idx := s.index(keyHash, i)
		s.incrementAt(i, idx)
	}
}

// incrementAt atomically increments the counter at the given position.
func (s *cmSketchLockFree) incrementAt(row int, idx uint64) {
	wordIdx := idx / 4
	bytePos := (idx % 4) * 8 // Position within the uint32 (0, 8, 16, or 24)

	for {
		old := s.rows[row][wordIdx].Load()
		val := uint8((old >> bytePos) & 0xFF)

		// Saturation: don't increment if already at max
		if val >= 255 {
			return
		}

		// Create new value with incremented counter
		newVal := (old &^ (0xFF << bytePos)) | (uint32(val+1) << bytePos)

		if s.rows[row][wordIdx].CompareAndSwap(old, newVal) {
			return
		}
		// CAS failed, retry
	}
}

// Estimate returns the estimated frequency for the given hash.
// Returns the minimum count across all rows (Count-Min Sketch property).
func (s *cmSketchLockFree) Estimate(keyHash uint64) int64 {
	min := int64(255) // Max value for 8-bit counter
	for i := 0; i < cmDepthLockFree; i++ {
		idx := s.index(keyHash, i)
		val := int64(s.getAt(i, idx))
		if val < min {
			min = val
		}
	}
	return min
}

// index computes the index for row i using fast hash mixing.
func (s *cmSketchLockFree) index(keyHash uint64, row int) uint64 {
	// Mix the hash with the row seed using a fast finalizer
	h := keyHash ^ s.seeds[row]
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return h & (s.width - 1)
}

// getAt returns the counter value at the given position.
func (s *cmSketchLockFree) getAt(row int, idx uint64) uint8 {
	wordIdx := idx / 4
	bytePos := (idx % 4) * 8

	word := s.rows[row][wordIdx].Load()
	return uint8((word >> bytePos) & 0xFF)
}

// Reset halves all counters (aging mechanism).
// This is performed atomically per word.
func (s *cmSketchLockFree) Reset() {
	for i := 0; i < cmDepthLockFree; i++ {
		for j := range s.rows[i] {
			for {
				old := s.rows[i][j].Load()
				// Halve all 4 bytes in the word
				// Mask to get each byte, shift right, recombine
				b0 := ((old >> 0) & 0xFF) >> 1
				b1 := ((old >> 8) & 0xFF) >> 1
				b2 := ((old >> 16) & 0xFF) >> 1
				b3 := ((old >> 24) & 0xFF) >> 1
				newVal := b0 | (b1 << 8) | (b2 << 16) | (b3 << 24)

				if s.rows[i][j].CompareAndSwap(old, newVal) {
					break
				}
			}
		}
	}
}

// Clear resets all counters to zero.
func (s *cmSketchLockFree) Clear() {
	for i := 0; i < cmDepthLockFree; i++ {
		for j := range s.rows[i] {
			s.rows[i][j].Store(0)
		}
	}
}

// IncrementBatch increments counters for multiple keys.
// This is more efficient for batch operations due to better cache locality.
func (s *cmSketchLockFree) IncrementBatch(keyHashes []uint64) {
	for _, keyHash := range keyHashes {
		s.Increment(keyHash)
	}
}

// EstimateBatch returns estimated frequencies for multiple keys.
func (s *cmSketchLockFree) EstimateBatch(keyHashes []uint64, results []int64) {
	for i, keyHash := range keyHashes {
		if i < len(results) {
			results[i] = s.Estimate(keyHash)
		}
	}
}
