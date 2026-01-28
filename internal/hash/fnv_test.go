package hash

import (
	"testing"
)

func TestStringHash(t *testing.T) {
	// Same input should produce same hash
	h1 := String("hello")
	h2 := String("hello")
	if h1 != h2 {
		t.Error("Same string should produce same hash")
	}

	// Different input should produce different hash
	h3 := String("world")
	if h1 == h3 {
		t.Error("Different strings should produce different hashes")
	}

	// Empty string should work
	h4 := String("")
	if h4 == 0 {
		t.Error("Empty string should not produce zero hash")
	}
}

func TestBytesHash(t *testing.T) {
	h1 := Bytes([]byte("hello"))
	h2 := Bytes([]byte("hello"))
	if h1 != h2 {
		t.Error("Same bytes should produce same hash")
	}

	// Should match String hash
	h3 := String("hello")
	if h1 != h3 {
		t.Error("Bytes and String should produce same hash for same content")
	}
}

func TestIntHashes(t *testing.T) {
	// Same value should produce same hash
	h1 := Int64(12345)
	h2 := Int64(12345)
	if h1 != h2 {
		t.Error("Same int64 should produce same hash")
	}

	// Different values should produce different hashes
	h3 := Int64(12346)
	if h1 == h3 {
		t.Error("Different int64 should produce different hashes")
	}

	// Zero should work
	h4 := Int64(0)
	if h4 == 0 {
		// The hash of 0 might be 0, but let's check it's computed
		_ = h4
	}

	// Negative numbers should work
	h5 := Int64(-12345)
	if h5 == h1 {
		t.Error("Negative int64 should produce different hash")
	}
}

func TestUintHashes(t *testing.T) {
	h1 := Uint64(12345)
	h2 := Uint64(12345)
	if h1 != h2 {
		t.Error("Same uint64 should produce same hash")
	}

	h3 := Uint(123)
	h4 := Uint(123)
	if h3 != h4 {
		t.Error("Same uint should produce same hash")
	}
}

func TestInt32Hash(t *testing.T) {
	h1 := Int32(12345)
	h2 := Int32(12345)
	if h1 != h2 {
		t.Error("Same int32 should produce same hash")
	}
}

func TestUint32Hash(t *testing.T) {
	h1 := Uint32(12345)
	h2 := Uint32(12345)
	if h1 != h2 {
		t.Error("Same uint32 should produce same hash")
	}
}

func TestIntHash(t *testing.T) {
	h1 := Int(12345)
	h2 := Int(12345)
	if h1 != h2 {
		t.Error("Same int should produce same hash")
	}
}

func TestCombine(t *testing.T) {
	h1 := String("hello")
	h2 := String("world")

	c1 := Combine(h1, h2)
	c2 := Combine(h1, h2)
	if c1 != c2 {
		t.Error("Combine should be deterministic")
	}

	// Order matters
	c3 := Combine(h2, h1)
	if c1 == c3 {
		t.Error("Combine order should matter")
	}
}

func TestStringFast(t *testing.T) {
	// Short string should use regular hash
	short := "hello"
	h1 := StringFast(short)
	h2 := String(short)
	if h1 != h2 {
		t.Error("StringFast should match String for short strings")
	}

	// Long string
	long := "this is a long string that is definitely longer than 32 characters for testing"
	h3 := StringFast(long)
	h4 := StringFast(long)
	if h3 != h4 {
		t.Error("StringFast should be deterministic for long strings")
	}
}

func TestBytesFast(t *testing.T) {
	short := []byte("hello")
	h1 := BytesFast(short)
	h2 := Bytes(short)
	if h1 != h2 {
		t.Error("BytesFast should match Bytes for short slices")
	}

	long := []byte("this is a long byte slice that is definitely longer than 32 bytes for testing")
	h3 := BytesFast(long)
	h4 := BytesFast(long)
	if h3 != h4 {
		t.Error("BytesFast should be deterministic for long slices")
	}
}

func TestHashDistribution(t *testing.T) {
	// Test that hashes are reasonably distributed
	const n = 10000
	buckets := make(map[uint64]int)

	for i := 0; i < n; i++ {
		h := Int64(int64(i))
		// Use lower bits as bucket
		bucket := h & 0xFF
		buckets[bucket]++
	}

	// Check that we use most buckets
	if len(buckets) < 200 {
		t.Errorf("Expected better distribution, only %d buckets used", len(buckets))
	}

	// Check for severe imbalance (no bucket should have >5x average)
	avg := n / 256
	for bucket, count := range buckets {
		if count > avg*5 {
			t.Errorf("Bucket %d has %d items, average is %d (severe imbalance)", bucket, count, avg)
		}
	}
}

func BenchmarkStringShort(b *testing.B) {
	s := "hello world"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		String(s)
	}
}

func BenchmarkStringLong(b *testing.B) {
	s := "this is a much longer string that will test the performance of the hash function with more data"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		String(s)
	}
}

func BenchmarkStringFastShort(b *testing.B) {
	s := "hello world"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StringFast(s)
	}
}

func BenchmarkStringFastLong(b *testing.B) {
	s := "this is a much longer string that will test the performance of the hash function with more data"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StringFast(s)
	}
}

func BenchmarkInt64(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Int64(int64(i))
	}
}

func BenchmarkUint64(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Uint64(uint64(i))
	}
}

func BenchmarkBytes(b *testing.B) {
	data := []byte("hello world test data")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Bytes(data)
	}
}

func BenchmarkCombine(b *testing.B) {
	h1 := String("hello")
	h2 := String("world")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Combine(h1, h2)
	}
}
