package mcache_test

import (
	"context"
	"time"

	theine "github.com/Yiling-J/theine-go"
	bigcache "github.com/allegro/bigcache/v3"
	freecache "github.com/coocood/freecache"
	ristretto "github.com/dgraph-io/ristretto/v2"
	lru "github.com/hashicorp/golang-lru/v2/expirable"
	ttlcache "github.com/jellydator/ttlcache/v3"
	otter "github.com/maypok86/otter/v2"
	gocache "github.com/patrickmn/go-cache"

	mcache "github.com/OrlovEvgeny/go-mcache"
)

type benchCapabilities struct {
	bounded    bool
	preciseTTL bool
}

type benchConfig struct {
	keyspace        int
	hotSet          int
	preload         int
	payloadSize     int
	valuePoolSize   int
	capacityEntries int
	byteLimit       int64
	ttl             time.Duration
}

type benchData struct {
	stringKeys []string
	byteKeys   [][]byte
	values     [][]byte
	zipfTrace  []int
}

type benchCache interface {
	Set(keyIdx int, value []byte, ttl time.Duration) bool
	Get(keyIdx int) ([]byte, bool)
	Delete(keyIdx int) bool
	Sync()
	Close()
}

type benchFactory struct {
	name string
	caps benchCapabilities
	new  func(cfg benchConfig, data *benchData) (benchCache, error)
}

func benchFactories() []benchFactory {
	return []benchFactory{
		{name: "go-mcache", caps: benchCapabilities{bounded: true, preciseTTL: true}, new: newMCacheAdapter},
		{name: "ristretto", caps: benchCapabilities{bounded: true, preciseTTL: true}, new: newRistrettoAdapter},
		{name: "bigcache", caps: benchCapabilities{bounded: true}, new: newBigCacheAdapter},
		{name: "freecache", caps: benchCapabilities{bounded: true}, new: newFreeCacheAdapter},
		{name: "go-cache", caps: benchCapabilities{preciseTTL: true}, new: newGoCacheAdapter},
		{name: "ttlcache", caps: benchCapabilities{bounded: true, preciseTTL: true}, new: newTTLCacheAdapter},
		{name: "golang-lru-expirable", caps: benchCapabilities{bounded: true, preciseTTL: true}, new: newLRUAdapter},
		{name: "otter", caps: benchCapabilities{bounded: true, preciseTTL: true}, new: newOtterAdapter},
		{name: "theine", caps: benchCapabilities{bounded: true, preciseTTL: true}, new: newTheineAdapter},
	}
}

type mcacheAdapter struct {
	cache *mcache.Cache[string, []byte]
	keys  []string
}

func newMCacheAdapter(cfg benchConfig, data *benchData) (benchCache, error) {
	opts := make([]mcache.Option[string, []byte], 0, 4)
	opts = append(opts,
		mcache.WithMetrics[string, []byte](false),
		mcache.WithExpirationResolution[string, []byte](100*time.Millisecond),
	)
	if cfg.byteLimit > 0 {
		opts = append(opts,
			mcache.WithMaxCost[string, []byte](cfg.byteLimit),
			mcache.WithCostFunc[string, []byte](func(v []byte) int64 { return int64(len(v)) }),
		)
	} else if cfg.capacityEntries > 0 {
		opts = append(opts, mcache.WithMaxEntries[string, []byte](int64(cfg.capacityEntries)))
	}
	return &mcacheAdapter{
		cache: mcache.NewCache[string, []byte](opts...),
		keys:  data.stringKeys,
	}, nil
}

func (a *mcacheAdapter) Set(keyIdx int, value []byte, ttl time.Duration) bool {
	return a.cache.Set(a.keys[keyIdx], value, ttl)
}

func (a *mcacheAdapter) Get(keyIdx int) ([]byte, bool) {
	return a.cache.Get(a.keys[keyIdx])
}

func (a *mcacheAdapter) Delete(keyIdx int) bool {
	return a.cache.Delete(a.keys[keyIdx])
}

