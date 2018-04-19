package go_mcache

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

//
func initStore() (context.Context, context.CancelFunc) {
	ctx, finish := context.WithCancel(context.Background())
	storage = safemap.NewStorage()
	gc = gcmap.NewGC(ctx, storage)
	return ctx, finish
}

//
type CacheDriver struct {
	ctx      context.Context
	closeCtx context.CancelFunc
}

//
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

//
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

//
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

//
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

//
func (mc *CacheDriver) SetPointer(key string, value interface{}, ttl time.Duration) error {
	expire := time.Now().Local().Add(ttl)
	go gc.Expired(mc.ctx, key, ttl)
	storage.Insert(key, entity.Item{Key: key, Expire: expire, DataLink: value})
	return nil
}

//
func (mc *CacheDriver) Remove(key string) {
	storage.Delete(key)
}

//
func (mc *CacheDriver) Truncate() {
	storage.Truncate()
}

//
func (mc *CacheDriver) Len() int {
	return storage.Len()
}

//
func (mc *CacheDriver) GCBufferQueue() int {
	return len(gcmap.KeyChan)
}

//
func (mc *CacheDriver) Close() map[string]interface{} {
	loadInstance = false
	mc.closeCtx()
	return storage.Close()
}

//
func encodeBytes(value interface{}) ([]byte, error) {
	return msgpack.Marshal(value)
}

//
func decodeBytes(buf []byte, value interface{}) error {
	return msgpack.Unmarshal(buf, value)
}
