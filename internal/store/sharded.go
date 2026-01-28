// Package store provides storage backends for the cache.
package store

import (
	"sync"
	"sync/atomic"

	"github.com/OrlovEvgeny/go-mcache/internal/clock"
	"github.com/OrlovEvgeny/go-mcache/internal/hash"
)

const (
	// DefaultShardCount is the default number of shards.
	DefaultShardCount = 1024
)

// Entry represents a cache entry.
type Entry[K comparable, V any] struct {
	Key      K
	Value    V
	KeyHash  uint64
	ExpireAt int64 // Unix nanoseconds, 0 = no expiration
	Cost     int64
}

// IsExpired returns true if the entry has expired.
func (e *Entry[K, V]) IsExpired() bool {
	return e.ExpireAt > 0 && clock.NowNano() > e.ExpireAt
}

// shard represents a single shard of the sharded store.
type shard[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]*Entry[K, V]
	_  [64 - 8]byte // Cache line padding to prevent false sharing
}

// ShardedStore is a sharded in-memory store.
type ShardedStore[K comparable, V any] struct {
	shards    []*shard[K, V]
	shardMask uint64
	size      atomic.Int64
	hasher    func(K) uint64
}

// NewShardedStore creates a new sharded store.
func NewShardedStore[K comparable, V any](shardCount int, hasher func(K) uint64) *ShardedStore[K, V] {
	if shardCount <= 0 {
		shardCount = DefaultShardCount
	}
	// Round up to power of 2
	shardCount = nextPowerOf2(shardCount)

	s := &ShardedStore[K, V]{
		shards:    make([]*shard[K, V], shardCount),
		shardMask: uint64(shardCount - 1),
		hasher:    hasher,
	}

	for i := range s.shards {
		s.shards[i] = &shard[K, V]{
			m: make(map[K]*Entry[K, V]),
		}
	}

	return s
}

// nextPowerOf2 returns the smallest power of 2 >= n.
func nextPowerOf2(n int) int {
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n++
	return n
}

// getShard returns the shard for the given key hash.
func (s *ShardedStore[K, V]) getShard(keyHash uint64) *shard[K, V] {
	return s.shards[keyHash&s.shardMask]
}

// getKeyHash computes the hash for a key.
func (s *ShardedStore[K, V]) getKeyHash(key K) uint64 {
	if s.hasher != nil {
		return s.hasher(key)
	}
	// Default hasher for common types
	return defaultHash(key)
}

// defaultHash provides default hashing for common types.
func defaultHash[K comparable](key K) uint64 {
	switch k := any(key).(type) {
	case string:
		return hash.String(k)
	case int:
		return hash.Int(k)
	case int64:
		return hash.Int64(k)
	case uint64:
		return hash.Uint64(k)
	case int32:
		return hash.Int32(k)
	case uint32:
		return hash.Uint32(k)
	default:
		// Fallback: use fmt.Sprint and hash
		// This is slow but works for any comparable type
		return hash.String(anyToString(key))
	}
}

// anyToString converts any value to string for hashing.
func anyToString[K comparable](key K) string {
	switch k := any(key).(type) {
	case string:
		return k
	default:
		// Use a simple representation
		return ""
	}
}

// Get retrieves an entry by key.
// Returns the entry and true if found and not expired, nil and false otherwise.
func (s *ShardedStore[K, V]) Get(key K) (*Entry[K, V], bool) {
	keyHash := s.getKeyHash(key)
	sh := s.getShard(keyHash)

	sh.mu.RLock()
	entry, exists := sh.m[key]
	sh.mu.RUnlock()

	if !exists {
		return nil, false
	}

	// Check expiration
	if entry.ExpireAt > 0 && clock.NowNano() > entry.ExpireAt {
		return nil, false
	}

	return entry, true
}

// GetByHash retrieves an entry by key when hash is already known.
func (s *ShardedStore[K, V]) GetByHash(key K, keyHash uint64) (*Entry[K, V], bool) {
	sh := s.getShard(keyHash)

	sh.mu.RLock()
	entry, exists := sh.m[key]
	sh.mu.RUnlock()

	if !exists {
		return nil, false
	}

	// Check expiration
	if entry.ExpireAt > 0 && clock.NowNano() > entry.ExpireAt {
		return nil, false
	}

	return entry, true
}

// Set stores an entry.
// Returns the previous entry if it existed, nil otherwise.
func (s *ShardedStore[K, V]) Set(entry *Entry[K, V]) *Entry[K, V] {
	if entry.KeyHash == 0 {
		entry.KeyHash = s.getKeyHash(entry.Key)
	}

	sh := s.getShard(entry.KeyHash)

	sh.mu.Lock()
	prev, existed := sh.m[entry.Key]
	sh.m[entry.Key] = entry
	sh.mu.Unlock()

	if !existed {
		s.size.Add(1)
	}

	return prev
}

// Delete removes an entry by key.
// Returns the deleted entry if it existed, nil otherwise.
func (s *ShardedStore[K, V]) Delete(key K) *Entry[K, V] {
	keyHash := s.getKeyHash(key)
	sh := s.getShard(keyHash)

	sh.mu.Lock()
	entry, existed := sh.m[key]
	if existed {
		delete(sh.m, key)
	}
	sh.mu.Unlock()

	if existed {
		s.size.Add(-1)
	}

	return entry
}

