package store

import (
	"github.com/OrlovEvgeny/go-mcache/internal/clock"
	"math"
	"math/rand/v2"
	"sync"
	"testing"
	"time"
)

func TestExpirationLifecycle(t *testing.T) {
	s := NewShardedStore[int, int](1, func(int) uint64 { return 1 })
	s.ConfigureExpiration(time.Nanosecond)
	base := clock.NowNano()
	set := func(k, v int, d int64) { s.Set(&Entry[int, int]{Key: k, Value: v, KeyHash: 1, ExpireAt: d}) }
	set(1, 1, base+10)
	w := s.shards[0].expiry
	id, _ := w.find(1)
	for range 10000 {
		set(1, 2, base+20)
	}
	if len(w.entries) != 1 {
		t.Fatal("overwrite accumulated registrations")
	}
	if got := s.AdvanceExpiration(base + 11); len(got) != 0 {
		t.Fatal("old deadline removed update")
	}
	set(1, 3, base+15)
	if got := s.AdvanceExpiration(base + 15); len(got) != 0 {
		t.Fatal("expired at equality")
	}
	if got := s.AdvanceExpiration(base + 16); len(got) != 1 || got[0].Value != 3 {
		t.Fatal("shortened deadline", got)
	}
	set(2, 2, base+30)
	if got, _ := w.find(2); got != id {
		t.Fatal("id not reused")
	}
	set(2, 3, 0)
	if len(w.ids) != 0 || len(w.buckets) != 0 || !w.first.IsEmpty() {
		t.Fatal("immortal registration retained")
	}
	s.AdvanceExpiration(base + 40)
	if e, ok := s.Get(2); !ok || e.Value != 3 {
		t.Fatal("immortal entry removed")
	}
	set(3, 1, base+50)
	s.Delete(3)
	set(3, 2, base+60)
	s.AdvanceExpiration(base + 51)
	if s.shards[0].m[3].Value != 2 {
		t.Fatal("delete/reinsert")
	}
	// Colliding user hashes still identify keys exactly.
	set(4, 4, base+70)
	set(5, 5, base+80)
	s.AdvanceExpiration(base + 71)
	if _, ok := s.shards[0].m[5]; !ok {
		t.Fatal("hash collision")
	}
	s.Clear()
	if s.Len() != 0 || s.shards[0].expiry != nil {
		t.Fatal("clear retained ttl metadata")
	}
}

func TestExpirationRotations(t *testing.T) {
	for _, jump := range []int64{1, defaultExpiryWheelBuckets, 10 * defaultExpiryWheelBuckets} {
		s := NewShardedStore[int, int](1, nil)
		s.ConfigureExpiration(time.Nanosecond)
		base := clock.NowNano()
		deadline := base + 3*defaultExpiryWheelBuckets + 17
		s.Set(&Entry[int, int]{Key: 1, ExpireAt: deadline})
		for now := base + jump; now <= deadline; now += jump {
			if len(s.AdvanceExpiration(now)) != 0 {
				t.Fatal("long ttl expired early")
			}
		}
		if len(s.AdvanceExpiration(deadline+1)) != 1 {
			t.Fatal("long ttl lost")
		}
	}
	s := NewShardedStore[int, int](1, nil)
	s.Set(&Entry[int, int]{Key: 1, ExpireAt: math.MaxInt64})
	if len(s.AdvanceExpiration(math.MaxInt64)) != 0 {
		t.Fatal("overflow/equality")
	}
}

func TestExpirationPastDeadline(t *testing.T) {
	s := NewShardedStore[int, int](1, nil)
	s.ConfigureExpiration(time.Nanosecond)
	now := clock.NowNano()
	s.Set(&Entry[int, int]{Key: 1, ExpireAt: now - 100})
	if len(s.AdvanceExpiration(now+1)) != 1 {
		t.Fatal("overdue insertion lost")
	}
}

