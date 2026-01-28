package store

import (
	"encoding/binary"
	"sync"
	"sync/atomic"

	"github.com/OrlovEvgeny/go-mcache/internal/clock"
)

const (
	// defaultGCFreeShardCount is the default number of shards for GC-free storage.
	defaultGCFreeShardCount = 256

	// entryHeaderSize is the size of entry header in bytes.
	// Format: [4:keyLen][4:valueLen][8:expireAt][8:cost] = 24 bytes
	entryHeaderSize = 24

	// defaultInitialSize is the default initial size for data buffer.
	defaultInitialSize = 1 << 20 // 1MB
)

// GCFreeEntry represents an entry in GC-free storage.
type GCFreeEntry struct {
	KeyHash  uint64
	Offset   uint32
	ExpireAt int64
	Cost     int64
}

// gcFreeShard is a single shard of GC-free storage.
type gcFreeShard struct {
	mu      sync.RWMutex
	hashmap map[uint64]uint32 // keyHash -> offset in data
	data    []byte            // Serialized entries
	tail    int               // Current write position
	deleted int               // Number of deleted/invalid bytes
	_       [64 - 40]byte     // Cache line padding
}

// GCFreeStore is a GC-free storage backend.
// It stores entries as serialized bytes, so the GC doesn't need to scan them.
// This is beneficial for large caches where GC pauses become problematic.
type GCFreeStore struct {
	shards    []*gcFreeShard
	shardMask uint32
	size      atomic.Int64
}

// NewGCFreeStore creates a new GC-free storage.
func NewGCFreeStore(shardCount int, initialSize int) *GCFreeStore {
	if shardCount <= 0 {
		shardCount = defaultGCFreeShardCount
	}
	shardCount = nextPowerOf2(shardCount)

	if initialSize <= 0 {
		initialSize = defaultInitialSize
	}
	shardSize := initialSize / shardCount
	if shardSize < 4096 {
		shardSize = 4096
	}

	s := &GCFreeStore{
		shards:    make([]*gcFreeShard, shardCount),
		shardMask: uint32(shardCount - 1),
	}

	for i := range s.shards {
		s.shards[i] = &gcFreeShard{
			hashmap: make(map[uint64]uint32),
			data:    make([]byte, shardSize),
		}
	}

	return s
}

// getShard returns the shard for the given key hash.
func (s *GCFreeStore) getShard(keyHash uint64) *gcFreeShard {
	return s.shards[keyHash&uint64(s.shardMask)]
}

// Get retrieves a value by key hash.
// Returns the value, expiration time, and true if found; nil, 0, false otherwise.
func (s *GCFreeStore) Get(keyHash uint64, key []byte) ([]byte, int64, bool) {
	sh := s.getShard(keyHash)

	sh.mu.RLock()
	offset, exists := sh.hashmap[keyHash]
	if !exists {
		sh.mu.RUnlock()
		return nil, 0, false
	}

	// Read entry header
	if int(offset)+entryHeaderSize > len(sh.data) {
		sh.mu.RUnlock()
		return nil, 0, false
	}

	data := sh.data[offset:]
	keyLen := binary.LittleEndian.Uint32(data[0:4])
	valueLen := binary.LittleEndian.Uint32(data[4:8])
	expireAt := int64(binary.LittleEndian.Uint64(data[8:16]))

	// Check if entry is valid
	entrySize := entryHeaderSize + int(keyLen) + int(valueLen)
	if int(offset)+entrySize > len(sh.data) {
		sh.mu.RUnlock()
		return nil, 0, false
	}

	// Verify key matches (in case of hash collision)
	storedKey := data[entryHeaderSize : entryHeaderSize+int(keyLen)]
	if !bytesEqual(storedKey, key) {
		sh.mu.RUnlock()
		return nil, 0, false
	}

	// Check expiration
	if expireAt > 0 && clock.NowNano() > expireAt {
		sh.mu.RUnlock()
		return nil, 0, false
	}

	// Copy value to avoid holding lock
	value := make([]byte, valueLen)
	copy(value, data[entryHeaderSize+int(keyLen):entryHeaderSize+int(keyLen)+int(valueLen)])
	sh.mu.RUnlock()

	return value, expireAt, true
}

// Set stores a key-value pair.
// Returns true if stored successfully, false if storage is full.
func (s *GCFreeStore) Set(keyHash uint64, key, value []byte, expireAt int64, cost int64) bool {
	sh := s.getShard(keyHash)

	entrySize := entryHeaderSize + len(key) + len(value)

	sh.mu.Lock()
	defer sh.mu.Unlock()

	// Check if key already exists
	if oldOffset, exists := sh.hashmap[keyHash]; exists {
		// Mark old entry as deleted by zeroing the key length
		if int(oldOffset)+4 <= len(sh.data) {
			binary.LittleEndian.PutUint32(sh.data[oldOffset:oldOffset+4], 0)
			oldKeyLen := binary.LittleEndian.Uint32(sh.data[oldOffset:oldOffset+4])
			oldValueLen := binary.LittleEndian.Uint32(sh.data[oldOffset+4:oldOffset+8])
			sh.deleted += entryHeaderSize + int(oldKeyLen) + int(oldValueLen)
		}
	} else {
		s.size.Add(1)
	}

	// Check if we need to grow or compact
	if sh.tail+entrySize > len(sh.data) {
		if sh.deleted > len(sh.data)/4 {
			// Compact to reclaim space
			sh.compact()
		} else {
			// Grow the buffer
			sh.grow(entrySize)
		}
	}

	// Write entry
	offset := uint32(sh.tail)
	binary.LittleEndian.PutUint32(sh.data[sh.tail:sh.tail+4], uint32(len(key)))
	binary.LittleEndian.PutUint32(sh.data[sh.tail+4:sh.tail+8], uint32(len(value)))
	binary.LittleEndian.PutUint64(sh.data[sh.tail+8:sh.tail+16], uint64(expireAt))
	binary.LittleEndian.PutUint64(sh.data[sh.tail+16:sh.tail+24], uint64(cost))
	copy(sh.data[sh.tail+entryHeaderSize:], key)
	copy(sh.data[sh.tail+entryHeaderSize+len(key):], value)

	sh.tail += entrySize
	sh.hashmap[keyHash] = offset

	return true
}

