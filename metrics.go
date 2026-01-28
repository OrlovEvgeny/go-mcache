package mcache

import "sync/atomic"

// Metrics holds cache statistics.
type Metrics struct {
	hits       atomic.Int64 // Cache hits
	misses     atomic.Int64 // Cache misses
	sets       atomic.Int64 // Successful sets
	deletes    atomic.Int64 // Successful deletes
	evictions  atomic.Int64 // Evictions due to size/cost limit
	expirations atomic.Int64 // Expirations due to TTL
	rejections atomic.Int64 // Rejections by TinyLFU admission policy
	costAdded  atomic.Int64 // Total cost added
	costEvicted atomic.Int64 // Total cost evicted
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
	HitRatio    float64 // Hit ratio (hits / (hits + misses))
}

// newMetrics creates a new Metrics instance.
func newMetrics() *Metrics {
	return &Metrics{}
}

// incHit increments the hit counter.
func (m *Metrics) incHit() {
	m.hits.Add(1)
}

// incMiss increments the miss counter.
func (m *Metrics) incMiss() {
	m.misses.Add(1)
}

// incSet increments the set counter.
func (m *Metrics) incSet() {
	m.sets.Add(1)
}

// incDelete increments the delete counter.
func (m *Metrics) incDelete() {
	m.deletes.Add(1)
}

// incEviction increments the eviction counter.
func (m *Metrics) incEviction() {
	m.evictions.Add(1)
}

// incExpiration increments the expiration counter.
func (m *Metrics) incExpiration() {
	m.expirations.Add(1)
}

// incRejection increments the rejection counter.
func (m *Metrics) incRejection() {
	m.rejections.Add(1)
}

// addCost adds to the cost added counter.
func (m *Metrics) addCost(cost int64) {
	m.costAdded.Add(cost)
}

// addEvictedCost adds to the cost evicted counter.
func (m *Metrics) addEvictedCost(cost int64) {
	m.costEvicted.Add(cost)
}

// Snapshot returns a point-in-time snapshot of the metrics.
func (m *Metrics) Snapshot() MetricsSnapshot {
	hits := m.hits.Load()
	misses := m.misses.Load()
	total := hits + misses

	var hitRatio float64
	if total > 0 {
		hitRatio = float64(hits) / float64(total)
	}

	return MetricsSnapshot{
		Hits:        hits,
		Misses:      misses,
		Sets:        m.sets.Load(),
		Deletes:     m.deletes.Load(),
		Evictions:   m.evictions.Load(),
		Expirations: m.expirations.Load(),
		Rejections:  m.rejections.Load(),
		CostAdded:   m.costAdded.Load(),
		CostEvicted: m.costEvicted.Load(),
		HitRatio:    hitRatio,
	}
}

// Reset resets all metrics to zero.
func (m *Metrics) Reset() {
	m.hits.Store(0)
	m.misses.Store(0)
	m.sets.Store(0)
	m.deletes.Store(0)
	m.evictions.Store(0)
	m.expirations.Store(0)
	m.rejections.Store(0)
	m.costAdded.Store(0)
	m.costEvicted.Store(0)
}
