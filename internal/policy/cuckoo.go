package policy

import (
	"math"
	"sync"
	"sync/atomic"
)

const (
	// cuckooBucketSize is the number of entries per bucket
	cuckooBucketSize = 4
	// cuckooMaxKicks is the maximum number of relocations before giving up
	cuckooMaxKicks = 500
	// cuckooFingerprintBits is the size of fingerprints in bits
	cuckooFingerprintBits = 16
)

// fingerprint is a compact representation of a key hash
type fingerprint uint16

// cuckooBucket holds multiple fingerprints
type cuckooBucket struct {
	fps [cuckooBucketSize]atomic.Uint32 // Each holds fingerprint (16 bits) + metadata
}

// CuckooFilter is a space-efficient probabilistic data structure
// that supports deletion (unlike bloom filters).
// It uses cuckoo hashing with fingerprints.
type CuckooFilter struct {
	buckets    []cuckooBucket
	numBuckets uint64
	count      atomic.Int64
	mu         sync.Mutex // Only for insert/delete operations
}

// NewCuckooFilter creates a new cuckoo filter.
// n is the expected number of items, fpRate influences fingerprint size.
func NewCuckooFilter(n int64, fpRate float64) *CuckooFilter {
	if n <= 0 {
		n = 10000
	}
	if fpRate <= 0 || fpRate >= 1 {
		fpRate = 0.01
	}

	// Calculate number of buckets needed
	// Load factor should be around 95% for cuckoo filters
	loadFactor := 0.95
	numBuckets := uint64(math.Ceil(float64(n) / (float64(cuckooBucketSize) * loadFactor)))

	// Round up to power of 2 for efficient modulo
	numBuckets = nextPowerOf2Uint64(numBuckets)
	if numBuckets < 1 {
		numBuckets = 1
	}

	return &CuckooFilter{
		buckets:    make([]cuckooBucket, numBuckets),
		numBuckets: numBuckets,
	}
}

// nextPowerOf2Uint64 returns the smallest power of 2 >= n.
func nextPowerOf2Uint64(n uint64) uint64 {
	if n == 0 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n |= n >> 32
	n++
	return n
}

// Add adds a key hash to the filter.
// Returns true if successful, false if the filter is full.
func (cf *CuckooFilter) Add(keyHash uint64) bool {
	fp := cf.fingerprint(keyHash)
	i1 := cf.index1(keyHash)
	i2 := cf.index2(i1, fp)

	cf.mu.Lock()
	defer cf.mu.Unlock()

	// Try to insert into bucket i1
	if cf.insertToBucket(i1, fp) {
		cf.count.Add(1)
		return true
	}

	// Try to insert into bucket i2
	if cf.insertToBucket(i2, fp) {
		cf.count.Add(1)
		return true
	}

	// Need to relocate existing items
	return cf.relocate(i1, i2, fp)
}

// insertToBucket tries to insert a fingerprint into a bucket.
// Returns true if successful.
func (cf *CuckooFilter) insertToBucket(idx uint64, fp fingerprint) bool {
	bucket := &cf.buckets[idx]
	for j := 0; j < cuckooBucketSize; j++ {
		if bucket.fps[j].Load() == 0 {
			bucket.fps[j].Store(uint32(fp) | 0x10000) // Set occupied bit
			return true
		}
	}
	return false
}

// relocate performs cuckoo hashing relocation.
func (cf *CuckooFilter) relocate(i1, i2 uint64, fp fingerprint) bool {
	// Choose a random starting bucket
	i := i1

	for kick := 0; kick < cuckooMaxKicks; kick++ {
		// Randomly select an entry to evict
		bucket := &cf.buckets[i]
		j := kick % cuckooBucketSize

		// Swap fingerprints
		oldFpVal := bucket.fps[j].Load()
		oldFp := fingerprint(oldFpVal & 0xFFFF)
		bucket.fps[j].Store(uint32(fp) | 0x10000)
		fp = oldFp

		// Calculate alternate bucket
		i = cf.indexAlt(i, fp)

		// Try to insert evicted fingerprint
		if cf.insertToBucket(i, fp) {
			cf.count.Add(1)
			return true
		}
	}

	// Filter is too full
	return false
}

// Contains checks if a key hash might be in the filter.
// Returns true if probably present, false if definitely absent.
func (cf *CuckooFilter) Contains(keyHash uint64) bool {
	fp := cf.fingerprint(keyHash)
	i1 := cf.index1(keyHash)
	i2 := cf.index2(i1, fp)

	return cf.bucketContains(i1, fp) || cf.bucketContains(i2, fp)
}

