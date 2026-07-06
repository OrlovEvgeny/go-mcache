package policy

import (
	"math/rand/v2"
)

const (
	// defaultSampleSize is the number of items to sample for eviction.
	defaultSampleSize = 5

	// maxSampleSize is the upper bound of adaptiveSampleSize.
	maxSampleSize = 20
)

// trackedItem stores the hash, cost and dense-array position for a tracked key.
type trackedItem struct {
	keyHash uint64
	cost    int64
	idx     int // position in keys[]
}

// SampledLFU implements sampled LFU eviction policy.
// Generic over K for exact-key identity (no hash collision ambiguity).
// Uses a dense array for O(sampleSize) random sampling.
//
// SampledLFU is NOT thread-safe: it is always accessed under the owning
// policy's mutex (see Policy and PolicyLockFree).
type SampledLFU[K comparable] struct {
	items      map[K]trackedItem // key -> {hash, cost, index}
	keys       []K               // dense array for O(1) random access
	maxCost    int64
	usedCost   int64
	maxEntries int64
	sampleSize int
	sampleBuf  []Victim[K] // reused by Sample; valid until the next Sample call
}

// NewSampledLFU creates a new SampledLFU eviction policy.
func NewSampledLFU[K comparable](maxCost int64, maxEntries int64) *SampledLFU[K] {
	return &SampledLFU[K]{
		items:      make(map[K]trackedItem),
		keys:       make([]K, 0, 64),
		maxCost:    maxCost,
		maxEntries: maxEntries,
		sampleSize: defaultSampleSize,
		sampleBuf:  make([]Victim[K], 0, maxSampleSize),
	}
}

// Add records a key with its cost.
func (s *SampledLFU[K]) Add(key K, keyHash uint64, cost int64) {
	if existing, exists := s.items[key]; exists {
		// Update cost
		s.usedCost += cost - existing.cost
		s.items[key] = trackedItem{keyHash: keyHash, cost: cost, idx: existing.idx}
		return
	}

	s.items[key] = trackedItem{keyHash: keyHash, cost: cost, idx: len(s.keys)}
	s.keys = append(s.keys, key)
	s.usedCost += cost
}

// Has checks if a key is tracked by the policy.
func (s *SampledLFU[K]) Has(key K) bool {
	_, exists := s.items[key]
	return exists
}

// Del removes a key from the policy.
func (s *SampledLFU[K]) Del(key K) {
	item, exists := s.items[key]
	if !exists {
		return
	}

	s.usedCost -= item.cost
	delete(s.items, key)

	// Swap-delete from dense array
	last := len(s.keys) - 1
	if item.idx != last {
		moved := s.keys[last]
		s.keys[item.idx] = moved
		if m, ok := s.items[moved]; ok {
			m.idx = item.idx
			s.items[moved] = m
		}
	}
	var zero K
	s.keys[last] = zero
	s.keys = s.keys[:last]
}

// Update updates the cost of an existing key.
func (s *SampledLFU[K]) Update(key K, keyHash uint64, cost int64) {
	if existing, exists := s.items[key]; exists {
		s.usedCost += cost - existing.cost
		s.items[key] = trackedItem{keyHash: keyHash, cost: cost, idx: existing.idx}
	}
}

// UsedCost returns the total cost of all tracked items.
func (s *SampledLFU[K]) UsedCost() int64 {
	return s.usedCost
}

// NumEntries returns the number of tracked entries.
func (s *SampledLFU[K]) NumEntries() int64 {
	return int64(len(s.items))
}

// NeedsEviction returns true if eviction is needed based on limits.
func (s *SampledLFU[K]) NeedsEviction() bool {
	if s.maxCost > 0 && s.usedCost > s.maxCost {
		return true
	}
	if s.maxEntries > 0 && int64(len(s.items)) > s.maxEntries {
		return true
	}
	return false
}

// Sample returns a random sample of tracked keys.
// O(sampleSize) time, zero allocations: the returned slice aliases an
// internal buffer and is only valid until the next Sample call.
func (s *SampledLFU[K]) Sample() []Victim[K] {
	n := s.adaptiveSampleSize()
	if n > len(s.keys) {
		n = len(s.keys)
	}
	if n == 0 {
		return nil
	}

	sample := s.sampleBuf[:0]

	// If cache is small, return all entries
	if len(s.keys) <= n {
		for _, key := range s.keys {
			item := s.items[key]
			sample = append(sample, Victim[K]{Key: key, KeyHash: item.keyHash})
		}
		s.sampleBuf = sample
		return sample
	}

	// Random selection from dense array — O(sampleSize).
	// Duplicate indices are rejected with a linear scan (n <= maxSampleSize).
	var picked [maxSampleSize]int
	count := 0
	for count < n {
		idx := rand.IntN(len(s.keys))
		dup := false
		for i := 0; i < count; i++ {
			if picked[i] == idx {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		picked[count] = idx
		count++

		key := s.keys[idx]
		item := s.items[key]
		sample = append(sample, Victim[K]{Key: key, KeyHash: item.keyHash})
	}
	s.sampleBuf = sample
	return sample
}

// adaptiveSampleSize returns an appropriate sample size based on the number of entries.
func (s *SampledLFU[K]) adaptiveSampleSize() int {
	n := len(s.keys)
	switch {
	case n < 100:
		return n // Sample all for small caches
	case n < 1000:
		return 10
	case n < 10000:
		return 15
	default:
		return maxSampleSize
	}
}

// Cost returns the cost of a specific key, or 0 if not found.
func (s *SampledLFU[K]) Cost(key K) int64 {
	if item, ok := s.items[key]; ok {
		return item.cost
	}
	return 0
}

// Clear removes all tracked keys.
func (s *SampledLFU[K]) Clear() {
	clear(s.items)
	var zero K
	for i := range s.keys {
		s.keys[i] = zero
	}
	s.keys = s.keys[:0]
	s.usedCost = 0
}

// SetMaxCost updates the maximum cost limit.
func (s *SampledLFU[K]) SetMaxCost(maxCost int64) {
	s.maxCost = maxCost
}

// SetMaxEntries updates the maximum entries limit.
func (s *SampledLFU[K]) SetMaxEntries(maxEntries int64) {
	s.maxEntries = maxEntries
}
