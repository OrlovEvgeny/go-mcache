package mcache

import (
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"testing"
	"time"
)

func BenchmarkCacheGet(b *testing.B) {
	c := NewCache[string, int]()
	defer c.Close()

	// Pre-populate
	for i := 0; i < 10000; i++ {
		c.Set(strconv.Itoa(i), i, 0)
	}

	keys := make([]string, 10000)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		for pb.Next() {
			c.Get(keys[rng.Intn(len(keys))])
		}
	})
}

func BenchmarkCacheSet(b *testing.B) {
	c := NewCache[string, int]()
	defer c.Close()

	keys := make([]string, 10000)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		i := 0
		for pb.Next() {
			c.Set(keys[rng.Intn(len(keys))], i, 0)
			i++
		}
	})
}

func BenchmarkCacheSetWithTTL(b *testing.B) {
	c := NewCache[string, int]()
	defer c.Close()

	keys := make([]string, 10000)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		i := 0
		for pb.Next() {
			c.Set(keys[rng.Intn(len(keys))], i, time.Minute)
			i++
		}
	})
}

func BenchmarkCacheMixed(b *testing.B) {
	c := NewCache[string, int]()
	defer c.Close()

	// Pre-populate
	for i := 0; i < 10000; i++ {
		c.Set(strconv.Itoa(i), i, 0)
	}

	keys := make([]string, 10000)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		i := 0
		for pb.Next() {
			key := keys[rng.Intn(len(keys))]
			if rng.Float32() < 0.8 {
				c.Get(key)
			} else {
				c.Set(key, i, 0)
			}
			i++
		}
	})
}

func BenchmarkCacheDelete(b *testing.B) {
	c := NewCache[string, int]()
	defer c.Close()

	// Pre-populate
	for i := 0; i < b.N; i++ {
		c.Set(strconv.Itoa(i), i, 0)
	}

	keys := make([]string, b.N)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Delete(keys[i])
	}
}

func BenchmarkCacheHas(b *testing.B) {
	c := NewCache[string, int]()
	defer c.Close()

	// Pre-populate
	for i := 0; i < 10000; i++ {
		c.Set(strconv.Itoa(i), i, 0)
	}

	keys := make([]string, 10000)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		for pb.Next() {
			c.Has(keys[rng.Intn(len(keys))])
		}
	})
}

func BenchmarkCacheGetMany(b *testing.B) {
	c := NewCache[string, int]()
	defer c.Close()

	// Pre-populate
	for i := 0; i < 10000; i++ {
		c.Set(strconv.Itoa(i), i, 0)
	}

	keys := make([]string, 100)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.GetMany(keys)
	}
}

func BenchmarkCacheSetMany(b *testing.B) {
	c := NewCache[string, int]()
	defer c.Close()

	items := make([]Item[string, int], 100)
	for i := range items {
		items[i] = Item[string, int]{
			Key:   strconv.Itoa(i),
			Value: i,
			TTL:   0,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.SetMany(items)
	}
}

func BenchmarkCacheIntKey(b *testing.B) {
	c := NewCache[int, int]()
	defer c.Close()

	// Pre-populate
	for i := 0; i < 10000; i++ {
		c.Set(i, i, 0)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		for pb.Next() {
			key := rng.Intn(10000)
			if rng.Float32() < 0.8 {
				c.Get(key)
			} else {
				c.Set(key, key, 0)
			}
		}
	})
}

func BenchmarkCacheWithEviction(b *testing.B) {
	c := NewCache[string, int](
		WithMaxEntries[string, int](1000),
	)
	defer c.Close()

	keys := make([]string, 10000)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		i := 0
		for pb.Next() {
			c.Set(keys[rng.Intn(len(keys))], i, 0)
			i++
		}
	})
}

func BenchmarkCacheIteration(b *testing.B) {
	c := NewCache[string, int]()
	defer c.Close()

	// Pre-populate
	for i := 0; i < 10000; i++ {
		c.Set(strconv.Itoa(i), i, 0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		iter := c.Scan(0, 100)
		for iter.Next() {
			_ = iter.Key()
			_ = iter.Value()
		}
	}
}

// Comparison with legacy API
func BenchmarkLegacyGet(b *testing.B) {
	c := New()
	defer c.Close()

	// Pre-populate
	for i := 0; i < 10000; i++ {
		c.Set(strconv.Itoa(i), i, 0)
	}

	keys := make([]string, 10000)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		for pb.Next() {
			c.Get(keys[rng.Intn(len(keys))])
		}
	})
}

func BenchmarkLegacySet(b *testing.B) {
	c := New()
	defer c.Close()

	keys := make([]string, 10000)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		i := 0
		for pb.Next() {
			c.Set(keys[rng.Intn(len(keys))], i, 0)
			i++
		}
	})
}

func BenchmarkLegacyMixed(b *testing.B) {
	c := New()
	defer c.Close()

	// Pre-populate
	for i := 0; i < 10000; i++ {
		c.Set(strconv.Itoa(i), i, 0)
	}

	keys := make([]string, 10000)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		i := 0
		for pb.Next() {
			key := keys[rng.Intn(len(keys))]
			if rng.Float32() < 0.8 {
				c.Get(key)
			} else {
				c.Set(key, i, 0)
			}
			i++
		}
	})
}

// Zipf distribution for hit ratio testing
func BenchmarkCacheZipf(b *testing.B) {
	c := NewCache[int, int](
		WithMaxEntries[int, int](1000),
	)
	defer c.Close()

	rng := rand.New(rand.NewSource(42))
	zipf := rand.NewZipf(rng, 1.2, 1, 10000)

	// Pre-populate
	for i := 0; i < 1000; i++ {
		c.Set(i, i, 0)
	}

	b.ResetTimer()
	hits := 0
	misses := 0
	for i := 0; i < b.N; i++ {
		key := int(zipf.Uint64())
		if _, ok := c.Get(key); ok {
			hits++
		} else {
			misses++
			c.Set(key, key, 0)
		}
	}
	b.ReportMetric(float64(hits)/float64(hits+misses)*100, "hit%")
}

// Concurrent read-heavy workload
func BenchmarkCacheReadHeavy(b *testing.B) {
	c := NewCache[string, int]()
	defer c.Close()

	// Pre-populate
	for i := 0; i < 10000; i++ {
		c.Set(fmt.Sprintf("key%d", i), i, 0)
	}

	b.ResetTimer()

	var wg sync.WaitGroup
	numReaders := 8
	numWriters := 2
	opsPerGoroutine := b.N / (numReaders + numWriters)

	// Readers (80%)
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(id)))
			for i := 0; i < opsPerGoroutine; i++ {
				key := fmt.Sprintf("key%d", rng.Intn(10000))
				c.Get(key)
			}
		}(r)
	}

	// Writers (20%)
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(id + 100)))
			for i := 0; i < opsPerGoroutine; i++ {
				key := fmt.Sprintf("key%d", rng.Intn(10000))
				c.Set(key, i, 0)
			}
		}(w)
	}

	wg.Wait()
}
