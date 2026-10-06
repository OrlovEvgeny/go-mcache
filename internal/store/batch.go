package store

import (
	"slices"
	"sync"

	"github.com/OrlovEvgeny/go-mcache/internal/clock"
)

type batchIndex struct {
	shard uint64
	index int
}
type batchScratch struct {
	order     []batchIndex
	deadlines []int64
}

// Scratch contains only integers, so a pool never retains caller keys/values.
var batchPool = sync.Pool{New: func() any { return new(batchScratch) }}

// GetBatchValues writes directly into caller-owned results. Hashes are computed
// by the cache once, and each sorted shard group takes one read lock.
func (s *ShardedStore[K, V]) GetBatchValues(keys []K, hashes []uint64, values []V, found []bool, ordered bool) {
	n := len(keys)
	var localDeadlines [16]int64
	var localOrder [16]batchIndex
	deadlines := localDeadlines[:]
	order := localOrder[:]
	var scratch *batchScratch
	if n > len(localDeadlines) {
		scratch = batchPool.Get().(*batchScratch)
		scratch.deadlines = slices.Grow(scratch.deadlines[:0], n)[:n]
		deadlines = scratch.deadlines
		if ordered {
			scratch.order = slices.Grow(scratch.order[:0], n)[:n]
			order = scratch.order
		}
	}
	deadlines = deadlines[:n]
	now := clock.NowNano()
	if ordered && n > 1 {
		order = order[:n]
		for i, h := range hashes {
			order[i] = batchIndex{h & s.shardMask, i}
		}
		slices.SortFunc(order, func(a, b batchIndex) int { return int(a.shard) - int(b.shard) })
		for start := 0; start < n; {
			sh := s.shards[order[start].shard]
			end := start + 1
			for end < n && order[end].shard == order[start].shard {
				end++
			}
			sh.mu.RLock()
			for _, info := range order[start:end] {
				i := info.index
				entry := sh.m[keys[i]]
				if entry != nil {
					values[i] = entry.Value
					found[i] = true
					deadlines[i] = entry.ExpireAt
				} else {
					deadlines[i] = 0
				}
			}
			sh.mu.RUnlock()
			start = end
		}
	} else {
		for i, k := range keys {
			sh := s.getShard(hashes[i])
			sh.mu.RLock()
			entry := sh.m[k]
			sh.mu.RUnlock()
			if entry != nil {
				values[i] = entry.Value
				found[i] = true
				deadlines[i] = entry.ExpireAt
			} else {
				deadlines[i] = 0
			}
		}
	}
	markExpired(deadlines, now)
	var zero V
	for i, expired := range deadlines {
		if expired != 0 {
			values[i] = zero
			found[i] = false
		}
	}
	if scratch != nil {
		// Bound retained scratch after unusually large requests. There are no
		// references to clear, and returned result arrays never enter this pool.
		if n <= 16384 {
			batchPool.Put(scratch)
		}
	}
}
