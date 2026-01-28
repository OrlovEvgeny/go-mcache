package mcache

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/OrlovEvgeny/go-mcache/internal/buffer"
	"github.com/OrlovEvgeny/go-mcache/internal/clock"
	"github.com/OrlovEvgeny/go-mcache/internal/glob"
	"github.com/OrlovEvgeny/go-mcache/internal/hash"
	"github.com/OrlovEvgeny/go-mcache/internal/policy"
	"github.com/OrlovEvgeny/go-mcache/internal/radix"
	"github.com/OrlovEvgeny/go-mcache/internal/store"
)

// Item represents an item to be stored in the cache.
type Item[K comparable, V any] struct {
	Key   K
	Value V
	Cost  int64         // 0 = auto-calculate (cost of 1)
	TTL   time.Duration // 0 = no expiration
}

// Cache is a generic, high-performance in-memory cache.
type Cache[K comparable, V any] struct {
	store       *store.ShardedStore[K, V]
	policy      *policy.Policy
	radixTree   *radix.Tree // Only for string keys
	metrics     *Metrics
	config      *config[K, V]
	writeBuffer *buffer.WriteBuffer[writeItem[K, V]]

	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	closed   atomic.Bool

	// For string key prefix/pattern operations
	isStringKey bool
}

// writeItem represents a pending write operation.
type writeItem[K comparable, V any] struct {
	entry   *store.Entry[K, V]
	isSet   bool // true = set, false = delete
}

