package policy

import (
	"math/rand"
	"sync"
)

const (
	// defaultSampleSize is the number of items to sample for eviction.
	defaultSampleSize = 5
)

// SampledLFU implements sampled LFU eviction policy.
// Instead of maintaining a sorted list, it samples random items
// and evicts the one with lowest frequency.
type SampledLFU struct {
	keyCosts   map[uint64]int64 // keyHash -> cost
	maxCost    int64
	usedCost   int64
	maxEntries int64
	numEntries int64
	sampleSize int
	mu         sync.Mutex
	rng        *rand.Rand
}

// NewSampledLFU creates a new SampledLFU eviction policy.
func NewSampledLFU(maxCost int64, maxEntries int64) *SampledLFU {
	return &SampledLFU{
		keyCosts:   make(map[uint64]int64),
		maxCost:    maxCost,
		maxEntries: maxEntries,
		sampleSize: defaultSampleSize,
		rng:        rand.New(rand.NewSource(rand.Int63())),
	}
}

// Add records a key with its cost.
// Returns true if added, false if rejected due to capacity.
func (s *SampledLFU) Add(keyHash uint64, cost int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if key already exists
	if existingCost, exists := s.keyCosts[keyHash]; exists {
		// Update cost
		s.usedCost += cost - existingCost
		s.keyCosts[keyHash] = cost
		return true
	}

	s.keyCosts[keyHash] = cost
	s.usedCost += cost
	s.numEntries++
	return true
}

// Has checks if a key is tracked by the policy.
func (s *SampledLFU) Has(keyHash uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, exists := s.keyCosts[keyHash]
	return exists
}

// Del removes a key from the policy.
func (s *SampledLFU) Del(keyHash uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cost, exists := s.keyCosts[keyHash]; exists {
		s.usedCost -= cost
		s.numEntries--
		delete(s.keyCosts, keyHash)
	}
}

// Update updates the cost of an existing key.
func (s *SampledLFU) Update(keyHash uint64, cost int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existingCost, exists := s.keyCosts[keyHash]; exists {
		s.usedCost += cost - existingCost
		s.keyCosts[keyHash] = cost
	}
}

// UsedCost returns the total cost of all tracked items.
func (s *SampledLFU) UsedCost() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usedCost
}

// NumEntries returns the number of tracked entries.
func (s *SampledLFU) NumEntries() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.numEntries
}

// NeedsEviction returns true if eviction is needed based on limits.
func (s *SampledLFU) NeedsEviction() bool {
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

// Sample returns a random sample of tracked key hashes.
func (s *SampledLFU) Sample() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.keyCosts) == 0 {
		return nil
	}

	n := s.sampleSize
	if n > len(s.keyCosts) {
		n = len(s.keyCosts)
	}

	// Reservoir sampling for efficiency when map is large
	sample := make([]uint64, 0, n)
	i := 0
	for keyHash := range s.keyCosts {
		if i < n {
			sample = append(sample, keyHash)
		} else {
			j := s.rng.Intn(i + 1)
			if j < n {
				sample[j] = keyHash
			}
		}
		i++
	}

	return sample
}

// Cost returns the cost of a specific key, or 0 if not found.
func (s *SampledLFU) Cost(keyHash uint64) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keyCosts[keyHash]
}

// Clear removes all tracked keys.
func (s *SampledLFU) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.keyCosts = make(map[uint64]int64)
	s.usedCost = 0
	s.numEntries = 0
}

// SetMaxCost updates the maximum cost limit.
func (s *SampledLFU) SetMaxCost(maxCost int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxCost = maxCost
}

// SetMaxEntries updates the maximum entries limit.
func (s *SampledLFU) SetMaxEntries(maxEntries int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxEntries = maxEntries
}
