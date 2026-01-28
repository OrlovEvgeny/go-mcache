package policy

import (
	"math/rand"
	"testing"
)

func TestCMSketch(t *testing.T) {
	s := newCMSketch(1024)

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

func TestBloomFilter(t *testing.T) {
	bf := newBloomFilter(1000, 0.01)

	// Test Add and Contains
	keyHash := uint64(12345)
	if bf.Contains(keyHash) {
		t.Error("New bloom filter should not contain any key")
	}

	bf.Add(keyHash)
	if !bf.Contains(keyHash) {
		t.Error("Bloom filter should contain added key")
	}

	// Add another key
	bf.Add(uint64(67890))
	if !bf.Contains(uint64(67890)) {
		t.Error("Bloom filter should contain second key")
	}

	// Test Reset
	bf.Reset()
	if bf.Contains(keyHash) {
		t.Error("Bloom filter should be empty after reset")
	}
}

func TestBloomFilterFalsePositiveRate(t *testing.T) {
	bf := newBloomFilter(10000, 0.01)

	// Add 10000 items
	for i := 0; i < 10000; i++ {
		bf.Add(uint64(i))
	}

	// Check false positive rate with items not added
	falsePositives := 0
	for i := 10000; i < 20000; i++ {
		if bf.Contains(uint64(i)) {
			falsePositives++
		}
	}

	fpRate := float64(falsePositives) / 10000.0
	t.Logf("False positive rate: %.4f", fpRate)

	// Allow some margin (up to 5%)
	if fpRate > 0.05 {
		t.Errorf("False positive rate too high: %.4f", fpRate)
	}
}

func TestTinyLFU(t *testing.T) {
	lfu := NewTinyLFU(1000)

	// Increment several times
	keyHash := uint64(12345)
	for i := 0; i < 10; i++ {
		lfu.Increment(keyHash)
	}

	est := lfu.Estimate(keyHash)
	if est < 5 {
		t.Errorf("Expected estimate >= 5, got %d", est)
	}

	// Test admission
	hotKey := uint64(11111)
	coldKey := uint64(22222)

	// Make hotKey "hot"
	for i := 0; i < 100; i++ {
		lfu.Increment(hotKey)
	}
	// Make coldKey "cold"
	lfu.Increment(coldKey)

	// Hot key should be admitted over cold key
	if !lfu.Admit(hotKey, coldKey) {
		t.Error("Hot key should be admitted over cold key")
	}
}

func TestSampledLFU(t *testing.T) {
	s := NewSampledLFU(100, 10)

	// Add entries
	s.Add(1, 10)
	s.Add(2, 20)
	s.Add(3, 30)

	if s.NumEntries() != 3 {
		t.Errorf("Expected 3 entries, got %d", s.NumEntries())
	}
	if s.UsedCost() != 60 {
		t.Errorf("Expected cost 60, got %d", s.UsedCost())
	}

	// Test Has
	if !s.Has(1) {
		t.Error("Expected key 1 to exist")
	}

	// Test Update
	s.Update(1, 15)
	if s.UsedCost() != 65 {
		t.Errorf("Expected cost 65 after update, got %d", s.UsedCost())
	}

	// Test Del
	s.Del(2)
	if s.NumEntries() != 2 {
		t.Errorf("Expected 2 entries after delete, got %d", s.NumEntries())
	}

	// Test Sample
	sample := s.Sample()
	if len(sample) == 0 {
		t.Error("Sample should not be empty")
	}

	// Test Clear
	s.Clear()
	if s.NumEntries() != 0 {
		t.Error("NumEntries should be 0 after clear")
	}
}

func TestPolicy(t *testing.T) {
	p := NewPolicy(1000, 100, 0) // 100 max cost, unlimited entries

	// Add entries
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

	// Total cost now 90, adding 30 more should trigger eviction
	victims, added := p.Add(4, 30)
	t.Logf("Victims after adding entry 4: %v, added: %v", victims, added)

	// Test Has
	if !p.Has(4) {
		t.Error("Key 4 should exist")
	}

	// Test Access
	p.Access(4)

	// Test Cost
	cost := p.Cost()
	t.Logf("Total cost: %d", cost)
}

func TestPolicyEviction(t *testing.T) {
	p := NewPolicy(1000, 0, 5) // Max 5 entries

	// Add 10 entries with different frequencies
	for i := 0; i < 10; i++ {
		keyHash := uint64(i)
		// Simulate different access patterns
		for j := 0; j < i+1; j++ {
			p.Access(keyHash)
		}
		p.Add(keyHash, 1)
	}

	entries := p.NumEntries()
	t.Logf("Entries after adding 10: %d", entries)

	// Should be around 5 entries due to max limit
	if entries > 10 {
		t.Errorf("Expected <= 10 entries, got %d", entries)
	}
}

func BenchmarkCMSketchIncrement(b *testing.B) {
	s := newCMSketch(1 << 20)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Increment(rng.Uint64())
	}
}

func BenchmarkCMSketchEstimate(b *testing.B) {
	s := newCMSketch(1 << 20)
	rng := rand.New(rand.NewSource(42))

	// Pre-populate
	for i := 0; i < 10000; i++ {
		s.Increment(rng.Uint64())
	}

	rng = rand.New(rand.NewSource(42))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Estimate(rng.Uint64())
	}
}

func BenchmarkBloomFilterAdd(b *testing.B) {
	bf := newBloomFilter(1000000, 0.01)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.Add(rng.Uint64())
	}
}

func BenchmarkTinyLFUIncrement(b *testing.B) {
	lfu := NewTinyLFU(1 << 20)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lfu.Increment(rng.Uint64())
	}
}

func BenchmarkPolicyAdd(b *testing.B) {
	p := NewPolicy(1<<20, 1<<30, 0)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Add(rng.Uint64(), 1)
	}
}
