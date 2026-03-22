package gcmap

import (
	"container/heap"
	"context"
	"sync"
	"time"

	"github.com/OrlovEvgeny/go-mcache/internal/clock"
	"github.com/OrlovEvgeny/go-mcache/safeMap"
)

// expiryItem represents a cache key and its expiration time for use in a min-heap.
type expiryItem struct {
	key      string
	expireAt int64 // Unix nano timestamp
	index    int
}

// expiryHeap implements a min-heap of expiryItems.
type expiryHeap []*expiryItem

func (h expiryHeap) Len() int           { return len(h) }
func (h expiryHeap) Less(i, j int) bool { return h[i].expireAt < h[j].expireAt }
func (h expiryHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }
func (h *expiryHeap) Push(x interface{}) {
	item := x.(*expiryItem)
	item.index = len(*h)
	*h = append(*h, item)
}
func (h *expiryHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[0 : n-1]
	return item
}

// GC handles automatic expiration of entries in SafeMap using a heap-based scheduler.
type GC struct {
	storage    safeMap.SafeMap
	mu         sync.Mutex
	pq         expiryHeap
	keys       map[string]*expiryItem
	timer      *time.Timer
	stopSignal chan struct{}
	wakeSignal chan struct{}
	wg         sync.WaitGroup
	stopOnce   sync.Once
	options    gcOptions
}

type gcOptions struct {
	defaultPollInterval time.Duration
}

// GCOption customizes GC behavior.
type GCOption func(*gcOptions)

// WithPollInterval sets the fallback sleep interval.
func WithPollInterval(d time.Duration) GCOption {
	return func(o *gcOptions) {
		if d > 0 {
			o.defaultPollInterval = d
		}
	}
}

// NewGC creates and starts a new GC instance.
func NewGC(ctx context.Context, store safeMap.SafeMap, opts ...GCOption) *GC {
	options := gcOptions{defaultPollInterval: time.Minute}
	for _, opt := range opts {
		opt(&options)
	}

	gc := &GC{
		storage:    store,
		pq:         make(expiryHeap, 0),
		keys:       make(map[string]*expiryItem),
		stopSignal: make(chan struct{}),
		wakeSignal: make(chan struct{}, 1),
		options:    options,
	}
	heap.Init(&gc.pq)

	gc.wg.Add(1)
	go gc.run(ctx)

	return gc
}

// Expired schedules a key for expiration after the given duration.
func (gc *GC) Expired(key string, duration time.Duration) {
	if duration <= 0 {
		return
	}
	expireAt := clock.NowNano() + int64(duration)

	gc.mu.Lock()
	if item, exists := gc.keys[key]; exists {
		item.expireAt = expireAt
		heap.Fix(&gc.pq, item.index)
	} else {
		item := &expiryItem{key: key, expireAt: expireAt}
		heap.Push(&gc.pq, item)
		gc.keys[key] = item
	}

	// If this entry is now the earliest to expire, wake the run loop.
	shouldWake := len(gc.pq) > 0 && gc.pq[0].key == key
	gc.mu.Unlock()

	if shouldWake {
		gc.signalWakeUp()
	}
}

// run is the main loop that waits for and processes expirations.
// Optimized to block properly without busy-looping.
func (gc *GC) run(ctx context.Context) {
	defer gc.wg.Done()
	defer func() {
		gc.mu.Lock()
		if gc.timer != nil {
			gc.timer.Stop()
		}
		gc.mu.Unlock()
	}()

	for {
		gc.mu.Lock()
		nextInterval := gc.calculateNextWakeIntervalLocked()

		// Handle immediate processing before entering select
		if nextInterval == 0 {
			gc.mu.Unlock()
			gc.processExpiredKeys()
			continue
		}

		// Setup timer based on next interval
		var timerC <-chan time.Time
		if nextInterval > 0 {
			// We have a scheduled expiration
			if gc.timer == nil {
				gc.timer = time.NewTimer(nextInterval)
			} else {
				if !gc.timer.Stop() {
					select {
					case <-gc.timer.C:
					default:
					}
				}
				gc.timer.Reset(nextInterval)
			}
			timerC = gc.timer.C
		} else {
			// Heap is empty, use default poll interval
			if gc.timer == nil {
				gc.timer = time.NewTimer(gc.options.defaultPollInterval)
			} else {
				if !gc.timer.Stop() {
					select {
					case <-gc.timer.C:
					default:
					}
				}
				gc.timer.Reset(gc.options.defaultPollInterval)
			}
			timerC = gc.timer.C
		}
		gc.mu.Unlock()

		// Block until one of the channels is ready - NO default case!
		select {
		case <-ctx.Done():
			return
		case <-gc.stopSignal:
			return
		case <-gc.wakeSignal:
			continue
		case <-timerC:
			gc.processExpiredKeys()
		}
	}
}

// calculateNextWakeIntervalLocked computes the next wake interval.
// Must be called with gc.mu held.
func (gc *GC) calculateNextWakeIntervalLocked() time.Duration {
	if len(gc.pq) == 0 {
		return -1
	}
	nowNano := clock.NowNano()
	delta := gc.pq[0].expireAt - nowNano
	if delta <= 0 {
		return 0
	}
	return time.Duration(delta)
}

// processExpiredKeys removes all entries whose expiration time has passed.
func (gc *GC) processExpiredKeys() {
	nowNano := clock.NowNano()
	var keys []string

	gc.mu.Lock()
	for len(gc.pq) > 0 && gc.pq[0].expireAt <= nowNano {
		expired := heap.Pop(&gc.pq).(*expiryItem)
		delete(gc.keys, expired.key)
		keys = append(keys, expired.key)
	}
	gc.mu.Unlock()

	if len(keys) > 0 {
		gc.storage.FlushKeys(keys, nowNano)
	}
}

func (gc *GC) signalWakeUp() {
	select {
	case gc.wakeSignal <- struct{}{}:
	default:
	}
}

// Stop shuts down the GC loop gracefully. Safe to call multiple times.
func (gc *GC) Stop() {
	gc.stopOnce.Do(func() {
		close(gc.stopSignal)
	})
	gc.wg.Wait()
}

// LenBufferKeyChan reports the number of pending expirations (for compatibility).
func (gc *GC) LenBufferKeyChan() int {
	gc.mu.Lock()
	defer gc.mu.Unlock()
	return len(gc.pq)
}

// Truncate clears all scheduled expirations.
func (gc *GC) Truncate() {
	gc.mu.Lock()
	defer gc.mu.Unlock()
	gc.pq = make(expiryHeap, 0)
	gc.keys = make(map[string]*expiryItem)
	if gc.timer != nil {
		gc.timer.Stop()
		gc.timer = nil
	}
}

// RemoveKey removes a key from expiration tracking.
func (gc *GC) RemoveKey(key string) {
	gc.mu.Lock()
	defer gc.mu.Unlock()

	if item, ok := gc.keys[key]; ok {
		heap.Remove(&gc.pq, item.index)
		delete(gc.keys, key)
	}
}
