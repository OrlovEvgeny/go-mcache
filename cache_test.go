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

// --- Prefix search tests ---

func TestCachePrefixSearchCorrectness(t *testing.T) {
	c := NewCache[string, string](
		WithPrefixSearch[string, string](true),
	)
	defer c.Close()

	// Add entries with different prefixes
	c.Set("user:1:name", "Alice", 0)
	c.Set("user:2:name", "Bob", 0)
	c.Set("user:1:email", "alice@test.com", 0)
	c.Set("order:1", "pending", 0)
	c.Set("order:2", "shipped", 0)
	c.Set("product:1", "widget", 0)
	c.Wait()

	// Scan for "user:" prefix
	iter := c.ScanPrefix("user:", 0, 100)
	found := make(map[string]string)
	for iter.Next() {
		found[iter.Key()] = iter.Value()
	}
	if iter.Err() != nil {
		t.Fatalf("Iterator error: %v", iter.Err())
	}

	if len(found) != 3 {
		t.Errorf("Expected 3 user: entries, got %d: %v", len(found), found)
	}
	if found["user:1:name"] != "Alice" {
		t.Errorf("Expected user:1:name=Alice, got %s", found["user:1:name"])
	}
	if found["user:2:name"] != "Bob" {
		t.Errorf("Expected user:2:name=Bob, got %s", found["user:2:name"])
	}
	if found["user:1:email"] != "alice@test.com" {
		t.Errorf("Expected user:1:email=alice@test.com, got %s", found["user:1:email"])
	}

	// Scan for "order:" prefix
	iter = c.ScanPrefix("order:", 0, 100)
	found = make(map[string]string)
	for iter.Next() {
		found[iter.Key()] = iter.Value()
	}
	if len(found) != 2 {
		t.Errorf("Expected 2 order: entries, got %d: %v", len(found), found)
	}

	// Scan for non-existent prefix
	iter = c.ScanPrefix("category:", 0, 100)
	count := 0
	for iter.Next() {
		count++
	}
	if count != 0 {
		t.Errorf("Expected 0 category: entries, got %d", count)
	}
}

func TestCachePrefixSearchAfterDelete(t *testing.T) {
	c := NewCache[string, int](
		WithPrefixSearch[string, int](true),
	)
	defer c.Close()

	c.Set("user:1", 1, 0)
	c.Set("user:2", 2, 0)
	c.Set("user:3", 3, 0)
	c.Wait()

	// Delete one
	c.Delete("user:2")
	c.Wait()

	iter := c.ScanPrefix("user:", 0, 100)
	found := make(map[string]int)
	for iter.Next() {
		found[iter.Key()] = iter.Value()
	}

	if len(found) != 2 {
		t.Errorf("Expected 2 entries after delete, got %d: %v", len(found), found)
	}
	if _, ok := found["user:2"]; ok {
		t.Error("Deleted key user:2 should not appear in prefix scan")
	}
}

func TestCachePrefixSearchAfterExpiry(t *testing.T) {
	c := NewCache[string, int](
		WithPrefixSearch[string, int](true),
	)
	defer c.Close()

	c.Set("user:1", 1, 50*time.Millisecond)
	c.Set("user:2", 2, 0) // no expiry
	c.Wait()

	// Wait for first entry to expire
	time.Sleep(100 * time.Millisecond)

	iter := c.ScanPrefix("user:", 0, 100)
	found := make(map[string]int)
	for iter.Next() {
		found[iter.Key()] = iter.Value()
	}

	if len(found) != 1 {
		t.Errorf("Expected 1 non-expired entry, got %d: %v", len(found), found)
	}
	if _, ok := found["user:2"]; !ok {
		t.Error("Expected user:2 to be found")
	}
}

// --- Pattern matching tests ---

func TestCachePatternMatchCorrectness(t *testing.T) {
	c := NewCache[string, int]()
	defer c.Close()

	c.Set("user:1:name", 1, 0)
	c.Set("user:2:name", 2, 0)
	c.Set("user:1:email", 3, 0)
	c.Set("order:1", 4, 0)
	c.Wait()

	// Match user:*:name
	iter := c.ScanMatch("user:*:name", 0, 100)
	count := 0
	for iter.Next() {
		count++
	}
	if count != 2 {
		t.Errorf("Expected 2 matches for user:*:name, got %d", count)
	}

	// Match *:name
	iter = c.ScanMatch("*:name", 0, 100)
	count = 0
	for iter.Next() {
		count++
	}
	if count != 2 {
		t.Errorf("Expected 2 matches for *:name, got %d", count)
	}

	// Match user:?:name (single char wildcard)
	iter = c.ScanMatch("user:?:name", 0, 100)
	count = 0
	for iter.Next() {
		count++
	}
	if count != 2 {
		t.Errorf("Expected 2 matches for user:?:name, got %d", count)
	}

	// Match user:[12]:name (charset)
	iter = c.ScanMatch("user:[12]:name", 0, 100)
	count = 0
	for iter.Next() {
		count++
	}
	if count != 2 {
		t.Errorf("Expected 2 matches for user:[12]:name, got %d", count)
	}
}

