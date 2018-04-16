package gcmap

import (
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
func NewGC(store safemap.SafeMap) *GC {
	if loadInstance {
		return gcInstance
	}

	gc := new(GC)
	gc.storage = store
	go gc.ExpireKey()
	gcInstance = gc
	loadInstance = true

	return gc
}

//
func (gc GC) ExpireKey() {
	kset := &keyset{Keys: make([]string, 0, 100)}
	go gc.heartBeatGC(kset)
	for key := range KeyChan {
		kset.Keys = append(kset.Keys, key)
	}
}

//
func (gc GC) heartBeatGC(kset *keyset) {
	for range time.Tick(time.Second * 3) {
		if len(kset.Keys) == 0 {
			continue
		}

		kset.Mutex.Lock()
		keys := make([]string, len(kset.Keys))
		copy(keys, kset.Keys)
		gc.storage.Flush(keys)
		kset.Keys = kset.Keys[:0]
		kset.Mutex.Unlock()
	}
}

//
func (gc GC) Expired(key string, duration time.Duration) {
	//TODO is it worth changing to - time.After(duration) ?
	time.Sleep(duration)
	KeyChan <- key
}
