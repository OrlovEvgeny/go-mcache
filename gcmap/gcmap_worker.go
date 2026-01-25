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
        pq         expiryHeap             // Min-heap of pending expirations
        keys       map[string]*expiryItem // Map of keys to the pending expirations pointers
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
        expireAt := time.Now().Add(duration)

        gc.mu.Lock()
        defer gc.mu.Unlock()

        if item, exists := gc.keys[key]; exists {
                item.expireAt = expireAt
                // The item.index is always up-to-date thanks to heap.Swap
                heap.Fix(&gc.pq, item.index)
        } else {
                item := &expiryItem{key: key, expireAt: expireAt}
                heap.Push(&gc.pq, item)
                gc.keys[key] = item
        }

        // If this entry is now the earliest to expire, wake the run loop.
        if len(gc.pq) > 0 && gc.pq[0].key == key {
                gc.signalWakeUp()
        }
  }

  // run is the main loop that waits for and processes expirations.
  // Fixed: Restructured to avoid busy loops by always blocking on a timer.
  func (gc *GC) run(ctx context.Context) {
        defer gc.wg.Done()

        for {
                nextInterval := gc.calculateNextWakeInterval()

                // Handle immediate processing (keys expired NOW)
                if nextInterval == 0 {
                        gc.processExpiredKeys()
                        continue // Recalculate interval after processing
                }

                // Determine wait duration
                var waitDuration time.Duration
                if nextInterval > 0 {
                        waitDuration = nextInterval
                } else {
                        // Heap is empty, use default poll interval
                        waitDuration = gc.options.defaultPollInterval
                }

                // Use a timer for waiting - this properly blocks
                timer := time.NewTimer(waitDuration)

                select {
                case <-ctx.Done():
                        timer.Stop()
                        return
                case <-gc.stopSignal:
                        timer.Stop()
                        return
                case <-gc.wakeSignal:
                        timer.Stop()
                        continue
                case <-timer.C:
                        // Time to check for expired keys
                        gc.processExpiredKeys()
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
                delete(gc.keys, expired.key)
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
        gc.keys = make(map[string]*expiryItem)
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
