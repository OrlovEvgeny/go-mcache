package policy

import (
	"sync/atomic"
)

// TinyLFULockFree implements a lock-free TinyLFU admission policy.
// It uses a lock-free Count-Min Sketch for frequency estimation and a lock-free
// bloom filter as a doorkeeper to avoid counting items that are only seen once.
type TinyLFULockFree struct {
	freq    *cmSketchLockFree
	door    *bloomFilterLockFree
	incrs   atomic.Int64 // Number of increments
	resetAt int64        // Reset threshold
}

// NewTinyLFULockFree creates a new lock-free TinyLFU admission policy.
// numCounters is the number of counters in the Count-Min Sketch.
func NewTinyLFULockFree(numCounters int64) *TinyLFULockFree {
	if numCounters <= 0 {
		numCounters = 1 << 20 // ~1M counters
	}

	return &TinyLFULockFree{
		freq:    newCMSketchLockFree(numCounters),
		door:    newBloomFilterLockFree(numCounters/10, 0.01),
		resetAt: numCounters, // Reset after this many increments
	}
}

// Increment records an access to the given key.
// This should be called on every cache access (hit or miss).
// This is lock-free and can be called concurrently from multiple goroutines.
func (t *TinyLFULockFree) Increment(keyHash uint64) {
	// Check doorkeeper: only count if seen before
	if t.door.Add(keyHash) {
		// Already in doorkeeper, increment count
		t.freq.Increment(keyHash)
	}

	// Atomic increment and check for reset
	incrs := t.incrs.Add(1)

	// Check if we need to reset (aging)
	// Use a simple threshold check - slight over-counting is acceptable
	if incrs >= t.resetAt {
		// Try to claim the reset operation
		if t.incrs.CompareAndSwap(incrs, 0) {
			t.reset()
		}
	}
}

// Estimate returns the estimated frequency of the given key.
// This is naturally lock-free.
func (t *TinyLFULockFree) Estimate(keyHash uint64) int64 {
	// Add 1 if in doorkeeper (represents the first access)
	estimate := t.freq.Estimate(keyHash)
	if t.door.Contains(keyHash) {
		estimate++
	}
	return estimate
}

// Admit decides whether a new item should be admitted to the cache.
// It compares the frequency of the incoming item with a candidate victim.
// Returns true if the incoming item should be admitted.
func (t *TinyLFULockFree) Admit(incomingHash, victimHash uint64) bool {
	incomingFreq := t.Estimate(incomingHash)
	victimFreq := t.Estimate(victimHash)

	// Admit if incoming frequency is higher
	// Tie-breaker: admit new item to allow exploration
	return incomingFreq >= victimFreq
}

// reset halves all counters and clears the doorkeeper.
// This implements the aging mechanism to adapt to changing access patterns.
func (t *TinyLFULockFree) reset() {
	t.freq.Reset()
	t.door.Reset()
}

// Clear resets the TinyLFU to its initial state.
func (t *TinyLFULockFree) Clear() {
	t.freq.Clear()
	t.door.Reset()
	t.incrs.Store(0)
}

// IncrementBatch records accesses for multiple keys.
// More efficient than individual Increment calls.
func (t *TinyLFULockFree) IncrementBatch(keyHashes []uint64) {
	for _, keyHash := range keyHashes {
		// Check doorkeeper: only count if seen before
		if t.door.Add(keyHash) {
			t.freq.Increment(keyHash)
		}
	}

	// Batch update the counter
	incrs := t.incrs.Add(int64(len(keyHashes)))

	// Check if we need to reset
	if incrs >= t.resetAt {
		if t.incrs.CompareAndSwap(incrs, 0) {
			t.reset()
		}
	}
}

// EstimateBatch returns estimated frequencies for multiple keys.
func (t *TinyLFULockFree) EstimateBatch(keyHashes []uint64, results []int64) {
	for i, keyHash := range keyHashes {
		if i < len(results) {
			estimate := t.freq.Estimate(keyHash)
			if t.door.Contains(keyHash) {
				estimate++
			}
			results[i] = estimate
		}
	}
}

// FillRatio returns the doorkeeper bloom filter fill ratio.
// Useful for monitoring and debugging.
func (t *TinyLFULockFree) FillRatio() float64 {
	return t.door.FillRatio()
}

// NumIncrements returns the current increment counter.
// Useful for monitoring.
func (t *TinyLFULockFree) NumIncrements() int64 {
	return t.incrs.Load()
}
