package mcache_test

import (
	"math/rand"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

const compareZipfTraceSize = 1 << 15

func BenchmarkCompare(b *testing.B) {
	b.Run("Core", func(b *testing.B) {
		cfg := benchConfig{
			keyspace:        16_384,
			hotSet:          4_096,
			preload:         4_096,
			payloadSize:     256,
			valuePoolSize:   4_096,
			capacityEntries: 65_536,
			byteLimit:       64 << 20,
		}
		runComparisonScenario(b, "ReadParallelHot", cfg, func(b *testing.B, cache benchCache, data *benchData) {
			preloadCache(b, cache, data, cfg.preload, 0)
			benchmarkReadParallel(b, cache, cfg.hotSet)
		})
		runComparisonScenario(b, "WriteParallelOverwrite", cfg, func(b *testing.B, cache benchCache, data *benchData) {
			preloadCache(b, cache, data, cfg.preload, 0)
			benchmarkWriteParallel(b, cache, data, cfg.hotSet, 0)
		})
		runComparisonScenario(b, "MixedParallel80_20", cfg, func(b *testing.B, cache benchCache, data *benchData) {
			preloadCache(b, cache, data, cfg.preload, 0)
			benchmarkMixedParallel(b, cache, data, cfg.hotSet, 8)
		})
		runComparisonScenario(b, "DeleteCycle", cfg, func(b *testing.B, cache benchCache, data *benchData) {
			preloadCache(b, cache, data, cfg.preload, 0)
			benchmarkDeleteCycle(b, cache, data, cfg.preload)
		})
	})

	b.Run("TTL", func(b *testing.B) {
		cfg := benchConfig{
			keyspace:        8_192,
			hotSet:          4_096,
			preload:         4_096,
			payloadSize:     256,
			valuePoolSize:   4_096,
			capacityEntries: 16_384,
			byteLimit:       16 << 20,
			ttl:             5 * time.Minute,
		}
		runComparisonScenario(b, "SetWithTTLParallel", cfg, func(b *testing.B, cache benchCache, data *benchData) {
			preloadCache(b, cache, data, cfg.preload, 0)
			benchmarkWriteParallel(b, cache, data, cfg.hotSet, cfg.ttl)
		})

		expiredCfg := cfg
		expiredCfg.keyspace = 4_096
		expiredCfg.hotSet = 4_096
		expiredCfg.preload = 4_096
		expiredCfg.ttl = 25 * time.Millisecond
		runComparisonScenarioWithFilter(b, "ExpiredRead", expiredCfg, func(factory benchFactory) bool {
			return factory.caps.preciseTTL
		}, func(b *testing.B, cache benchCache, data *benchData) {
			preloadCache(b, cache, data, expiredCfg.preload, expiredCfg.ttl)
			time.Sleep(expiredCfg.ttl + 15*time.Millisecond)
			benchmarkReadParallel(b, cache, expiredCfg.hotSet)
		})
	})

	b.Run("Bounded", func(b *testing.B) {
		cfg := benchConfig{
			keyspace:        32_768,
			hotSet:          4_096,
			preload:         4_096,
			payloadSize:     256,
			valuePoolSize:   4_096,
			capacityEntries: 4_096,
			byteLimit:       4 << 20,
		}
		runComparisonScenarioWithFilter(b, "MixedParallelZipf95_5", cfg, func(factory benchFactory) bool {
			return factory.caps.bounded
		}, func(b *testing.B, cache benchCache, data *benchData) {
			preloadCache(b, cache, data, cfg.preload, 0)
			benchmarkZipfMixedParallel(b, cache, data, 19)
		})
		runComparisonScenarioWithFilter(b, "MissThenSetZipf", cfg, func(factory benchFactory) bool {
			return factory.caps.bounded
		}, func(b *testing.B, cache benchCache, data *benchData) {
			preloadCache(b, cache, data, cfg.preload, 0)
			benchmarkZipfMissThenSet(b, cache, data)
		})
	})
}

func runComparisonScenario(
	b *testing.B,
	name string,
	cfg benchConfig,
	fn func(*testing.B, benchCache, *benchData),
) {
	runComparisonScenarioWithFilter(b, name, cfg, func(benchFactory) bool { return true }, fn)
}

func runComparisonScenarioWithFilter(
	b *testing.B,
	name string,
	cfg benchConfig,
	filter func(benchFactory) bool,
	fn func(*testing.B, benchCache, *benchData),
) {
	b.Run(name, func(b *testing.B) {
		data := newBenchData(cfg)
		for _, factory := range benchFactories() {
			if !filter(factory) {
				continue
			}
			b.Run(factory.name, func(b *testing.B) {
				cache, err := factory.new(cfg, data)
				if err != nil {
					b.Fatalf("new cache %s: %v", factory.name, err)
				}
				b.Cleanup(cache.Close)
				b.ReportAllocs()
				b.SetBytes(int64(cfg.payloadSize))
				fn(b, cache, data)
			})
		}
	})
}

func newBenchData(cfg benchConfig) *benchData {
	stringKeys := make([]string, cfg.keyspace)
	byteKeys := make([][]byte, cfg.keyspace)
	for i := 0; i < cfg.keyspace; i++ {
		key := "cmp:" + strconv.Itoa(i)
		stringKeys[i] = key
		byteKeys[i] = []byte(key)
	}

	values := make([][]byte, cfg.valuePoolSize)
	for i := range values {
		values[i] = makePayload(i, cfg.payloadSize)
	}

	return &benchData{
		stringKeys: stringKeys,
		byteKeys:   byteKeys,
		values:     values,
		zipfTrace:  makeZipfTrace(compareZipfTraceSize, cfg.keyspace),
	}
}

func makePayload(seed, size int) []byte {
	buf := make([]byte, size)
	rng := newXorShift64(uint64(seed) + 1)
	for i := range buf {
		buf[i] = byte(rng.next())
	}
	return buf
}

func makeZipfTrace(n, universe int) []int {
	trace := make([]int, n)
	if universe <= 1 {
		return trace
	}
	rng := rand.New(rand.NewSource(42))
	zipf := rand.NewZipf(rng, 1.15, 1, uint64(universe-1))
	for i := range trace {
		trace[i] = int(zipf.Uint64())
	}
	return trace
}

func preloadCache(b *testing.B, cache benchCache, data *benchData, count int, ttl time.Duration) {
	b.Helper()
	for i := 0; i < count; i++ {
		keyIdx := i % len(data.stringKeys)
		value := data.values[i%len(data.values)]
		cache.Set(keyIdx, value, ttl)
	}
	cache.Sync()
}

func benchmarkReadParallel(b *testing.B, cache benchCache, hotSet int) {
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := newWorkerRNG()
		for pb.Next() {
			cache.Get(int(rng.next() % uint64(hotSet)))
		}
	})
}