func TestExpirationConcurrent(t *testing.T) {
	s := NewShardedStore[int, int](4, nil)
	s.ConfigureExpiration(time.Nanosecond)
	var wg sync.WaitGroup
	for worker := range 6 {
		wg.Go(func() {
			for i := range 1000 {
				switch worker {
				case 0:
					s.AdvanceExpiration(clock.NowNano() + int64(i))
				case 1:
					s.Clear()
				case 2:
					s.Delete(i % 32)
				default:
					s.Set(&Entry[int, int]{Key: i % 32, Value: i, ExpireAt: clock.NowNano() + int64(i%10)})
				}
			}
		})
	}
	wg.Wait()
	var count int
	for _, sh := range s.shards {
		count += len(sh.m)
		w := sh.expiry
		if w == nil {
			continue
		}
		var ids int
		buckets := map[uint16]*expiryBucket{w.firstSlot: &w.first}
		for slot, b := range w.buckets {
			buckets[slot] = b
		}
		for slot, b := range buckets {
			it := b.Iterator()
			for it.HasNext() {
				id := it.Next()
				e := w.entries[id]
				if e.ExpireAt == 0 || sh.m[e.Key] == nil || sh.m[e.Key].ExpireAt != e.ExpireAt || w.slots[id] != slot {
					t.Fatal("stale registration")
				}
				ids++
			}
		}
		if ids != len(w.entries)-len(w.free) {
			t.Fatal("duplicate bitmap membership")
		}
	}
	if s.Len() != count {
		t.Fatalf("len=%d entries=%d", s.Len(), count)
	}
}

func TestExpirationModel(t *testing.T) {
	s := NewShardedStore[int, int](1, func(int) uint64 { return 1 })
	s.ConfigureExpiration(time.Nanosecond)
	now := clock.NowNano()
	model := make(map[int]*Entry[int, int])
	rng := rand.New(rand.NewPCG(123, 456))
	for i := range 20000 {
		k := rng.IntN(600)
		switch rng.IntN(6) {
		case 0:
			s.Delete(k)
			delete(model, k)
		case 1:
			now += int64(rng.IntN(5000))
			want := 0
			for k, e := range model {
				if e.ExpireAt > 0 && now > e.ExpireAt {
					delete(model, k)
					want++
				}
			}
			if got := len(s.AdvanceExpiration(now)); got != want {
				t.Fatalf("iteration %d: expired=%d want=%d", i, got, want)
			}
		default:
			deadline := now + int64(rng.IntN(20000))
			if rng.IntN(10) == 0 {
				deadline = 0
			}
			e := &Entry[int, int]{Key: k, Value: i, KeyHash: 1, ExpireAt: deadline}
			s.Set(e)
			model[k] = e
		}
		if len(model) != s.Len() {
			t.Fatal("size diverged")
		}
		for k, e := range model {
			if s.shards[0].m[k] != e {
				t.Fatal("value diverged")
			}
		}
	}
	s.Clear()
	// Exercise multiple full vector chunks in a single bitmap, plus ID reuse.
	for round := range 2 {
		for k := range 1025 {
			s.Set(&Entry[int, int]{Key: k, Value: round, KeyHash: 1, ExpireAt: now + 1})
		}
		if n := len(s.AdvanceExpiration(now + 2)); n != 1025 {
			t.Fatal("chunked cleanup", n)
		}
		now += 3
	}
}

func TestExpirationRegistryReleasesEntries(t *testing.T) {
	s := NewShardedStore[int, *[1024]byte](1, nil)
	for k := range 40 {
		s.Set(&Entry[int, *[1024]byte]{Key: k, Value: new([1024]byte), ExpireAt: clock.NowNano() + int64(time.Hour)})
	}
	w := s.shards[0].expiry
	for k := range 40 {
		s.Delete(k)
	}
	for _, e := range w.entries {
		if e.ExpireAt != 0 {
			t.Fatal("deleted entry retained in registry")
		}
	}
	for _, e := range w.inlineEntries {
		if e.ExpireAt != 0 {
			t.Fatal("entry retained in old inline backing array")
		}
	}
	if len(w.ids) != 0 || !w.first.IsEmpty() || len(w.buckets) != 0 || len(w.free) != 40 {
		t.Fatal("ttl indices not released")
	}
}
