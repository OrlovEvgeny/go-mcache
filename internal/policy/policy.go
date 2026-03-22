package policy

import (
	"sync"
)

// Policy combines TinyLFU admission with SampledLFU eviction.
// Generic over K for exact-key identity.
type Policy[K comparable] struct {
	admit *TinyLFU
	evict *SampledLFU[K]
	mu    sync.Mutex
}

// NewPolicy creates a new combined admission/eviction policy.
func NewPolicy[K comparable](numCounters int64, maxCost int64, maxEntries int64) *Policy[K] {
	if numCounters <= 0 && maxEntries > 0 {
		numCounters = maxEntries * 10
	}
	if numCounters <= 0 {
		numCounters = 1 << 20 // Default ~1M
	}

	return &Policy[K]{
		admit: NewTinyLFU(numCounters),
		evict: NewSampledLFU[K](maxCost, maxEntries),
	}
}

// Add attempts to add a key with the given cost.
func (p *Policy[K]) Add(key K, keyHash uint64, cost int64) (victims []Victim[K], added bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Record access for frequency estimation
	p.admit.Increment(keyHash)

	// Check if already tracked (update case)
	if p.evict.Has(key) {
		p.evict.Update(key, keyHash, cost)
		return nil, true
	}

	// Check if we need to evict
	victims = p.findVictimsLocked(keyHash, cost)

	// Add to eviction policy
	p.evict.Add(key, keyHash, cost)

	return victims, true
}

// findVictimsLocked finds victims to evict to make room for a new item.
// Must be called with p.mu held.
func (p *Policy[K]) findVictimsLocked(incomingHash uint64, cost int64) []Victim[K] {
	victims := make([]Victim[K], 0, 8)

	for p.evict.NeedsEviction() {
		sample := p.evict.Sample()
		if len(sample) == 0 {
			break
		}

		var victim Victim[K]
		lowestFreq := int64(1<<63 - 1)
		for _, v := range sample {
			freq := p.admit.Estimate(v.KeyHash)
			if freq < lowestFreq {
				lowestFreq = freq
				victim = v
			}
		}

		if !p.admit.Admit(incomingHash, victim.KeyHash) {
			break
		}

		p.evict.Del(victim.Key)
		victims = append(victims, victim)
	}

	return victims
}

// Access records an access to a key (hit).
func (p *Policy[K]) Access(keyHash uint64) {
	p.admit.Increment(keyHash)
}

// Has checks if a key is tracked by the policy.
func (p *Policy[K]) Has(key K) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.evict.Has(key)
}

// Del removes a key from the policy.
func (p *Policy[K]) Del(key K, keyHash uint64) {
	p.mu.Lock()
	p.evict.Del(key)
	p.mu.Unlock()
}

// Update updates the cost of an existing key.
func (p *Policy[K]) Update(key K, keyHash uint64, cost int64) {
	p.mu.Lock()
	p.evict.Update(key, keyHash, cost)
	p.mu.Unlock()
}

// Cost returns the current total cost.
func (p *Policy[K]) Cost() int64 {
	return p.evict.UsedCost()
}

// NumEntries returns the current number of entries.
func (p *Policy[K]) NumEntries() int64 {
	return p.evict.NumEntries()
}

// Clear resets the policy to its initial state.
func (p *Policy[K]) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.admit.Clear()
	p.evict.Clear()
}

// SetMaxCost updates the maximum cost limit.
func (p *Policy[K]) SetMaxCost(maxCost int64) {
	p.evict.SetMaxCost(maxCost)
}

// SetMaxEntries updates the maximum entries limit.
func (p *Policy[K]) SetMaxEntries(maxEntries int64) {
	p.evict.SetMaxEntries(maxEntries)
}
