package policy

import (
	"sync"
)

// PolicyLockFree combines lock-free TinyLFU admission with SampledLFU eviction.
// The Access() method is completely lock-free, which is critical for read-heavy workloads.
type PolicyLockFree struct {
	admit *TinyLFULockFree
	evict *SampledLFU
	mu    sync.Mutex // Only for Add/Del operations that modify evict
}

// NewPolicyLockFree creates a new policy with lock-free admission tracking.
// numCounters is used for TinyLFU (recommended: 10x maxEntries).
// maxCost is the maximum total cost (0 for unlimited).
// maxEntries is the maximum number of entries (0 for unlimited).
func NewPolicyLockFree(numCounters int64, maxCost int64, maxEntries int64) *PolicyLockFree {
	if numCounters <= 0 && maxEntries > 0 {
		numCounters = maxEntries * 10
	}
	if numCounters <= 0 {
		numCounters = 1 << 20 // Default ~1M
	}

	return &PolicyLockFree{
		admit: NewTinyLFULockFree(numCounters),
		evict: NewSampledLFU(maxCost, maxEntries),
	}
}

// Add attempts to add a key with the given cost.
// Returns (victims to evict, whether the item should be added).
// This still requires locking due to eviction policy updates.
func (p *PolicyLockFree) Add(keyHash uint64, cost int64) (victims []uint64, added bool) {
	// Record access (lock-free)
	p.admit.Increment(keyHash)

	p.mu.Lock()
	defer p.mu.Unlock()

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
func (p *PolicyLockFree) findVictimsLocked(incomingHash uint64, cost int64) []uint64 {
	var victims []uint64

	for p.evict.NeedsEviction() {
		sample := p.evict.Sample()
		if len(sample) == 0 {
			break
		}

		var victim uint64
		lowestFreq := int64(1<<63 - 1)
		for _, keyHash := range sample {
			freq := p.admit.Estimate(keyHash)
			if freq < lowestFreq {
				lowestFreq = freq
				victim = keyHash
			}
		}

		if !p.admit.Admit(incomingHash, victim) {
			break
		}

		p.evict.Del(victim)
		victims = append(victims, victim)
	}

	return victims
}

// Access records an access to a key (hit).
// This is completely LOCK-FREE and can be called concurrently.
func (p *PolicyLockFree) Access(keyHash uint64) {
	p.admit.Increment(keyHash)
}

// AccessBatch records accesses for multiple keys.
// More efficient than individual Access calls.
func (p *PolicyLockFree) AccessBatch(keyHashes []uint64) {
	p.admit.IncrementBatch(keyHashes)
}

// Has checks if a key is tracked by the policy.
func (p *PolicyLockFree) Has(keyHash uint64) bool {
	return p.evict.Has(keyHash)
}

// Del removes a key from the policy.
func (p *PolicyLockFree) Del(keyHash uint64) {
	p.evict.Del(keyHash)
}

// Update updates the cost of an existing key.
func (p *PolicyLockFree) Update(keyHash uint64, cost int64) {
	p.evict.Update(keyHash, cost)
}

// Cost returns the current total cost.
func (p *PolicyLockFree) Cost() int64 {
	return p.evict.UsedCost()
}

// NumEntries returns the current number of entries.
func (p *PolicyLockFree) NumEntries() int64 {
	return p.evict.NumEntries()
}

// Clear resets the policy to its initial state.
func (p *PolicyLockFree) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.admit.Clear()
	p.evict.Clear()
}

// SetMaxCost updates the maximum cost limit.
func (p *PolicyLockFree) SetMaxCost(maxCost int64) {
	p.evict.SetMaxCost(maxCost)
}

// SetMaxEntries updates the maximum entries limit.
func (p *PolicyLockFree) SetMaxEntries(maxEntries int64) {
	p.evict.SetMaxEntries(maxEntries)
}

// FillRatio returns the doorkeeper bloom filter fill ratio.
func (p *PolicyLockFree) FillRatio() float64 {
	return p.admit.FillRatio()
}

// NumIncrements returns the number of access increments.
func (p *PolicyLockFree) NumIncrements() int64 {
	return p.admit.NumIncrements()
}

// Estimate returns the estimated frequency for a key.
func (p *PolicyLockFree) Estimate(keyHash uint64) int64 {
	return p.admit.Estimate(keyHash)
}
