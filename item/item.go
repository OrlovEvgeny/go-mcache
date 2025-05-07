// Package item defines the structure used for cache entries.
// This structure may be exposed to cache consumers if needed.
package item

import "time"

// Item represents a cache entry.
type Item struct {
	Key      string      // Cache key
	Expire   time.Time   // Expiration timestamp (zero means no expiration)
	Data     []byte      // Raw value bytes
	DataLink interface{} // Optional associated object
}

// IsExpired returns true if the item has expired by the provided time.
func (i *Item) IsExpired(now time.Time) bool {
	if i.Expire.IsZero() {
		return false
	}
	return i.Expire.Before(now)
}

// IsActuallyExpired checks expiration against the current system time.
// Avoid in tight loops due to time.Now() cost.
func (i *Item) IsActuallyExpired() bool {
	return i.IsExpired(time.Now())
}
