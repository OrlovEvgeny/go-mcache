package go_mcache

import (
	gcmap "github.com/OrlovEvgeny/go-mcache/gcmap"
	entity "github.com/OrlovEvgeny/go-mcache/item"
	safemap "github.com/OrlovEvgeny/go-mcache/safeMap"
	"github.com/vmihailenco/msgpack"
	"log"
	"time"
)

//
var (
	storage      safemap.SafeMap
	gc           *gcmap.GC
	instance     *CacheDriver
	loadInstance = false
)

//
func initStore() {
	storage = safemap.NewStorage()
	gc = gcmap.NewGC(storage)
}

//
type CacheDriver struct{}

//
func StartInstance() *CacheDriver {
	if loadInstance {
		return instance
	}
	initStore()
	instance = new(CacheDriver)
	loadInstance = true
	return instance
}

//
func (mc *CacheDriver) Get(key string, struc interface{}) bool {
	data, ok := storage.Find(key)
	if !ok {
		return false
	}
	item := data.(entity.Item)
	if entity.IsExpire(item.Expire) {
		return false
	}
	err := decodeBytes(item.Data, struc)
	if err != nil {
		log.Fatal("error Decoding bytes cache data: ", err)
	}
	return true
}

//
func (mc *CacheDriver) GetPointer(key string) (interface{}, bool) {
	data, ok := storage.Find(key)
	if !ok {
		return entity.Item{}.DataLink, false
	}
	item := data.(entity.Item)
	if entity.IsExpire(item.Expire) {
		return entity.Item{}.DataLink, false
	}
	return item.DataLink, true
}

//
func (mc *CacheDriver) Set(key string, value interface{}, ttl time.Duration) error {
	expire := time.Now().Local().Add(ttl)
	go gc.Expired(key, ttl)
	v, err := encodeBytes(value)
	if err != nil {
		log.Println("MCACHE SET ERROR: ", err)
		return err
	}
	storage.Insert(key, entity.Item{Key: key, Expire: expire, Data: v})
	return nil
}

//
func (mc *CacheDriver) SetPointer(key string, value interface{}, ttl time.Duration) error {
	expire := time.Now().Local().Add(ttl)
	go gc.Expired(key, ttl)
	storage.Insert(key, entity.Item{Key: key, Expire: expire, DataLink: value})
	return nil
}

//
func (mc *CacheDriver) Remove(key string) {
	storage.Delete(key)
}

//
func (mc *CacheDriver) Len() int {
	return storage.Len()
}

//
func encodeBytes(value interface{}) ([]byte, error) {
	return msgpack.Marshal(value)
}

//
func decodeBytes(buf []byte, value interface{}) error {
	return msgpack.Unmarshal(buf, value)
}