// NewCache creates a new generic Cache with the given options.
func NewCache[K comparable, V any](opts ...Option[K, V]) *Cache[K, V] {
	cfg := defaultConfig[K, V]()
	for _, opt := range opts {
		opt(cfg)
	}

	// Determine NumCounters if not set
	if cfg.NumCounters <= 0 {
		if cfg.MaxEntries > 0 {
			cfg.NumCounters = cfg.MaxEntries * 10
		} else {
			cfg.NumCounters = 1 << 20 // Default 1M
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &Cache[K, V]{
		store:   store.NewShardedStore[K, V](cfg.ShardCount, cfg.KeyHasher),
		policy:  policy.NewPolicy(cfg.NumCounters, cfg.MaxCost, cfg.MaxEntries),
		metrics: newMetrics(),
		config:  cfg,
		ctx:     ctx,
		cancel:  cancel,
	}

	// Check if K is string for prefix search support
	var zeroK K
	if _, ok := any(zeroK).(string); ok {
		c.isStringKey = true
		c.radixTree = radix.New()
	}

	// Setup write buffer if buffering is enabled
	if cfg.BufferItems > 0 {
		c.writeBuffer = buffer.NewWriteBuffer[writeItem[K, V]](
			int(cfg.BufferItems*2),
			int(cfg.BufferItems),
			10*time.Microsecond,
			c.processWriteBatch,
		)
	}

	// Start background expiration worker
	c.wg.Add(1)
	go c.expirationWorker()

	return c
}

// Get retrieves a value from the cache.
// Returns the value and true if found, zero value and false otherwise.
func (c *Cache[K, V]) Get(key K) (V, bool) {
	var zero V

	if c.closed.Load() {
		return zero, false
	}

	keyHash := c.store.KeyHash(key)
	entry, ok := c.store.GetByHash(key, keyHash)
	if !ok {
		c.metrics.incMiss()
		return zero, false
	}

	// Update frequency in policy
	c.policy.Access(keyHash)
	c.metrics.incHit()

	return entry.Value, true
}

// Set stores a value in the cache with the given TTL.
// A TTL of 0 means the entry never expires.
// Returns true if the value was stored, false if rejected by admission policy.
func (c *Cache[K, V]) Set(key K, value V, ttl time.Duration) bool {
	return c.SetWithCost(key, value, 1, ttl)
}

// SetWithCost stores a value with a specified cost.
// Cost is used for eviction decisions when MaxCost is set.
func (c *Cache[K, V]) SetWithCost(key K, value V, cost int64, ttl time.Duration) bool {
	if c.closed.Load() {
		return false
	}

	if cost <= 0 {
		if c.config.CostFunc != nil {
			cost = c.config.CostFunc(value)
		} else {
			cost = 1
		}
	}

	// Apply default TTL if none specified
	if ttl == 0 && c.config.DefaultTTL > 0 {
		ttl = c.config.DefaultTTL
	}

	var expireAt int64
	if ttl > 0 {
		expireAt = clock.NowNano() + int64(ttl)
	}

	keyHash := c.store.KeyHash(key)

	entry := &store.Entry[K, V]{
		Key:      key,
		Value:    value,
		KeyHash:  keyHash,
		ExpireAt: expireAt,
		Cost:     cost,
	}

	if c.writeBuffer != nil {
		// Buffered write
		return c.writeBuffer.Push(writeItem[K, V]{entry: entry, isSet: true})
	}

	return c.doSet(entry)
}

// doSet performs the actual set operation.
func (c *Cache[K, V]) doSet(entry *store.Entry[K, V]) bool {
	// Check admission policy and get victims to evict
	victims, added := c.policy.Add(entry.KeyHash, entry.Cost)
	if !added {
		c.metrics.incRejection()
		if c.config.OnReject != nil {
			c.config.OnReject(entry.Key, entry.Value)
		}
		return false
	}

	// Evict victims
	for _, victimHash := range victims {
		c.evictByHash(victimHash)
	}

	// Store the entry
	prev := c.store.Set(entry)

	// Update radix tree for string keys
	if c.isStringKey && c.radixTree != nil {
		if strKey, ok := any(entry.Key).(string); ok {
			c.radixTree.Insert(strKey, entry.KeyHash)
		}
	}

	// Call eviction callback if we're replacing an existing entry
	if prev != nil && c.config.OnEvict != nil {
		c.config.OnEvict(prev.Key, prev.Value, prev.Cost)
	}

	c.metrics.incSet()
	c.metrics.addCost(entry.Cost)

	return true
}

// evictByHash evicts an entry by its key hash.
func (c *Cache[K, V]) evictByHash(keyHash uint64) {
	// Find the entry in the store
	var foundEntry *store.Entry[K, V]
	c.store.Range(func(entry *store.Entry[K, V]) bool {
		if entry.KeyHash == keyHash {
			foundEntry = entry
			return false // Stop iteration
		}
		return true
	})

	if foundEntry == nil {
		return
	}

	// Delete from store
	deleted := c.store.DeleteByHash(foundEntry.Key, keyHash)
	if deleted == nil {
		return
	}

	// Remove from radix tree
	if c.isStringKey && c.radixTree != nil {
		if strKey, ok := any(foundEntry.Key).(string); ok {
			c.radixTree.Delete(strKey)
		}
	}

	c.metrics.incEviction()
	c.metrics.addEvictedCost(deleted.Cost)

	if c.config.OnEvict != nil {
		c.config.OnEvict(deleted.Key, deleted.Value, deleted.Cost)
	}
}

// Delete removes a value from the cache.
// Returns true if the value was found and deleted.
func (c *Cache[K, V]) Delete(key K) bool {
	if c.closed.Load() {
		return false
	}

	keyHash := c.store.KeyHash(key)

	if c.writeBuffer != nil {
		// Buffered delete
		var zero V
		entry := &store.Entry[K, V]{Key: key, KeyHash: keyHash, Value: zero}
		return c.writeBuffer.Push(writeItem[K, V]{entry: entry, isSet: false})
	}

	return c.doDelete(key, keyHash)
}

// doDelete performs the actual delete operation.
func (c *Cache[K, V]) doDelete(key K, keyHash uint64) bool {
	deleted := c.store.DeleteByHash(key, keyHash)
	if deleted == nil {
		return false
	}

	// Remove from policy
	c.policy.Del(keyHash)

	// Remove from radix tree
	if c.isStringKey && c.radixTree != nil {
		if strKey, ok := any(key).(string); ok {
			c.radixTree.Delete(strKey)
		}
	}

	c.metrics.incDelete()
	return true
}

// Has checks if a key exists in the cache.
func (c *Cache[K, V]) Has(key K) bool {
	if c.closed.Load() {
		return false
	}
	return c.store.Has(key)
}

// GetMany retrieves multiple values from the cache.
// Returns a map of found keys to their values.
func (c *Cache[K, V]) GetMany(keys []K) map[K]V {
	result := make(map[K]V, len(keys))
	for _, key := range keys {
		if value, ok := c.Get(key); ok {
			result[key] = value
		}
	}
	return result
}

// SetMany stores multiple items in the cache.
// Returns the number of items successfully stored.
func (c *Cache[K, V]) SetMany(items []Item[K, V]) int {
	count := 0
	for _, item := range items {
		if c.SetWithCost(item.Key, item.Value, item.Cost, item.TTL) {
			count++
		}
	}
	return count
}

// DeleteMany removes multiple keys from the cache.
// Returns the number of keys successfully deleted.
func (c *Cache[K, V]) DeleteMany(keys []K) int {
	count := 0
	for _, key := range keys {
		if c.Delete(key) {
			count++
		}
	}
	return count
}

// Wait waits for all pending write operations to complete.
func (c *Cache[K, V]) Wait() {
	if c.writeBuffer != nil {
		c.writeBuffer.Flush()
		// Give a small amount of time for the flush to complete
		time.Sleep(time.Millisecond)
	}
}

// Scan returns an iterator over cache entries.
// cursor is the starting position (0 for beginning).
// count is the maximum number of entries to return per iteration.
func (c *Cache[K, V]) Scan(cursor uint64, count int) *Iterator[K, V] {
	return newIterator(c, cursor, count, "", nil)
}

// ScanPrefix returns an iterator over entries with keys matching the prefix.
// Only works when K is string.
func (c *Cache[K, V]) ScanPrefix(prefix string, cursor uint64, count int) *Iterator[K, V] {
	if !c.isStringKey {
		return newEmptyIterator[K, V]()
	}
	return newIterator(c, cursor, count, prefix, nil)
}

// ScanMatch returns an iterator over entries with keys matching the glob pattern.
// Only works when K is string.
// Supported patterns: * (any chars), ? (single char), [abc] (char class).
func (c *Cache[K, V]) ScanMatch(pattern string, cursor uint64, count int) *Iterator[K, V] {
	if !c.isStringKey {
		return newEmptyIterator[K, V]()
	}

	pat, err := glob.Compile(pattern)
	if err != nil {
		return newEmptyIterator[K, V]()
	}

	return newIterator(c, cursor, count, pat.Prefix(), pat)
}

// Metrics returns the cache metrics.
func (c *Cache[K, V]) Metrics() MetricsSnapshot {
	return c.metrics.Snapshot()
}

// Len returns the number of entries in the cache.
func (c *Cache[K, V]) Len() int {
	return c.store.Len()
}

// Clear removes all entries from the cache.
func (c *Cache[K, V]) Clear() {
	c.Wait()
	c.store.Clear()
	c.policy.Clear()
	if c.radixTree != nil {
		c.radixTree.Clear()
	}
	c.metrics.Reset()
}

// Close stops the cache and releases resources.
func (c *Cache[K, V]) Close() {
	if c.closed.Swap(true) {
		return // Already closed
	}

	c.cancel()

	if c.writeBuffer != nil {
		c.writeBuffer.Close()
	}

	c.wg.Wait()
}

// processWriteBatch processes a batch of pending writes.
func (c *Cache[K, V]) processWriteBatch(items []writeItem[K, V]) {
	for _, item := range items {
		if item.isSet {
			c.doSet(item.entry)
		} else {
			c.doDelete(item.entry.Key, item.entry.KeyHash)
		}
	}
}

// expirationWorker periodically removes expired entries.
func (c *Cache[K, V]) expirationWorker() {
	defer c.wg.Done()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.removeExpired()
		}
	}
}

// removeExpired removes all expired entries.
func (c *Cache[K, V]) removeExpired() {
	now := clock.NowNano()

	c.store.Range(func(entry *store.Entry[K, V]) bool {
		if entry.ExpireAt > 0 && now > entry.ExpireAt {
			// Delete expired entry
			deleted := c.store.DeleteByHash(entry.Key, entry.KeyHash)
			if deleted != nil {
				c.policy.Del(entry.KeyHash)

				if c.isStringKey && c.radixTree != nil {
					if strKey, ok := any(entry.Key).(string); ok {
						c.radixTree.Delete(strKey)
					}
				}

				c.metrics.incExpiration()

				if c.config.OnExpire != nil {
					c.config.OnExpire(entry.Key, entry.Value)
				}
			}
		}
		return true
	})
}

// defaultKeyHasher returns the default hasher for a key type.
func defaultKeyHasher[K comparable](key K) uint64 {
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
		return 0
	}
}
