package mcache

import "time"

// config holds the configuration for a Cache instance.
type config[K comparable, V any] struct {
	// Size limits
	MaxEntries  int64 // Maximum number of entries (0 = unlimited)
	MaxCost     int64 // Maximum total cost in bytes (0 = unlimited)
	NumCounters int64 // Number of TinyLFU counters (10x MaxEntries recommended)

	// Sharding
	ShardCount int // Number of shards (power of 2, default 1024)

	// Buffers
	BufferItems int64 // Write buffer size (default 64)

	// Callbacks
	OnEvict  func(key K, value V, cost int64) // Called when entry is evicted
	OnExpire func(key K, value V)             // Called when entry expires
	OnReject func(key K, value V)             // Called when entry is rejected by TinyLFU

	// Cost estimation
	CostFunc func(value V) int64 // Custom cost calculator

	// Key handling
	KeyHasher func(K) uint64 // Custom key hasher

	// GC settings
	DefaultTTL time.Duration // Default TTL for entries without explicit TTL

	// Storage type
	UseGCFreeStorage bool // Use GC-free storage (requires []byte values)

	// Advanced
	IgnoreInternalCost bool // Ignore internal metadata cost in cost calculations
}

// Option is a function that configures a Cache.
type Option[K comparable, V any] func(*config[K, V])

// defaultConfig returns the default configuration.
func defaultConfig[K comparable, V any]() *config[K, V] {
	return &config[K, V]{
		MaxEntries:  0,    // unlimited
		MaxCost:     0,    // unlimited
		NumCounters: 0,    // will be set based on MaxEntries
		ShardCount:  1024, // 1024 shards
		BufferItems: 0,    // No buffering by default (synchronous writes)
	}
}

// WithMaxEntries sets the maximum number of entries in the cache.
// When the limit is reached, entries are evicted using the configured policy.
// A value of 0 means unlimited entries (default).
func WithMaxEntries[K comparable, V any](n int64) Option[K, V] {
	return func(c *config[K, V]) {
		c.MaxEntries = n
	}
}

// WithMaxCost sets the maximum total cost of entries in the cache.
// Each entry's cost is determined by CostFunc or defaults to 1.
// A value of 0 means unlimited cost (default).
func WithMaxCost[K comparable, V any](cost int64) Option[K, V] {
	return func(c *config[K, V]) {
		c.MaxCost = cost
	}
}

// WithNumCounters sets the number of counters for TinyLFU frequency estimation.
// Recommended value is 10x the expected number of entries.
// A value of 0 uses a default based on MaxEntries.
func WithNumCounters[K comparable, V any](n int64) Option[K, V] {
	return func(c *config[K, V]) {
		c.NumCounters = n
	}
}

// WithShardCount sets the number of shards for concurrent access.
// Must be a power of 2. Default is 1024.
func WithShardCount[K comparable, V any](n int) Option[K, V] {
	return func(c *config[K, V]) {
		// Ensure power of 2
		if n <= 0 {
			n = 1024
		}
		// Round up to nearest power of 2
		n--
		n |= n >> 1
		n |= n >> 2
		n |= n >> 4
		n |= n >> 8
		n |= n >> 16
		n++
		c.ShardCount = n
	}
}

// WithBufferItems sets the write buffer size.
// Writes are batched in this buffer before being applied to the cache.
// Default is 64.
func WithBufferItems[K comparable, V any](n int64) Option[K, V] {
	return func(c *config[K, V]) {
		c.BufferItems = n
	}
}

// WithOnEvict sets a callback function that is called when an entry is evicted.
// The callback receives the key, value, and cost of the evicted entry.
func WithOnEvict[K comparable, V any](fn func(K, V, int64)) Option[K, V] {
	return func(c *config[K, V]) {
		c.OnEvict = fn
	}
}

// WithOnExpire sets a callback function that is called when an entry expires.
// The callback receives the key and value of the expired entry.
func WithOnExpire[K comparable, V any](fn func(K, V)) Option[K, V] {
	return func(c *config[K, V]) {
		c.OnExpire = fn
	}
}

// WithOnReject sets a callback function that is called when an entry is rejected
// by the TinyLFU admission policy.
func WithOnReject[K comparable, V any](fn func(K, V)) Option[K, V] {
	return func(c *config[K, V]) {
		c.OnReject = fn
	}
}

// WithCostFunc sets a custom function to calculate the cost of a value.
// If not set, each entry has a cost of 1.
func WithCostFunc[K comparable, V any](fn func(V) int64) Option[K, V] {
	return func(c *config[K, V]) {
		c.CostFunc = fn
	}
}

// WithKeyHasher sets a custom function to hash keys.
// If not set, a default hasher is used based on the key type.
func WithKeyHasher[K comparable, V any](fn func(K) uint64) Option[K, V] {
	return func(c *config[K, V]) {
		c.KeyHasher = fn
	}
}

// WithDefaultTTL sets the default TTL for entries that don't specify one.
// A value of 0 means no expiration (default).
func WithDefaultTTL[K comparable, V any](ttl time.Duration) Option[K, V] {
	return func(c *config[K, V]) {
		c.DefaultTTL = ttl
	}
}

// WithGCFreeStorage enables GC-free storage mode.
// This mode stores values as serialized bytes, reducing GC pressure.
// Best suited for caches with []byte values.
func WithGCFreeStorage[K comparable, V any]() Option[K, V] {
	return func(c *config[K, V]) {
		c.UseGCFreeStorage = true
	}
}

// WithStandardStorage uses the standard storage mode (default).
// This mode supports any value type but values are tracked by GC.
func WithStandardStorage[K comparable, V any]() Option[K, V] {
	return func(c *config[K, V]) {
		c.UseGCFreeStorage = false
	}
}

// WithIgnoreInternalCost configures whether internal metadata cost
// should be ignored when calculating total cache cost.
func WithIgnoreInternalCost[K comparable, V any](ignore bool) Option[K, V] {
	return func(c *config[K, V]) {
		c.IgnoreInternalCost = ignore
	}
}
