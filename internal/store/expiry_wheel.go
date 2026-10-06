package store

import (
	"math"
	"time"

	"github.com/OrlovEvgeny/go-mcache/internal/clock"
)

const defaultExpiryWheelBuckets = 4096

// expiryWheel belongs to one storage shard. Every access, including bitmap
// iteration, requires that shard's write lock. Entries are immutable; slots
// refer only to the currently published TTL entry, never to an update history.
type ttlRegistration[K comparable] struct {
	Key      K
	ExpireAt int64
}

type expiryWheel[K comparable, V any] struct {
	resolution    int64
	currentTick   int64
	slotStart     int64
	slotEnd       int64
	slotTick      int64
	ids           map[K]uint32
	entries       []ttlRegistration[K]
	free          []uint32
	slots         []uint16
	buckets       map[uint16]*expiryBucket
	first         expiryBucket
	firstSlot     uint16
	inlineEntries [16]ttlRegistration[K]
	inlineSlots   [16]uint16
}

func newExpiryWheel[K comparable, V any](resolution int64) *expiryWheel[K, V] {
	w := &expiryWheel[K, V]{resolution: resolution, currentTick: clock.NowNano() / resolution}
	w.entries = w.inlineEntries[:0]
	w.slots = w.inlineSlots[:0]
	return w
}

// The common case has one active deadline bucket. Embed it to avoid a map
// and bitmap allocation per small shard; allocate the map only on divergence.
func (w *expiryWheel[K, V]) bucket(slot uint16) *expiryBucket {
	if slot == w.firstSlot {
		return &w.first
	}
	return w.buckets[slot]
}
func (w *expiryWheel[K, V]) ensureBucket(slot uint16) *expiryBucket {
	if b := w.bucket(slot); b != nil {
		return b
	}
	if w.first.IsEmpty() {
		w.firstSlot = slot
		return &w.first
	}
	if w.buckets == nil {
		w.buckets = make(map[uint16]*expiryBucket)
	}
	b := new(expiryBucket)
	w.buckets[slot] = b
	return b
}

// Schedule on the first tick strictly after the deadline: reads expire only
// when now > deadline. Division before addition also avoids int64 overflow.
func (w *expiryWheel[K, V]) slot(deadline int64) uint16 {
	if deadline >= w.slotStart && deadline <= w.slotEnd && w.slotTick > w.currentTick {
		return uint16(uint64(w.slotTick) & (defaultExpiryWheelBuckets - 1))
	}
	return w.computeSlot(deadline)
}

// Consecutive TTL writes usually share a coarse bucket. Cache its range to
// avoid integer division on every overwrite, without rounding the deadline.
func (w *expiryWheel[K, V]) computeSlot(deadline int64) uint16 {
	quotient := deadline / w.resolution
	w.slotStart = quotient * w.resolution
	w.slotEnd = w.slotStart + w.resolution - 1
	if w.slotEnd < w.slotStart {
		w.slotEnd = math.MaxInt64
	}
	tick := quotient + 1
	if quotient == math.MaxInt64 {
		tick = quotient
	}
	if tick <= w.currentTick {
		tick = w.currentTick + 1
	}
	w.slotTick = tick
	return uint16(uint64(tick) & (defaultExpiryWheelBuckets - 1))
}

func (w *expiryWheel[K, V]) removeFromBucket(id uint32) {
	slot := w.slots[id]
	b := w.bucket(slot)
	if b != nil {
		b.Remove(id)
		if b.IsEmpty() {
			delete(w.buckets, slot)
		}
	}
}

// Small shards avoid a second map allocation and its retained capacity.
// Larger registries switch to exact-key lookup once linear search would grow.
func (w *expiryWheel[K, V]) find(key K) (uint32, bool) {
	if w.ids != nil {
		id, ok := w.ids[key]
		return id, ok
	}
	for i, e := range w.entries {
		if e.ExpireAt > 0 && e.Key == key {
			return uint32(i), true
		}
	}
	return 0, false
}

// The caller obtained entry from this shard while holding its write lock.
func (w *expiryWheel[K, V]) findEntry(entry *Entry[K, V]) (uint32, bool) {
	if w.ids != nil {
		id, ok := w.ids[entry.Key]
		return id, ok
	}
	for i, e := range w.entries {
		if e.Key == entry.Key && e.ExpireAt > 0 {
			return uint32(i), true
		}
	}
	return 0, false
}

