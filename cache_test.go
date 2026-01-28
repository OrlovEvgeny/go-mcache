package mcache

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"
)

func TestCacheBasicOperations(t *testing.T) {
	c := NewCache[string, int]()
	defer c.Close()

	// Test Set and Get
	c.Set("key1", 100, 0)
	val, ok := c.Get("key1")
	if !ok || val != 100 {
		t.Errorf("Expected 100, got %d, ok=%v", val, ok)
	}

	// Test Has
	if !c.Has("key1") {
		t.Error("Expected Has to return true for existing key")
	}
	if c.Has("nonexistent") {
		t.Error("Expected Has to return false for nonexistent key")
	}

	// Test Delete
	c.Delete("key1")
	if c.Has("key1") {
		t.Error("Expected key to be deleted")
	}

	// Test Len
	c.Set("a", 1, 0)
	c.Set("b", 2, 0)
	c.Set("c", 3, 0)
	if c.Len() != 3 {
		t.Errorf("Expected Len=3, got %d", c.Len())
	}

	// Test Clear
	c.Clear()
	if c.Len() != 0 {
		t.Errorf("Expected Len=0 after Clear, got %d", c.Len())
	}
}

func TestCacheExpiration(t *testing.T) {
	c := NewCache[string, int]()
	defer c.Close()

	c.Set("expire", 42, 50*time.Millisecond)

	// Should exist immediately
	val, ok := c.Get("expire")
	if !ok || val != 42 {
		t.Error("Expected value to exist before expiration")
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	_, ok = c.Get("expire")
	if ok {
		t.Error("Expected value to be expired")
	}
}

func TestCacheWithCost(t *testing.T) {
	// Cache with max cost of 100
	c := NewCache[string, []byte](
		WithMaxCost[string, []byte](100),
	)
	defer c.Close()

	// Add entries with costs
	c.SetWithCost("a", []byte("data"), 30, 0)
	c.SetWithCost("b", []byte("data"), 30, 0)
	c.SetWithCost("c", []byte("data"), 30, 0)
	c.Wait()

	// All should fit (total = 90)
	if c.Len() != 3 {
		t.Errorf("Expected 3 entries, got %d", c.Len())
	}

	// Adding more should trigger eviction
	c.SetWithCost("d", []byte("data"), 30, 0)
	c.Wait()

	// Should have 3-4 entries depending on eviction timing
	if c.Len() > 4 || c.Len() < 3 {
		t.Errorf("Expected 3-4 entries after eviction, got %d", c.Len())
	}
}

func TestCacheWithMaxEntries(t *testing.T) {
	c := NewCache[int, int](
		WithMaxEntries[int, int](5),
	)
	defer c.Close()

	// Add more entries than the limit
	for i := 0; i < 10; i++ {
		c.Set(i, i*10, 0)
		c.Wait()
	}

	// Should be limited to around 5 entries
	if c.Len() > 6 {
		t.Errorf("Expected max ~5 entries, got %d", c.Len())
	}
}

func TestCacheCallbacks(t *testing.T) {
	var evictedKeys []string
	var expiredKeys []string
	var mu sync.Mutex

	c := NewCache[string, int](
		WithOnEvict(func(key string, value int, cost int64) {
			mu.Lock()
			evictedKeys = append(evictedKeys, key)
			mu.Unlock()
		}),
		WithOnExpire(func(key string, value int) {
			mu.Lock()
			expiredKeys = append(expiredKeys, key)
			mu.Unlock()
		}),
		WithMaxEntries[string, int](3),
	)
	defer c.Close()

	// Add entries that will cause eviction
	for i := 0; i < 5; i++ {
		c.Set(fmt.Sprintf("key%d", i), i, 0)
		c.Wait()
	}

	// Add expiring entry
	c.Set("expiring", 99, 50*time.Millisecond)
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	// Should have some evictions
	t.Logf("Evicted keys: %v", evictedKeys)
	t.Logf("Expired keys: %v", expiredKeys)
	mu.Unlock()
}

func TestCacheBatchOperations(t *testing.T) {
	c := NewCache[string, int]()
	defer c.Close()

	// SetMany
	items := []Item[string, int]{
		{Key: "a", Value: 1, TTL: 0},
		{Key: "b", Value: 2, TTL: 0},
		{Key: "c", Value: 3, TTL: 0},
	}
	count := c.SetMany(items)
	if count != 3 {
		t.Errorf("Expected SetMany to store 3 items, got %d", count)
	}

	// GetMany
	result := c.GetMany([]string{"a", "b", "c", "d"})
	if len(result) != 3 {
		t.Errorf("Expected GetMany to return 3 items, got %d", len(result))
	}
	if result["a"] != 1 || result["b"] != 2 || result["c"] != 3 {
		t.Error("GetMany returned incorrect values")
	}

	// DeleteMany
	deleted := c.DeleteMany([]string{"a", "b"})
	if deleted != 2 {
		t.Errorf("Expected DeleteMany to delete 2 items, got %d", deleted)
	}
	if c.Len() != 1 {
		t.Errorf("Expected 1 item remaining, got %d", c.Len())
	}
}

func TestCacheIterator(t *testing.T) {
	c := NewCache[string, int]()
	defer c.Close()

	// Add some entries
	for i := 0; i < 100; i++ {
		c.Set(fmt.Sprintf("key%03d", i), i, 0)
	}
	c.Wait()

	// Test Scan
	iter := c.Scan(0, 10)
	count := 0
	for iter.Next() {
		count++
	}
	if err := iter.Err(); err != nil {
		t.Errorf("Iterator error: %v", err)
	}
	if count < 10 {
		t.Errorf("Expected at least 10 items from scan, got %d", count)
	}
}

func TestCachePrefixScan(t *testing.T) {
	c := NewCache[string, int]()
	defer c.Close()

	// Add entries with different prefixes
	for i := 0; i < 50; i++ {
		c.Set(fmt.Sprintf("user:%d", i), i, 0)
	}
	for i := 0; i < 30; i++ {
		c.Set(fmt.Sprintf("order:%d", i), i, 0)
	}
	c.Wait()

	// Scan for user: prefix
	iter := c.ScanPrefix("user:", 0, 100)
	count := 0
	for iter.Next() {
		count++
	}
	t.Logf("Found %d entries with 'user:' prefix", count)
}

func TestCachePatternMatch(t *testing.T) {
	c := NewCache[string, int]()
	defer c.Close()

	// Add entries
	c.Set("user:1:name", 1, 0)
	c.Set("user:2:name", 2, 0)
	c.Set("user:1:email", 3, 0)
	c.Set("order:1", 4, 0)
	c.Wait()

	// Match pattern user:*:name
	iter := c.ScanMatch("user:*:name", 0, 100)
	count := 0
	for iter.Next() {
		count++
	}
	t.Logf("Found %d entries matching 'user:*:name'", count)
}

func TestCacheMetrics(t *testing.T) {
	c := NewCache[string, int]()
	defer c.Close()

	// Generate some hits and misses
	c.Set("key1", 1, 0)
	c.Get("key1") // hit
	c.Get("key1") // hit
	c.Get("key2") // miss
	c.Get("key3") // miss

	metrics := c.Metrics()
	if metrics.Hits != 2 {
		t.Errorf("Expected 2 hits, got %d", metrics.Hits)
	}
	if metrics.Misses != 2 {
		t.Errorf("Expected 2 misses, got %d", metrics.Misses)
	}
	if metrics.HitRatio != 0.5 {
		t.Errorf("Expected hit ratio 0.5, got %f", metrics.HitRatio)
	}
}

func TestCacheParallelAccess(t *testing.T) {
	c := NewCache[int, int]()
	defer c.Close()

	const numGoroutines = 100
	const numOps = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(id)))
			for i := 0; i < numOps; i++ {
				key := rng.Intn(1000)
				if rng.Float32() < 0.7 {
					// 70% reads
					c.Get(key)
				} else {
					// 30% writes
					c.Set(key, key*10, 0)
				}
			}
		}(g)
	}

	wg.Wait()
	t.Logf("Final cache size: %d", c.Len())
}

