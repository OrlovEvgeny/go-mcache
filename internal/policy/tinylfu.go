package policy

import (
	"sync"
)

// TinyLFU implements the TinyLFU admission policy.
// It uses a Count-Min Sketch for frequency estimation and a bloom filter
// as a doorkeeper to avoid counting items that are only seen once.
type TinyLFU struct {
	freq    *cmSketch
	door    *bloomFilter
	incrs   int64     // Number of increments
	resetAt int64     // Reset threshold
	mu      sync.Mutex
}

// NewTinyLFU creates a new TinyLFU admission policy.
// numCounters is the number of counters in the Count-Min Sketch.
func NewTinyLFU(numCounters int64) *TinyLFU {
	if numCounters <= 0 {
		numCounters = 1 << 20 // ~1M counters
	}

	return &TinyLFU{
		freq:    newCMSketch(numCounters),
		door:    newBloomFilter(numCounters/10, 0.01),
		resetAt: numCounters, // Reset after this many increments
	}
}

// Increment records an access to the given key.
// This should be called on every cache access (hit or miss).
func (t *TinyLFU) Increment(keyHash uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Check doorkeeper: only count if seen before
	if t.door.Add(keyHash) {
		// Already in doorkeeper, increment count
		t.freq.Increment(keyHash)
	}

	t.incrs++
	if t.incrs >= t.resetAt {
		t.reset()
	}
}

// Estimate returns the estimated frequency of the given key.
func (t *TinyLFU) Estimate(keyHash uint64) int64 {
	t.mu.Lock()
	defer t.mu.Unlock()

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
func (t *TinyLFU) Admit(incomingHash, victimHash uint64) bool {
	incomingFreq := t.Estimate(incomingHash)
	victimFreq := t.Estimate(victimHash)

	// Admit if incoming frequency is higher
	// Tie-breaker: admit new item to allow exploration
	return incomingFreq >= victimFreq
}

// reset halves all counters and clears the doorkeeper.
// This implements the aging mechanism to adapt to changing access patterns.
func (t *TinyLFU) reset() {
	t.freq.Reset()
	t.door.Reset()
	t.incrs = 0
}

// Clear resets the TinyLFU to its initial state.
func (t *TinyLFU) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.freq.Clear()
	t.door.Reset()
	t.incrs = 0
}
