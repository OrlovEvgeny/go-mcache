package mcache

import (
	"time"
)

// TTL_FOREVER represents an infinite TTL (no expiration).
const TTL_FOREVER = 0

// CacheDriver is the legacy v1 cache API.
//
// It is now a thin wrapper over the generic Cache engine: the old
// safeMap storage and channel-based GC worker are gone, so legacy callers
// get the sharded store and timing-wheel expiration without code changes.
type CacheDriver struct {
	cache *Cache[string, any]
}

// StartInstance is deprecated; use New instead.
func StartInstance() *CacheDriver {
	return New()
}

// New creates and initializes a new CacheDriver.
func New() *CacheDriver {
	return &CacheDriver{
		cache: NewCache[string, any](),
	}
}

// Get retrieves a value by key. Returns (value, true) if found and not expired.
func (mc *CacheDriver) Get(key string) (interface{}, bool) {
	return mc.cache.Get(key)
}

// Set inserts or updates a key with the given value and TTL.
func (mc *CacheDriver) Set(key string, value interface{}, ttl time.Duration) error {
	mc.cache.Set(key, value, ttl)
	return nil
}

// Remove deletes a key from the cache and expiration tracking.
func (mc *CacheDriver) Remove(key string) {
	mc.cache.Delete(key)
}

// Truncate clears all cache entries and pending expirations.
func (mc *CacheDriver) Truncate() {
	mc.cache.Clear()
}

// Len returns the number of current cache entries.
func (mc *CacheDriver) Len() int {
	return mc.cache.Len()
}

// GCBufferQueue returns the count of pending expirations in the GC.
//
// Deprecated: the channel-based GC queue no longer exists; expiration is
// handled by a timing wheel. Always returns 0.
func (mc *CacheDriver) GCBufferQueue() int {
	return 0
}

// Close stops the cache and returns all non-expired entries.
func (mc *CacheDriver) Close() map[string]interface{} {
	result := make(map[string]interface{}, mc.cache.Len())
	it := mc.cache.Scan(0, 512)
	for it.Next() {
		result[it.Key()] = it.Value()
	}
	mc.cache.Close()
	return result
}

// SetPointer is deprecated; use Set instead.
func (mc *CacheDriver) SetPointer(key string, value interface{}, ttl time.Duration) error {
	return mc.Set(key, value, ttl)
}

// GetPointer is deprecated; use Get instead.
func (mc *CacheDriver) GetPointer(key string) (interface{}, bool) {
	return mc.Get(key)
}
