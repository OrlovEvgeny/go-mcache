package policy

import (
	"math/rand"
	"sync"
	"testing"
)

func TestCMSketchLockFree(t *testing.T) {
	s := newCMSketchLockFree(1024)

	// Test increment and estimate
	keyHash := uint64(12345)
	s.Increment(keyHash)
	s.Increment(keyHash)
	s.Increment(keyHash)

	est := s.Estimate(keyHash)
	if est < 3 {
		t.Errorf("Expected estimate >= 3, got %d", est)
	}

	// Test reset (halving)
	s.Reset()
	estAfterReset := s.Estimate(keyHash)
	if estAfterReset > est {
		t.Errorf("Expected estimate to decrease after reset")
	}

	// Test clear
	s.Clear()
	estAfterClear := s.Estimate(keyHash)
	if estAfterClear != 0 {
		t.Errorf("Expected estimate 0 after clear, got %d", estAfterClear)
	}
}

func TestCMSketchLockFreeConcurrent(t *testing.T) {
	s := newCMSketchLockFree(1 << 16)
	const numGoroutines = 16
	const numOps = 10000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(id)))
			for i := 0; i < numOps; i++ {
				h := rng.Uint64()
				s.Increment(h)
				s.Estimate(h)
			}
		}(g)
	}

	wg.Wait()
}

func TestBloomFilterLockFree(t *testing.T) {
	bf := newBloomFilterLockFree(1000, 0.01)

	keyHash := uint64(12345)
	if bf.Contains(keyHash) {
		t.Error("New bloom filter should not contain any key")
	}

	bf.Add(keyHash)
	if !bf.Contains(keyHash) {
		t.Error("Bloom filter should contain added key")
	}

	bf.Add(uint64(67890))
	if !bf.Contains(uint64(67890)) {
		t.Error("Bloom filter should contain second key")
	}

	bf.Reset()
	if bf.Contains(keyHash) {
		t.Error("Bloom filter should be empty after reset")
	}
}

func TestBloomFilterLockFreeConcurrent(t *testing.T) {
	bf := newBloomFilterLockFree(10000, 0.01)
	const numGoroutines = 16
	const numOps = 10000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(id)))
			for i := 0; i < numOps; i++ {
				h := rng.Uint64()
				bf.Add(h)
				bf.Contains(h)
			}
		}(g)
	}

	wg.Wait()
}

func TestTinyLFULockFree(t *testing.T) {
	lfu := NewTinyLFULockFree(1000)

	keyHash := uint64(12345)
	for i := 0; i < 10; i++ {
		lfu.Increment(keyHash)
	}

	est := lfu.Estimate(keyHash)
	if est < 5 {
		t.Errorf("Expected estimate >= 5, got %d", est)
	}

	hotKey := uint64(11111)
	coldKey := uint64(22222)

	for i := 0; i < 100; i++ {
		lfu.Increment(hotKey)
	}
	lfu.Increment(coldKey)

	if !lfu.Admit(hotKey, coldKey) {
		t.Error("Hot key should be admitted over cold key")
	}
}

func TestTinyLFULockFreeConcurrent(t *testing.T) {
	lfu := NewTinyLFULockFree(1 << 16)
	const numGoroutines = 16
	const numOps = 10000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(id)))
			for i := 0; i < numOps; i++ {
				h := rng.Uint64()
				lfu.Increment(h)
				lfu.Estimate(h)
			}
		}(g)
	}

	wg.Wait()
}

func TestPolicyLockFree(t *testing.T) {
	p := NewPolicyLockFree(1000, 100, 0)

	_, added := p.Add(1, 30)
	if !added {
		t.Error("First entry should be added")
	}

	_, added = p.Add(2, 30)
	if !added {
		t.Error("Second entry should be added")
	}

	_, added = p.Add(3, 30)
	if !added {
		t.Error("Third entry should be added")
	}

	if !p.Has(1) {
		t.Error("Key 1 should exist")
	}

	p.Access(1)
	p.Access(1)
	p.Access(1)
}

func TestPolicyLockFreeConcurrentAccess(t *testing.T) {
	p := NewPolicyLockFree(1 << 16, 0, 0)
	const numGoroutines = 16
	const numOps = 10000

	// Pre-add some keys
	for i := 0; i < 100; i++ {
		p.Add(uint64(i), 1)
	}

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(id)))
			for i := 0; i < numOps; i++ {
				h := uint64(rng.Intn(100))
				p.Access(h)
			}
		}(g)
	}

	wg.Wait()
}

