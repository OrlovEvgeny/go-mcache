package safeMap

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/OrlovEvgeny/go-mcache/internal/clock"
	"github.com/OrlovEvgeny/go-mcache/item"
)

const (
	defaultShardCount    = 1024
	defaultMapPrealloc   = 256
)

// shard represents a single bucket in the sharded map.
type shard struct {
	mu sync.RWMutex
	m  map[string]*item.Item
	_  [64 - 8]byte // Cache line padding to prevent false sharing
}

// Storage implements the sharded in-memory cache.
type Storage struct {
	shards    []*shard
	shardMask uint32
	size      int64
}

// SafeMap defines the interface expected by mcache.go and gcmap.
type SafeMap interface {
	Insert(key string, value interface{})
	InsertItem(key string, it *item.Item)
	Delete(key string)
	Truncate()
	FlushKeys(keys []string, nowNano int64)
	Find(key string) (interface{}, bool)
	FindItem(key string) (*item.Item, bool)
	Len() int
	Close() map[string]interface{}
	RemoveIfExpired(key string, nowNano int64) bool
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
		s.shards[i] = &shard{m: make(map[string]*item.Item, defaultMapPrealloc)}
	}
	return s
}

var _ SafeMap = (*Storage)(nil)

func (s *Storage) bucket(key string) *shard {
	return s.shards[s.bucketIndex(key)]
}

// bucketIndex computes FNV-1a hash of key without allocations.
func (s *Storage) bucketIndex(key string) uint32 {
	const (
		offset = 2166136261
		prime  = 16777619
	)
	h := uint32(offset)
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= prime
	}
	return h & s.shardMask
}

// Insert adds or replaces a key with a given item.Item value.
// Accepts interface{} for backward compatibility.
func (s *Storage) Insert(key string, value interface{}) {
	switch v := value.(type) {
	case *item.Item:
		s.InsertItem(key, v)
	case item.Item:
		// Legacy support: convert value to pointer
		it := &item.Item{
			Key:      v.Key,
			ExpireAt: v.ExpireAt,
			Data:     v.Data,
			DataLink: v.DataLink,
		}
		s.InsertItem(key, it)
	}
}

// InsertItem adds or replaces a key with a given *item.Item pointer.
// This is the optimized path that avoids allocations.
func (s *Storage) InsertItem(key string, it *item.Item) {
	b := s.bucket(key)
	b.mu.Lock()
	if _, exists := b.m[key]; !exists {
		atomic.AddInt64(&s.size, 1)
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
		atomic.AddInt64(&s.size, -1)
	}
	b.mu.Unlock()
}

// Find retrieves the item.Item for a given key, if it exists.
// Returns interface{} for backward compatibility.
func (s *Storage) Find(key string) (interface{}, bool) {
	it, ok := s.FindItem(key)
	if !ok {
		return nil, false
	}
	return it, true
}

// FindItem retrieves the *item.Item for a given key without boxing.
// This is the optimized path that avoids interface{} allocation.
func (s *Storage) FindItem(key string) (*item.Item, bool) {
	b := s.bucket(key)
	b.mu.RLock()
	it, ok := b.m[key]
	b.mu.RUnlock()
	return it, ok
}

// Len returns the current number of keys.
func (s *Storage) Len() int {
	return int(atomic.LoadInt64(&s.size))
}

// Truncate clears all entries in all shards.
func (s *Storage) Truncate() {
	for _, sh := range s.shards {
		sh.mu.Lock()
		if len(sh.m) > 0 {
			atomic.AddInt64(&s.size, -int64(len(sh.m)))
		}
		sh.m = make(map[string]*item.Item, defaultMapPrealloc)
		sh.mu.Unlock()
	}
}

// FlushKeys removes expired keys from the provided list.
// Uses int64 Unix nano for expiration check.
func (s *Storage) FlushKeys(keys []string, nowNano int64) {
	for _, k := range keys {
		b := s.bucket(k)
		b.mu.Lock()
		if it, ok := b.m[k]; ok {
			if it.IsExpired(nowNano) {
				delete(b.m, k)
				atomic.AddInt64(&s.size, -1)
			}
		}
		b.mu.Unlock()
	}
}

// RemoveIfExpired checks and deletes the key if it is expired.
// Uses int64 Unix nano for expiration check.
func (s *Storage) RemoveIfExpired(key string, nowNano int64) bool {
	b := s.bucket(key)
	b.mu.Lock()
	defer b.mu.Unlock()
	if it, ok := b.m[key]; ok {
		if it.IsExpired(nowNano) {
			delete(b.m, key)
			atomic.AddInt64(&s.size, -1)
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
	nowNano := clock.NowNano()
	for _, sh := range s.shards {
		sh.mu.RLock()
		for k, v := range sh.m {
			if !v.IsExpired(nowNano) {
				allData[k] = v
			}
		}
		sh.mu.RUnlock()
	}
	return allData
}
