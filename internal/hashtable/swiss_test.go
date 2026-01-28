package hashtable

import (
	"math/rand"
	"sync"
	"testing"
)

func TestSwissTableBasic(t *testing.T) {
	st := NewSwissTable(100)

	// Test insert
	if !st.Insert(123, 10) {
		t.Error("First insert should return true (new key)")
	}

	if st.Insert(123, 20) {
		t.Error("Second insert should return false (update)")
	}

	// Test get
	cost, found := st.Get(123)
	if !found {
		t.Error("Key should be found")
	}
	if cost != 20 {
		t.Errorf("Expected cost 20, got %d", cost)
	}

	// Test has
	if !st.Has(123) {
		t.Error("Has should return true")
	}
	if st.Has(456) {
		t.Error("Has should return false for non-existent key")
	}

	// Test delete
	cost, found = st.Delete(123)
	if !found {
		t.Error("Delete should find the key")
	}
	if cost != 20 {
		t.Errorf("Deleted cost should be 20, got %d", cost)
	}

	if st.Has(123) {
		t.Error("Key should not exist after delete")
	}
}

func TestSwissTableGrow(t *testing.T) {
	st := NewSwissTable(16)

	// Insert more than initial capacity
	for i := int64(0); i < 100; i++ {
		st.Insert(uint64(i), i*10)
	}

	// Verify all entries
	for i := int64(0); i < 100; i++ {
		cost, found := st.Get(uint64(i))
		if !found {
			t.Errorf("Key %d should be found", i)
		}
		if cost != i*10 {
			t.Errorf("Key %d: expected cost %d, got %d", i, i*10, cost)
		}
	}
}

func TestSwissTableSample(t *testing.T) {
	st := NewSwissTable(100)

	for i := int64(0); i < 50; i++ {
		st.Insert(uint64(i), i)
	}

	sample := st.Sample(5)
	if len(sample) != 5 {
		t.Errorf("Expected 5 samples, got %d", len(sample))
	}

	for _, entry := range sample {
		if entry.KeyHash >= 50 {
			t.Errorf("Invalid key in sample: %d", entry.KeyHash)
		}
	}
}

func TestSwissTableConcurrent(t *testing.T) {
	st := NewSwissTable(1000)
	const numGoroutines = 8
	const numOps = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(id)))
			for i := 0; i < numOps; i++ {
				key := rng.Uint64() % 500
				op := rng.Intn(3)
				switch op {
				case 0:
					st.Insert(key, int64(i))
				case 1:
					st.Get(key)
				case 2:
					st.Delete(key)
				}
			}
		}(g)
	}

	wg.Wait()
}

func TestSwissTableRange(t *testing.T) {
	st := NewSwissTable(100)

	for i := int64(0); i < 10; i++ {
		st.Insert(uint64(i), i*10)
	}

	count := 0
	st.Range(func(entry SwissEntry) bool {
		count++
		return true
	})

	if count != 10 {
		t.Errorf("Expected 10 entries in range, got %d", count)
	}
}

func BenchmarkSwissTableInsert(b *testing.B) {
	st := NewSwissTable(int64(b.N))
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st.Insert(rng.Uint64(), int64(i))
	}
}

func BenchmarkSwissTableGet(b *testing.B) {
	st := NewSwissTable(100000)
	rng := rand.New(rand.NewSource(42))

	// Pre-populate
	for i := 0; i < 100000; i++ {
		st.Insert(rng.Uint64(), int64(i))
	}

	rng = rand.New(rand.NewSource(42))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st.Get(rng.Uint64())
	}
}

func BenchmarkSwissTableParallel(b *testing.B) {
	st := NewSwissTable(100000)

	// Pre-populate
	for i := 0; i < 10000; i++ {
		st.Insert(uint64(i), int64(i))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(rand.Int63()))
		for pb.Next() {
			key := rng.Uint64() % 10000
			st.Get(key)
		}
	})
}
