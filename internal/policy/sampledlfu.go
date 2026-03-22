package policy

import (
	"math/rand"
	"sync"
)

const (
	// defaultSampleSize is the number of items to sample for eviction.
	defaultSampleSize = 5
)

// trackedItem stores the hash and cost for a tracked key.
type trackedItem struct {
	keyHash uint64
	cost    int64
}

// SampledLFU implements sampled LFU eviction policy.
// Generic over K for exact-key identity (no hash collision ambiguity).
// Uses a dense array for O(sampleSize) random sampling.
type SampledLFU[K comparable] struct {
	costs    map[K]trackedItem // key -> {hash, cost}
	keys     []K               // dense array for O(1) random access
	keyIndex map[K]int          // key -> index in keys[]
	maxCost    int64
	usedCost   int64
	maxEntries int64
	numEntries int64
	sampleSize int
	mu         sync.Mutex
	rng        *rand.Rand
}

// NewSampledLFU creates a new SampledLFU eviction policy.
func NewSampledLFU[K comparable](maxCost int64, maxEntries int64) *SampledLFU[K] {
	return &SampledLFU[K]{
		costs:      make(map[K]trackedItem),
		keys:       make([]K, 0, 64),
		keyIndex:   make(map[K]int),
		maxCost:    maxCost,
		maxEntries: maxEntries,
		sampleSize: defaultSampleSize,
		rng:        rand.New(rand.NewSource(rand.Int63())),
	}
}

// Add records a key with its cost.
func (s *SampledLFU[K]) Add(key K, keyHash uint64, cost int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, exists := s.costs[key]; exists {
		// Update cost
		s.usedCost += cost - existing.cost
		s.costs[key] = trackedItem{keyHash: keyHash, cost: cost}
		return
	}

	s.costs[key] = trackedItem{keyHash: keyHash, cost: cost}
	// Add to dense array
	s.keyIndex[key] = len(s.keys)
	s.keys = append(s.keys, key)
	s.usedCost += cost
	s.numEntries++
}

// Has checks if a key is tracked by the policy.
func (s *SampledLFU[K]) Has(key K) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, exists := s.costs[key]
	return exists
}

// Del removes a key from the policy.
func (s *SampledLFU[K]) Del(key K) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, exists := s.costs[key]
	if !exists {
		return
	}

	s.usedCost -= item.cost
	s.numEntries--
	delete(s.costs, key)

	// Swap-delete from dense array
	idx, ok := s.keyIndex[key]
	if ok {
		last := len(s.keys) - 1
		if idx != last {
			s.keys[idx] = s.keys[last]
			s.keyIndex[s.keys[idx]] = idx
		}
		var zero K
		s.keys[last] = zero
		s.keys = s.keys[:last]
		delete(s.keyIndex, key)
	}
}

// Update updates the cost of an existing key.
func (s *SampledLFU[K]) Update(key K, keyHash uint64, cost int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, exists := s.costs[key]; exists {
		s.usedCost += cost - existing.cost
		s.costs[key] = trackedItem{keyHash: keyHash, cost: cost}
	}
}

// UsedCost returns the total cost of all tracked items.
func (s *SampledLFU[K]) UsedCost() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usedCost
}

// NumEntries returns the number of tracked entries.
func (s *SampledLFU[K]) NumEntries() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.numEntries
}

// NeedsEviction returns true if eviction is needed based on limits.
func (s *SampledLFU[K]) NeedsEviction() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.maxCost > 0 && s.usedCost > s.maxCost {
		return true
	}
	if s.maxEntries > 0 && s.numEntries > s.maxEntries {
		return true
	}
	return false
}

// Sample returns a random sample of tracked keys.
// O(sampleSize) time complexity via dense array random indexing.
func (s *SampledLFU[K]) Sample() []Victim[K] {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := s.adaptiveSampleSize()
	if n > len(s.keys) {
		n = len(s.keys)
	}
	if n == 0 {
		return nil
	}

	// If cache is small, return all entries
	if len(s.keys) <= n {
		sample := make([]Victim[K], len(s.keys))
		for i, key := range s.keys {
			item := s.costs[key]
			sample[i] = Victim[K]{Key: key, KeyHash: item.keyHash}
		}
		return sample
	}

	// Random selection from dense array — O(sampleSize)
	sample := make([]Victim[K], 0, n)
	seen := make(map[int]struct{}, n)
	for len(sample) < n {
		idx := s.rng.Intn(len(s.keys))
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		key := s.keys[idx]
		item := s.costs[key]
		sample = append(sample, Victim[K]{Key: key, KeyHash: item.keyHash})
	}
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
		return 20
	}
}

// Cost returns the cost of a specific key, or 0 if not found.
func (s *SampledLFU[K]) Cost(key K) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item, ok := s.costs[key]; ok {
		return item.cost
	}
	return 0
}

// Clear removes all tracked keys.
func (s *SampledLFU[K]) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.costs = make(map[K]trackedItem)
	s.keys = s.keys[:0]
	s.keyIndex = make(map[K]int)
	s.usedCost = 0
	s.numEntries = 0
}

// SetMaxCost updates the maximum cost limit.
func (s *SampledLFU[K]) SetMaxCost(maxCost int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxCost = maxCost
}

// SetMaxEntries updates the maximum entries limit.
func (s *SampledLFU[K]) SetMaxEntries(maxEntries int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxEntries = maxEntries
}