func (a *mcacheAdapter) Sync() {
	a.cache.Wait()
}

func (a *mcacheAdapter) Close() {
	a.cache.Close()
}

type ristrettoAdapter struct {
	cache *ristretto.Cache[string, []byte]
	keys  []string
}

func newRistrettoAdapter(cfg benchConfig, data *benchData) (benchCache, error) {
	maxCost := cfg.byteLimit
	if maxCost <= 0 {
		maxCost = int64(maxInt(cfg.capacityEntries, 1))
	}
	cache, err := ristretto.NewCache(&ristretto.Config[string, []byte]{
		NumCounters:        int64(maxInt(cfg.keyspace*10, 1024)),
		MaxCost:            maxCost,
		BufferItems:        64,
		IgnoreInternalCost: true,
		Cost:               func(v []byte) int64 { return int64(len(v)) },
	})
	if err != nil {
		return nil, err
	}
	return &ristrettoAdapter{cache: cache, keys: data.stringKeys}, nil
}

func (a *ristrettoAdapter) Set(keyIdx int, value []byte, ttl time.Duration) bool {
	if ttl > 0 {
		return a.cache.SetWithTTL(a.keys[keyIdx], value, 0, ttl)
	}
	return a.cache.Set(a.keys[keyIdx], value, 0)
}

func (a *ristrettoAdapter) Get(keyIdx int) ([]byte, bool) {
	return a.cache.Get(a.keys[keyIdx])
}

func (a *ristrettoAdapter) Delete(keyIdx int) bool {
	a.cache.Del(a.keys[keyIdx])
	return true
}

func (a *ristrettoAdapter) Sync() {
	a.cache.Wait()
}

func (a *ristrettoAdapter) Close() {
	a.cache.Close()
}

type bigCacheAdapter struct {
	cache *bigcache.BigCache
	keys  []string
}

func newBigCacheAdapter(cfg benchConfig, data *benchData) (benchCache, error) {
	lifeWindow := ttlOrDefault(cfg.ttl, 24*time.Hour)
	bigCfg := bigcache.DefaultConfig(lifeWindow)
	bigCfg.CleanWindow = 0
	bigCfg.Verbose = false
	bigCfg.MaxEntriesInWindow = maxInt(cfg.keyspace, 1024)
	bigCfg.MaxEntrySize = maxInt(cfg.payloadSize, 128)
	if cfg.byteLimit > 0 {
		bigCfg.HardMaxCacheSize = bytesToMiB(cfg.byteLimit)
	}
	cache, err := bigcache.New(context.Background(), bigCfg)
	if err != nil {
		return nil, err
	}
	return &bigCacheAdapter{cache: cache, keys: data.stringKeys}, nil
}

func (a *bigCacheAdapter) Set(keyIdx int, value []byte, _ time.Duration) bool {
	return a.cache.Set(a.keys[keyIdx], value) == nil
}

func (a *bigCacheAdapter) Get(keyIdx int) ([]byte, bool) {
	value, err := a.cache.Get(a.keys[keyIdx])
	return value, err == nil
}

func (a *bigCacheAdapter) Delete(keyIdx int) bool {
	return a.cache.Delete(a.keys[keyIdx]) == nil
}

func (a *bigCacheAdapter) Sync() {}

func (a *bigCacheAdapter) Close() {
	_ = a.cache.Close()
}

type freeCacheAdapter struct {
	cache *freecache.Cache
	keys  [][]byte
}

func newFreeCacheAdapter(cfg benchConfig, data *benchData) (benchCache, error) {
	size := int(cfg.byteLimit)
	if size <= 0 {
		size = int(maxInt64(int64(cfg.keyspace*cfg.payloadSize*4), 4<<20))
	}
	return &freeCacheAdapter{
		cache: freecache.NewCache(size),
		keys:  data.byteKeys,
	}, nil
}

func (a *freeCacheAdapter) Set(keyIdx int, value []byte, ttl time.Duration) bool {
	return a.cache.Set(a.keys[keyIdx], value, ttlSeconds(ttl)) == nil
}

