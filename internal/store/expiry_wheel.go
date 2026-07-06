package store

import (
	"sync"
	"time"

	"github.com/OrlovEvgeny/go-mcache/internal/clock"
)

const defaultExpiryWheelBuckets = 4096

// ExpiryWheelEntry is a scheduled expiration event.
type ExpiryWheelEntry[K comparable] struct {
	Key      K
	KeyHash  uint64
	ExpireAt int64
}

// wheelBucket holds the entries scheduled for one tick slot.
type wheelBucket[K comparable] struct {
	mu    sync.Mutex
	items []ExpiryWheelEntry[K]
}

// ExpiryWheel is a coarse hashed timing wheel for best-effort background
// expiration. Exact TTL enforcement still happens on reads.
//
// Locking is per bucket: a Schedule only touches the slot its tick maps to,
// so concurrent TTL writes across shards don't serialize on one wheel-wide
// mutex. Advance is serialized separately and takes one bucket at a time.
type ExpiryWheel[K comparable] struct {
	resolution int64
	mask       uint64

	advanceMu   sync.Mutex // serializes Advance/Clear; guards currentTick
	currentTick int64

	buckets []wheelBucket[K]
}

// NewExpiryWheel creates a timing wheel with the provided resolution.
func NewExpiryWheel[K comparable](resolution time.Duration) *ExpiryWheel[K] {
	if resolution <= 0 {
		resolution = 100 * time.Millisecond
	}

	bucketCount := defaultExpiryWheelBuckets
	return &ExpiryWheel[K]{
		resolution:  int64(resolution),
		mask:        uint64(bucketCount - 1),
		currentTick: clock.NowNano() / int64(resolution),
		buckets:     make([]wheelBucket[K], bucketCount),
	}
}

// Resolution returns the configured wheel resolution.
func (w *ExpiryWheel[K]) Resolution() time.Duration {
	return time.Duration(w.resolution)
}

// Schedule registers a future expiration.
func (w *ExpiryWheel[K]) Schedule(key K, keyHash uint64, expireAt int64) {
	if expireAt <= 0 {
		return
	}

	tick := (expireAt + w.resolution - 1) / w.resolution
	b := &w.buckets[uint64(tick)&w.mask]

	b.mu.Lock()
	b.items = append(b.items, ExpiryWheelEntry[K]{
		Key:      key,
		KeyHash:  keyHash,
		ExpireAt: expireAt,
	})
	b.mu.Unlock()
}

// Advance drains all buckets up to now and returns entries that are due.
func (w *ExpiryWheel[K]) Advance(now int64) []ExpiryWheelEntry[K] {
	nowTick := now / w.resolution

	w.advanceMu.Lock()
	defer w.advanceMu.Unlock()

	if nowTick <= w.currentTick {
		return nil
	}

	var expired []ExpiryWheelEntry[K]
	for w.currentTick < nowTick {
		w.currentTick++
		b := &w.buckets[uint64(w.currentTick)&w.mask]

		b.mu.Lock()
		bucket := b.items
		if len(bucket) == 0 {
			b.mu.Unlock()
			continue
		}
		b.items = nil
		b.mu.Unlock()

		for _, item := range bucket {
			if item.ExpireAt <= now {
				expired = append(expired, item)
				continue
			}

			// Not due yet (long TTL wrapped around) — reschedule.
			futureTick := (item.ExpireAt + w.resolution - 1) / w.resolution
			fb := &w.buckets[uint64(futureTick)&w.mask]
			fb.mu.Lock()
			fb.items = append(fb.items, item)
			fb.mu.Unlock()
		}
	}

	return expired
}

// Clear removes all scheduled items and resets the current cursor.
func (w *ExpiryWheel[K]) Clear() {
	w.advanceMu.Lock()
	defer w.advanceMu.Unlock()

	for i := range w.buckets {
		b := &w.buckets[i]
		b.mu.Lock()
		b.items = nil
		b.mu.Unlock()
	}
	w.currentTick = clock.NowNano() / w.resolution
}
