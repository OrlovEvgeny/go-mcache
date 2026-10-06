package store

import (
	"sync"
	"testing"
	"time"
)

func TestExpiryWheelBoundaryAndRotations(t *testing.T) {
	w := NewExpiryWheel[int](time.Nanosecond)
	base := w.currentTick
	w.Schedule(1, 1, base+10)
	if len(w.Advance(base+10)) != 0 {
		t.Fatal("expired at equality")
	}
	if got := w.Advance(base + 11); len(got) != 1 || got[0].Key != 1 {
		t.Fatal("lost boundary expiration", got)
	}
	deadline := base + 3*defaultExpiryWheelBuckets + 20
	for k := range 1025 {
		w.Schedule(k, uint64(k), deadline)
	}
	if len(w.Advance(base+defaultExpiryWheelBuckets)) != 0 {
		t.Fatal("long ttl expired early")
	}
	if len(w.Advance(deadline)) != 0 {
		t.Fatal("expired at equality after rotation")
	}
	if got := len(w.Advance(deadline + 1)); got != 1025 {
		t.Fatal("lost long ttl entries", got)
	}
	w.Schedule(1, 1, deadline+10)
	w.Clear()
	if len(w.Advance(deadline+11)) != 0 {
		t.Fatal("clear retained events")
	}
}

func TestExpiryCandidateDoesNotDeleteUpdatedValue(t *testing.T) {
	s := NewShardedStore[int, int](1, func(int) uint64 { return 1 })
	s.Set(&Entry[int, int]{Key: 1, Value: 10, ExpireAt: 100, KeyHash: 1})
	s.Set(&Entry[int, int]{Key: 2, Value: 20, ExpireAt: 100, KeyHash: 1})
	s.Set(&Entry[int, int]{Key: 1, Value: 11, ExpireAt: 200, KeyHash: 1})
	if s.DeleteIfExpired(1, 1, 100, 150) != nil {
		t.Fatal("old candidate deleted update")
	}
	if e := s.DeleteIfExpired(2, 1, 100, 150); e == nil || e.Key != 2 {
		t.Fatal("hash collision")
	}
	s.Delete(1)
	s.Set(&Entry[int, int]{Key: 1, Value: 12, KeyHash: 1})
	if s.DeleteIfExpired(1, 1, 200, 300) != nil {
		t.Fatal("candidate deleted reinserted immortal value")
	}
}

func TestConcurrentClearSize(t *testing.T) {
	s := NewShardedStore[int, int](4, nil)
	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Go(func() {
			for i := range 1000 {
				switch worker {
				case 0:
					s.Clear()
				case 1:
					s.Delete(i % 32)
				default:
					s.Set(&Entry[int, int]{Key: i % 32, Value: i})
				}
			}
		})
	}
	wg.Wait()
	count := 0
	for _, sh := range s.shards {
		count += len(sh.m)
	}
	if s.Len() != count {
		t.Fatalf("len=%d entries=%d", s.Len(), count)
	}
}
