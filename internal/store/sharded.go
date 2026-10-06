// Package store provides storage backends for the cache.
package store

import (
	"hash/maphash"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/OrlovEvgeny/go-mcache/internal/clock"
	"github.com/OrlovEvgeny/go-mcache/internal/prefetch"
)

const (
	// DefaultShardCount is the default number of shards.
	DefaultShardCount = 1024

	// cacheLineSize is the typical CPU cache line size.
	cacheLineSize = 64
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
// Optimized with cache line padding to prevent false sharing between shards.
type shard[K comparable, V any] struct {
	// Hot data: frequently accessed together
	mu     sync.RWMutex       // 24 bytes on 64-bit
	m      map[K]*Entry[K, V] // 8 bytes (pointer to map header)
	expiry *expiryWheel[K, V]
	_      [cacheLineSize - 40]byte // Pad to cache line boundary
}

// ShardedStore is a sharded in-memory store.
type ShardedStore[K comparable, V any] struct {
	shards           []*shard[K, V]
	shardMask        uint64
	size             atomic.Int64
	hasher           func(K) uint64
	seed             maphash.Seed
	expiryResolution int64
}

// NewShardedStore creates a new sharded store.
func NewShardedStore[K comparable, V any](shardCount int, hasher func(K) uint64) *ShardedStore[K, V] {
	if shardCount <= 0 {
		shardCount = DefaultShardCount
	}
	// Round up to power of 2
	shardCount = nextPowerOf2(shardCount)

	s := &ShardedStore[K, V]{
		shards:           make([]*shard[K, V], shardCount),
		shardMask:        uint64(shardCount - 1),
		hasher:           hasher,
		seed:             maphash.MakeSeed(),
		expiryResolution: int64(100 * time.Millisecond),
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
// The default hasher is the runtime's seeded maphash: it covers every
// comparable key type without boxing or allocation and resists hash flooding.
func (s *ShardedStore[K, V]) getKeyHash(key K) uint64 {
	if s.hasher != nil {
		return s.hasher(key)
	}
	return maphash.Comparable(s.seed, key)
}

// Get retrieves an entry by key.
// Returns the entry and true if found and not expired, nil and false otherwise.
func (s *ShardedStore[K, V]) Get(key K) (*Entry[K, V], bool) {
	keyHash := s.getKeyHash(key)
	sh := s.getShard(keyHash)

	// Prefetch the map header before acquiring lock
	prefetch.PrefetchT0(unsafe.Pointer(&sh.m))

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

	// Prefetch the map header before acquiring lock
	prefetch.PrefetchT0(unsafe.Pointer(&sh.m))

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

// Update replaces an existing entry under a single shard lock.
// Returns (previous entry, true) when the key existed, (nil, false) otherwise.
// Stored entries are treated as immutable after publication, so the entry
// pointer itself is stored — the caller must not mutate it afterwards.
func (s *ShardedStore[K, V]) Update(entry *Entry[K, V]) (*Entry[K, V], bool) {
	sh := s.getShard(entry.KeyHash)

	sh.mu.Lock()
	prev, exists := sh.m[entry.Key]
	if exists {
		sh.m[entry.Key] = entry
		if prev == nil || prev.ExpireAt != entry.ExpireAt {
			s.registerTTL(sh, entry, prev)
		}
	}
	sh.mu.Unlock()

	if !exists {
		return nil, false
	}
	return prev, true
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
	if prev == nil || prev.ExpireAt != entry.ExpireAt {
		s.registerTTL(sh, entry, prev)
	}
	if !existed {
		s.size.Add(1)
	}
	sh.mu.Unlock()

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
		if sh.expiry != nil {
			sh.expiry.remove(entry)
		}
	}
	if existed {
		s.size.Add(-1)
	}
	sh.mu.Unlock()

	return entry
}

// DeleteByHash removes an entry by key when hash is already known.
func (s *ShardedStore[K, V]) DeleteByHash(key K, keyHash uint64) *Entry[K, V] {
	sh := s.getShard(keyHash)

	sh.mu.Lock()
	entry, existed := sh.m[key]
	if existed {
		delete(sh.m, key)
		if sh.expiry != nil {
			sh.expiry.remove(entry)
		}
	}
	if existed {
		s.size.Add(-1)
	}
	sh.mu.Unlock()

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
		s.size.Add(-int64(len(sh.m)))
		sh.m = make(map[K]*Entry[K, V])
		sh.expiry = nil
		sh.mu.Unlock()
	}
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
	expired := s.CollectExpired(clock.NowNano())
	return len(expired)
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
//
// The cursor is the index of the next shard to read and each shard is
// returned whole: resuming mid-shard by item offset would rely on Go's
// randomized map iteration order and skip or duplicate entries between
// calls. count is a hint — a page may exceed it by up to one shard.
func (s *ShardedStore[K, V]) Scan(cursor uint64, count int) ([]*Entry[K, V], uint64) {
	if count <= 0 {
		count = 10
	}

	shardIdx := int(cursor)
	if shardIdx >= len(s.shards) {
		return nil, 0
	}

	entries := make([]*Entry[K, V], 0, count)
	for shardIdx < len(s.shards) {
		sh := s.shards[shardIdx]

		sh.mu.RLock()
		for _, entry := range sh.m {
			entries = append(entries, entry)
		}
		sh.mu.RUnlock()

		shardIdx++
		if len(entries) >= count {
			break
		}
	}

	if shardIdx >= len(s.shards) {
		return entries, 0
	}
	return entries, uint64(shardIdx)
}

// KeyHash returns the hash function used by this store.
func (s *ShardedStore[K, V]) KeyHash(key K) uint64 {
	return s.getKeyHash(key)
}

// CollectExpired atomically removes all expired entries from each shard.
// Returns the removed entries for post-processing (policy/heap/radix cleanup).
// Uses a single write lock per shard for the entire sweep.
func (s *ShardedStore[K, V]) CollectExpired(now int64) []*Entry[K, V] {
	var expired []*Entry[K, V]
	for _, sh := range s.shards {
		sh.mu.Lock()
		for key, entry := range sh.m {
			if entry.ExpireAt > 0 && now > entry.ExpireAt {
				delete(sh.m, key)
				if sh.expiry != nil {
					sh.expiry.remove(entry)
				}
				expired = append(expired, entry)
				s.size.Add(-1)
			}
		}
		sh.mu.Unlock()
	}
	return expired
}

// DeleteIfExpired deletes an entry only if the current expiration matches
// the scheduled one and the entry is expired at now.
func (s *ShardedStore[K, V]) DeleteIfExpired(key K, keyHash uint64, expireAt int64, now int64) *Entry[K, V] {
	sh := s.getShard(keyHash)

	sh.mu.Lock()
	entry, exists := sh.m[key]
	if exists && entry.ExpireAt == expireAt && entry.ExpireAt > 0 && now > entry.ExpireAt {
		delete(sh.m, key)
		if sh.expiry != nil {
			sh.expiry.remove(entry)
		}
	} else {
		entry = nil
	}
	if entry != nil {
		s.size.Add(-1)
	}
	sh.mu.Unlock()
	return entry
}

// ShardStats returns hit/miss statistics for a shard.
func (s *ShardedStore[K, V]) ShardStats(shardIdx int) (hits, misses uint64) {
	_ = shardIdx
	return 0, 0
}

// TotalStats returns aggregate hit/miss statistics across all shards.
func (s *ShardedStore[K, V]) TotalStats() (hits, misses uint64) {
	return hits, misses
}

// ResetStats resets all shard statistics.
func (s *ShardedStore[K, V]) ResetStats() {
}
