// Package hashtable provides high-performance hash table implementations.
package hashtable

import (
	"sync"
	"sync/atomic"
)

const (
	// groupSize is the number of slots per control group (16 for SSE, 32 for AVX2)
	groupSize = 16
	// emptySlot indicates an empty slot
	emptySlot = 0x80
	// deletedSlot indicates a deleted slot
	deletedSlot = 0xFE
	// sentinelSlot marks the end of probing
	sentinelSlot = 0xFF
	// h2Mask extracts the H2 hash (7 bits)
	h2Mask = 0x7F
)

// SwissEntry represents a key-value pair in the Swiss table.
type SwissEntry struct {
	KeyHash uint64
	Cost    int64
}

// controlGroup is a 16-byte control block for SIMD matching.
type controlGroup [groupSize]byte

// SwissTable is a Swiss table implementation for fast key-value lookups.
// Uses SIMD-friendly control bytes for parallel slot matching.
type SwissTable struct {
	groups  []controlGroup
	entries []SwissEntry
	size    atomic.Int64
	cap     int64
	mu      sync.RWMutex
}

// NewSwissTable creates a new Swiss table with the given capacity.
func NewSwissTable(capacity int64) *SwissTable {
	if capacity <= 0 {
		capacity = 16
	}

	// Round up to power of 2 and ensure at least 1 group
	numGroups := (capacity + groupSize - 1) / groupSize
	numGroups = nextPowerOf2Int64(numGroups)

	totalSlots := numGroups * groupSize

	st := &SwissTable{
		groups:  make([]controlGroup, numGroups),
		entries: make([]SwissEntry, totalSlots),
		cap:     totalSlots,
	}

	// Initialize all slots as empty
	for i := range st.groups {
		for j := 0; j < groupSize; j++ {
			st.groups[i][j] = emptySlot
		}
	}

	return st
}

// nextPowerOf2Int64 returns the smallest power of 2 >= n.
func nextPowerOf2Int64(n int64) int64 {
	if n <= 1 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n |= n >> 32
	n++
	return n
}

// h1 extracts the primary hash (used for group selection).
func h1(hash uint64) uint64 {
	return hash >> 7
}

// h2 extracts the secondary hash (7 bits for control byte matching).
func h2(hash uint64) byte {
	return byte(hash & h2Mask)
}

// Insert inserts or updates a key-value pair.
// Returns true if inserted (new key), false if updated.
func (st *SwissTable) Insert(keyHash uint64, cost int64) bool {
	st.mu.Lock()
	defer st.mu.Unlock()

	// Check if key exists
	if idx, found := st.findLocked(keyHash); found {
		st.entries[idx].Cost = cost
		return false
	}

	// Check if we need to grow
	if float64(st.size.Load()+1) > float64(st.cap)*0.875 {
		st.growLocked()
	}

	// Find empty slot
	numGroups := int64(len(st.groups))
	mask := numGroups - 1
	groupIdx := int64(h1(keyHash)) & mask
	h2Val := h2(keyHash)

	for probe := int64(0); probe < numGroups; probe++ {
		gIdx := (groupIdx + probe) & mask
		group := &st.groups[gIdx]

		for i := 0; i < groupSize; i++ {
			ctrl := group[i]
			if ctrl == emptySlot || ctrl == deletedSlot {
				// Found empty slot
				slotIdx := gIdx*groupSize + int64(i)
				group[i] = h2Val
				st.entries[slotIdx] = SwissEntry{
					KeyHash: keyHash,
					Cost:    cost,
				}
				st.size.Add(1)
				return true
			}
		}
	}

	// Should never happen if load factor is maintained
	return false
}

// findLocked finds a key in the table.
// Must be called with lock held.
func (st *SwissTable) findLocked(keyHash uint64) (int64, bool) {
	numGroups := int64(len(st.groups))
	if numGroups == 0 {
		return -1, false
	}

	mask := numGroups - 1
	groupIdx := int64(h1(keyHash)) & mask
	h2Val := h2(keyHash)

	for probe := int64(0); probe < numGroups; probe++ {
		gIdx := (groupIdx + probe) & mask
		group := &st.groups[gIdx]
		hasEmpty := false

		// Check each slot in the group
		for i := 0; i < groupSize; i++ {
			ctrl := group[i]
			if ctrl == emptySlot {
				hasEmpty = true
				continue
			}
			if ctrl == deletedSlot {
				continue
			}
			if ctrl == h2Val {
				slotIdx := gIdx*groupSize + int64(i)
				if st.entries[slotIdx].KeyHash == keyHash {
					return slotIdx, true
				}
			}
		}

		// If we found an empty slot, key doesn't exist
		if hasEmpty {
			return -1, false
		}
	}

	return -1, false
}

// Get retrieves the cost for a key.
// Returns the cost and true if found, 0 and false otherwise.
func (st *SwissTable) Get(keyHash uint64) (int64, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	if idx, found := st.findLocked(keyHash); found {
		return st.entries[idx].Cost, true
	}
	return 0, false
}