func TestBlockedBloomFilter(t *testing.T) {
	bf := NewBlockedBloomFilter(10000, 0.01)

	keyHash := uint64(12345)
	if bf.Contains(keyHash) {
		t.Error("New bloom filter should not contain any key")
	}

	bf.Add(keyHash)
	if !bf.Contains(keyHash) {
		t.Error("Blocked bloom filter should contain added key")
	}

	bf.Reset()
	if bf.Contains(keyHash) {
		t.Error("Blocked bloom filter should be empty after reset")
	}
}

func TestCuckooFilter(t *testing.T) {
	cf := NewCuckooFilter(1000, 0.01)

	keyHash := uint64(12345)
	if cf.Contains(keyHash) {
		t.Error("New cuckoo filter should not contain any key")
	}

	if !cf.Add(keyHash) {
		t.Error("Should be able to add key")
	}

	if !cf.Contains(keyHash) {
		t.Error("Cuckoo filter should contain added key")
	}

	if !cf.Delete(keyHash) {
		t.Error("Should be able to delete key")
	}

	if cf.Contains(keyHash) {
		t.Error("Cuckoo filter should not contain deleted key")
	}
}

func BenchmarkCMSketchLockFreeIncrement(b *testing.B) {
	s := newCMSketchLockFree(1 << 20)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Increment(rng.Uint64())
	}
}

func BenchmarkCMSketchLockFreeIncrementParallel(b *testing.B) {
	s := newCMSketchLockFree(1 << 20)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(rand.Int63()))
		for pb.Next() {
			s.Increment(rng.Uint64())
		}
	})
}

func BenchmarkBloomFilterLockFreeAdd(b *testing.B) {
	bf := newBloomFilterLockFree(1000000, 0.01)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.Add(rng.Uint64())
	}
}

func BenchmarkBloomFilterLockFreeAddParallel(b *testing.B) {
	bf := newBloomFilterLockFree(1000000, 0.01)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(rand.Int63()))
		for pb.Next() {
			bf.Add(rng.Uint64())
		}
	})
}

func BenchmarkTinyLFULockFreeIncrement(b *testing.B) {
	lfu := NewTinyLFULockFree(1 << 20)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lfu.Increment(rng.Uint64())
	}
}

func BenchmarkTinyLFULockFreeIncrementParallel(b *testing.B) {
	lfu := NewTinyLFULockFree(1 << 20)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(rand.Int63()))
		for pb.Next() {
			lfu.Increment(rng.Uint64())
		}
	})
}

func BenchmarkPolicyLockFreeAccess(b *testing.B) {
	p := NewPolicyLockFree(1<<20, 0, 0)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Access(rng.Uint64())
	}
}

func BenchmarkPolicyLockFreeAccessParallel(b *testing.B) {
	p := NewPolicyLockFree(1<<20, 0, 0)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(rand.Int63()))
		for pb.Next() {
			p.Access(rng.Uint64())
		}
	})
}

func BenchmarkBlockedBloomFilterAdd(b *testing.B) {
	bf := NewBlockedBloomFilter(1000000, 0.01)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.Add(rng.Uint64())
	}
}

func BenchmarkBlockedBloomFilterContains(b *testing.B) {
	bf := NewBlockedBloomFilter(1000000, 0.01)
	rng := rand.New(rand.NewSource(42))

	// Pre-populate
	for i := 0; i < 100000; i++ {
		bf.Add(rng.Uint64())
	}

	rng = rand.New(rand.NewSource(42))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.Contains(rng.Uint64())
	}
}

func BenchmarkCuckooFilterAdd(b *testing.B) {
	cf := NewCuckooFilter(1000000, 0.01)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cf.Add(rng.Uint64())
	}
}

func BenchmarkCuckooFilterContains(b *testing.B) {
	cf := NewCuckooFilter(1000000, 0.01)
	rng := rand.New(rand.NewSource(42))

	// Pre-populate
	for i := 0; i < 100000; i++ {
		cf.Add(rng.Uint64())
	}

	rng = rand.New(rand.NewSource(42))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cf.Contains(rng.Uint64())
	}
}

// Compare old vs new implementations
func BenchmarkCMSketchOriginalIncrement(b *testing.B) {
	s := newCMSketch(1 << 20)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Increment(rng.Uint64())
	}
}

func BenchmarkBloomFilterOriginalAdd(b *testing.B) {
	bf := newBloomFilter(1000000, 0.01)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.Add(rng.Uint64())
	}
}

func BenchmarkTinyLFUOriginalIncrement(b *testing.B) {
	lfu := NewTinyLFU(1 << 20)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lfu.Increment(rng.Uint64())
	}
}
