package gcmap

import (
	safemap "github.com/OrlovEvgeny/go-mcache/safeMap"
	"sync"
	"time"
)

var (
	KeyChan      = make(chan string, 10000)
	stopHB       = make(chan bool, 1)
	stopEK       = make(chan bool, 1)
	gcInstance   *GC
	loadInstance = false
	gcStoped     = false
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

	for {
		select {
		case key := <-KeyChan:
			kset.Keys = append(kset.Keys, key)

		case <-stopEK:
			return
		}
	}
}

//
func (gc GC) heartBeatGC(kset *keyset) {
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

		case <-stopHB:
			return
		}
	}
}

//
func (gc GC) Expired(key string, duration time.Duration) {
	//TODO is it worth changing to - time.After(duration) ?
	time.Sleep(duration)
	if gcStoped {
		return
	}
	KeyChan <- key
}

//
func GCStop() {
	loadInstance = false
	gcStoped = true
	stopHB <- gcStoped
	stopEK <- gcStoped
	close(stopHB)
	close(stopEK)
	close(KeyChan)
}
