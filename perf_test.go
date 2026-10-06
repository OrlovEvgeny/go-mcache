package mcache

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OrlovEvgeny/go-mcache/internal/clock"
	"github.com/OrlovEvgeny/go-mcache/internal/policy"
	"github.com/OrlovEvgeny/go-mcache/internal/store"
)

func BenchmarkBatchSizes(b *testing.B) {
	for _, optimized := range []bool{false, true} {
		for _, n := range []int{1, 16, 64, 256, 1024} {
			b.Run(fmt.Sprintf("optimized=%t/n=%d", optimized, n), func(b *testing.B) {
				c := NewCache[int, int]()
				defer c.Close()
				keys := make([]int, n)
				for i := range keys {
					keys[i] = i
					c.Set(i, i, time.Hour)
				}
				get := c.GetBatch
				if optimized {
					get = c.GetBatchOptimized
				}
				b.ReportAllocs()
				for b.Loop() {
					r := get(keys)
					if !r.Found[n-1] {
						b.Fatal("missing hit")
					}
				}
			})
		}
	}
}

// Reset between fixed-size insertion runs so unique inserts do not grow without bound.
func BenchmarkTTLUnique(b *testing.B) {
	c := NewCache[int, int]()
	defer c.Close()
	for b.Loop() {
		c.Clear()
		for i := range 10000 {
			c.Set(i, i, time.Hour)
		}
	}
	b.ReportMetric(10000, "entries/op")
}

func BenchmarkTTLRetained(b *testing.B) {
	for _, rounds := range []int{1, 100} {
		b.Run(fmt.Sprintf("rounds=%d", rounds), func(b *testing.B) {
			var retained uint64
			for b.Loop() {
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				c := NewCache[int, int]()
				for range rounds {
					for i := range 10000 {
						c.Set(i, i, time.Hour)
					}
				}
				runtime.GC()
				runtime.ReadMemStats(&after)
				if after.HeapAlloc > before.HeapAlloc {
					retained += after.HeapAlloc - before.HeapAlloc
				}
				c.Close()
				runtime.KeepAlive(c)
			}
			b.ReportMetric(float64(retained)/float64(b.N), "retained-B")
		})
	}
}

func TestBatchSemantics(t *testing.T) {
	for _, lockfree := range []bool{false, true} {
		for _, optimized := range []bool{false, true} {
			t.Run(fmt.Sprintf("lockfree=%t/optimized=%t", lockfree, optimized), func(t *testing.T) {
				c := NewCache[int, int](WithMaxEntries[int, int](8), WithLockFreePolicy[int, int](lockfree), WithMetrics[int, int](true), WithKeyHasher[int, int](func(int) uint64 { return 7 }), WithExpirationResolution[int, int](time.Hour))
				defer c.Close()
				for i := range 4 {
					c.Set(i, 10+i, 0)
				}
				c.store.Set(&store.Entry[int, int]{Key: 4, Value: 14, KeyHash: 7, ExpireAt: clock.NowNano() - 1})
				tracked := &accessPolicy{Policer: c.policy}
				c.policy = tracked
				get := c.GetBatch
				if optimized {
					get = c.GetBatchOptimized
				}
				if r := get(nil); len(r.Values) != 0 || len(r.Found) != 0 || len(r.Hashes) != 0 {
					t.Fatal(r)
				}
				keys := []int{2, 9, 2, 4, 0, 3}
				r := get(keys)
				for i, k := range keys {
					want := k < 4
					if r.Keys[i] != k || r.Hashes[i] != 7 || r.Found[i] != want {
						t.Fatalf("result[%d]=%+v", i, r)
					}
					value := 0
					if want {
						value = 10 + k
					}
					if r.Values[i] != value {
						t.Fatalf("value[%d]=%d", i, r.Values[i])
					}
				}
				m := c.Metrics()
				if m.Hits != 4 || m.Misses != 2 {
					t.Fatalf("metrics: %+v", m)
				}
				c.Wait()
				if tracked.accesses.Load() != 4 {
					t.Fatalf("policy accesses=%d", tracked.accesses.Load())
				}
				get([]int{9, 9, 9, 9, 9, 9})
				if r.Values[0] != 12 || !r.Found[0] {
					t.Fatal("returned slices were reused")
				}
			})
		}
	}
}

func TestExpirationCallbackCanReenter(t *testing.T) {
	var c *Cache[int, int]
	done := make(chan struct{})
	c = NewCache[int, int](WithExpirationResolution[int, int](time.Millisecond), WithOnExpire[int, int](func(k, v int) {
		c.Clear()
		c.Set(k, v+1, 0)
		close(done)
	}))
	defer c.Close()
	c.Set(1, 7, time.Millisecond)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expiration callback did not finish")
	}
	if v, ok := c.Get(1); !ok || v != 8 {
		t.Fatal(v, ok)
	}
}

func TestBatchHashesOnce(t *testing.T) {
	calls := 0
	c := NewCache[int, int](WithKeyHasher[int, int](func(k int) uint64 { calls++; return uint64(k) }))
	defer c.Close()
	c.Set(1, 7, 0)
	calls = 0
	r := c.GetBatchOptimized([]int{1, 2, 1})
	if calls != 3 || !r.Found[0] || r.Found[1] || !r.Found[2] {
		t.Fatalf("hash calls=%d result=%+v", calls, r)
	}
}

func TestLargeBatchMixedTTL(t *testing.T) {
	for _, lockfree := range []bool{false, true} {
		for _, optimized := range []bool{false, true} {
			t.Run(fmt.Sprintf("lockfree=%t/optimized=%t", lockfree, optimized), func(t *testing.T) {
				c := NewCache[int, int](WithMaxEntries[int, int](256), WithLockFreePolicy[int, int](lockfree), WithMetrics[int, int](true), WithShardCount[int, int](4), WithExpirationResolution[int, int](time.Hour))
				defer c.Close()
				now := clock.NowNano()
				keys := make([]int, 1025)
				for i := range keys {
					k := i % 256
					keys[i] = k
					if k%3 == 0 {
						continue
					}
					deadline := int64(0)
					if k%3 == 1 {
						deadline = now - 1
					}
					c.Set(k, k+10, 0)
					c.store.Set(&store.Entry[int, int]{Key: k, Value: k + 10, KeyHash: c.store.KeyHash(k), ExpireAt: deadline})
				}
				get := c.GetBatch
				if optimized {
					get = c.GetBatchOptimized
				}
				for round := range 2 {
					result := get(keys)
					hits := int64(0)
					for i, k := range keys {
						want := k%3 == 2
						if result.Found[i] != want {
							t.Fatal(i, k, result.Found[i])
						}
						if want {
							hits++
							if result.Values[i] != k+10 {
								t.Fatal("value")
							}
						} else if result.Values[i] != 0 {
							t.Fatal("nonzero miss")
						}
					}
					m := c.Metrics()
					if m.Hits != hits*int64(round+1) || m.Misses != (1025-hits)*int64(round+1) {
						t.Fatal(m)
					}
				}
			})
		}
	}
}

// Count the real policy calls, including the buffered policy's replay.
type accessPolicy struct {
	policy.Policer[int]
	accesses atomic.Int64
}

func (p *accessPolicy) Access(hash uint64) { p.accesses.Add(1); p.Policer.Access(hash) }
