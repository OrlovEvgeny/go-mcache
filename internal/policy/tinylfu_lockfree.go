package policy

import (
	"sync/atomic"
)

// incrStripes is the number of stripes for the increment counter.
// Must be a power of two.
const incrStripes = 64

// paddedCounter is an atomic counter padded to a cache line to prevent
// false sharing between adjacent stripes.
type paddedCounter struct {
	v atomic.Int64
	_ [56]byte
}

// TinyLFULockFree implements a lock-free TinyLFU admission policy.
// It uses a lock-free Count-Min Sketch for frequency estimation and a lock-free
// bloom filter as a doorkeeper to avoid counting items that are only seen once.
type TinyLFULockFree struct {
	freq *cmSketchLockFree
	door *bloomFilterLockFree

	// incrs is striped by key hash: a single shared counter would turn
	// every access into a CAS on one cache line shared by all cores.
	incrs     [incrStripes]paddedCounter
	resetAt   int64 // Reset threshold
	resetting atomic.Bool
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

	t.countIncrement(keyHash, 1)
}

// countIncrement adds n to the striped increment counter and occasionally
// checks whether the aging reset threshold has been reached.
func (t *TinyLFULockFree) countIncrement(keyHash uint64, n int64) {
	stripe := t.incrs[keyHash&(incrStripes-1)].v.Add(n)

	// Amortize the reset check: summing all stripes on every increment
	// would defeat the striping, so only check every 256th increment
	// of the local stripe.
	if stripe&0xFF == 0 {
		t.maybeReset()
	}
}

// maybeReset performs the aging reset when the total increment count has
// reached the threshold. At most one goroutine resets at a time; the check
// is approximate, which is acceptable for a probabilistic sketch.
func (t *TinyLFULockFree) maybeReset() {
	if t.NumIncrements() < t.resetAt {
		return
	}
	if !t.resetting.CompareAndSwap(false, true) {
		return
	}
	defer t.resetting.Store(false)

	if t.NumIncrements() < t.resetAt {
		return
	}
	t.reset()
	for i := range t.incrs {
		t.incrs[i].v.Store(0)
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
	for i := range t.incrs {
		t.incrs[i].v.Store(0)
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
	var total int64
	for i := range t.incrs {
		total += t.incrs[i].v.Load()
	}
	return total
}
