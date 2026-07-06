package mcache

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/OrlovEvgeny/go-mcache/internal/buffer"
	"github.com/OrlovEvgeny/go-mcache/internal/clock"
	"github.com/OrlovEvgeny/go-mcache/internal/glob"
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
	policy      policy.Policer[K]
	expiryWheel *store.ExpiryWheel[K]
	radixTree   *radix.Tree // Only for string keys
	metrics     *Metrics
	config      *config[K, V]
	writeBuffer *buffer.WriteBuffer[writeItem[K, V]]
	readBuffer  *buffer.LossyBuffer[uint64]

	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	closed   atomic.Bool
	clearMu  sync.Mutex // Serializes Clear() with removeExpired()
	liveCost atomic.Int64

	// For string key prefix/pattern operations
	isStringKey bool
}

// writeItem represents a pending write operation.
type writeItem[K comparable, V any] struct {
	entry   *store.Entry[K, V] // non-nil for set operations
	key     K                  // delete target when entry is nil
	keyHash uint64
	isSet   bool // true = set, false = delete
}

// NewCache creates a new generic Cache with the given options.
func NewCache[K comparable, V any](opts ...Option[K, V]) *Cache[K, V] {
	clock.Ensure()

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

	// Create policy based on configuration.
	// Without size/cost limits there is nothing to admit or evict, so no
	// policy is created at all: this keeps the unbounded cache from paying
	// TinyLFU updates and from tracking a second copy of every key.
	var pol policy.Policer[K]
	if cfg.MaxEntries > 0 || cfg.MaxCost > 0 {
		if cfg.UseLockFreePolicy {
			pol = policy.NewPolicyLockFree[K](cfg.NumCounters, cfg.MaxCost, cfg.MaxEntries)
		} else {
			pol = policy.NewPolicy[K](cfg.NumCounters, cfg.MaxCost, cfg.MaxEntries)
		}
	}

	c := &Cache[K, V]{
		store:       store.NewShardedStore[K, V](cfg.ShardCount, cfg.KeyHasher),
		policy:      pol,
		expiryWheel: store.NewExpiryWheel[K](cfg.ExpiryResolution),
		config:      cfg,
		ctx:         ctx,
		cancel:      cancel,
	}
	if cfg.MetricsEnabled {
		c.metrics = newMetrics()
	}

	// Check if K is string for prefix search support
	var zeroK K
	if _, ok := any(zeroK).(string); ok {
		c.isStringKey = true
		// Only create radix tree if prefix search is explicitly enabled
		if cfg.EnablePrefixSearch {
			c.radixTree = radix.New()
		}
	}

	// Setup write buffer if buffering is enabled.
	// Flushing is driven by batch-size signals; the interval is only a
	// fallback so an idle buffer doesn't wake the flush goroutine
	// tens of thousands of times per second.
	if cfg.BufferItems > 0 {
		c.writeBuffer = buffer.NewWriteBuffer[writeItem[K, V]](
			int(cfg.BufferItems*2),
			int(cfg.BufferItems),
			time.Millisecond,
			c.processWriteBatch,
		)
	}
	// The read buffer only pays off for the mutex-based policy, where it
	// batches accesses to amortize lock acquisitions. The lock-free policy's
	// Access is a handful of CAS ops on scattered sketch words, which scales
	// better than funneling every Get through one shared ring buffer tail.
	if (cfg.MaxEntries > 0 || cfg.MaxCost > 0) && !cfg.UseLockFreePolicy {
		c.readBuffer = buffer.NewLossyBuffer[uint64](
			1024,
			64,
			time.Millisecond,
			c.processReadBatch,
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
		c.metrics.incMiss(keyHash)
		return zero, false
	}

	c.recordAccess(keyHash)
	c.metrics.incHit(keyHash)

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

	// Apply default TTL if none specified or negative
	if ttl <= 0 && c.config.DefaultTTL > 0 {
		ttl = c.config.DefaultTTL
	}

	var expireAt int64
	if ttl > 0 {
		expireAt = clock.NowNano() + int64(ttl)
	}

	entry := &store.Entry[K, V]{
		Key:      key,
		Value:    value,
		KeyHash:  c.store.KeyHash(key),
		ExpireAt: expireAt,
		Cost:     cost,
	}

	if c.writeBuffer != nil {
		// Buffered write with synchronous fallback on buffer saturation
		if !c.writeBuffer.Push(writeItem[K, V]{entry: entry, isSet: true}) {
			c.metrics.incBufferDrop()
			return c.setSync(entry)
		}
		return true
	}

	return c.setSync(entry)
}

// setSync inserts or replaces an entry under a single shard lock.
// The admission policy distinguishes inserts from updates internally,
// and store.Set returns the previous entry for cost reconciliation.
func (c *Cache[K, V]) setSync(entry *store.Entry[K, V]) bool {
	if c.policy != nil {
		// Check admission policy and get victims to evict
		victims, added := c.policy.Add(entry.Key, entry.KeyHash, entry.Cost)
		if !added {
			c.metrics.incRejection()
			if c.config.OnReject != nil {
				c.config.OnReject(entry.Key, entry.Value)
			}
			return false
		}

		// Evict victims by exact key (no hash-based reverse lookup needed)
		for _, victim := range victims {
			c.evictVictim(victim)
		}
	}

	// Store the entry
	prev := c.store.Set(entry)
	if prev != nil {
		c.liveCost.Add(entry.Cost - prev.Cost)
	} else {
		c.liveCost.Add(entry.Cost)
	}

	// Schedule background expiration if entry has TTL.
	if entry.ExpireAt > 0 {
		c.expiryWheel.Schedule(entry.Key, entry.KeyHash, entry.ExpireAt)
	}

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

	c.metrics.incSet(entry.KeyHash)
	c.metrics.addCost(entry.KeyHash, entry.Cost)

	return true
}

// evictVictim evicts an entry selected by the admission policy.
// Uses exact key from the victim — no hash-based reverse lookup needed.
func (c *Cache[K, V]) evictVictim(victim policy.Victim[K]) {
	// Delete from store by exact key
	deleted := c.store.DeleteByHash(victim.Key, victim.KeyHash)
	if deleted == nil {
		return
	}
	c.liveCost.Add(-deleted.Cost)

	// Remove from radix tree
	if c.isStringKey && c.radixTree != nil {
		if strKey, ok := any(victim.Key).(string); ok {
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
		// Buffered delete with synchronous fallback on buffer saturation
		if !c.writeBuffer.Push(writeItem[K, V]{key: key, keyHash: keyHash}) {
			c.metrics.incBufferDrop()
			return c.doDelete(key, keyHash)
		}
		return true
	}

	return c.doDelete(key, keyHash)
}

// doDelete performs the actual delete operation.
func (c *Cache[K, V]) doDelete(key K, keyHash uint64) bool {
	deleted := c.store.DeleteByHash(key, keyHash)
	if deleted == nil {
		return false
	}
	c.liveCost.Add(-deleted.Cost)

	// Remove from policy
	if c.policy != nil {
		c.policy.Del(key, keyHash)
	}

	// Remove from radix tree
	if c.isStringKey && c.radixTree != nil {
		if strKey, ok := any(key).(string); ok {
			c.radixTree.Delete(strKey)
		}
	}

	c.metrics.incDelete(keyHash)
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

// BatchResult holds the result of a batch get operation.
type BatchResult[K comparable, V any] struct {
	Keys   []K
	Values []V
	Found  []bool
	Hashes []uint64
}

// GetBatch retrieves multiple values from the cache with optimized prefetching.
// This is more efficient than calling Get in a loop, especially for large batches.
func (c *Cache[K, V]) GetBatch(keys []K) *BatchResult[K, V] {
	if c.closed.Load() {
		return &BatchResult[K, V]{Keys: keys}
	}

	n := len(keys)
	result := &BatchResult[K, V]{
		Keys:   keys,
		Values: make([]V, n),
		Found:  make([]bool, n),
		Hashes: make([]uint64, n),
	}

	if n == 0 {
		return result
	}

	// Compute hashes
	for i, key := range keys {
		result.Hashes[i] = c.store.KeyHash(key)
	}

	// Use optimized batch get from store
	req := &store.BatchRequest[K, V]{
		Keys:   keys,
		Hashes: result.Hashes,
	}
	c.store.GetBatch(req)

	// Copy results and update policy
	for i := 0; i < n; i++ {
		if req.Found[i] {
			result.Values[i] = req.Results[i].Value
			result.Found[i] = true
			c.recordAccess(result.Hashes[i])
			c.metrics.incHit(result.Hashes[i])
		} else {
			c.metrics.incMiss(result.Hashes[i])
		}
	}

	return result
}

// GetBatchOptimized retrieves multiple values with shard-order optimization.
// Keys are processed in shard order for better cache locality, but results
// are returned in the original key order.
func (c *Cache[K, V]) GetBatchOptimized(keys []K) *BatchResult[K, V] {
	if c.closed.Load() {
		return &BatchResult[K, V]{Keys: keys}
	}

	n := len(keys)
	result := &BatchResult[K, V]{
		Keys:   keys,
		Values: make([]V, n),
		Found:  make([]bool, n),
		Hashes: make([]uint64, n),
	}

	if n == 0 {
		return result
	}

	// Compute hashes
	for i, key := range keys {
		result.Hashes[i] = c.store.KeyHash(key)
	}

	// Use shard-order batch get
	entries, found := c.store.GetBatchByShardOrder(keys)

	// Copy results and update policy
	for i := 0; i < n; i++ {
		if found[i] && entries[i] != nil {
			result.Values[i] = entries[i].Value
			result.Found[i] = true
			c.recordAccess(result.Hashes[i])
			c.metrics.incHit(result.Hashes[i])
		} else {
			c.metrics.incMiss(result.Hashes[i])
		}
	}

	return result
}

// GetBatchToMap retrieves multiple values and returns them as a map.
// More convenient than GetBatch when you need map access.
func (c *Cache[K, V]) GetBatchToMap(keys []K) map[K]V {
	batch := c.GetBatch(keys)
	result := make(map[K]V, len(keys))
	for i, key := range batch.Keys {
		if batch.Found[i] {
			result[key] = batch.Values[i]
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
		c.writeBuffer.FlushSync()
	}
	if c.readBuffer != nil {
		c.readBuffer.FlushSync()
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
	c.clearMu.Lock()
	defer c.clearMu.Unlock()
	c.store.Clear()
	if c.policy != nil {
		c.policy.Clear()
	}
	c.expiryWheel.Clear()
	if c.radixTree != nil {
		c.radixTree.Clear()
	}
	c.metrics.Reset()
	c.liveCost.Store(0)
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
	if c.readBuffer != nil {
		c.readBuffer.Close()
	}

	c.wg.Wait()
}

// processWriteBatch processes a batch of pending writes.
func (c *Cache[K, V]) processWriteBatch(items []writeItem[K, V]) {
	for _, item := range items {
		if item.isSet {
			c.setSync(item.entry)
		} else {
			c.doDelete(item.key, item.keyHash)
		}
	}
}

// processReadBatch replays access events in batches.
func (c *Cache[K, V]) processReadBatch(items []uint64) {
	if len(items) == 0 || c.policy == nil {
		return
	}
	if batcher, ok := c.policy.(interface{ AccessBatch([]uint64) }); ok {
		batcher.AccessBatch(items)
		return
	}
	for _, keyHash := range items {
		c.policy.Access(keyHash)
	}
}

// expirationWorker periodically removes expired entries.
func (c *Cache[K, V]) expirationWorker() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.expiryWheel.Resolution())
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

// removeExpired drains the timing wheel and deletes entries lazily if their
// stored expiration still matches the scheduled one.
func (c *Cache[K, V]) removeExpired() {
	c.clearMu.Lock()
	defer c.clearMu.Unlock()

	now := clock.NowNano()
	expired := c.expiryWheel.Advance(now)

	for _, item := range expired {
		entry := c.store.DeleteIfExpired(item.Key, item.KeyHash, item.ExpireAt, now)
		if entry == nil {
			continue
		}
		c.liveCost.Add(-entry.Cost)
		if c.policy != nil {
			c.policy.Del(entry.Key, entry.KeyHash)
		}

		if c.isStringKey && c.radixTree != nil {
			if strKey, ok := any(item.Key).(string); ok {
				c.radixTree.Delete(strKey)
			}
		}

		c.metrics.incExpiration()

		if c.config.OnExpire != nil {
			c.config.OnExpire(entry.Key, entry.Value)
		}
	}
}

func (c *Cache[K, V]) recordAccess(keyHash uint64) {
	if !c.shouldTrackAccess() {
		return
	}
	if c.readBuffer != nil {
		c.readBuffer.Push(keyHash)
		return
	}
	c.policy.Access(keyHash)
}

func (c *Cache[K, V]) shouldTrackAccess() bool {
	if c.policy == nil {
		return false
	}
	if c.config.MaxEntries > 0 && int64(c.store.Len()) >= c.config.MaxEntries/2 {
		return true
	}
	if c.config.MaxCost > 0 && c.liveCost.Load() >= c.config.MaxCost/2 {
		return true
	}
	return false
}

