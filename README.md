# go-mcache

[![Go Report Card](https://goreportcard.com/badge/github.com/OrlovEvgeny/go-mcache?v1)](https://goreportcard.com/report/github.com/OrlovEvgeny/go-mcache) [![GoDoc](https://pkg.go.dev/badge/github.com/OrlovEvgeny/go-mcache)](https://pkg.go.dev/github.com/OrlovEvgeny/go-mcache)

Thread-safe in-memory cache for Go with TTL support.

## Installation

```bash
go get github.com/OrlovEvgeny/go-mcache
```

## Quick Start

```go
cache := mcache.New()
defer cache.Close()

cache.Set("key", "value", 5*time.Minute)

if val, ok := cache.Get("key"); ok {
    fmt.Println(val)
}
```

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                     CacheDriver                         │
├─────────────────────────────────────────────────────────┤
│  Storage (1024 shards)          │  GC (single goroutine)│
│  ┌─────┐ ┌─────┐     ┌─────┐   │  ┌─────────────────┐  │
│  │shard│ │shard│ ... │shard│   │  │   min-heap      │  │
│  │  0  │ │  1  │     │ N-1 │   │  │  (expirations)  │  │
│  └─────┘ └─────┘     └─────┘   │  └─────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

Storage is split into 1024 shards (configurable, must be power of 2). Each shard has its own `sync.RWMutex`. Key-to-shard mapping uses FNV-1a hash computed without allocations.

Expiration is handled by a single background goroutine with a min-heap. No per-key timers, no polling — the GC sleeps until the next expiration time.

## API

```go
// Create
cache := mcache.New()

// Write (ttl=0 means no expiration)
cache.Set(key string, value interface{}, ttl time.Duration) error

// Read
cache.Get(key string) (interface{}, bool)

// Delete
cache.Remove(key string)

// Count
cache.Len() int

// Clear all
cache.Truncate()

// Shutdown (returns remaining entries)
cache.Close() map[string]interface{}
```

## Performance

Benchmarks on Apple M4 Pro (arm64):

```
BenchmarkCacheOperationsPreallocated/Write-12       6103268    216 ns/op    71 B/op    1 allocs/op
BenchmarkCacheOperationsPreallocated/Read-12       55988211     21 ns/op     0 B/op    0 allocs/op
BenchmarkCacheOperationsPreallocated/WriteRead-12   5674326    242 ns/op    71 B/op    1 allocs/op
BenchmarkCacheOperationsPreallocated/ParallelRW-12  6378766    193 ns/op    71 B/op    1 allocs/op
```

Read path is zero-allocation. Write allocates one `Item` struct (71 bytes).

Run benchmarks:
```bash
go test -bench=. -benchmem
```

## Implementation Details

**Hashing**: FNV-1a computed inline without `hash.Hash` interface overhead.

**Time handling**: Uses cached `time.Now()` updated every 1ms in a background goroutine. Expiration timestamps stored as `int64` (Unix nano) instead of `time.Time` for faster comparisons.

**Memory layout**: Items stored as pointers in maps. Shards have 64-byte padding to prevent false sharing on cache lines.

**GC**: Single goroutine, min-heap ordered by expiration time. Wakes only when needed, no periodic polling when heap is empty.

## License

MIT