// DeleteByHash removes an entry by key when hash is already known.
func (s *ShardedStore[K, V]) DeleteByHash(key K, keyHash uint64) *Entry[K, V] {
	sh := s.getShard(keyHash)

	sh.mu.Lock()
	entry, existed := sh.m[key]
	if existed {
		delete(sh.m, key)
	}
	sh.mu.Unlock()

	if existed {
		s.size.Add(-1)
	}

	return entry
}

// Has checks if a key exists and is not expired.
func (s *ShardedStore[K, V]) Has(key K) bool {
	_, ok := s.Get(key)
	return ok
}

// Len returns the total number of entries.
func (s *ShardedStore[K, V]) Len() int {
	return int(s.size.Load())
}

// Clear removes all entries.
func (s *ShardedStore[K, V]) Clear() {
	for _, sh := range s.shards {
		sh.mu.Lock()
		sh.m = make(map[K]*Entry[K, V])
		sh.mu.Unlock()
	}
	s.size.Store(0)
}

// Range iterates over all entries, calling fn for each.
// If fn returns false, iteration stops.
// Note: This may include expired entries.
func (s *ShardedStore[K, V]) Range(fn func(entry *Entry[K, V]) bool) {
	for _, sh := range s.shards {
		sh.mu.RLock()
		for _, entry := range sh.m {
			if !fn(entry) {
				sh.mu.RUnlock()
				return
			}
		}
		sh.mu.RUnlock()
	}
}

// RangeShard iterates over entries in a specific shard.
func (s *ShardedStore[K, V]) RangeShard(shardIdx int, fn func(entry *Entry[K, V]) bool) {
	if shardIdx < 0 || shardIdx >= len(s.shards) {
		return
	}

	sh := s.shards[shardIdx]
	sh.mu.RLock()
	for _, entry := range sh.m {
		if !fn(entry) {
			break
		}
	}
	sh.mu.RUnlock()
}

// ShardCount returns the number of shards.
func (s *ShardedStore[K, V]) ShardCount() int {
	return len(s.shards)
}

// DeleteExpired removes all expired entries.
// Returns the number of entries removed.
func (s *ShardedStore[K, V]) DeleteExpired() int {
	now := clock.NowNano()
	removed := 0

	for _, sh := range s.shards {
		sh.mu.Lock()
		for key, entry := range sh.m {
			if entry.ExpireAt > 0 && now > entry.ExpireAt {
				delete(sh.m, key)
				removed++
			}
		}
		sh.mu.Unlock()
	}

	s.size.Add(-int64(removed))
	return removed
}

// CollectExpired collects entries that have expired.
// Useful for calling expiration callbacks.
func (s *ShardedStore[K, V]) CollectExpired() []*Entry[K, V] {
	now := clock.NowNano()
	var expired []*Entry[K, V]

	for _, sh := range s.shards {
		sh.mu.RLock()
		for _, entry := range sh.m {
			if entry.ExpireAt > 0 && now > entry.ExpireAt {
				expired = append(expired, entry)
			}
		}
		sh.mu.RUnlock()
	}

	return expired
}

// Keys returns all keys (may include expired entries).
func (s *ShardedStore[K, V]) Keys() []K {
	keys := make([]K, 0, s.Len())
	for _, sh := range s.shards {
		sh.mu.RLock()
		for key := range sh.m {
			keys = append(keys, key)
		}
		sh.mu.RUnlock()
	}
	return keys
}

// Entries returns all non-expired entries.
func (s *ShardedStore[K, V]) Entries() []*Entry[K, V] {
	now := clock.NowNano()
	entries := make([]*Entry[K, V], 0, s.Len())

	for _, sh := range s.shards {
		sh.mu.RLock()
		for _, entry := range sh.m {
			if entry.ExpireAt == 0 || now <= entry.ExpireAt {
				entries = append(entries, entry)
			}
		}
		sh.mu.RUnlock()
	}

	return entries
}

// Scan returns entries starting from cursor position with a limit.
// Returns entries and the next cursor position.
func (s *ShardedStore[K, V]) Scan(cursor uint64, count int) ([]*Entry[K, V], uint64) {
	if count <= 0 {
		count = 10
	}

	entries := make([]*Entry[K, V], 0, count)
	shardIdx := int(cursor >> 32)
	itemIdx := int(cursor & 0xFFFFFFFF)

	for shardIdx < len(s.shards) && len(entries) < count {
		sh := s.shards[shardIdx]

		sh.mu.RLock()
		idx := 0
		for _, entry := range sh.m {
			if idx >= itemIdx {
				entries = append(entries, entry)
				if len(entries) >= count {
					// Return cursor for next position
					nextCursor := (uint64(shardIdx) << 32) | uint64(idx+1)
					sh.mu.RUnlock()
					return entries, nextCursor
				}
			}
			idx++
		}
		sh.mu.RUnlock()

		shardIdx++
		itemIdx = 0
	}

	// Iteration complete
	return entries, 0
}

// KeyHash returns the hash function used by this store.
func (s *ShardedStore[K, V]) KeyHash(key K) uint64 {
	return s.getKeyHash(key)
}
