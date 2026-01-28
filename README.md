# go-mcache

[![Go Report Card](https://goreportcard.com/badge/github.com/OrlovEvgeny/go-mcache?v1)](https://goreportcard.com/report/github.com/OrlovEvgeny/go-mcache) [![GoDoc](https://pkg.go.dev/badge/github.com/OrlovEvgeny/go-mcache)](https://pkg.go.dev/github.com/OrlovEvgeny/go-mcache)

High-performance, thread-safe in-memory cache for Go with generics, TinyLFU eviction, and Redis-style iterators.

## Features

- **Generic API** — Type-safe `Cache[K, V]` with any comparable key and value types
- **TinyLFU Eviction** — Smart admission policy for better hit ratios (inspired by Ristretto)
- **Cost-Based Eviction** — Evict by memory cost, not just entry count
- **Redis-Style Iterators** — `Scan`, `ScanPrefix`, `ScanMatch` with cursor-based pagination
- **Glob Pattern Matching** — Find keys with `*`, `?`, `[abc]`, `[a-z]` patterns
- **Prefix Search** — O(k) prefix lookups via radix tree (for string keys)
- **Callbacks** — `OnEvict`, `OnExpire`, `OnReject` hooks
- **Metrics** — Hit ratio, evictions, expirations tracking
- **GC-Free Storage** — Optional mode for reduced GC pressure (BigCache-style)
- **Zero-Allocation Reads** — Optimized read path
- **Backward Compatible** — Legacy `CacheDriver` API still works

## Installation

```bash
go get github.com/OrlovEvgeny/go-mcache
```

Requires Go 1.21+

## Quick Start

### Generic API (Recommended)

```go
package main

import (
    "fmt"
    "time"

    "github.com/OrlovEvgeny/go-mcache"
)

func main() {
    // Create a cache with string keys and int values
    cache := mcache.NewCache[string, int]()
    defer cache.Close()

    // Set with TTL
    cache.Set("counter", 42, 5*time.Minute)

    // Get (type-safe, no casting needed)
    if val, ok := cache.Get("counter"); ok {
        fmt.Printf("counter = %d\n", val)
    }

    // Delete
    cache.Delete("counter")
}
```

### Legacy API

```go
cache := mcache.New()
defer cache.Close()

cache.Set("key", "value", 5*time.Minute)

if val, ok := cache.Get("key"); ok {
    fmt.Println(val.(string))
}
```

## Configuration

```go
cache := mcache.NewCache[string, []byte](
    // Size limits
    mcache.WithMaxEntries[string, []byte](100000),      // Max 100k entries
    mcache.WithMaxCost[string, []byte](1<<30),          // Max 1GB total cost

    // Cost function (for []byte, use length)
    mcache.WithCostFunc[string, []byte](func(v []byte) int64 {
        return int64(len(v))
    }),

    // Callbacks
    mcache.WithOnEvict[string, []byte](func(key string, val []byte, cost int64) {
        fmt.Printf("evicted: %s (cost=%d)\n", key, cost)
    }),
    mcache.WithOnExpire[string, []byte](func(key string, val []byte) {
        fmt.Printf("expired: %s\n", key)
    }),

    // Performance tuning
    mcache.WithShardCount[string, []byte](2048),        // More shards = less contention
    mcache.WithNumCounters[string, []byte](1000000),    // TinyLFU counters (10x entries)
    mcache.WithBufferItems[string, []byte](64),         // Async write buffer

    // Default TTL
    mcache.WithDefaultTTL[string, []byte](time.Hour),
)
```

### All Options

| Option | Description | Default |
|--------|-------------|---------|
| `WithMaxEntries` | Maximum number of entries | unlimited |
| `WithMaxCost` | Maximum total cost | unlimited |
| `WithNumCounters` | TinyLFU counters (recommend 10x max entries) | auto |
| `WithShardCount` | Number of shards (power of 2) | 1024 |
| `WithBufferItems` | Async write buffer size (0 = sync) | 0 |
| `WithOnEvict` | Called when entry is evicted | nil |
| `WithOnExpire` | Called when entry expires | nil |
| `WithOnReject` | Called when entry rejected by TinyLFU | nil |
| `WithCostFunc` | Custom cost calculator | cost=1 |
| `WithKeyHasher` | Custom key hash function | auto |
| `WithDefaultTTL` | Default TTL for entries | 0 (no expiry) |
| `WithGCFreeStorage` | Use GC-free storage | false |

## API Reference

### Basic Operations

```go
// Set with TTL (0 = no expiration)
cache.Set(key K, value V, ttl time.Duration) bool

// Set with explicit cost
cache.SetWithCost(key K, value V, cost int64, ttl time.Duration) bool

// Get value
cache.Get(key K) (V, bool)

// Check existence
cache.Has(key K) bool

// Delete
cache.Delete(key K) bool

// Count entries
cache.Len() int

// Clear all
cache.Clear()

// Shutdown
cache.Close()
```

### Batch Operations

```go
// Get multiple keys
results := cache.GetMany([]string{"a", "b", "c"})
// returns map[string]V with found entries

// Set multiple items
items := []mcache.Item[string, int]{
    {Key: "a", Value: 1, TTL: time.Minute},
    {Key: "b", Value: 2, TTL: time.Minute},
    {Key: "c", Value: 3, Cost: 100, TTL: time.Hour},
}
count := cache.SetMany(items)

// Delete multiple keys
deleted := cache.DeleteMany([]string{"a", "b"})
```

### Iterators (Redis-style)

```go
// Scan all entries
iter := cache.Scan(0, 100)  // cursor=0, count=100
for iter.Next() {
    fmt.Printf("%v = %v\n", iter.Key(), iter.Value())
}
if err := iter.Err(); err != nil {
    log.Fatal(err)
}

// Resume from cursor
nextCursor := iter.Cursor()
iter2 := cache.Scan(nextCursor, 100)
```

### Prefix Search (string keys only)

```go
cache := mcache.NewCache[string, int]()

cache.Set("user:1:name", 1, 0)
cache.Set("user:1:email", 2, 0)
cache.Set("user:2:name", 3, 0)
cache.Set("order:1", 4, 0)

// Find all user:* keys
iter := cache.ScanPrefix("user:", 0, 100)
for iter.Next() {
    fmt.Println(iter.Key())
}
// Output:
// user:1:name
// user:1:email
// user:2:name
```

### Pattern Matching (string keys only)

```go
// Find keys matching pattern
iter := cache.ScanMatch("user:*:name", 0, 100)
for iter.Next() {
    fmt.Println(iter.Key())
}
// Output:
// user:1:name
// user:2:name
```

**Supported patterns:**
- `*` — matches any characters
- `?` — matches single character
- `**` — matches any characters including path separator
- `[abc]` — matches any character in set
- `[a-z]` — matches any character in range
- `[^abc]` — matches any character NOT in set

### Iterator Helpers

```go
// Collect all remaining entries
items := iter.All()  // []Item[K, V]

// Collect keys only
keys := iter.Keys()  // []K

// Collect values only
values := iter.Values()  // []V

// Count remaining
count := iter.Count()

// ForEach with early exit
iter.ForEach(func(key K, value V) bool {
    fmt.Println(key, value)
    return true  // continue, false to stop
})
```

### Metrics

```go
metrics := cache.Metrics()

fmt.Printf("Hits: %d\n", metrics.Hits)
fmt.Printf("Misses: %d\n", metrics.Misses)
fmt.Printf("Hit Ratio: %.2f%%\n", metrics.HitRatio*100)
fmt.Printf("Evictions: %d\n", metrics.Evictions)
fmt.Printf("Expirations: %d\n", metrics.Expirations)
fmt.Printf("Rejections: %d\n", metrics.Rejections)  // TinyLFU rejections
```

### Async Writes

```go
// Enable write buffering for higher throughput
cache := mcache.NewCache[string, int](
    mcache.WithBufferItems[string, int](64),
)

// Writes are batched asynchronously
cache.Set("key", 42, 0)

// Wait for pending writes to complete
cache.Wait()

// Now read is guaranteed to see the value
val, _ := cache.Get("key")
```

## Architecture

```
┌──────────────────────────────────────────────────────────────────────────┐
│                            Cache[K, V]                                    │
├──────────────────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────────┐  │
│  │  ShardedStore   │  │     Policy      │  │      Radix Tree         │  │
│  │  (1024 shards)  │  │ (TinyLFU+SLFU)  │  │   (prefix search)       │  │
│  │  ┌───┐ ┌───┐    │  │  ┌──────────┐   │  │                         │  │
│  │  │ 0 │ │ 1 │... │  │  │ CM Sketch│   │  │  Only for string keys   │  │
│  │  └───┘ └───┘    │  │  │ Bloom    │   │  │                         │  │
│  │  map[K]*Entry   │  │  │ SampledLFU│  │  │                         │  │
│  └─────────────────┘  │  └──────────┘   │  └─────────────────────────┘  │
│                       └─────────────────┘                                │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────────┐  │
│  │  Write Buffer   │  │    Metrics      │  │    Expiration Worker    │  │
│  │ (ring buffer)   │  │  (atomic ops)   │  │  (background goroutine) │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────────┘
```

### TinyLFU Admission Policy

When the cache is full and a new item arrives:

1. **Doorkeeper** — Bloom filter checks if item was seen before
2. **Count-Min Sketch** — Estimates access frequency (4-bit counters)
3. **Sampled LFU** — Samples 5 random victims, picks lowest frequency
4. **Admission** — New item admitted only if frequency > victim

This prevents "one-hit wonders" from evicting frequently accessed items.

### Storage Options

**Standard (default):**
- `map[K]*Entry[K,V]` per shard
- Works with any value type
- Values tracked by GC

**GC-Free (opt-in):**
- `map[uint64]uint32` + byte slices
- Values serialized, invisible to GC
- Best for `[]byte` values and large caches
- Reduces GC pause times

```go
cache := mcache.NewCache[string, []byte](
    mcache.WithGCFreeStorage[string, []byte](),
)
```

## Performance

Benchmarks on Apple M4 Pro (arm64):

### Generic API (with TinyLFU)

```
BenchmarkCacheGet-12         3328647    364 ns/op      0 B/op    0 allocs/op
BenchmarkCacheSet-12         1640882    728 ns/op     49 B/op    1 allocs/op
BenchmarkCacheMixed-12       2327482    517 ns/op      9 B/op    0 allocs/op
```

### Legacy API (no TinyLFU)

```
BenchmarkLegacyGet-12       57359919     21 ns/op      0 B/op    0 allocs/op
BenchmarkLegacySet-12        6964370    222 ns/op     72 B/op    1 allocs/op
BenchmarkLegacyMixed-12     23142158     75 ns/op     14 B/op    0 allocs/op
```

The generic API is slower due to TinyLFU policy overhead, but provides:
- Better hit ratio on skewed workloads
- Cost-based eviction
- Iterator support
- Prefix search
- Metrics

**Run benchmarks:**
```bash
go test -bench=. -benchmem
```

## Examples

### Session Cache

```go
type Session struct {
    UserID    int64
    Token     string
    ExpiresAt time.Time
}

cache := mcache.NewCache[string, *Session](
    mcache.WithMaxEntries[string, *Session](100000),
    mcache.WithDefaultTTL[string, *Session](24*time.Hour),
    mcache.WithOnExpire[string, *Session](func(key string, s *Session) {
        log.Printf("session expired: user=%d", s.UserID)
    }),
)

// Store session
cache.Set(sessionToken, &Session{
    UserID: 123,
    Token:  sessionToken,
}, 0)  // Uses default TTL

// Lookup
if session, ok := cache.Get(sessionToken); ok {
    fmt.Printf("User: %d\n", session.UserID)
}
```

### Rate Limiter

```go
cache := mcache.NewCache[string, int](
    mcache.WithDefaultTTL[string, int](time.Minute),
)

func checkRateLimit(ip string, limit int) bool {
    count, _ := cache.Get(ip)
    if count >= limit {
        return false
    }
    cache.Set(ip, count+1, 0)
    return true
}
```

### LRU-style Cache with Cost

```go
cache := mcache.NewCache[string, []byte](
    mcache.WithMaxCost[string, []byte](100<<20),  // 100MB
    mcache.WithCostFunc[string, []byte](func(v []byte) int64 {
        return int64(len(v))
    }),
)

// Large values will evict multiple small ones
cache.Set("large", make([]byte, 10<<20), 0)  // 10MB
```

### User Data with Prefix Queries

```go
cache := mcache.NewCache[string, string]()

// Store user data with namespaced keys
cache.Set("user:1:name", "Alice", 0)
cache.Set("user:1:email", "alice@example.com", 0)
cache.Set("user:2:name", "Bob", 0)
cache.Set("user:2:email", "bob@example.com", 0)

// Get all data for user 1
iter := cache.ScanPrefix("user:1:", 0, 100)
for iter.Next() {
    fmt.Printf("%s = %s\n", iter.Key(), iter.Value())
}

// Find all email keys
iter = cache.ScanMatch("user:*:email", 0, 100)
emails := iter.Values()
```

### Different Key Types

```go
// Integer keys
intCache := mcache.NewCache[int, string]()
intCache.Set(42, "answer", 0)

// Struct keys (must be comparable)
type CacheKey struct {
    Namespace string
    ID        int64
}
structCache := mcache.NewCache[CacheKey, []byte]()
structCache.Set(CacheKey{"users", 123}, data, time.Hour)
```

## Migration from v1

The legacy API (`mcache.New()`) remains fully functional. To migrate to the generic API:

```go
// Before (v1)
cache := mcache.New()
cache.Set("key", myValue, time.Hour)
val, ok := cache.Get("key")
if ok {
    typed := val.(MyType)  // Type assertion needed
}

// After (v2)
cache := mcache.NewCache[string, MyType]()
cache.Set("key", myValue, time.Hour)
val, ok := cache.Get("key")  // val is already MyType
```

## Thread Safety

All operations are thread-safe. The cache uses:
- Sharded storage with per-shard `sync.RWMutex`
- Atomic operations for metrics
- Lock-free ring buffer for async writes

## License

MIT
