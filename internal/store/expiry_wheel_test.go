package store

import (
	"github.com/OrlovEvgeny/go-mcache/internal/clock"
	"github.com/RoaringBitmap/roaring/v2"
	"math"
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
		buckets := map[uint16]*roaring.Bitmap{w.firstSlot: &w.first}
		for slot, b := range w.buckets {
			buckets[slot] = b
		}
		for slot, b := range buckets {
			it := b.Iterator()
			for it.HasNext() {
				id := it.Next()
				e := w.entries[id]
				if e == nil || sh.m[e.Key] != e || w.slots[id] != slot {
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
