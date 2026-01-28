package policy

import (
	"sync"
)

// Policy combines TinyLFU admission with SampledLFU eviction.
type Policy struct {
	admit    *TinyLFU
	evict    *SampledLFU
	mu       sync.Mutex
}

// NewPolicy creates a new combined admission/eviction policy.
// numCounters is used for TinyLFU (recommended: 10x maxEntries).
// maxCost is the maximum total cost (0 for unlimited).
// maxEntries is the maximum number of entries (0 for unlimited).
func NewPolicy(numCounters int64, maxCost int64, maxEntries int64) *Policy {
	if numCounters <= 0 && maxEntries > 0 {
		numCounters = maxEntries * 10
	}
	if numCounters <= 0 {
		numCounters = 1 << 20 // Default ~1M
	}

	return &Policy{
		admit: NewTinyLFU(numCounters),
		evict: NewSampledLFU(maxCost, maxEntries),
	}
}

// Add attempts to add a key with the given cost.
// Returns (victims to evict, whether the item should be added).
// If the item is rejected by admission policy, victims will be empty
// and added will be false.
func (p *Policy) Add(keyHash uint64, cost int64) (victims []uint64, added bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Record access for frequency estimation
	p.admit.Increment(keyHash)

	// Check if already tracked (update case)
	if p.evict.Has(keyHash) {
		p.evict.Update(keyHash, cost)
		return nil, true
	}

	// Check if we need to evict
	victims = p.findVictimsLocked(keyHash, cost)

	// Add to eviction policy
	p.evict.Add(keyHash, cost)

	return victims, true
}

// findVictimsLocked finds victims to evict to make room for a new item.
// Must be called with p.mu held.
func (p *Policy) findVictimsLocked(incomingHash uint64, cost int64) []uint64 {
	var victims []uint64

	// Evict until we have room
	for p.evict.NeedsEviction() {
		// Sample candidates
		sample := p.evict.Sample()
		if len(sample) == 0 {
			break
		}

		// Find the one with lowest frequency
		var victim uint64
		lowestFreq := int64(1<<63 - 1)
		for _, keyHash := range sample {
			freq := p.admit.Estimate(keyHash)
			if freq < lowestFreq {
				lowestFreq = freq
				victim = keyHash
			}
		}

		// Check if incoming item should be admitted over victim
		if !p.admit.Admit(incomingHash, victim) {
			// Incoming item has lower frequency, reject it
			break
		}

		// Evict the victim
		p.evict.Del(victim)
		victims = append(victims, victim)
	}

	return victims
}

// Access records an access to a key (hit).
// This updates the frequency estimation for the key.
func (p *Policy) Access(keyHash uint64) {
	p.admit.Increment(keyHash)
}

// Has checks if a key is tracked by the policy.
func (p *Policy) Has(keyHash uint64) bool {
	return p.evict.Has(keyHash)
}

// Del removes a key from the policy.
func (p *Policy) Del(keyHash uint64) {
	p.evict.Del(keyHash)
}

// Update updates the cost of an existing key.
func (p *Policy) Update(keyHash uint64, cost int64) {
	p.evict.Update(keyHash, cost)
}

// Cost returns the current total cost.
func (p *Policy) Cost() int64 {
	return p.evict.UsedCost()
}

// NumEntries returns the current number of entries.
func (p *Policy) NumEntries() int64 {
	return p.evict.NumEntries()
}

// Clear resets the policy to its initial state.
func (p *Policy) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.admit.Clear()
	p.evict.Clear()
}

// SetMaxCost updates the maximum cost limit.
func (p *Policy) SetMaxCost(maxCost int64) {
	p.evict.SetMaxCost(maxCost)
}

// SetMaxEntries updates the maximum entries limit.
func (p *Policy) SetMaxEntries(maxEntries int64) {
	p.evict.SetMaxEntries(maxEntries)
}
