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

// ExpiryWheel is a coarse hashed timing wheel for best-effort background
// expiration. Exact TTL enforcement still happens on reads.
type ExpiryWheel[K comparable] struct {
	resolution int64
	mask       uint64

	mu          sync.Mutex
	currentTick int64
	buckets     [][]ExpiryWheelEntry[K]
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
		buckets:     make([][]ExpiryWheelEntry[K], bucketCount),
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
	idx := uint64(tick) & w.mask

	w.mu.Lock()
	w.buckets[idx] = append(w.buckets[idx], ExpiryWheelEntry[K]{
		Key:      key,
		KeyHash:  keyHash,
		ExpireAt: expireAt,
	})
	w.mu.Unlock()
}

// Advance drains all buckets up to now and returns entries that are due.
func (w *ExpiryWheel[K]) Advance(now int64) []ExpiryWheelEntry[K] {
	nowTick := now / w.resolution

	w.mu.Lock()
	defer w.mu.Unlock()

	if nowTick <= w.currentTick {
		return nil
	}

	var expired []ExpiryWheelEntry[K]
	for w.currentTick < nowTick {
		w.currentTick++
		idx := uint64(w.currentTick) & w.mask
		bucket := w.buckets[idx]
		if len(bucket) == 0 {
			continue
		}
		w.buckets[idx] = nil

		for _, item := range bucket {
			if item.ExpireAt <= now {
				expired = append(expired, item)
				continue
			}

			futureTick := (item.ExpireAt + w.resolution - 1) / w.resolution
			futureIdx := uint64(futureTick) & w.mask
			w.buckets[futureIdx] = append(w.buckets[futureIdx], item)
		}
	}

	return expired
}

// Clear removes all scheduled items and resets the current cursor.
func (w *ExpiryWheel[K]) Clear() {
	w.mu.Lock()
	for i := range w.buckets {
		w.buckets[i] = nil
	}
	w.currentTick = clock.NowNano() / w.resolution
	w.mu.Unlock()
}