// bucketContains checks if a bucket contains a fingerprint.
func (cf *CuckooFilter) bucketContains(idx uint64, fp fingerprint) bool {
	bucket := &cf.buckets[idx]
	for j := 0; j < cuckooBucketSize; j++ {
		val := bucket.fps[j].Load()
		if val != 0 && fingerprint(val&0xFFFF) == fp {
			return true
		}
	}
	return false
}

// Delete removes a key hash from the filter.
// Returns true if the key was found and deleted.
func (cf *CuckooFilter) Delete(keyHash uint64) bool {
	fp := cf.fingerprint(keyHash)
	i1 := cf.index1(keyHash)
	i2 := cf.index2(i1, fp)

	cf.mu.Lock()
	defer cf.mu.Unlock()

	if cf.deleteFromBucket(i1, fp) {
		cf.count.Add(-1)
		return true
	}
	if cf.deleteFromBucket(i2, fp) {
		cf.count.Add(-1)
		return true
	}
	return false
}

// deleteFromBucket removes a fingerprint from a bucket.
func (cf *CuckooFilter) deleteFromBucket(idx uint64, fp fingerprint) bool {
	bucket := &cf.buckets[idx]
	for j := 0; j < cuckooBucketSize; j++ {
		val := bucket.fps[j].Load()
		if val != 0 && fingerprint(val&0xFFFF) == fp {
			bucket.fps[j].Store(0)
			return true
		}
	}
	return false
}

// fingerprint extracts a fingerprint from a hash.
func (cf *CuckooFilter) fingerprint(keyHash uint64) fingerprint {
	// Use bits that aren't used for indexing
	fp := fingerprint(keyHash>>48) | 1 // Ensure non-zero
	return fp
}

// index1 computes the first bucket index.
func (cf *CuckooFilter) index1(keyHash uint64) uint64 {
	return keyHash & (cf.numBuckets - 1)
}

// index2 computes the second bucket index using the fingerprint.
func (cf *CuckooFilter) index2(i1 uint64, fp fingerprint) uint64 {
	return cf.indexAlt(i1, fp)
}

// indexAlt computes the alternate index using XOR.
// This ensures i1 = alt(alt(i1, fp), fp).
func (cf *CuckooFilter) indexAlt(i uint64, fp fingerprint) uint64 {
	// Hash the fingerprint to get a displacement
	fpHash := uint64(fp) * 0x5bd1e995
	return (i ^ fpHash) & (cf.numBuckets - 1)
}

// Reset clears the filter.
func (cf *CuckooFilter) Reset() {
	cf.mu.Lock()
	defer cf.mu.Unlock()

	for i := range cf.buckets {
		for j := 0; j < cuckooBucketSize; j++ {
			cf.buckets[i].fps[j].Store(0)
		}
	}
	cf.count.Store(0)
}

// Count returns the number of items in the filter.
func (cf *CuckooFilter) Count() int64 {
	return cf.count.Load()
}

// LoadFactor returns the current load factor.
func (cf *CuckooFilter) LoadFactor() float64 {
	return float64(cf.count.Load()) / float64(cf.numBuckets*cuckooBucketSize)
}

// Capacity returns the maximum capacity of the filter.
func (cf *CuckooFilter) Capacity() int64 {
	return int64(cf.numBuckets * cuckooBucketSize)
}

// NumBuckets returns the number of buckets.
func (cf *CuckooFilter) NumBuckets() uint64 {
	return cf.numBuckets
}

// AddBatch adds multiple key hashes to the filter.
// Returns the number successfully added.
func (cf *CuckooFilter) AddBatch(keyHashes []uint64) int {
	added := 0
	for _, h := range keyHashes {
		if cf.Add(h) {
			added++
		}
	}
	return added
}

// ContainsBatch checks multiple key hashes.
func (cf *CuckooFilter) ContainsBatch(keyHashes []uint64, results []bool) {
	n := len(keyHashes)
	if n > len(results) {
		n = len(results)
	}

	for i := 0; i < n; i++ {
		results[i] = cf.Contains(keyHashes[i])
	}
}

// DeleteBatch removes multiple key hashes.
// Returns the number successfully deleted.
func (cf *CuckooFilter) DeleteBatch(keyHashes []uint64) int {
	deleted := 0
	for _, h := range keyHashes {
		if cf.Delete(h) {
			deleted++
		}
	}
	return deleted
}
