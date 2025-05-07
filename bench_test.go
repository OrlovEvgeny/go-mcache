// Package mcache_test provides benchmarks for common cache operations.
// Benchmarks are grouped and report allocations for performance analysis.
package mcache_test

import (
	"strconv"
	"testing"

	"github.com/OrlovEvgeny/go-mcache"
)

// prepareCache populates the cache with given number of entries.
func prepareCache(count int) *mcache.CacheDriver {
	c := mcache.New()
	for i := 0; i < count; i++ {
		c.Set(strconv.Itoa(i), i, mcache.TTL_FOREVER)
	}
	return c
}

func BenchmarkCacheOperations(b *testing.B) {
	const existing = 100000

	b.Run("Write", func(b *testing.B) {
		c := mcache.New()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			c.Set(strconv.Itoa(i), i, mcache.TTL_FOREVER)
		}
		b.StopTimer()
		b.Cleanup(func() {
			c.Truncate()
			c.Close()
		})
	})

	b.Run("Read", func(b *testing.B) {
		c := prepareCache(existing)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			c.Get(strconv.Itoa(i % existing))
		}
		b.StopTimer()
		b.Cleanup(func() {
			c.Truncate()
			c.Close()
		})
	})

	b.Run("WriteRead", func(b *testing.B) {
		c := mcache.New()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			k := strconv.Itoa(i)
			c.Set(k, i, mcache.TTL_FOREVER)
			c.Get(k)
		}
		b.StopTimer()
		b.Cleanup(func() {
			c.Truncate()
			c.Close()
		})
	})

	b.Run("ParallelReadWrite", func(b *testing.B) {
		c := prepareCache(existing)
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				k := strconv.Itoa(i)
				c.Set(k, i, mcache.TTL_FOREVER)
				c.Get(strconv.Itoa(i % existing))
				i++
			}
		})
		b.StopTimer()
		b.Cleanup(func() {
			c.Truncate()
			c.Close()
		})
	})
}
