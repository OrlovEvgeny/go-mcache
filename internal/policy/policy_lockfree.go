package policy

import (
	"sync"
)

// PolicyLockFree combines lock-free TinyLFU admission with SampledLFU eviction.
// The Access() method is completely lock-free, which is critical for read-heavy workloads.
// Generic over K for exact-key identity.
type PolicyLockFree[K comparable] struct {
	admit *TinyLFULockFree
	evict *SampledLFU[K]
	mu    sync.Mutex // For Add/Del/Update operations that modify evict
}

// NewPolicyLockFree creates a new policy with lock-free admission tracking.
func NewPolicyLockFree[K comparable](numCounters int64, maxCost int64, maxEntries int64) *PolicyLockFree[K] {
	if numCounters <= 0 && maxEntries > 0 {
		numCounters = maxEntries * 10
	}
	if numCounters <= 0 {
		numCounters = 1 << 20 // Default ~1M
	}

	return &PolicyLockFree[K]{
		admit: NewTinyLFULockFree(numCounters),
		evict: NewSampledLFU[K](maxCost, maxEntries),
	}
}

// Add attempts to add a key with the given cost.
func (p *PolicyLockFree[K]) Add(key K, keyHash uint64, cost int64) (victims []Victim[K], added bool) {
	// Record access (lock-free)
	p.admit.Increment(keyHash)

	p.mu.Lock()
	defer p.mu.Unlock()

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
func (p *PolicyLockFree[K]) findVictimsLocked(incomingHash uint64, cost int64) []Victim[K] {
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
// This is completely LOCK-FREE and can be called concurrently.
func (p *PolicyLockFree[K]) Access(keyHash uint64) {
	p.admit.Increment(keyHash)
}

// AccessBatch records accesses for multiple keys.
func (p *PolicyLockFree[K]) AccessBatch(keyHashes []uint64) {
	p.admit.IncrementBatch(keyHashes)
}

// Has checks if a key is tracked by the policy.
func (p *PolicyLockFree[K]) Has(key K) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.evict.Has(key)
}

// Del removes a key from the policy.
func (p *PolicyLockFree[K]) Del(key K, keyHash uint64) {
	p.mu.Lock()
	p.evict.Del(key)
	p.mu.Unlock()
}

// Update updates the cost of an existing key.
func (p *PolicyLockFree[K]) Update(key K, keyHash uint64, cost int64) {
	p.mu.Lock()
	p.evict.Update(key, keyHash, cost)
	p.mu.Unlock()
}

// Cost returns the current total cost.
func (p *PolicyLockFree[K]) Cost() int64 {
	return p.evict.UsedCost()
}

// NumEntries returns the current number of entries.
func (p *PolicyLockFree[K]) NumEntries() int64 {
	return p.evict.NumEntries()
}

// Clear resets the policy to its initial state.
func (p *PolicyLockFree[K]) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.admit.Clear()
	p.evict.Clear()
}

// SetMaxCost updates the maximum cost limit.
func (p *PolicyLockFree[K]) SetMaxCost(maxCost int64) {
	p.evict.SetMaxCost(maxCost)
}

// SetMaxEntries updates the maximum entries limit.
func (p *PolicyLockFree[K]) SetMaxEntries(maxEntries int64) {
	p.evict.SetMaxEntries(maxEntries)
}

// FillRatio returns the doorkeeper bloom filter fill ratio.
func (p *PolicyLockFree[K]) FillRatio() float64 {
	return p.admit.FillRatio()
}

// NumIncrements returns the number of access increments.
func (p *PolicyLockFree[K]) NumIncrements() int64 {
	return p.admit.NumIncrements()
}

// Estimate returns the estimated frequency for a key.
func (p *PolicyLockFree[K]) Estimate(keyHash uint64) int64 {
	return p.admit.Estimate(keyHash)
}
