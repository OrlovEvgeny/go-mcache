package gcmap

import (
	"context"
	safemap "github.com/OrlovEvgeny/go-mcache/safeMap"
	"sync"
	"time"
)

var (
	KeyChan      = make(chan string, 10000)
	gcInstance   *GC
	loadInstance = false
)

//
type keyset struct {
	Keys []string
	sync.Mutex
}

//
type GC struct {
	storage safemap.SafeMap
}

//
func NewGC(ctx context.Context, store safemap.SafeMap) *GC {
	if loadInstance {
		return gcInstance
	}

	gc := new(GC)
	gc.storage = store
	go gc.ExpireKey(ctx)
	gcInstance = gc
	loadInstance = true

	return gc
}

//
func (gc GC) ExpireKey(ctx context.Context) {
	kset := &keyset{Keys: make([]string, 0, 100)}
	go gc.heartBeatGC(ctx, kset)

	for {
		select {
		case key := <-KeyChan:
			kset.Keys = append(kset.Keys, key)

		case <-ctx.Done():
			loadInstance = false
			return
		}
	}
}

//
func (gc GC) heartBeatGC(ctx context.Context, kset *keyset) {
	ticker := time.NewTicker(time.Second * 3)
	for {
		select {
		case <-ticker.C:
			if len(kset.Keys) == 0 {
				continue
			}
			kset.Mutex.Lock()
			keys := make([]string, len(kset.Keys))
			copy(keys, kset.Keys)
			gc.storage.Flush(keys)
			kset.Keys = kset.Keys[:0]
			kset.Mutex.Unlock()

		case <-ctx.Done():
			return
		}
	}
}

//
func (gc GC) Expired(ctx context.Context, key string, duration time.Duration) {

	select {
	case <-time.After(duration):
		KeyChan <- key
		return
	case <-ctx.Done():
		return
	}
}
