// Package store provides storage backends for the cache.
package store

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/OrlovEvgeny/go-mcache/internal/clock"
	"github.com/OrlovEvgeny/go-mcache/internal/hash"
	"github.com/OrlovEvgeny/go-mcache/internal/prefetch"
)

const (
	// DefaultShardCount is the default number of shards.
	DefaultShardCount = 1024

	// cacheLineSize is the typical CPU cache line size.
	cacheLineSize = 64

	// prefetchDistance is the number of shards to prefetch ahead in batch operations.
	prefetchDistance = 4
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
	mu sync.RWMutex             // 24 bytes on 64-bit
	m  map[K]*Entry[K, V]       // 8 bytes (pointer to map header)
	_  [cacheLineSize - 32]byte // Pad to cache line boundary
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
		return fmt.Sprintf("%v", key)
	}
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

// PeekByHash retrieves an entry by key when hash is already known without
// applying expiration checks or statistics updates.
func (s *ShardedStore[K, V]) PeekByHash(key K, keyHash uint64) (*Entry[K, V], bool) {
	sh := s.getShard(keyHash)

	sh.mu.RLock()
	entry, exists := sh.m[key]
	sh.mu.RUnlock()

	return entry, exists
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

// UpdateExistingByHash updates an existing entry in-place.
// Returns the previous entry snapshot only when capturePrevious is true.
func (s *ShardedStore[K, V]) UpdateExistingByHash(
	key K,
	keyHash uint64,
	value V,
	cost int64,
	expireAt int64,
	capturePrevious bool,
) (prev *Entry[K, V], updated bool, costDelta int64, oldExpireAt int64) {
	sh := s.getShard(keyHash)

	sh.mu.Lock()
	defer sh.mu.Unlock()

	entry, exists := sh.m[key]
	if !exists {
		return nil, false, 0, 0
	}

	if capturePrevious {
		snapshot := *entry
		prev = &snapshot
	}

	oldExpireAt = entry.ExpireAt
	costDelta = cost - entry.Cost
	entry.Value = value
	entry.Cost = cost
	entry.ExpireAt = expireAt
	return prev, true, costDelta, oldExpireAt
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
				expired = append(expired, entry)
			}
		}
		sh.mu.Unlock()
	}
	if len(expired) > 0 {
		s.size.Add(-int64(len(expired)))
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
	} else {
		entry = nil
	}
	sh.mu.Unlock()

	if entry != nil {
		s.size.Add(-1)
	}
	return entry
}

// BatchRequest represents a batch get request.
type BatchRequest[K comparable, V any] struct {
	Keys    []K
	Hashes  []uint64 // Pre-computed hashes (optional)
	Results []*Entry[K, V]
	Found   []bool
}

// GetBatch retrieves multiple entries with optimized prefetching.
// This is more efficient than calling Get in a loop.
func (s *ShardedStore[K, V]) GetBatch(req *BatchRequest[K, V]) {
	n := len(req.Keys)
	if n == 0 {
		return
	}

	// Ensure results slices are properly sized
	if len(req.Results) < n {
		req.Results = make([]*Entry[K, V], n)
	}
	if len(req.Found) < n {
		req.Found = make([]bool, n)
	}

	// Compute hashes if not provided
	if len(req.Hashes) < n {
		req.Hashes = make([]uint64, n)
		for i, key := range req.Keys {
			req.Hashes[i] = s.getKeyHash(key)
		}
	}

	now := clock.NowNano()

	// Process with prefetching
	for i := 0; i < n; i++ {
		// Prefetch upcoming shards
		if i+prefetchDistance < n {
			futureHash := req.Hashes[i+prefetchDistance]
			futureShard := s.shards[futureHash&s.shardMask]
			prefetch.PrefetchT0(unsafe.Pointer(&futureShard.m))
		}

		keyHash := req.Hashes[i]
		sh := s.getShard(keyHash)

		sh.mu.RLock()
		entry, exists := sh.m[req.Keys[i]]
		sh.mu.RUnlock()

		if !exists {
			req.Results[i] = nil
			req.Found[i] = false
			continue
		}

		// Check expiration
		if entry.ExpireAt > 0 && now > entry.ExpireAt {
			req.Results[i] = nil
			req.Found[i] = false
			continue
		}

		req.Results[i] = entry
		req.Found[i] = true
	}
}

// GetBatchByShardOrder retrieves entries sorted by shard for better cache locality.
// Returns results in the original key order.
func (s *ShardedStore[K, V]) GetBatchByShardOrder(keys []K) ([]*Entry[K, V], []bool) {
	n := len(keys)
	if n == 0 {
		return nil, nil
	}

	// Compute hashes and shard indices
	type keyInfo struct {
		key       K
		hash      uint64
		shardIdx  uint64
		origIndex int
	}

	infos := make([]keyInfo, n)
	for i, key := range keys {
		h := s.getKeyHash(key)
		infos[i] = keyInfo{
			key:       key,
			hash:      h,
			shardIdx:  h & s.shardMask,
			origIndex: i,
		}
	}

	// Sort by shard index for cache locality — O(n log n)
	slices.SortFunc(infos, func(a, b keyInfo) int {
		if a.shardIdx < b.shardIdx {
			return -1
		}
		if a.shardIdx > b.shardIdx {
			return 1
		}
		return 0
	})

	results := make([]*Entry[K, V], n)
	found := make([]bool, n)
	now := clock.NowNano()

	// Process in shard order with prefetching
	for i := 0; i < n; i++ {
		info := &infos[i]

		// Prefetch upcoming shards
		if i+prefetchDistance < n {
			futureShard := s.shards[infos[i+prefetchDistance].shardIdx]
			prefetch.PrefetchT0(unsafe.Pointer(&futureShard.m))
		}

		sh := s.shards[info.shardIdx]

		sh.mu.RLock()
		entry, exists := sh.m[info.key]
		sh.mu.RUnlock()

		origIdx := info.origIndex

		if !exists {
			results[origIdx] = nil
			found[origIdx] = false
			continue
		}

		if entry.ExpireAt > 0 && now > entry.ExpireAt {
			results[origIdx] = nil
			found[origIdx] = false
			continue
		}

		results[origIdx] = entry
		found[origIdx] = true
	}

	return results, found
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
