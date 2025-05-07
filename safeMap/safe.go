package safeMap

import (
	"errors"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/OrlovEvgeny/go-mcache/item"
)

const (
	defaultShardCount = 256
)

// cacheEntry holds a cache item and optional metadata.
// A separate pool isn't used because item.Item is passed externally.
// Internal allocation is unnecessary in this setup.
type cacheEntry struct {
	it item.Item
	// Metadata (e.g., access timestamp for LRU) could be added if needed.
}

// shard represents a single bucket in the sharded map.
type shard struct {
	mu sync.RWMutex
	m  map[string]item.Item
}

// Storage implements the sharded in-memory cache.
type Storage struct {
	shards    []*shard
	shardMask uint32
	size      atomic.Int64
	// Expiration is handled externally via gcmap.
}

// SafeMap defines the interface expected by mcache.go and gcmap.
type SafeMap interface {
	Insert(key string, value interface{})
	Delete(key string)
	Truncate()
	Flush(keys []string)
	Find(key string) (interface{}, bool)
	Len() int
	Close() map[string]interface{}
	RemoveIfExpired(key string, now time.Time) bool
	GetAllKeys() []string
}

// Option pattern to configure shard count.
type Option func(*options)
type options struct{ shards int }

// WithShardCount sets a custom number of shards (must be power of two).
func WithShardCount(n int) Option {
	return func(o *options) {
		if n > 0 && (n&(n-1)) == 0 {
			o.shards = n
		}
	}
}

// NewStorage initializes a new sharded SafeMap instance.
func NewStorage(opts ...Option) SafeMap {
	cfg := options{shards: defaultShardCount}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.shards <= 0 || (cfg.shards&(cfg.shards-1)) != 0 {
		panic(errors.New("safeMap: shard count must be a positive power of two"))
	}

	s := &Storage{
		shards:    make([]*shard, cfg.shards),
		shardMask: uint32(cfg.shards - 1),
	}
	for i := range s.shards {
		s.shards[i] = &shard{m: make(map[string]item.Item)}
	}
	return s
}

var _ SafeMap = (*Storage)(nil)

func (s *Storage) bucket(key string) *shard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return s.shards[h.Sum32()&s.shardMask]
}

// bucketIndex computes FNV-1a hash of key without allocations.
func (c *Storage) bucketIndex(key string) uint32 {
	const (
		offset = 2166136261
		prime  = 16777619
	)
	h := uint32(offset)
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= prime
	}
	return h & c.shardMask
}

// Insert adds or replaces a key with a given item.Item value.
func (s *Storage) Insert(key string, value interface{}) {
	it, ok := value.(item.Item)
	if !ok {
		return
	}
	b := s.bucket(key)
	b.mu.Lock()
	if _, exists := b.m[key]; !exists {
		s.size.Add(1)
	}
	b.m[key] = it
	b.mu.Unlock()
}

// Delete removes a key if present.
func (s *Storage) Delete(key string) {
	b := s.bucket(key)
	b.mu.Lock()
	if _, ok := b.m[key]; ok {
		delete(b.m, key)
		s.size.Add(-1)
	}
	b.mu.Unlock()
}

// Find retrieves the item.Item for a given key, if it exists.
// Expiration checks are done externally.
func (s *Storage) Find(key string) (interface{}, bool) {
	b := s.bucket(key)
	b.mu.RLock()
	it, ok := b.m[key]
	b.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return it, true
}

// Len returns the current number of keys.
func (s *Storage) Len() int { return int(s.size.Load()) }

// Truncate clears all entries in all shards.
func (s *Storage) Truncate() {
	for _, sh := range s.shards {
		sh.mu.Lock()
		sh.m = make(map[string]item.Item)
		sh.mu.Unlock()
	}
	s.size.Store(0)
}

// Flush removes expired keys from the provided list.
func (s *Storage) Flush(keys []string) {
	now := time.Now()
	for _, k := range keys {
		b := s.bucket(k)
		b.mu.Lock()
		if it, ok := b.m[k]; ok {
			if it.IsExpired(now) {
				delete(b.m, k)
				s.size.Add(-1)
			}
		}
		b.mu.Unlock()
	}
}

// RemoveIfExpired checks and deletes the key if it is expired.
func (s *Storage) RemoveIfExpired(key string, now time.Time) bool {
	b := s.bucket(key)
	b.mu.Lock()
	defer b.mu.Unlock()
	if it, ok := b.m[key]; ok {
		if it.IsExpired(now) {
			delete(b.m, key)
			s.size.Add(-1)
			return true
		}
	}
	return false
}

// GetAllKeys returns all keys across all shards.
func (s *Storage) GetAllKeys() []string {
	keys := make([]string, 0, s.Len())
	for _, sh := range s.shards {
		sh.mu.RLock()
		for k := range sh.m {
			keys = append(keys, k)
		}
		sh.mu.RUnlock()
	}
	return keys
}

// Close returns all non-expired items.
// This matches the original Close contract for compatibility.
func (s *Storage) Close() map[string]interface{} {
	allData := make(map[string]interface{})
	now := time.Now()
	for _, sh := range s.shards {
		sh.mu.RLock()
		for k, v := range sh.m {
			if !v.IsExpired(now) {
				allData[k] = v
			}
		}
		sh.mu.RUnlock()
	}
	return allData
}
