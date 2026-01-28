// Package clock provides a cached time source for high-performance scenarios.
// The cached time is updated every millisecond, reducing time.Now() calls
// in hot paths while maintaining sub-second accuracy.
package clock

import (
	"sync/atomic"
	"time"
)

// cachedNano stores the current time in Unix nanoseconds.
var cachedNano atomic.Int64

func init() {
	cachedNano.Store(time.Now().UnixNano())
	go func() {
		ticker := time.NewTicker(time.Millisecond)
		for range ticker.C {
			cachedNano.Store(time.Now().UnixNano())
		}
	}()
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