func (w *expiryWheel[K, V]) set(entry, prev *Entry[K, V]) {
	var id uint32
	var ok bool
	if prev != nil && prev.ExpireAt > 0 {
		id, ok = w.findEntry(prev)
	}
	slot := w.slot(entry.ExpireAt)
	if ok {
		if w.slots[id] == slot {
			w.entries[id].ExpireAt = entry.ExpireAt
			return
		}
		w.removeFromBucket(id)
	} else {
		if n := len(w.free); n > 0 {
			id = w.free[n-1]
			w.free = w.free[:n-1]
		} else {
			// A shard cannot hold more than 2^32 simultaneously live TTL entries.
			if uint64(len(w.entries)) >= 1<<32 {
				panic("mcache: ttl identifier space exhausted")
			}
			id = uint32(len(w.entries))
			w.entries = append(w.entries, ttlRegistration[K]{})
			if len(w.entries) == len(w.inlineEntries)+1 {
				clear(w.inlineEntries[:])
			}
			w.slots = append(w.slots, 0)
		}
		if w.ids != nil {
			w.ids[entry.Key] = id
		} else if len(w.entries) > 32 {
			w.ids = make(map[K]uint32, len(w.entries))
			for i, e := range w.entries {
				if e.ExpireAt > 0 {
					w.ids[e.Key] = uint32(i)
				}
			}
			w.ids[entry.Key] = id
		}
	}
	w.entries[id] = ttlRegistration[K]{entry.Key, entry.ExpireAt}
	w.slots[id] = slot
	b := w.ensureBucket(slot)
	b.Add(id)
}

func (w *expiryWheel[K, V]) remove(entry *Entry[K, V]) {
	if entry == nil || entry.ExpireAt <= 0 {
		return
	}
	id, ok := w.findEntry(entry)
	if !ok {
		return
	}
	w.removeFromBucket(id)
	delete(w.ids, entry.Key)
	w.entries[id] = ttlRegistration[K]{}
	w.free = append(w.free, id)
}

// ConfigureExpiration must be called before the store is shared.
func (s *ShardedStore[K, V]) ConfigureExpiration(resolution time.Duration) {
	if resolution <= 0 {
		resolution = 100 * time.Millisecond
	}
	s.expiryResolution = int64(resolution)
}

func (s *ShardedStore[K, V]) registerTTL(sh *shard[K, V], entry, prev *Entry[K, V]) {
	if entry.ExpireAt > 0 {
		if sh.expiry == nil {
			sh.expiry = newExpiryWheel[K, V](s.expiryResolution)
		}
		sh.expiry.set(entry, prev)
	} else if sh.expiry != nil {
		sh.expiry.remove(prev)
	}
}

// AdvanceExpiration removes due entries under their owner's lock. A full
// rotation visits each bucket once, even after a long pause. Long TTL entries
// stay in their bucket until their exact deadline is passed.
func (s *ShardedStore[K, V]) AdvanceExpiration(now int64) []*Entry[K, V] {
	var expired []*Entry[K, V]
	for _, sh := range s.shards {
		sh.mu.Lock()
		w := sh.expiry
		if w == nil || now/w.resolution <= w.currentTick {
			sh.mu.Unlock()
			continue
		}
		nowTick := now / w.resolution
		ticks := nowTick - w.currentTick
		if ticks > defaultExpiryWheelBuckets {
			ticks = defaultExpiryWheelBuckets
		}
		for range ticks {
			w.currentTick++
			slot := uint16(uint64(w.currentTick) & (defaultExpiryWheelBuckets - 1))
			b := w.bucket(slot)
			if b == nil || b.IsEmpty() {
				continue
			}
			// Iterate into private chunks and classify deadlines before deleting.
			// Bitmap mutation waits until its iterator is exhausted; IDs become
			// reusable only after every old membership has been removed.
			var ids [256]uint32
			var deadlines [256]int64
			freeStart := len(w.free)
			it := b.Iterator()
			for it.HasNext() {
				n := 0
				for n < len(ids) && it.HasNext() {
					id := it.Next()
					ids[n] = id
					deadlines[n] = w.entries[id].ExpireAt
					n++
				}
				markExpired(deadlines[:n], now)
				for i, mask := range deadlines[:n] {
					if mask == 0 {
						continue
					}
					id := ids[i]
					registration := w.entries[id]
					entry := sh.m[registration.Key]
					if entry != nil && entry.ExpireAt == registration.ExpireAt {
						delete(sh.m, entry.Key)
						expired = append(expired, entry)
						s.size.Add(-1)
					}
					delete(w.ids, registration.Key)
					w.entries[id] = ttlRegistration[K]{}
					w.free = append(w.free, id)
				}
			}
			for _, id := range w.free[freeStart:] {
				b.Remove(id)
			}
			if b.IsEmpty() {
				delete(w.buckets, slot)
			}
		}
		w.currentTick = nowTick
		sh.mu.Unlock()
	}
	return expired
}
