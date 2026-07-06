// Package clock provides a cached time source for high-performance scenarios.
// The cached time is updated every millisecond, reducing time.Now() calls
// in hot paths while maintaining sub-second accuracy.
package clock

import (
	"sync"
	"sync/atomic"
	"time"
)

// cachedNano stores the current time in Unix nanoseconds.
var (
	cachedNano atomic.Int64
	stopCh     = make(chan struct{})
	stopOnce   sync.Once
	startOnce  sync.Once
)

func init() {
	// Only snapshot the time at import; the updater goroutine is started
	// lazily by Ensure so merely importing the library does not spawn a
	// goroutine that wakes up 1000 times per second.
	cachedNano.Store(time.Now().UnixNano())
}

// Ensure starts the background updater goroutine on first call.
// Cache constructors call it; until then NowNano returns the import-time
// snapshot. Safe to call from multiple goroutines.
func Ensure() {
	startOnce.Do(func() {
		cachedNano.Store(time.Now().UnixNano())
		go func() {
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					cachedNano.Store(time.Now().UnixNano())
				case <-stopCh:
					return
				}
			}
		}()
	})
}

// NowNano returns the cached current time in Unix nanoseconds.
// This is significantly faster than time.Now().UnixNano() but may be
// up to 1ms stale.
func NowNano() int64 {
	return cachedNano.Load()
}

// Now returns the cached current time as time.Time.
// This is faster than time.Now() but may be up to 1ms stale.
func Now() time.Time {
	return time.Unix(0, cachedNano.Load())
}

// Stop stops the clock update goroutine.
// After Stop, NowNano() returns a stale value.
// Primarily useful for clean test teardown. Safe to call multiple times.
func Stop() {
	stopOnce.Do(func() {
		close(stopCh)
	})
}