func TestCachePatternMatchEmpty(t *testing.T) {
	c := NewCache[string, int]()
	defer c.Close()

	// Empty cache
	iter := c.ScanMatch("*", 0, 100)
	count := 0
	for iter.Next() {
		count++
	}
	if count != 0 {
		t.Errorf("Expected 0 matches on empty cache, got %d", count)
	}
}

// --- Iterator edge cases ---

func TestIteratorEmpty(t *testing.T) {
	c := NewCache[string, int]()
	defer c.Close()

	iter := c.Scan(0, 10)
	if iter.Next() {
		t.Error("Expected Next() to return false on empty cache")
	}
	if iter.Err() != nil {
		t.Errorf("Unexpected error: %v", iter.Err())
	}
}

func TestIteratorSingleElement(t *testing.T) {
	c := NewCache[string, int]()
	defer c.Close()

	c.Set("only", 42, 0)
	c.Wait()

	iter := c.Scan(0, 10)
	if !iter.Next() {
		t.Fatal("Expected Next() to return true")
	}
	if iter.Key() != "only" || iter.Value() != 42 {
		t.Errorf("Expected (only, 42), got (%s, %d)", iter.Key(), iter.Value())
	}
	if iter.Next() {
		t.Error("Expected Next() to return false after last element")
	}
}

func TestIteratorAll(t *testing.T) {
	c := NewCache[string, int]()
	defer c.Close()

	for i := 0; i < 50; i++ {
		c.Set(fmt.Sprintf("k%d", i), i, 0)
	}
	c.Wait()

	items := c.Scan(0, 10).All()
	if len(items) < 50 {
		t.Errorf("Expected at least 50 items, got %d", len(items))
	}

	// Verify all items are present
	found := make(map[string]int)
	for _, item := range items {
		found[item.Key] = item.Value
	}
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("k%d", i)
		if v, ok := found[key]; !ok {
			t.Errorf("Missing key %s", key)
		} else if v != i {
			t.Errorf("Key %s: expected %d, got %d", key, i, v)
		}
	}
}

func TestIteratorKeys(t *testing.T) {
	c := NewCache[string, int]()
	defer c.Close()

	c.Set("a", 1, 0)
	c.Set("b", 2, 0)
	c.Set("c", 3, 0)
	c.Wait()

	keys := c.Scan(0, 10).Keys()
	if len(keys) < 3 {
		t.Errorf("Expected at least 3 keys, got %d", len(keys))
	}
}

func TestIteratorValues(t *testing.T) {
	c := NewCache[string, int]()
	defer c.Close()

	c.Set("a", 1, 0)
	c.Set("b", 2, 0)
	c.Set("c", 3, 0)
	c.Wait()

	values := c.Scan(0, 10).Values()
	if len(values) < 3 {
		t.Errorf("Expected at least 3 values, got %d", len(values))
	}
}

func TestIteratorForEach(t *testing.T) {
	c := NewCache[string, int]()
	defer c.Close()

	for i := 0; i < 20; i++ {
		c.Set(fmt.Sprintf("k%d", i), i, 0)
	}
	c.Wait()

	count := 0
	c.Scan(0, 5).ForEach(func(key string, value int) bool {
		count++
		return true // continue
	})
	if count < 20 {
		t.Errorf("Expected at least 20 ForEach calls, got %d", count)
	}

	// Test early termination
	count = 0
	c.Scan(0, 5).ForEach(func(key string, value int) bool {
		count++
		return count < 5 // stop after 5
	})
	if count != 5 {
		t.Errorf("Expected ForEach to stop at 5, got %d", count)
	}
}

func TestIteratorCount(t *testing.T) {
	c := NewCache[string, int]()
	defer c.Close()

	for i := 0; i < 30; i++ {
		c.Set(fmt.Sprintf("k%d", i), i, 0)
	}
	c.Wait()

	count := c.Scan(0, 5).Count()
	if count < 30 {
		t.Errorf("Expected at least 30, got %d", count)
	}
}