func (a *freeCacheAdapter) Get(keyIdx int) ([]byte, bool) {
	value, err := a.cache.Get(a.keys[keyIdx])
	return value, err == nil
}

func (a *freeCacheAdapter) Delete(keyIdx int) bool {
	return a.cache.Del(a.keys[keyIdx])
}

func (a *freeCacheAdapter) Sync()  {}
func (a *freeCacheAdapter) Close() {}

type goCacheAdapter struct {
	cache *gocache.Cache
	keys  []string
}

func newGoCacheAdapter(_ benchConfig, data *benchData) (benchCache, error) {
	return &goCacheAdapter{
		cache: gocache.New(gocache.NoExpiration, 0),
		keys:  data.stringKeys,
	}, nil
}

func (a *goCacheAdapter) Set(keyIdx int, value []byte, ttl time.Duration) bool {
	exp := gocache.NoExpiration
	if ttl > 0 {
		exp = ttl
	}
	a.cache.Set(a.keys[keyIdx], value, exp)
	return true
}

func (a *goCacheAdapter) Get(keyIdx int) ([]byte, bool) {
	value, ok := a.cache.Get(a.keys[keyIdx])
	if !ok {
		return nil, false
	}
	buf, ok := value.([]byte)
	return buf, ok
}

func (a *goCacheAdapter) Delete(keyIdx int) bool {
	a.cache.Delete(a.keys[keyIdx])
	return true
}

func (a *goCacheAdapter) Sync()  {}
func (a *goCacheAdapter) Close() {}

type ttlCacheAdapter struct {
	cache *ttlcache.Cache[string, []byte]
	keys  []string
}

func newTTLCacheAdapter(cfg benchConfig, data *benchData) (benchCache, error) {
	opts := []ttlcache.Option[string, []byte]{ttlcache.WithDisableTouchOnHit[string, []byte]()}
	if cfg.byteLimit > 0 {
		opts = append(opts, ttlcache.WithMaxCost[string, []byte](uint64(cfg.byteLimit), func(item ttlcache.CostItem[string, []byte]) uint64 {
			return uint64(len(item.Value))
		}))
	} else if cfg.capacityEntries > 0 {
		opts = append(opts, ttlcache.WithCapacity[string, []byte](uint64(cfg.capacityEntries)))
	}
	return &ttlCacheAdapter{
		cache: ttlcache.New[string, []byte](opts...),
		keys:  data.stringKeys,
	}, nil
}

func (a *ttlCacheAdapter) Set(keyIdx int, value []byte, ttl time.Duration) bool {
	if ttl <= 0 {
		ttl = ttlcache.NoTTL
	}
	return a.cache.Set(a.keys[keyIdx], value, ttl) != nil
}

func (a *ttlCacheAdapter) Get(keyIdx int) ([]byte, bool) {
	item := a.cache.Get(a.keys[keyIdx])
	if item == nil {
		return nil, false
	}
	return item.Value(), true
}

func (a *ttlCacheAdapter) Delete(keyIdx int) bool {
	a.cache.Delete(a.keys[keyIdx])
	return true
}

func (a *ttlCacheAdapter) Sync()  {}
func (a *ttlCacheAdapter) Close() {}

type lruAdapter struct {
	cache *lru.LRU[string, []byte]
	keys  []string
}

func newLRUAdapter(cfg benchConfig, data *benchData) (benchCache, error) {
	size := cfg.capacityEntries
	if size <= 0 {
		if cfg.byteLimit > 0 {
			size = maxInt(1, int(cfg.byteLimit/int64(maxInt(cfg.payloadSize, 1))))
		} else {
			size = maxInt(cfg.keyspace*2, 1024)
		}
	}
	return &lruAdapter{
		cache: lru.NewLRU[string, []byte](size, nil, cfg.ttl),
		keys:  data.stringKeys,
	}, nil
}

func (a *lruAdapter) Set(keyIdx int, value []byte, _ time.Duration) bool {
	a.cache.Add(a.keys[keyIdx], value)
	return true
}

