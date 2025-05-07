# go-mcache

[![Go Report Card](https://goreportcard.com/badge/github.com/OrlovEvgeny/go-mcache?v1)](https://goreportcard.com/report/github.com/OrlovEvgeny/go-mcache) [![GoDoc](https://pkg.go.dev/badge/github.com/OrlovEvgeny/go-mcache)](https://pkg.go.dev/github.com/OrlovEvgeny/go-mcache)

High-performance, thread-safe in-memory key-value cache with expiration support and minimal GC pressure.

## Features

* **Sharded storage**: 256 configurable shards for lock contention reduction and horizontal scalability.
* **Single expiration scheduler**: One background goroutine and min-heap for all TTL handling, eliminating per-key timers.
* **Zero-allocation hot path**: Pooled entries and direct value storage avoid reflection and reduce GC churn.
* **Atomic counters**: O(1) length retrieval without iterating shards.
* **Standard interface**: Drop‑in replacement for `map[string]interface{}` with methods:

    * `Set(key string, value interface{}, ttl time.Duration) error`
    * `Get(key string) (value interface{}, ok bool)`
    * `Delete(key string)`
    * `Len() int`
    * `Close() map[string]interface{}`
* **Graceful shutdown**: `Close` stops expiration scheduler and returns remaining items.
* **Customization options**:

    * `WithShardCount(n int)` to adjust shard count (power of two).
    * `WithPollInterval(d time.Duration)` to set GC polling interval.
* **Extensive tests & benchmarks**: Built‑in race‑safe tests and microbenchmarks for throughput analysis.

## Installation

```bash
go get -u github.com/OrlovEvgeny/go-mcache
```

## Usage

```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/OrlovEvgeny/go-mcache"
)

type User struct {
	Name string
	Age  int
}

func main() {
	// Create cache with default settings (256 shards)
	cache := mcache.New()
	defer func() {
		// Stop internal scheduler and retrieve remaining items
		items := cache.Close()
		fmt.Printf("Remaining entries: %d\n", len(items))
	}()

	// Store a struct pointer with 20-minute TTL
	key := "user:123"
	user := &User{Name: "Alice", Age: 30}
	if err := cache.Set(key, user, 20*time.Minute); err != nil {
		log.Fatalf("Set error: %v", err)
	}

	// Retrieve value
	if v, ok := cache.Get(key); ok {
		loaded := v.(*User)
		fmt.Printf("Loaded user: %s (age %d)\n", loaded.Name, loaded.Age)
	} else {
		fmt.Println("Key expired or not found")
	}

	// Delete entry
	cache.Delete(key)
}
```

## Advanced Configuration

```go
// 512 shards and 30s GC interval
cache := mcache.New(
	mcache.WithShardCount(512),
	mcache.WithPollInterval(30*time.Second),
)
```

## Benchmarks

```bash
go test -bench . -benchmem
```

Typical results on Apple M1 Pro:

```
goos: darwin
goarch: arm64
pkg: github.com/OrlovEvgeny/go-mcache
cpu: Apple M1 Pro
BenchmarkCacheOperations
BenchmarkCacheOperations/Write
BenchmarkCacheOperations/Write-8         	 1836590	       663.0 ns/op	     605 B/op	       3 allocs/op
BenchmarkCacheOperations/Read
BenchmarkCacheOperations/Read-8          	 6350031	       167.5 ns/op	      85 B/op	       1 allocs/op
BenchmarkCacheOperations/WriteRead
BenchmarkCacheOperations/WriteRead-8     	 1786278	       704.6 ns/op	     665 B/op	       4 allocs/op
BenchmarkCacheOperations/ParallelReadWrite
BenchmarkCacheOperations/ParallelReadWrite-8         	 4477581	       288.0 ns/op	     227 B/op	       5 allocs/op
```

## Contributing

Contributions are welcome! Please fork, improve, and submit pull requests. Ensure new code includes tests and adheres to Go standards (go fmt, go vet).

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
