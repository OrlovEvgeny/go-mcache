# go-mcache

[![Go Report Card](https://goreportcard.com/badge/github.com/OrlovEvgeny/go-mcache)](https://goreportcard.com/report/github.com/OrlovEvgeny/go-mcache)
[![GoDoc](https://pkg.go.dev/badge/github.com/OrlovEvgeny/go-mcache)](https://pkg.go.dev/github.com/OrlovEvgeny/go-mcache)
[![Tests](https://github.com/OrlovEvgeny/go-mcache/actions/workflows/test.yml/badge.svg)](https://github.com/OrlovEvgeny/go-mcache/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/OrlovEvgeny/go-mcache)](https://github.com/OrlovEvgeny/go-mcache/releases/latest)

Generic in-memory cache for Go with TinyLFU admission, sharded storage, and lock-free read path.

## Design

mcache combines two ideas to achieve both high hit ratios and high throughput:

**Admission control via TinyLFU.** Not every new item gets into the cache. On insertion, a Count-Min Sketch estimates the access frequency of the incoming item and compares it against a random sample of existing entries (SampledLFU). If the new item has lower frequency, it is rejected. This prevents one-time keys from evicting frequently accessed data — a problem that LRU caches have by design. The frequency sketch uses 4-bit counters (8 bytes per ~16 tracked keys) and resets periodically to adapt to changing access patterns.

**Lock-free read path.** The TinyLFU frequency tracking (Count-Min Sketch + Bloom filter doorkeeper) is implemented with atomic operations — no mutexes on reads. The storage layer uses 1024 shards with per-shard `sync.RWMutex` and cache-line padding between shards to eliminate false sharing. As a result, read throughput scales linearly with cores.

### Architecture

```
Cache[K, V]
├── ShardedStore         1024 shards, cache-line padded, per-shard RWMutex
│   └── map[K]*Entry     standard Go map per shard
├── Policy[K]            generic over key type (no hash-collision ambiguity)
│   ├── TinyLFU          doorkeeper (Bloom filter) + Count-Min Sketch
│   └── SampledLFU[K]    dense array + map for O(1) random sampling
├── ExpiryHeap           min-heap for TTL-based expiration, O(log n) ops
├── RadixTree            opt-in, for prefix search on string keys
├── WriteBuffer          lock-free ring buffer for async batching
└── Metrics              atomic counters, zero-cost when not read
```

### Why exact-key policy matters

The policy tracks entries by exact key `K`, not by hash. This means:
- No hash collision ambiguity — two keys with the same hash are tracked independently
- Eviction victims are returned as `{Key, KeyHash}` pairs — no reverse lookup needed
- The `SampledLFU` uses a dense array with swap-delete, so random sampling is `O(sampleSize)` instead of `O(n)` map iteration

### Expiration

Entries with TTL are tracked in a min-heap. A background goroutine sweeps expired entries once per second using a shard-local collect-and-delete: each shard is locked once for the entire sweep, expired entries are removed in bulk, and policy/radix/callback updates happen outside the shard lock.

## Install

```
go get github.com/OrlovEvgeny/go-mcache
```

Requires Go 1.23+

## Usage

```go
cache := mcache.NewCache[string, int](
    mcache.WithMaxEntries[string, int](100_000),
)
defer cache.Close()

cache.Set("key", 42, 5*time.Minute)

val, ok := cache.Get("key")  // type-safe, no assertion
```

### Cost-based eviction

```go
cache := mcache.NewCache[string, []byte](
    mcache.WithMaxCost[string, []byte](100 << 20), // 100 MB
    mcache.WithCostFunc[string, []byte](func(v []byte) int64 {
        return int64(len(v))
    }),
)

// A 10 MB value may evict multiple smaller entries
cache.Set("large", make([]byte, 10<<20), 0)
```

### Batch reads

```go
batch := cache.GetBatchOptimized(keys)
// Keys are sorted by shard index before lookup
// for sequential memory access and reduced lock contention
for i, key := range batch.Keys {
    if batch.Found[i] {
        process(key, batch.Values[i])
    }
}
```

### Prefix search (string keys, opt-in)

```go
cache := mcache.NewCache[string, int](
    mcache.WithPrefixSearch[string, int](true),
)

cache.Set("user:1:name", 1, 0)
cache.Set("user:1:email", 2, 0)
cache.Set("user:2:name", 3, 0)

iter := cache.ScanPrefix("user:1:", 0, 100)
for iter.Next() {
    fmt.Println(iter.Key()) // user:1:name, user:1:email
}
```

### Async writes

```go
cache := mcache.NewCache[string, int](
    mcache.WithBufferItems[string, int](64),
)

cache.Set("key", 1, 0)    // buffered, returns immediately
cache.Wait()               // blocks until buffer is flushed
val, _ := cache.Get("key") // guaranteed to see the value
```

When the write buffer is full, the operation falls back to synchronous execution instead of dropping the entry.

## Configuration

| Option | Description | Default |
|--------|-------------|---------|
| `WithMaxEntries` | Maximum number of entries | unlimited |
| `WithMaxCost` | Maximum total cost | unlimited |
| `WithNumCounters` | TinyLFU counters (recommend 10x max entries) | auto |
| `WithShardCount` | Number of shards (power of 2) | 1024 |
| `WithBufferItems` | Async write buffer size (0 = sync) | 0 |
| `WithDefaultTTL` | Default TTL for entries without explicit TTL | 0 (no expiry) |
| `WithCostFunc` | Custom cost calculator | cost = 1 |
| `WithKeyHasher` | Custom key hash function | auto (FNV-1a) |
| `WithLockFreePolicy` | Use lock-free TinyLFU for reads | true |
| `WithPrefixSearch` | Enable radix tree for ScanPrefix | false |
| `WithOnEvict` | Callback on eviction | nil |
| `WithOnExpire` | Callback on TTL expiration | nil |
| `WithOnReject` | Callback when TinyLFU rejects entry | nil |

### Supported key types

Built-in zero-allocation hashing for: `string`, `int`, `int8`–`int64`, `uint`–`uint64`, `float32`, `float64`, `bool`, `uintptr`. Other `comparable` types use `fmt.Sprintf` fallback — provide `WithKeyHasher` for production use with custom types.

## API

```go
// Core
cache.Set(key K, value V, ttl time.Duration) bool
cache.SetWithCost(key K, value V, cost int64, ttl time.Duration) bool
cache.Get(key K) (V, bool)
cache.Has(key K) bool
cache.Delete(key K) bool
cache.Len() int
cache.Clear()
cache.Close()
cache.Wait()

// Batch
cache.GetMany(keys []K) map[K]V
cache.GetBatch(keys []K) *BatchResult[K, V]
cache.GetBatchOptimized(keys []K) *BatchResult[K, V]
cache.SetMany(items []Item[K, V]) int
cache.DeleteMany(keys []K) int

// Iterators (cursor-based, Redis-style)
cache.Scan(cursor uint64, count int) *Iterator[K, V]
cache.ScanPrefix(prefix string, cursor uint64, count int) *Iterator[K, V]
cache.ScanMatch(pattern string, cursor uint64, count int) *Iterator[K, V]

// Iterator methods
iter.Next() bool
iter.Key() K
iter.Value() V
iter.All() []Item[K, V]
iter.Keys() []K
iter.ForEach(func(K, V) bool)
iter.Count() int
iter.Cursor() uint64
iter.Close()

// Metrics
cache.Metrics() MetricsSnapshot
// Fields: Hits, Misses, HitRatio, Sets, Deletes, Evictions,
//         Expirations, Rejections, CostAdded, CostEvicted, BufferDrops
```

## Benchmarks

Apple M4 Pro, Go 1.23. Run with `go test -bench=. -benchmem`.

```
BenchmarkCacheGet                  4,915,591     445.9 ns/op     0 B/op    0 allocs/op
BenchmarkCacheSet                  2,185,386    1061   ns/op    49 B/op    1 allocs/op
BenchmarkCacheSetWithTTL           2,511,756     973.9 ns/op    49 B/op    1 allocs/op
BenchmarkCacheMixed                4,878,026     473.3 ns/op     9 B/op    0 allocs/op
BenchmarkCacheHas                137,653,788      17.5 ns/op     0 B/op    0 allocs/op
BenchmarkCacheOperations/Read     40,523,341      58.0 ns/op     5 B/op    0 allocs/op
BenchmarkCacheZipf                   683,647    3624   ns/op    13 B/op    0 allocs/op
```

`CacheGet` includes TinyLFU frequency tracking on every access. `CacheHas` is a pure shard lookup without policy update. The difference (~17 ns vs ~446 ns) shows the cost of the admission policy — which is the tradeoff for better hit ratios on skewed workloads (`CacheZipf` achieves 87.4% hit rate).

Lock-free internals:

```
BenchmarkCMSketchLockFreeIncrement            58,410,620    20.8 ns/op    0 B/op
BenchmarkCMSketchLockFreeIncrementParallel    43,926,879    24.5 ns/op    0 B/op
BenchmarkBloomFilterLockFreeAddParallel      889,117,638     1.3 ns/op    0 B/op
BenchmarkPolicyLockFreeAccess                 35,165,865    36.3 ns/op    0 B/op
```

Parallel CM Sketch throughput degrades only ~18% from single-goroutine, showing the lock-free design scales under contention.

## Thread safety

All operations are safe for concurrent use. The concurrency model:

- **Reads**: shard RLock + lock-free TinyLFU access → no global contention
- **Writes**: shard Lock + policy mutex (held only during eviction decision)
- **Expiration**: shard-local sweep under write lock, policy/callback updates outside lock
- **Metrics**: atomic counters, no locks
- **Shards**: cache-line padded to prevent false sharing between cores

## Legacy API

The `mcache.New()` / `CacheDriver` API from v1 still works. It uses `safeMap` + GC-based expiration without TinyLFU. See `mcache.go` and `gcmap/` for details. For new code, use the generic `NewCache[K, V]` API.

## License

MIT
