package gcmap

import (
	"container/heap"
	"context"
	"sync"
	"time"

	"github.com/OrlovEvgeny/go-mcache/safeMap"
)

// expiryItem represents a cache key and its expiration time for use in a min-heap.
type expiryItem struct {
	key      string    // Cache key
	expireAt time.Time // Expiration timestamp
	index    int       // Position in the heap
}

// expiryHeap implements a min-heap of expiryItems.
type expiryHeap []*expiryItem

func (h expiryHeap) Len() int           { return len(h) }
func (h expiryHeap) Less(i, j int) bool { return h[i].expireAt.Before(h[j].expireAt) }
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
	old[n-1] = nil  // Prevent memory leak
	item.index = -1 // Invalidate index
	*h = old[0 : n-1]
	return item
}

// GC handles automatic expiration of entries in SafeMap using a heap-based scheduler.
type GC struct {
	storage    safeMap.SafeMap
	mu         sync.Mutex
	pq         expiryHeap     // Min-heap of pending expirations
	keyIndex   map[string]int // Map of keys to their heap index
	timer      *time.Timer
	stopSignal chan struct{}
	wakeSignal chan struct{} // Signal to recalculate timer early
	wg         sync.WaitGroup
	options    gcOptions
}

type gcOptions struct {
	defaultPollInterval time.Duration // Fallback interval when heap is empty
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
// Maintains the signature expected by mcache.go.
func NewGC(ctx context.Context, store safeMap.SafeMap, opts ...GCOption) *GC {
	options := gcOptions{defaultPollInterval: time.Minute}
	for _, opt := range opts {
		opt(&options)
	}

	gc := &GC{
		storage:    store,
		pq:         make(expiryHeap, 0),
		keyIndex:   make(map[string]int),
		stopSignal: make(chan struct{}),
		wakeSignal: make(chan struct{}, 1),
		options:    options,
	}
	heap.Init(&gc.pq)

	gc.wg.Add(1)
	go gc.run(ctx)

	return gc
}

func (gc *GC) fix(idx int) {
	heap.Fix(&gc.pq, idx)
	for _, item := range gc.pq {
		gc.keyIndex[item.key] = item.index
	}
}

// Expired schedules a key for expiration after the given duration.
func (gc *GC) Expired(key string, duration time.Duration) {
	if duration <= 0 {
		return
	}
	expireAt := time.Now().Add(duration)

	gc.mu.Lock()
	defer gc.mu.Unlock()

	if idx, exists := gc.keyIndex[key]; exists {
		gc.pq[idx].expireAt = expireAt
		gc.fix(idx)
	} else {
		item := &expiryItem{key: key, expireAt: expireAt}
		heap.Push(&gc.pq, item)
		gc.keyIndex[key] = item.index
	}

	// If this entry is now the earliest to expire, wake the run loop.
	if len(gc.pq) > 0 && gc.pq[0].key == key {
		gc.signalWakeUp()
	}
}

// run is the main loop that waits for and processes expirations.
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
		nextInterval := gc.calculateNextWakeInterval()

		gc.mu.Lock()
		switch {
		case nextInterval > 0:
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
			gc.mu.Unlock()
		case nextInterval == 0:
			gc.mu.Unlock()
		default: // heap is empty
			if gc.timer != nil {
				if !gc.timer.Stop() {
					select {
					case <-gc.timer.C:
					default:
					}
				}
				gc.timer = nil
			}
			nextInterval = gc.options.defaultPollInterval
			gc.mu.Unlock()
		}

		var timerC <-chan time.Time
		gc.mu.Lock()
		if gc.timer != nil && nextInterval > 0 {
			timerC = gc.timer.C
		}
		gc.mu.Unlock()

		processNow := nextInterval == 0

		select {
		case <-ctx.Done():
			return
		case <-gc.stopSignal:
			return
		case <-gc.wakeSignal:
			continue
		case <-timerC:
			gc.processExpiredKeys()
		default:
			if processNow {
				gc.processExpiredKeys()
			} else if nextInterval < 0 {
				select {
				case <-time.After(gc.options.defaultPollInterval):
				case <-ctx.Done():
					return
				case <-gc.stopSignal:
					return
				case <-gc.wakeSignal:
					continue
				}
			}
		}
	}
}

func (gc *GC) calculateNextWakeInterval() time.Duration {
	gc.mu.Lock()
	defer gc.mu.Unlock()

	if len(gc.pq) == 0 {
		return -1
	}
	delta := time.Until(gc.pq[0].expireAt)
	if delta <= 0 {
		return 0
	}
	return delta
}

// processExpiredKeys removes all entries whose expiration time has passed.
func (gc *GC) processExpiredKeys() {
	now := time.Now()
	var keys []string

	gc.mu.Lock()
	for len(gc.pq) > 0 && !gc.pq[0].expireAt.After(now) {
		expired := heap.Pop(&gc.pq).(*expiryItem)
		delete(gc.keyIndex, expired.key)
		keys = append(keys, expired.key)
	}
	gc.mu.Unlock()

	if len(keys) > 0 {
		gc.storage.Flush(keys)
	}
}

func (gc *GC) signalWakeUp() {
	select {
	case gc.wakeSignal <- struct{}{}:
	default:
	}
}

// Stop shuts down the GC loop gracefully.
func (gc *GC) Stop() {
	close(gc.stopSignal)
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
	gc.keyIndex = make(map[string]int)
	if gc.timer != nil {
		gc.timer.Stop()
		gc.timer = nil
	}
}

// RemoveKey removes a key from expiration tracking.
func (gc *GC) RemoveKey(key string) {
	gc.mu.Lock()
	defer gc.mu.Unlock()

	if idx, ok := gc.keyIndex[key]; ok {
		heap.Remove(&gc.pq, idx)
		delete(gc.keyIndex, key)
		for i, item := range gc.pq {
			item.index = i
			gc.keyIndex[item.key] = i
		}
	}
}
