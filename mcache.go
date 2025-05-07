package mcache

import (
	"context"
	"time"

	"github.com/OrlovEvgeny/go-mcache/gcmap"
	"github.com/OrlovEvgeny/go-mcache/item"
	"github.com/OrlovEvgeny/go-mcache/safeMap"
)

// TTL_FOREVER represents an infinite TTL (no expiration).
const TTL_FOREVER = 0

// initStore initializes the underlying storage and GC, returning a context and cancel function.
func (mc *CacheDriver) initStore() (context.Context, context.CancelFunc) {
	ctx, finish := context.WithCancel(context.Background())

	// Initialize sharded storage with default settings.
	mc.storage = safeMap.NewStorage()
	// Start garbage collector with lifecycle tied to ctx.
	mc.gc = gcmap.NewGC(ctx, mc.storage)
	return ctx, finish
}

// CacheDriver manages cache operations with storage and expiration.
type CacheDriver struct {
	ctx      context.Context
	closeCtx context.CancelFunc
	storage  safeMap.SafeMap
	gc       *gcmap.GC
}

// StartInstance is deprecated; use New instead.
func StartInstance() *CacheDriver {
	return New()
}

// New creates and initializes a new CacheDriver.
func New() *CacheDriver {
	cdriver := new(CacheDriver)
	// storage and gc are set up in initStore.
	ctx, finish := cdriver.initStore()
	cdriver.ctx = ctx
	cdriver.closeCtx = finish
	return cdriver
}

// Get retrieves a value by key. Returns (value, true) if found and not expired.
func (mc *CacheDriver) Get(key string) (interface{}, bool) {
	data, ok := mc.storage.Find(key)
	if !ok {
		return nil, false
	}
	entity, ok := data.(item.Item)
	if !ok {
		return nil, false
	}

	// Handle non-expiring entries.
	if entity.Expire.IsZero() {
		return entity.DataLink, true
	}
	// Passive expiration check.
	if entity.IsExpired(time.Now()) {
		return nil, false
	}
	return entity.DataLink, true
}

// Set inserts or updates a key with the given value and TTL.
func (mc *CacheDriver) Set(key string, value interface{}, ttl time.Duration) error {
	var expireTime time.Time
	if ttl == TTL_FOREVER || ttl <= 0 {
		expireTime = time.Time{}
	} else {
		expireTime = time.Now().Add(ttl)
	}

	cacheItem := item.Item{
		Key:      key,
		Expire:   expireTime,
		DataLink: value,
	}
	mc.storage.Insert(key, cacheItem)

	if !expireTime.IsZero() {
		mc.gc.Expired(key, ttl)
	} else {
		// Remove any existing expiration if setting to infinite.
		mc.gc.RemoveKey(key)
	}
	return nil
}

// Remove deletes a key from the cache and expiration tracking.
func (mc *CacheDriver) Remove(key string) {
	mc.storage.Delete(key)
	mc.gc.RemoveKey(key)
}

// Truncate clears all cache entries and pending expirations.
func (mc *CacheDriver) Truncate() {
	mc.storage.Truncate()
	mc.gc.Truncate()
}

// Len returns the number of current cache entries.
func (mc *CacheDriver) Len() int {
	return mc.storage.Len()
}

// GCBufferQueue returns the count of pending expirations in the GC.
func (mc *CacheDriver) GCBufferQueue() int {
	return mc.gc.LenBufferKeyChan()
}

// Close stops the GC and returns all non-expired entries.
func (mc *CacheDriver) Close() map[string]interface{} {
	if mc.closeCtx != nil {
		mc.closeCtx()
	}
	return mc.storage.Close()
}

// SetPointer is deprecated; use Set instead.
func (mc *CacheDriver) SetPointer(key string, value interface{}, ttl time.Duration) error {
	return mc.Set(key, value, ttl)
}

// GetPointer is deprecated; use Get instead.
func (mc *CacheDriver) GetPointer(key string) (interface{}, bool) {
	return mc.Get(key)
}