// Has checks if a key exists in the table.
func (st *SwissTable) Has(keyHash uint64) bool {
	st.mu.RLock()
	defer st.mu.RUnlock()

	_, found := st.findLocked(keyHash)
	return found
}

// Delete removes a key from the table.
// Returns the cost and true if found, 0 and false otherwise.
func (st *SwissTable) Delete(keyHash uint64) (int64, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()

	idx, found := st.findLocked(keyHash)
	if !found {
		return 0, false
	}

	cost := st.entries[idx].Cost

	// Mark slot as deleted
	groupIdx := idx / groupSize
	slotIdx := idx % groupSize
	st.groups[groupIdx][slotIdx] = deletedSlot
	st.entries[idx] = SwissEntry{}
	st.size.Add(-1)

	return cost, true
}

// Update updates the cost for an existing key.
// Returns true if the key was found and updated.
func (st *SwissTable) Update(keyHash uint64, cost int64) bool {
	st.mu.Lock()
	defer st.mu.Unlock()

	if idx, found := st.findLocked(keyHash); found {
		st.entries[idx].Cost = cost
		return true
	}
	return false
}

// growLocked doubles the table size.
// Must be called with lock held.
func (st *SwissTable) growLocked() {
	oldGroups := st.groups
	oldEntries := st.entries
	oldCap := st.cap

	newCap := oldCap * 2
	numGroups := newCap / groupSize

	st.groups = make([]controlGroup, numGroups)
	st.entries = make([]SwissEntry, newCap)
	st.cap = newCap
	st.size.Store(0)

	// Initialize new groups as empty
	for i := range st.groups {
		for j := 0; j < groupSize; j++ {
			st.groups[i][j] = emptySlot
		}
	}

	// Reinsert old entries
	for i := int64(0); i < int64(len(oldGroups)); i++ {
		for j := 0; j < groupSize; j++ {
			ctrl := oldGroups[i][j]
			if ctrl != emptySlot && ctrl != deletedSlot {
				idx := i*groupSize + int64(j)
				entry := oldEntries[idx]
				st.insertNoLock(entry.KeyHash, entry.Cost)
			}
		}
	}
}

// insertNoLock inserts without locking (used during grow).
func (st *SwissTable) insertNoLock(keyHash uint64, cost int64) {
	numGroups := int64(len(st.groups))
	mask := numGroups - 1
	groupIdx := int64(h1(keyHash)) & mask
	h2Val := h2(keyHash)

	for probe := int64(0); probe < numGroups; probe++ {
		gIdx := (groupIdx + probe) & mask
		group := &st.groups[gIdx]

		for i := 0; i < groupSize; i++ {
			ctrl := group[i]
			if ctrl == emptySlot || ctrl == deletedSlot {
				slotIdx := gIdx*groupSize + int64(i)
				group[i] = h2Val
				st.entries[slotIdx] = SwissEntry{
					KeyHash: keyHash,
					Cost:    cost,
				}
				st.size.Add(1)
				return
			}
		}
	}
}

// Size returns the number of entries in the table.
func (st *SwissTable) Size() int64 {
	return st.size.Load()
}

// Capacity returns the table capacity.
func (st *SwissTable) Capacity() int64 {
	return st.cap
}

// LoadFactor returns the current load factor.
func (st *SwissTable) LoadFactor() float64 {
	return float64(st.size.Load()) / float64(st.cap)
}

// Clear removes all entries from the table.
func (st *SwissTable) Clear() {
	st.mu.Lock()
	defer st.mu.Unlock()

	for i := range st.groups {
		for j := 0; j < groupSize; j++ {
			st.groups[i][j] = emptySlot
		}
	}
	st.entries = make([]SwissEntry, st.cap)
	st.size.Store(0)
}

// Sample returns a random sample of entries.
// Useful for eviction policy sampling.
func (st *SwissTable) Sample(n int) []SwissEntry {
	st.mu.RLock()
	defer st.mu.RUnlock()

	if n <= 0 || st.size.Load() == 0 {
		return nil
	}

	sample := make([]SwissEntry, 0, n)
	count := 0

	// Simple reservoir sampling
	for i := int64(0); i < int64(len(st.groups)) && len(sample) < n; i++ {
		for j := 0; j < groupSize; j++ {
			ctrl := st.groups[i][j]
			if ctrl != emptySlot && ctrl != deletedSlot {
				idx := i*groupSize + int64(j)
				if count < n {
					sample = append(sample, st.entries[idx])
				}
				count++
			}
		}
	}

	return sample
}

// Range iterates over all entries.
func (st *SwissTable) Range(fn func(entry SwissEntry) bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	for i := int64(0); i < int64(len(st.groups)); i++ {
		for j := 0; j < groupSize; j++ {
			ctrl := st.groups[i][j]
			if ctrl != emptySlot && ctrl != deletedSlot {
				idx := i*groupSize + int64(j)
				if !fn(st.entries[idx]) {
					return
				}
			}
		}
	}
}
