// Package mcache_test contains unit tests for the mcache package.
// Tests cover basic cache operations and TTL behavior.
package mcache_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"math/rand"

	"github.com/OrlovEvgeny/go-mcache"
)

// testData is a sample struct stored in the cache.
type testData struct {
	ID   int
	Name string
	Age  int
}

// newCache initializes a fresh CacheDriver for each test.
func newCache() *mcache.CacheDriver {
	return mcache.New()
}

// TestSetAndGet verifies that Set stores and Get retrieves values correctly.
func TestSetAndGet(t *testing.T) {
	cache := newCache()
	data := &testData{ID: 1, Name: "Alice", Age: 30}

	if err := cache.Set("user1", data, mcache.TTL_FOREVER); err != nil {
		t.Fatalf("Set returned an error: %v", err)
	}

	value, found := cache.Get("user1")
	if !found {
		t.Fatalf("Expected key 'user1' to exist")
	}

	retrieved, ok := value.(*testData)
	if !ok {
		t.Fatalf("Expected *testData, got %T", value)
	}

	if retrieved.Age != data.Age {
		t.Errorf("Expected Age %d, got %d", data.Age, retrieved.Age)
	}
}

// TestLenAndRemove checks that Len reflects insertions and removals.
func TestLenAndRemove(t *testing.T) {
	cache := newCache()
	cache.Set("a", &testData{}, mcache.TTL_FOREVER)
	cache.Set("b", &testData{}, mcache.TTL_FOREVER)

	if got := cache.Len(); got != 2 {
		t.Errorf("Len = %d; want 2", got)
	}

	cache.Remove("a")
	if got := cache.Len(); got != 1 {
		t.Errorf("Len after Remove = %d; want 1", got)
	}

	cache.Remove("b")
	if got := cache.Len(); got != 0 {
		t.Errorf("Len after Remove = %d; want 0", got)
	}
}

// TestExpiration ensures entries expire after their TTL elapses.
func TestExpiration(t *testing.T) {
	cache := newCache()
	cache.Set("temp", &testData{}, 50*time.Millisecond)

	time.Sleep(75 * time.Millisecond)
	_, found := cache.Get("temp")
	if found {
		t.Errorf("Expected key 'temp' to expire")
	}
}

// TestCloseReturnsRemaining verifies that Close returns all non-expired entries.
func TestCloseReturnsRemaining(t *testing.T) {
	cache := newCache()
	cache.Set("x", &testData{ID: 10}, mcache.TTL_FOREVER)
	cache.Set("y", &testData{ID: 20}, mcache.TTL_FOREVER)

	data := cache.Close()
	if _, ok := data["x"]; !ok {
		t.Errorf("Close missing key 'x'")
	}
	if _, ok := data["y"]; !ok {
		t.Errorf("Close missing key 'y'")
	}
}

func TestParallelAccess(t *testing.T) {
	cache := newCache()

	const total = 10000
	var wg sync.WaitGroup

	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", i)
			// value == key, TTL 1 minute
			cache.Set(key, key, time.Minute)
		}(i)
	}
	wg.Wait()

	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", i)
			v, found := cache.Get(key)
			if !found {
				t.Errorf("Get(%q): not found", key)
				return
			}
			s, ok := v.(string)
			if !ok || s != key {
				t.Errorf("Get(%q): expected %q, got %#v", key, key, v)
			}
		}(i)
	}
	wg.Wait()
}

func TestAccessOverwriteTTL(t *testing.T) {
	cache := newCache()

	const (
		numKeys  = 100
		totalOps = 1000
		ttl      = 2 * time.Second
	)

	var wg sync.WaitGroup

	// Pre-populate cache with some initial data
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key-%d", i)
		cache.Set(key, key, ttl)
	}

	for i := 0; i < totalOps; i++ {
		wg.Add(1)

		time.Sleep(time.Duration(rand.Int()%20) * time.Millisecond)
		go func(n int) {
			defer wg.Done()

			key := fmt.Sprintf("key-%d", n%numKeys)
			cache.Set(key, key, ttl)
		}(i)
	}

	wg.Wait()

	// We just waited on all the goroutines to finish, but the last ones should
	// still be not expired.
	if cache.Len() == 0 {
		t.Error("Cache should not be empty after parallel operations")
	}
}
