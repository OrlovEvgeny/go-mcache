package go_mcache

import (
	"sync"
	"time"
)

//
type keyset struct {
	Keys []string
	sync.Mutex
}

// check expire cache
func isExpire(t time.Time) bool {
	return t.Before(time.Now().Local())
}

//
func expireKey(keyChan chan string) {
	ks := &keyset{Keys: make([]string, 0, 100)}
	go heartBeatGC(ks)
	for key := range keyChan {
		ks.Keys = append(ks.Keys, key)
	}
}

//
func heartBeatGC(ks *keyset) {
	for range time.Tick(time.Second * 3) {
		ks.Mutex.Lock()
		if len(ks.Keys) > 0 {
			keys := make([]string, len(ks.Keys))
			copy(keys, ks.Keys)
			storage.Flush(keys)
			ks.Keys = ks.Keys[:0]
		}
		ks.Mutex.Unlock()
	}
}

//
func expired(key string, duration time.Duration) {
	<-time.After(duration)
	keyChan <- key
	return
}