// Delete removes an entry by key hash.
// Returns true if deleted, false if not found.
func (s *GCFreeStore) Delete(keyHash uint64) bool {
	sh := s.getShard(keyHash)

	sh.mu.Lock()
	offset, exists := sh.hashmap[keyHash]
	if !exists {
		sh.mu.Unlock()
		return false
	}

	// Mark as deleted and remove from hashmap
	if int(offset)+entryHeaderSize <= len(sh.data) {
		keyLen := binary.LittleEndian.Uint32(sh.data[offset:offset+4])
		valueLen := binary.LittleEndian.Uint32(sh.data[offset+4:offset+8])
		binary.LittleEndian.PutUint32(sh.data[offset:offset+4], 0) // Zero key length marks as deleted
		sh.deleted += entryHeaderSize + int(keyLen) + int(valueLen)
	}

	delete(sh.hashmap, keyHash)
	sh.mu.Unlock()

	s.size.Add(-1)
	return true
}

// Has checks if a key exists and is not expired.
func (s *GCFreeStore) Has(keyHash uint64, key []byte) bool {
	_, _, ok := s.Get(keyHash, key)
	return ok
}

// Len returns the number of entries.
func (s *GCFreeStore) Len() int {
	return int(s.size.Load())
}

// Clear removes all entries.
func (s *GCFreeStore) Clear() {
	for _, sh := range s.shards {
		sh.mu.Lock()
		sh.hashmap = make(map[uint64]uint32)
		sh.tail = 0
		sh.deleted = 0
		sh.mu.Unlock()
	}
	s.size.Store(0)
}

// compact reclaims space from deleted entries.
func (sh *gcFreeShard) compact() {
	newData := make([]byte, len(sh.data))
	newTail := 0

	for keyHash, offset := range sh.hashmap {
		if int(offset)+entryHeaderSize > len(sh.data) {
			delete(sh.hashmap, keyHash)
			continue
		}

		keyLen := binary.LittleEndian.Uint32(sh.data[offset : offset+4])
		if keyLen == 0 {
			// Deleted entry
			delete(sh.hashmap, keyHash)
			continue
		}

		valueLen := binary.LittleEndian.Uint32(sh.data[offset+4 : offset+8])
		entrySize := entryHeaderSize + int(keyLen) + int(valueLen)

		// Copy entry to new location
		copy(newData[newTail:], sh.data[offset:int(offset)+entrySize])
		sh.hashmap[keyHash] = uint32(newTail)
		newTail += entrySize
	}

	sh.data = newData
	sh.tail = newTail
	sh.deleted = 0
}

// grow increases the buffer size.
func (sh *gcFreeShard) grow(needed int) {
	newSize := len(sh.data) * 2
	for newSize < sh.tail+needed {
		newSize *= 2
	}

	newData := make([]byte, newSize)
	copy(newData, sh.data[:sh.tail])
	sh.data = newData
}

// Range iterates over all valid entries.
func (s *GCFreeStore) Range(fn func(keyHash uint64, key, value []byte, expireAt, cost int64) bool) {
	now := clock.NowNano()

	for _, sh := range s.shards {
		sh.mu.RLock()
		for keyHash, offset := range sh.hashmap {
			if int(offset)+entryHeaderSize > len(sh.data) {
				continue
			}

			keyLen := binary.LittleEndian.Uint32(sh.data[offset : offset+4])
			if keyLen == 0 {
				continue // Deleted
			}

			valueLen := binary.LittleEndian.Uint32(sh.data[offset+4 : offset+8])
			expireAt := int64(binary.LittleEndian.Uint64(sh.data[offset+8 : offset+16]))
			cost := int64(binary.LittleEndian.Uint64(sh.data[offset+16 : offset+24]))

			// Check expiration
			if expireAt > 0 && now > expireAt {
				continue
			}

			key := sh.data[offset+entryHeaderSize : offset+entryHeaderSize+keyLen]
			value := sh.data[offset+entryHeaderSize+keyLen : offset+entryHeaderSize+keyLen+valueLen]

			if !fn(keyHash, key, value, expireAt, cost) {
				sh.mu.RUnlock()
				return
			}
		}
		sh.mu.RUnlock()
	}
}

// bytesEqual compares two byte slices for equality.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// MemoryUsage returns the total memory used by the storage.
func (s *GCFreeStore) MemoryUsage() int64 {
	var total int64
	for _, sh := range s.shards {
		sh.mu.RLock()
		total += int64(len(sh.data))
		total += int64(len(sh.hashmap) * 16) // Approximate map overhead
		sh.mu.RUnlock()
	}
	return total
}

// Efficiency returns the ratio of used space to total space.
func (s *GCFreeStore) Efficiency() float64 {
	var used, total int64
	for _, sh := range s.shards {
		sh.mu.RLock()
		used += int64(sh.tail - sh.deleted)
		total += int64(len(sh.data))
		sh.mu.RUnlock()
	}
	if total == 0 {
		return 1.0
	}
	return float64(used) / float64(total)
}
