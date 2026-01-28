// Package policy provides cache eviction policies.
package policy

import (
	"math/rand"
	"sync"
)

const (
	// cmDepth is the number of rows in the Count-Min Sketch.
	cmDepth = 4
	// cmWidth determines the accuracy of frequency estimation.
	// Using 4-bit counters packed into bytes.
	cmWidth = 1 << 20 // ~1M counters per row
)

// cmSketch is a Count-Min Sketch for frequency estimation.
// Uses 4-bit counters (0-15) packed into bytes.
type cmSketch struct {
	rows   [cmDepth][]byte
	seeds  [cmDepth]uint64
	mu     sync.RWMutex
	width  uint64
}

// newCMSketch creates a new Count-Min Sketch with the given width.
// Width is the number of 4-bit counters per row.
func newCMSketch(width int64) *cmSketch {
	if width <= 0 {
		width = cmWidth
	}
	// Round up to power of 2 for efficient modulo
	w := int64(1)
	for w < width {
		w <<= 1
	}

	s := &cmSketch{
		width: uint64(w),
	}

	// Each byte holds 2 counters (4 bits each)
	byteWidth := (w + 1) / 2
	for i := 0; i < cmDepth; i++ {
		s.rows[i] = make([]byte, byteWidth)
		s.seeds[i] = rand.Uint64()
	}

	return s
}

// Increment increments the frequency counter for the given hash.
func (s *cmSketch) Increment(keyHash uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := 0; i < cmDepth; i++ {
		idx := s.index(keyHash, i)
		s.incrementAt(i, idx)
	}
}

// Estimate returns the estimated frequency for the given hash.
func (s *cmSketch) Estimate(keyHash uint64) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	min := int64(15) // Max value for 4-bit counter
	for i := 0; i < cmDepth; i++ {
		idx := s.index(keyHash, i)
		val := int64(s.getAt(i, idx))
		if val < min {
			min = val
		}
	}
	return min
}

// index computes the index for row i.
func (s *cmSketch) index(keyHash uint64, row int) uint64 {
	// Mix the hash with the row seed
	h := keyHash ^ s.seeds[row]
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return h & (s.width - 1)
}

// incrementAt increments the counter at the given position.
func (s *cmSketch) incrementAt(row int, idx uint64) {
	byteIdx := idx / 2
	nibble := idx & 1

	b := s.rows[row][byteIdx]
	var val uint8
	if nibble == 0 {
		val = b & 0x0F
	} else {
		val = (b >> 4) & 0x0F
	}

	// Only increment if not at max (15)
	if val < 15 {
		val++
		if nibble == 0 {
			s.rows[row][byteIdx] = (b & 0xF0) | val
		} else {
			s.rows[row][byteIdx] = (b & 0x0F) | (val << 4)
		}
	}
}

// getAt returns the counter value at the given position.
func (s *cmSketch) getAt(row int, idx uint64) uint8 {
	byteIdx := idx / 2
	nibble := idx & 1

	b := s.rows[row][byteIdx]
	if nibble == 0 {
		return b & 0x0F
	}
	return (b >> 4) & 0x0F
}

// Reset halves all counters (aging mechanism).
func (s *cmSketch) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := 0; i < cmDepth; i++ {
		for j := range s.rows[i] {
			// Halve both nibbles
			b := s.rows[i][j]
			low := (b & 0x0F) >> 1
			high := ((b >> 4) & 0x0F) >> 1
			s.rows[i][j] = low | (high << 4)
		}
	}
}

// Clear resets all counters to zero.
func (s *cmSketch) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := 0; i < cmDepth; i++ {
		for j := range s.rows[i] {
			s.rows[i][j] = 0
		}
	}
}
