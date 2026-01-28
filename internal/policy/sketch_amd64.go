//go:build amd64

package policy

import (
	"sync/atomic"
	"unsafe"

	"github.com/OrlovEvgeny/go-mcache/internal/alloc"
)

// cmSketchSIMD is a SIMD-optimized Count-Min Sketch.
// Uses 8-bit counters with memory aligned for AVX2 operations.
type cmSketchSIMD struct {
	// Aligned rows for SIMD access
	rows  [cmDepthLockFree]*alloc.AlignedBuffer
	seeds [cmDepthLockFree]uint64
	width uint64

	// Atomic counters view (for lock-free operations)
	atomicRows [cmDepthLockFree][]atomic.Uint32
}

// newCMSketchSIMD creates a new SIMD-optimized Count-Min Sketch.
func newCMSketchSIMD(width int64) *cmSketchSIMD {
	if width <= 0 {
		width = cmWidthLockFree
	}
	// Round up to power of 2 for efficient modulo
	w := int64(1)
	for w < width {
		w <<= 1
	}

	s := &cmSketchSIMD{
		width: uint64(w),
	}

	// Allocate aligned memory for each row
	// Each byte is a counter, aligned to 32 bytes for AVX2
	for i := 0; i < cmDepthLockFree; i++ {
		// Allocate bytes, then create atomic view
		s.rows[i] = alloc.NewAlignedBuffer(int(w), alloc.AVX2Alignment)
		s.seeds[i] = uint64(i+1) * 0x9e3779b97f4a7c15 // Golden ratio based seeds

		// Create atomic view for CAS operations
		// Each uint32 holds 4 counters
		wordCount := (w + 3) / 4
		s.atomicRows[i] = make([]atomic.Uint32, wordCount)
	}

	return s
}

// Increment increments the frequency counter for the given hash.
// Uses SIMD-friendly access patterns with fallback to atomic operations.
func (s *cmSketchSIMD) Increment(keyHash uint64) {
	for i := 0; i < cmDepthLockFree; i++ {
		idx := s.index(keyHash, i)
		s.incrementAt(i, idx)
	}
}

// incrementAt atomically increments a counter.
func (s *cmSketchSIMD) incrementAt(row int, idx uint64) {
	wordIdx := idx / 4
	bytePos := (idx % 4) * 8

	for {
		old := s.atomicRows[row][wordIdx].Load()
		val := uint8((old >> bytePos) & 0xFF)

		if val >= 255 {
			return
		}

		newVal := (old &^ (0xFF << bytePos)) | (uint32(val+1) << bytePos)
		if s.atomicRows[row][wordIdx].CompareAndSwap(old, newVal) {
			return
		}
	}
}

// Estimate returns the estimated frequency for the given hash.
func (s *cmSketchSIMD) Estimate(keyHash uint64) int64 {
	min := int64(255)
	for i := 0; i < cmDepthLockFree; i++ {
		idx := s.index(keyHash, i)
		val := int64(s.getAt(i, idx))
		if val < min {
			min = val
		}
	}
	return min
}

// getAt returns the counter value at the given position.
func (s *cmSketchSIMD) getAt(row int, idx uint64) uint8 {
	wordIdx := idx / 4
	bytePos := (idx % 4) * 8
	word := s.atomicRows[row][wordIdx].Load()
	return uint8((word >> bytePos) & 0xFF)
}

// index computes the index for row i.
func (s *cmSketchSIMD) index(keyHash uint64, row int) uint64 {
	h := keyHash ^ s.seeds[row]
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return h & (s.width - 1)
}

// IncrementBatchSIMD increments counters for multiple keys using SIMD-friendly patterns.
// Computes all indices first, then performs updates in batches.
func (s *cmSketchSIMD) IncrementBatchSIMD(keyHashes []uint64) {
	n := len(keyHashes)
	if n == 0 {
		return
	}

	// Pre-compute all indices for better cache utilization
	indices := make([][cmDepthLockFree]uint64, n)
	for i, h := range keyHashes {
		for row := 0; row < cmDepthLockFree; row++ {
			indices[i][row] = s.index(h, row)
		}
	}

	// Process by row for better cache locality
	for row := 0; row < cmDepthLockFree; row++ {
		for i := 0; i < n; i++ {
			s.incrementAt(row, indices[i][row])
		}
	}
}

// EstimateBatchSIMD estimates frequencies for multiple keys.
func (s *cmSketchSIMD) EstimateBatchSIMD(keyHashes []uint64, results []int64) {
	n := len(keyHashes)
	if n > len(results) {
		n = len(results)
	}

	for i := 0; i < n; i++ {
		results[i] = s.Estimate(keyHashes[i])
	}
}

// Reset halves all counters.
func (s *cmSketchSIMD) Reset() {
	for row := 0; row < cmDepthLockFree; row++ {
		for j := range s.atomicRows[row] {
			for {
				old := s.atomicRows[row][j].Load()
				b0 := ((old >> 0) & 0xFF) >> 1
				b1 := ((old >> 8) & 0xFF) >> 1
				b2 := ((old >> 16) & 0xFF) >> 1
				b3 := ((old >> 24) & 0xFF) >> 1
				newVal := b0 | (b1 << 8) | (b2 << 16) | (b3 << 24)
				if s.atomicRows[row][j].CompareAndSwap(old, newVal) {
					break
				}
			}
		}
	}
}

// Clear resets all counters to zero.
func (s *cmSketchSIMD) Clear() {
	for row := 0; row < cmDepthLockFree; row++ {
		for j := range s.atomicRows[row] {
			s.atomicRows[row][j].Store(0)
		}
	}
}

// RowPtr returns the pointer to a row's data for SIMD operations.
func (s *cmSketchSIMD) RowPtr(row int) unsafe.Pointer {
	if row < 0 || row >= cmDepthLockFree {
		return nil
	}
	return s.rows[row].Ptr()
}

// Width returns the sketch width.
func (s *cmSketchSIMD) Width() uint64 {
	return s.width
}
