package go_mcache

import (
	safemap "gopkg.in/OrlovEvgeny/go-mcache.v1/safeMap"
	gcmap "gopkg.in/OrlovEvgeny/go-mcache.v1/gcmap"
	i "gopkg.in/OrlovEvgeny/go-mcache.v1/item"
	"github.com/vmihailenco/msgpack"
	"log"
	"time"
)


//
var (
	storage      safemap.SafeMap
	gc			 *gcmap.GC
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
	if !loadInstance {
		initStore()
		instance = new(CacheDriver)
		loadInstance = true
		return instance
	}
	return instance
}

//
func (mc *CacheDriver) Get(key string, struc interface{}) bool {
	data, ok := storage.Find(key)
	if ok {
		item := data.(i.Item)
		if i.IsExpire(item.Expire) {
			return false
		}
		err := decodeBytes(item.Data, struc)
		if err != nil {
			log.Fatal("error Decoding bytes cache data: ", err)
		}
		return true
	}
	return false
}

//
func (mc *CacheDriver) GetPointer(key string) (interface{}, bool) {
	if data, ok := storage.Find(key); ok {
		item := data.(i.Item)
		if i.IsExpire(item.Expire) {
			return i.Item{}.DataLink, false
		}
		return item.DataLink, true
	}
	return i.Item{}.DataLink, false
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
	storage.Insert(key, i.Item{Key: key, Expire: expire, Data: v})
	return nil
}

//
func (mc *CacheDriver) SetPointer(key string, value interface{}, ttl time.Duration) error {
	expire := time.Now().Local().Add(ttl)
	go gc.Expired(key, ttl)
	storage.Insert(key, i.Item{Key: key, Expire: expire, DataLink: value})
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
