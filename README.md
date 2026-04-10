# go-mcache

[![Go Report Card](https://goreportcard.com/badge/github.com/OrlovEvgeny/go-mcache)](https://goreportcard.com/report/github.com/OrlovEvgeny/go-mcache)
[![GoDoc](https://pkg.go.dev/badge/github.com/OrlovEvgeny/go-mcache)](https://pkg.go.dev/github.com/OrlovEvgeny/go-mcache)
[![Tests](https://github.com/OrlovEvgeny/go-mcache/actions/workflows/test.yml/badge.svg)](https://github.com/OrlovEvgeny/go-mcache/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/OrlovEvgeny/go-mcache)](https://github.com/OrlovEvgeny/go-mcache/releases/latest)

Generic in-memory cache for Go with TinyLFU admission, sharded storage, batched read tracking, and a timing-wheel expiration path.

## Design

mcache combines two ideas to achieve both high hit ratios and high throughput:

**Admission control via TinyLFU.** Not every new item gets into the cache. On insertion, a Count-Min Sketch estimates the access frequency of the incoming item and compares it against a random sample of existing entries (SampledLFU). If the new item has lower frequency, it is rejected. This prevents one-time keys from evicting frequently accessed data — a problem that LRU caches have by design. The frequency sketch uses 4-bit counters (8 bytes per ~16 tracked keys) and resets periodically to adapt to changing access patterns.

**Cheap read path.** Reads always go through a sharded map lookup. Frequency tracking is updated in a best-effort batched buffer once the cache becomes meaningfully occupied, which avoids paying TinyLFU bookkeeping on every hot read while the cache is still far from capacity.

### Architecture

```
Cache[K, V]
├── ShardedStore         1024 shards, cache-line padded, per-shard RWMutex
│   └── map[K]*Entry     standard Go map per shard
├── Policy[K]            generic over key type (no hash-collision ambiguity)
│   ├── TinyLFU          doorkeeper (Bloom filter) + Count-Min Sketch
│   └── SampledLFU[K]    dense array + map for O(1) random sampling
├── ExpiryWheel          coarse timing wheel for best-effort background expiry
├── RadixTree            opt-in, for prefix search on string keys
├── WriteBuffer          lock-free ring buffer for async batching
├── ReadBuffer           lossy batched policy-access replay
└── Metrics              optional atomic counters
```

### Why exact-key policy matters

The policy tracks entries by exact key `K`, not by hash. This means:
- No hash collision ambiguity — two keys with the same hash are tracked independently
- Eviction victims are returned as `{Key, KeyHash}` pairs — no reverse lookup needed
- The `SampledLFU` uses a dense array with swap-delete, so random sampling is `O(sampleSize)` instead of `O(n)` map iteration

### Expiration

Entries with TTL are scheduled into a coarse timing wheel for best-effort background cleanup. Exact TTL enforcement still happens on `Get`/`Has` by checking the entry's `ExpireAt`, while the background worker lazily removes entries whose scheduled expiration still matches the live entry.

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
| `WithMetrics` | Enable cache metrics collection | true |
| `WithExpirationResolution` | Background expiration tick resolution | 100ms |
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

The repo includes a cross-library comparison suite for local in-process caches:
`go-mcache`, `ristretto`, `bigcache`, `freecache`, `go-cache`, `ttlcache`,
`golang-lru/expirable`, `otter`, and `theine`.

Run the same suite used for the numbers below:

```bash
env GOCACHE=/tmp/go-build-cache GOTOOLCHAIN=auto \
go test -run '^$' -bench '^BenchmarkCompare/' -benchmem -benchtime=1s -count=3 .
```

Current reference run:
- machine: `Apple M4 Pro`
- metric shown in tables: median `ns/op` across `count=3` runs (`lower is better`)

The suite separates:
- core throughput (`ReadParallelHot`, `WriteParallelOverwrite`, `MixedParallel80_20`, `DeleteCycle`)
- TTL overhead (`SetWithTTLParallel`, `ExpiredRead` for precise TTL implementations)
- bounded-cache pressure (`MixedParallelZipf95_5`, `MissThenSetZipf`)

These numbers are useful as a comparative signal for this repository, but they
are still microbenchmarks on one machine. They should not be read as a universal
"best cache for every workload" claim.

Core scenarios:

| Scenario | `go-mcache` | `ristretto` | `bigcache` | `freecache` | `go-cache` | `ttlcache` | `golang-lru-expirable` | `otter` | `theine` |
|---|---|---|---|---|---|---|---|---|---|
| Core/ReadParallelHot | 8.598 | 14.640 | 51.220 | 52.180 | 120.700 | 408.300 | 319.300 | 4.919 | 8.412 |
| Core/WriteParallelOverwrite | 21.600 | 238.000 | 50.130 | 52.840 | 227.900 | 365.100 | 320.500 | 393.600 | 284.500 |
| Core/MixedParallel80_20 | 15.040 | 75.300 | 38.090 | 51.160 | 49.070 | 401.000 | 340.900 | 85.120 | 92.620 |
| Core/DeleteCycle | 148.500 | 241.700 | 62.730 | 39.670 | 36.910 | 78.370 | 85.140 | 129.500 | 197.400 |

TTL and bounded scenarios:

| Scenario | `go-mcache` | `ristretto` | `bigcache` | `freecache` | `go-cache` | `ttlcache` | `golang-lru-expirable` | `otter` | `theine` |
|---|---|---|---|---|---|---|---|---|---|
| TTL/SetWithTTLParallel | 237.000 | 507.300 | 59.800 | 53.430 | 271.900 | 261.900 | 298.600 | 401.600 | 339.300 |
| TTL/ExpiredRead | 7.032 | 20.850 | — | — | 158.100 | 350.100 | 100.900 | 3.635 | 11.010 |
| Bounded/MixedParallelZipf95_5 | 37.430 | 64.920 | 86.130 | 122.200 | — | 320.700 | 263.100 | 21.790 | 49.720 |
| Bounded/MissThenSetZipf | 22.440 | 26.600 | 54.650 | 120.700 | — | 324.400 | 262.100 | 6.258 | 9.766 |

`—` means the scenario is not part of that library's comparison set in this suite
(for example, `ExpiredRead` is only run for caches with precise TTL semantics).

## Thread safety

All operations are safe for concurrent use. The concurrency model:

- **Reads**: shard `RLock` + optional best-effort read buffering for policy replay
- **Writes**: overwrite fast path updates entries in-place when possible; inserts go through admission/eviction
- **Expiration**: timing-wheel scheduling on writes, lazy delete on background ticks
- **Metrics**: atomic counters when enabled
- **Shards**: cache-line padded to prevent false sharing between cores

## Legacy API

The `mcache.New()` / `CacheDriver` API from v1 still works. It uses `safeMap` + GC-based expiration without TinyLFU. See `mcache.go` and `gcmap/` for details. For new code, use the generic `NewCache[K, V]` API.

## License

MIT
