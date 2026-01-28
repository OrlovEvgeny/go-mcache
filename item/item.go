// Package item defines the structure used for cache entries.
// This structure may be exposed to cache consumers if needed.
package item

// Item represents a cache entry.
type Item struct {
	Key      string      // Cache key
	ExpireAt int64       // Expiration timestamp in Unix nano (0 means no expiration)
	Data     []byte      // Raw value bytes
	DataLink interface{} // Optional associated object
}

// IsExpired returns true if the item has expired by the provided time (Unix nano).
func (i *Item) IsExpired(nowNano int64) bool {
	return i.ExpireAt > 0 && nowNano > i.ExpireAt
}

// IsExpiredZero returns true if the item never expires.
func (i *Item) IsExpiredZero() bool {
	return i.ExpireAt == 0
}
