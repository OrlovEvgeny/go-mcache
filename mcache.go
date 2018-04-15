package go_mcache

import (
	safemap "safeMap"
	"github.com/vmihailenco/msgpack"
	"log"
	"time"
)

//
type Item struct {
	Key      string
	Expire   time.Time
	Data     []byte
	DataLink interface{}
}

//
var (
	storage      safemap.SafeMap
	instance     *CacheDriver
	loadInstance = false
	keyChan      = make(chan string, 1000)
)

//
func initStore() {
	storage = safemap.New()
	go expireKey(keyChan)
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
		item := data.(Item)
		if isExpire(item.Expire) {
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
		item := data.(Item)
		if isExpire(item.Expire) {
			return Item{}.DataLink, false
		}
		return item.DataLink, true
	}
	return Item{}.DataLink, false
}

//
func (mc *CacheDriver) Set(key string, value interface{}, ttl time.Duration) error {
	expire := time.Now().Local().Add(ttl)
	go expired(key, ttl)
	v, err := encodeBytes(value)
	if err != nil {
		log.Println("MCACHE SET ERROR: ", err)
		return err
	}
	storage.Insert(key, Item{Key: key, Expire: expire, Data: v})
	return nil
}

//
func (mc *CacheDriver) SetPointer(key string, value interface{}, ttl time.Duration) error {
	expire := time.Now().Local().Add(ttl)
	go expired(key, ttl)
	storage.Insert(key, Item{Key: key, Expire: expire, DataLink: value})
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
	buf, err := msgpack.Marshal(value)
	if err != nil {
		return buf, err
	}
	return buf, err
}

//
func decodeBytes(buf []byte, value interface{}) error {
	err := msgpack.Unmarshal(buf, value)
	if err != nil {
		return err
	}
	return nil
}