func (a *lruAdapter) Get(keyIdx int) ([]byte, bool) {
	return a.cache.Get(a.keys[keyIdx])
}

func (a *lruAdapter) Delete(keyIdx int) bool {
	return a.cache.Remove(a.keys[keyIdx])
}

func (a *lruAdapter) Sync()  {}
func (a *lruAdapter) Close() {}

type otterAdapter struct {
	cache *otter.Cache[string, []byte]
	keys  []string
}

func newOtterAdapter(cfg benchConfig, data *benchData) (benchCache, error) {
	opts := &otter.Options[string, []byte]{
		InitialCapacity: maxInt(cfg.preload, 16),
	}
	if cfg.byteLimit > 0 {
		opts.MaximumWeight = uint64(cfg.byteLimit)
		opts.Weigher = func(_ string, value []byte) uint32 {
			return uint32(len(value))
		}
	} else if cfg.capacityEntries > 0 {
		opts.MaximumSize = cfg.capacityEntries
	}
	if cfg.ttl > 0 {
		opts.ExpiryCalculator = otter.ExpiryWriting[string, []byte](cfg.ttl)
	}
	cache, err := otter.New(opts)
	if err != nil {
		return nil, err
	}
	return &otterAdapter{cache: cache, keys: data.stringKeys}, nil
}

func (a *otterAdapter) Set(keyIdx int, value []byte, _ time.Duration) bool {
	_, inserted := a.cache.Set(a.keys[keyIdx], value)
	return inserted
}

func (a *otterAdapter) Get(keyIdx int) ([]byte, bool) {
	return a.cache.GetIfPresent(a.keys[keyIdx])
}

func (a *otterAdapter) Delete(keyIdx int) bool {
	_, invalidated := a.cache.Invalidate(a.keys[keyIdx])
	return invalidated
}

func (a *otterAdapter) Sync() {}

func (a *otterAdapter) Close() {
	a.cache.InvalidateAll()
	a.cache.CleanUp()
}

type theineAdapter struct {
	cache *theine.Cache[string, []byte]
	keys  []string
}

func newTheineAdapter(cfg benchConfig, data *benchData) (benchCache, error) {
	builder := theine.NewBuilder[string, []byte](int64(maxInt(cfg.capacityEntries, 1)))
	if cfg.byteLimit > 0 {
		builder = theine.NewBuilder[string, []byte](cfg.byteLimit).Cost(func(v []byte) int64 { return int64(len(v)) })
	} else if cfg.capacityEntries <= 0 {
		builder = theine.NewBuilder[string, []byte](int64(maxInt(cfg.keyspace*2, 1024)))
	}
	cache, err := builder.Build()
	if err != nil {
		return nil, err
	}
	return &theineAdapter{cache: cache, keys: data.stringKeys}, nil
}

func (a *theineAdapter) Set(keyIdx int, value []byte, ttl time.Duration) bool {
	if ttl > 0 {
		return a.cache.SetWithTTL(a.keys[keyIdx], value, 0, ttl)
	}
	return a.cache.Set(a.keys[keyIdx], value, 0)
}

func (a *theineAdapter) Get(keyIdx int) ([]byte, bool) {
	return a.cache.Get(a.keys[keyIdx])
}

func (a *theineAdapter) Delete(keyIdx int) bool {
	a.cache.Delete(a.keys[keyIdx])
	return true
}

func (a *theineAdapter) Sync() {
	a.cache.Wait()
}

func (a *theineAdapter) Close() {
	a.cache.Close()
}

func ttlOrDefault(ttl, fallback time.Duration) time.Duration {
	if ttl > 0 {
		return ttl
	}
	return fallback
}

func ttlSeconds(ttl time.Duration) int {
	if ttl <= 0 {
		return 0
	}
	return int((ttl + time.Second - 1) / time.Second)
}

func bytesToMiB(size int64) int {
	if size <= 0 {
		return 0
	}
	return maxInt(1, int((size+(1<<20)-1)/(1<<20)))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