// --- Wait() synchronization tests ---

func TestCacheWaitSync(t *testing.T) {
	c := NewCache[string, int](
		WithBufferItems[string, int](64),
	)
	defer c.Close()

	// With buffering enabled, Wait should ensure all writes are visible
	for i := 0; i < 100; i++ {
		c.Set(fmt.Sprintf("key%d", i), i, 0)
	}
	c.Wait()

	if c.Len() != 100 {
		t.Errorf("Expected 100 entries after Wait(), got %d", c.Len())
	}

	// Every key should be retrievable
	for i := 0; i < 100; i++ {
		val, ok := c.Get(fmt.Sprintf("key%d", i))
		if !ok {
			t.Errorf("Key key%d not found after Wait()", i)
		} else if val != i {
			t.Errorf("Key key%d: expected %d, got %d", i, i, val)
		}
	}
}

// --- Default hasher for non-primitive types ---

type customKey struct {
	ID   int
	Name string
}

func TestCacheCustomKeyType(t *testing.T) {
	c := NewCache[customKey, string]()
	defer c.Close()

	k1 := customKey{ID: 1, Name: "alice"}
	k2 := customKey{ID: 2, Name: "bob"}

	c.Set(k1, "value1", 0)
	c.Set(k2, "value2", 0)

	v1, ok := c.Get(k1)
	if !ok || v1 != "value1" {
		t.Errorf("Expected value1, got %s, ok=%v", v1, ok)
	}

	v2, ok := c.Get(k2)
	if !ok || v2 != "value2" {
		t.Errorf("Expected value2, got %s, ok=%v", v2, ok)
	}

	// Different struct should not match
	k3 := customKey{ID: 1, Name: "charlie"}
	_, ok = c.Get(k3)
	if ok {
		t.Error("Expected miss for different key")
	}
}

// --- Eviction + metrics ---

func TestCacheEvictionMetrics(t *testing.T) {
	c := NewCache[string, int](
		WithMaxEntries[string, int](5),
	)
	defer c.Close()

	for i := 0; i < 20; i++ {
		c.Set(fmt.Sprintf("key%d", i), i, 0)
		c.Wait()
	}

	m := c.Metrics()
	if m.Evictions == 0 {
		t.Error("Expected non-zero evictions with MaxEntries=5 and 20 inserts")
	}
	if m.Sets != 20 {
		t.Errorf("Expected 20 sets, got %d", m.Sets)
	}
}

func TestCacheExpirationMetrics(t *testing.T) {
	c := NewCache[string, int]()
	defer c.Close()

	c.Set("exp1", 1, 30*time.Millisecond)
	c.Set("exp2", 2, 30*time.Millisecond)
	c.Wait()

	// Expiration worker runs every 1 second — wait for it
	time.Sleep(1200 * time.Millisecond)

	m := c.Metrics()
	if m.Expirations < 2 {
		t.Errorf("Expected at least 2 expirations, got %d", m.Expirations)
	}
}

// --- Clear and Close ---

func TestCacheClear(t *testing.T) {
	c := NewCache[string, int](
		WithPrefixSearch[string, int](true),
	)
	defer c.Close()

	for i := 0; i < 50; i++ {
		c.Set(fmt.Sprintf("key%d", i), i, 0)
	}
	c.Wait()

	if c.Len() != 50 {
		t.Errorf("Expected 50, got %d", c.Len())
	}

	c.Clear()

	if c.Len() != 0 {
		t.Errorf("Expected 0 after Clear, got %d", c.Len())
	}

	// Metrics should be reset
	m := c.Metrics()
	if m.Sets != 0 {
		t.Errorf("Expected 0 sets after Clear, got %d", m.Sets)
	}
}

func TestCacheDoubleClose(t *testing.T) {
	c := NewCache[string, int]()
	c.Set("key", 1, 0)
	c.Close()
	// Second close should be a no-op, not panic
	c.Close()
}

// --- Concurrency with expiry ---

func TestCacheConcurrentSetExpiry(t *testing.T) {
	c := NewCache[int, int]()
	defer c.Close()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.Set(i, i*10, 50*time.Millisecond)
		}(i)
	}
	wg.Wait()

	// Wait for all to expire
	time.Sleep(200 * time.Millisecond)

	// All should be expired
	for i := 0; i < 50; i++ {
		if _, ok := c.Get(i); ok {
			t.Errorf("Key %d should be expired", i)
		}
	}
}