func benchmarkWriteParallel(b *testing.B, cache benchCache, data *benchData, hotSet int, ttl time.Duration) {
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := newWorkerRNG()
		for pb.Next() {
			keyIdx := int(rng.next() % uint64(hotSet))
			value := data.values[int(rng.next()%uint64(len(data.values)))]
			cache.Set(keyIdx, value, ttl)
		}
	})
	cache.Sync()
}

func benchmarkMixedParallel(b *testing.B, cache benchCache, data *benchData, hotSet int, readThreshold uint64) {
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := newWorkerRNG()
		for pb.Next() {
			keyIdx := int(rng.next() % uint64(hotSet))
			if rng.next()%10 < readThreshold {
				cache.Get(keyIdx)
				continue
			}
			value := data.values[int(rng.next()%uint64(len(data.values)))]
			cache.Set(keyIdx, value, 0)
		}
	})
	cache.Sync()
}

func benchmarkDeleteCycle(b *testing.B, cache benchCache, data *benchData, keyspace int) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i > 0 && i%keyspace == 0 {
			b.StopTimer()
			preloadCache(b, cache, data, keyspace, 0)
			b.StartTimer()
		}
		cache.Delete(i % keyspace)
	}
}

func benchmarkZipfMixedParallel(b *testing.B, cache benchCache, data *benchData, readThreshold uint64) {
	trace := data.zipfTrace
	mask := len(trace) - 1
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := newWorkerRNG()
		pos := int(rng.next()) & mask
		for pb.Next() {
			keyIdx := trace[pos]
			pos = (pos + 1) & mask
			if rng.next()%20 < readThreshold {
				cache.Get(keyIdx)
				continue
			}
			value := data.values[int(rng.next()%uint64(len(data.values)))]
			cache.Set(keyIdx, value, 0)
		}
	})
	cache.Sync()
}

func benchmarkZipfMissThenSet(b *testing.B, cache benchCache, data *benchData) {
	trace := data.zipfTrace
	mask := len(trace) - 1
	var hits atomic.Int64
	var misses atomic.Int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rng := newWorkerRNG()
		pos := int(rng.next()) & mask
		localHits := int64(0)
		localMisses := int64(0)
		for pb.Next() {
			keyIdx := trace[pos]
			pos = (pos + 1) & mask
			if _, ok := cache.Get(keyIdx); ok {
				localHits++
				continue
			}
			localMisses++
			value := data.values[int(rng.next()%uint64(len(data.values)))]
			cache.Set(keyIdx, value, 0)
		}
		hits.Add(localHits)
		misses.Add(localMisses)
	})
	cache.Sync()

	total := hits.Load() + misses.Load()
	if total > 0 {
		b.ReportMetric(float64(hits.Load())*100/float64(total), "hit%")
	}
}

var benchWorkerSeed atomic.Uint64

type xorshift64 struct {
	state uint64
}

func newXorShift64(seed uint64) xorshift64 {
	if seed == 0 {
		seed = 1
	}
	return xorshift64{state: seed}
}

func newWorkerRNG() xorshift64 {
	seed := benchWorkerSeed.Add(0x9e3779b97f4a7c15)
	return newXorShift64(seed)
}

func (x *xorshift64) next() uint64 {
	s := x.state
	s ^= s << 13
	s ^= s >> 7
	s ^= s << 17
	x.state = s
	return s
}
