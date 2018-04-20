package gcmap

import (
	"context"
	safemap "github.com/OrlovEvgeny/go-mcache/safeMap"
	"sync"
	"time"
)

var (
	gcInstance   *GC
	loadInstance = false
)

//keyset - sync slice for expired keys
type keyset struct {
	Keys []string
	sync.Mutex
}

//GC garbage clean struct
type GC struct {
	storage safemap.SafeMap
	keyChan chan string
}

//NewGC - singleton func, returns *GC struct
func NewGC(ctx context.Context, store safemap.SafeMap) *GC {
	if loadInstance {
		return gcInstance
	}

	gc := new(GC)
	gc.storage = store
	gc.keyChan = make(chan string, 10000)
	go gc.ExpireKey(ctx)
	gcInstance = gc
	loadInstance = true

	return gc
}

//LenBufferKeyChan - returns len usage buffet of keyChan chanel
func (gc GC) LenBufferKeyChan() int {
	return len(gc.keyChan)
}

//collects foul keys, what to remove later
func (gc GC) ExpireKey(ctx context.Context) {
	kset := &keyset{Keys: make([]string, 0, 100)}
	go gc.heartBeatGC(ctx, kset)

	for {
		select {
		case key := <-gc.keyChan:
			kset.Keys = append(kset.Keys, key)

		case <-ctx.Done():
			loadInstance = false
			return
		}
	}
}

//removes old keys by timer
func (gc GC) heartBeatGC(ctx context.Context, kset *keyset) {
	//TODO it may be worthwhile to set a custom interval for deleting old keys
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

//fund Expired - gorutine which is launched every time the method is called, and ensures that the key is removed from the repository after the time expires
func (gc GC) Expired(ctx context.Context, key string, duration time.Duration) {
	select {
	case <-time.After(duration):
		gc.keyChan <- key
		return
	case <-ctx.Done():
		return
	}
}
