package mcache

import "sync/atomic"

// metricStripes is the number of stripes for hot-path counters.
// Must be a power of two.
const metricStripes = 8

// metricLine holds the hot-path counters for one stripe, padded to a full
// cache line so stripes never share a line. hits/misses are bumped on every
// Get: a single shared counter would serialize all reader cores on one line.
type metricLine struct {
	hits      atomic.Int64
	misses    atomic.Int64
	sets      atomic.Int64
	deletes   atomic.Int64
	costAdded atomic.Int64
	_         [24]byte // pad to 64 bytes
}

// Metrics holds cache statistics.
// Hot counters (hits/misses/sets/deletes/costAdded) are striped by key hash;
// cold counters are plain atomics updated only on eviction/expiry paths.
type Metrics struct {
	stripes [metricStripes]metricLine

	evictions   atomic.Int64 // Evictions due to size/cost limit
	expirations atomic.Int64 // Expirations due to TTL
	rejections  atomic.Int64 // Rejections by TinyLFU admission policy
	costEvicted atomic.Int64 // Total cost evicted
	bufferDrops atomic.Int64 // Buffer saturation drops (sync fallback used)
}

// MetricsSnapshot is a point-in-time snapshot of cache metrics.
type MetricsSnapshot struct {
	Hits        int64   // Total cache hits
	Misses      int64   // Total cache misses
	Sets        int64   // Total successful sets
	Deletes     int64   // Total successful deletes
	Evictions   int64   // Total evictions due to size/cost limit
	Expirations int64   // Total expirations due to TTL
	Rejections  int64   // Total rejections by admission policy
	CostAdded   int64   // Total cost added over time
	CostEvicted int64   // Total cost evicted over time
	BufferDrops int64   // Times buffer was full and sync fallback was used
	HitRatio    float64 // Hit ratio (hits / (hits + misses))
}

// newMetrics creates a new Metrics instance.
func newMetrics() *Metrics {
	return &Metrics{}
}

// line returns the stripe for the given key hash.
func (m *Metrics) line(keyHash uint64) *metricLine {
	return &m.stripes[keyHash&(metricStripes-1)]
}

// incHit increments the hit counter.
func (m *Metrics) incHit(keyHash uint64) {
	if m == nil {
		return
	}
	m.line(keyHash).hits.Add(1)
}

// incMiss increments the miss counter.
func (m *Metrics) incMiss(keyHash uint64) {
	if m == nil {
		return
	}
	m.line(keyHash).misses.Add(1)
}

// incSet increments the set counter.
func (m *Metrics) incSet(keyHash uint64) {
	if m == nil {
		return
	}
	m.line(keyHash).sets.Add(1)
}

// incDelete increments the delete counter.
func (m *Metrics) incDelete(keyHash uint64) {
	if m == nil {
		return
	}
	m.line(keyHash).deletes.Add(1)
}

// addCost adds to the cost added counter.
func (m *Metrics) addCost(keyHash uint64, cost int64) {
	if m == nil {
		return
	}
	m.line(keyHash).costAdded.Add(cost)
}

// incEviction increments the eviction counter.
func (m *Metrics) incEviction() {
	if m == nil {
		return
	}
	m.evictions.Add(1)
}

// incExpiration increments the expiration counter.
func (m *Metrics) incExpiration() {
	if m == nil {
		return
	}
	m.expirations.Add(1)
}

// incRejection increments the rejection counter.
func (m *Metrics) incRejection() {
	if m == nil {
		return
	}
	m.rejections.Add(1)
}

// addEvictedCost adds to the cost evicted counter.
func (m *Metrics) addEvictedCost(cost int64) {
	if m == nil {
		return
	}
	m.costEvicted.Add(cost)
}

// incBufferDrop increments the buffer drop counter.
func (m *Metrics) incBufferDrop() {
	if m == nil {
		return
	}
	m.bufferDrops.Add(1)
}

// Snapshot returns a point-in-time snapshot of the metrics.
func (m *Metrics) Snapshot() MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{}
	}

	var hits, misses, sets, deletes, costAdded int64
	for i := range m.stripes {
		line := &m.stripes[i]
		hits += line.hits.Load()
		misses += line.misses.Load()
		sets += line.sets.Load()
		deletes += line.deletes.Load()
		costAdded += line.costAdded.Load()
	}

	total := hits + misses
	var hitRatio float64
	if total > 0 {
		hitRatio = float64(hits) / float64(total)
	}

	return MetricsSnapshot{
		Hits:        hits,
		Misses:      misses,
		Sets:        sets,
		Deletes:     deletes,
		Evictions:   m.evictions.Load(),
		Expirations: m.expirations.Load(),
		Rejections:  m.rejections.Load(),
		CostAdded:   costAdded,
		CostEvicted: m.costEvicted.Load(),
		BufferDrops: m.bufferDrops.Load(),
		HitRatio:    hitRatio,
	}
}

// Reset resets all metrics to zero.
func (m *Metrics) Reset() {
	if m == nil {
		return
	}
	for i := range m.stripes {
		line := &m.stripes[i]
		line.hits.Store(0)
		line.misses.Store(0)
		line.sets.Store(0)
		line.deletes.Store(0)
		line.costAdded.Store(0)
	}
	m.evictions.Store(0)
	m.expirations.Store(0)
	m.rejections.Store(0)
	m.costEvicted.Store(0)
	m.bufferDrops.Store(0)
}
