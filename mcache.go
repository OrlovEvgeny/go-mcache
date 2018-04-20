package gomcache

import (
	"context"
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

//initStore - returns context and context close func. Inited map storage and remove old cache
func initStore() (context.Context, context.CancelFunc) {
	ctx, finish := context.WithCancel(context.Background())
	storage = safemap.NewStorage()
	gc = gcmap.NewGC(ctx, storage)
	return ctx, finish
}

//CacheDriver context struct
type CacheDriver struct {
	ctx      context.Context
	closeCtx context.CancelFunc
}

//StartInstance - singleton func, returns CacheDriver struct
func StartInstance() *CacheDriver {
	if loadInstance {
		return instance
	}

	ctx, finish := initStore()

	instance = new(CacheDriver)
	instance.ctx = ctx
	instance.closeCtx = finish

	loadInstance = true
	return instance
}

//Get - returns serialize data
func (mc *CacheDriver) Get(key string, struc interface{}) bool {
	data, ok := storage.Find(key)
	if !ok {
		return false
	}
	item := data.(entity.Item)
	if item.IsExpire() {
		return false
	}
	err := decodeBytes(item.Data, struc)
	if err != nil {
		log.Fatal("error Decoding bytes cache data: ", err)
	}
	return true
}

//GetPointer - returns &pointer
func (mc *CacheDriver) GetPointer(key string) (interface{}, bool) {
	data, ok := storage.Find(key)
	if !ok {
		return entity.Item{}.DataLink, false
	}
	item := data.(entity.Item)
	if item.IsExpire() {
		return entity.Item{}.DataLink, false
	}
	return item.DataLink, true
}

//Set - add cache data and serialize/deserialize value
func (mc *CacheDriver) Set(key string, value interface{}, ttl time.Duration) error {
	expire := time.Now().Local().Add(ttl)
	go gc.Expired(mc.ctx, key, ttl)
	v, err := encodeBytes(value)
	if err != nil {
		log.Println("MCACHE SET ERROR: ", err)
		return err
	}
	storage.Insert(key, entity.Item{Key: key, Expire: expire, Data: v})
	return nil
}

//SetPointer - add cache &pointer data (more and example info README.md)
func (mc *CacheDriver) SetPointer(key string, value interface{}, ttl time.Duration) error {
	expire := time.Now().Local().Add(ttl)
	go gc.Expired(mc.ctx, key, ttl)
	storage.Insert(key, entity.Item{Key: key, Expire: expire, DataLink: value})
	return nil
}

//Remove - value by key
func (mc *CacheDriver) Remove(key string) {
	storage.Delete(key)
}

//Truncate - clean cache storage
func (mc *CacheDriver) Truncate() {
	storage.Truncate()
}

//Len - returns current count storage
func (mc *CacheDriver) Len() int {
	return storage.Len()
}

//GCBufferQueue - returns the current use len KeyChan chanel buffer
func (mc *CacheDriver) GCBufferQueue() int {
	return gc.LenBufferKeyChan()
}

//Close - close all MCache
func (mc *CacheDriver) Close() map[string]interface{} {
	loadInstance = false
	mc.closeCtx()
	return storage.Close()
}

//deserialize value
func encodeBytes(value interface{}) ([]byte, error) {
	return msgpack.Marshal(value)
}

//serialize value
func decodeBytes(buf []byte, value interface{}) error {
	return msgpack.Unmarshal(buf, value)
}