func TestCacheWithDifferentKeyTypes(t *testing.T) {
	// Test with int keys
	intCache := NewCache[int, string]()
	defer intCache.Close()

	intCache.Set(1, "one", 0)
	intCache.Set(2, "two", 0)

	val, ok := intCache.Get(1)
	if !ok || val != "one" {
		t.Error("Int cache failed")
	}

	// Test with int64 keys
	int64Cache := NewCache[int64, string]()
	defer int64Cache.Close()

	int64Cache.Set(int64(1), "one", 0)
	val, ok = int64Cache.Get(int64(1))
	if !ok || val != "one" {
		t.Error("Int64 cache failed")
	}
}

func TestCacheWithCustomHasher(t *testing.T) {
	c := NewCache[string, int](
		WithKeyHasher[string, int](func(key string) uint64 {
			// Simple custom hasher
			h := uint64(0)
			for _, c := range key {
				h = h*31 + uint64(c)
			}
			return h
		}),
	)
	defer c.Close()

	c.Set("test", 42, 0)
	val, ok := c.Get("test")
	if !ok || val != 42 {
		t.Error("Custom hasher cache failed")
	}
}

func TestCacheWithCostFunc(t *testing.T) {
	c := NewCache[string, []byte](
		WithMaxCost[string, []byte](1000),
		WithCostFunc[string, []byte](func(v []byte) int64 {
			return int64(len(v))
		}),
	)
	defer c.Close()

	// Add entries with varying sizes
	c.Set("small", make([]byte, 100), 0)
	c.Set("medium", make([]byte, 300), 0)
	c.Set("large", make([]byte, 500), 0)
	c.Wait()

	// All should fit (total = 900)
	if c.Len() != 3 {
		t.Errorf("Expected 3 entries, got %d", c.Len())
	}
}

func TestCacheDefaultTTL(t *testing.T) {
	c := NewCache[string, int](
		WithDefaultTTL[string, int](50 * time.Millisecond),
	)
	defer c.Close()

	c.Set("key", 42, 0) // No TTL specified, should use default

	// Should exist immediately
	if _, ok := c.Get("key"); !ok {
		t.Error("Key should exist before expiration")
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	if _, ok := c.Get("key"); ok {
		t.Error("Key should be expired")
	}
}

func TestCacheShardCount(t *testing.T) {
	// Test with custom shard count
	c := NewCache[string, int](
		WithShardCount[string, int](64),
	)
	defer c.Close()

	// Basic operations should work
	c.Set("test", 123, 0)
	val, ok := c.Get("test")
	if !ok || val != 123 {
		t.Error("Cache with custom shard count failed")
	}
}
