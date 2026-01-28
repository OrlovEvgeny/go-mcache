package mcache

import (
	"github.com/OrlovEvgeny/go-mcache/internal/glob"
	"github.com/OrlovEvgeny/go-mcache/internal/store"
)

// Iterator provides a Redis-style iterator over cache entries.
type Iterator[K comparable, V any] struct {
	cache   *Cache[K, V]
	cursor  uint64
	count   int
	prefix  string
	pattern *glob.Pattern
	page    []*store.Entry[K, V]
	pos     int
	err     error
	done    bool
}

// newIterator creates a new iterator.
func newIterator[K comparable, V any](c *Cache[K, V], cursor uint64, count int, prefix string, pattern *glob.Pattern) *Iterator[K, V] {
	if count <= 0 {
		count = 10
	}

	return &Iterator[K, V]{
		cache:   c,
		cursor:  cursor,
		count:   count,
		prefix:  prefix,
		pattern: pattern,
	}
}

// newEmptyIterator creates an empty iterator (for unsupported operations).
func newEmptyIterator[K comparable, V any]() *Iterator[K, V] {
	return &Iterator[K, V]{done: true}
}

// Next advances the iterator to the next entry.
// Returns true if there is an entry available, false when exhausted.
func (it *Iterator[K, V]) Next() bool {
	if it.done || it.err != nil {
		return false
	}

	// Move to next position in current page
	it.pos++

	// Need to fetch next page?
	if it.pos >= len(it.page) {
		if !it.fetchPage() {
			return false
		}
	}

	return true
}

// fetchPage retrieves the next page of entries.
func (it *Iterator[K, V]) fetchPage() bool {
	if it.cache == nil {
		it.done = true
		return false
	}

	// Use prefix search if we have a prefix and string keys
	if it.prefix != "" && it.cache.isStringKey && it.cache.radixTree != nil {
		return it.fetchPrefixPage()
	}

	// Full scan
	entries, nextCursor := it.cache.store.Scan(it.cursor, it.count*2) // Fetch extra for filtering
	if len(entries) == 0 {
		it.done = true
		return false
	}

	// Filter entries if we have a pattern
	filtered := make([]*store.Entry[K, V], 0, it.count)
	for _, entry := range entries {
		if it.matchEntry(entry) {
			filtered = append(filtered, entry)
			if len(filtered) >= it.count {
				break
			}
		}
	}

	it.page = filtered
	it.pos = 0
	it.cursor = nextCursor

	if nextCursor == 0 {
		it.done = len(it.page) == 0
	}

	return len(it.page) > 0
}

// fetchPrefixPage retrieves the next page using radix tree prefix search.
func (it *Iterator[K, V]) fetchPrefixPage() bool {
	// Get key hashes matching the prefix
	hashes := it.cache.radixTree.FindByPrefix(it.prefix, it.count*2)
	if len(hashes) == 0 {
		it.done = true
		return false
	}

	// Skip to cursor position
	start := int(it.cursor)
	if start >= len(hashes) {
		it.done = true
		return false
	}

	// Collect entries
	entries := make([]*store.Entry[K, V], 0, it.count)
	for i := start; i < len(hashes) && len(entries) < it.count; i++ {
		keyHash := hashes[i]
		// Find entry by hash
		it.cache.store.Range(func(entry *store.Entry[K, V]) bool {
			if entry.KeyHash == keyHash {
				if it.matchEntry(entry) {
					entries = append(entries, entry)
				}
				return false // Found it
			}
			return true
		})
	}

	it.page = entries
	it.pos = 0
	it.cursor = uint64(start + len(entries))

	if start+len(entries) >= len(hashes) {
		it.done = len(entries) == 0
	}

	return len(entries) > 0
}

// matchEntry checks if an entry matches the iterator's filters.
func (it *Iterator[K, V]) matchEntry(entry *store.Entry[K, V]) bool {
	// Check expiration
	if entry.IsExpired() {
		return false
	}

	// Check pattern match for string keys
	if it.pattern != nil {
		if strKey, ok := any(entry.Key).(string); ok {
			if !it.pattern.Match(strKey) {
				return false
			}
		}
	}

	return true
}

// Key returns the current entry's key.
func (it *Iterator[K, V]) Key() K {
	if it.pos < 0 || it.pos >= len(it.page) {
		var zero K
		return zero
	}
	return it.page[it.pos].Key
}

// Value returns the current entry's value.
func (it *Iterator[K, V]) Value() V {
	if it.pos < 0 || it.pos >= len(it.page) {
		var zero V
		return zero
	}
	return it.page[it.pos].Value
}

// Entry returns the current entry (key, value pair).
func (it *Iterator[K, V]) Entry() (K, V) {
	return it.Key(), it.Value()
}

// Cursor returns the current cursor position.
// This can be used to resume iteration later.
func (it *Iterator[K, V]) Cursor() uint64 {
	return it.cursor
}

// Err returns any error that occurred during iteration.
func (it *Iterator[K, V]) Err() error {
	return it.err
}

// All collects all remaining entries and returns them.
// Warning: This may be memory-intensive for large result sets.
func (it *Iterator[K, V]) All() []Item[K, V] {
	var items []Item[K, V]
	for it.Next() {
		items = append(items, Item[K, V]{
			Key:   it.Key(),
			Value: it.Value(),
		})
	}
	return items
}

// Keys collects all remaining keys and returns them.
func (it *Iterator[K, V]) Keys() []K {
	var keys []K
	for it.Next() {
		keys = append(keys, it.Key())
	}
	return keys
}

// Values collects all remaining values and returns them.
func (it *Iterator[K, V]) Values() []V {
	var values []V
	for it.Next() {
		values = append(values, it.Value())
	}
	return values
}

// Count counts the remaining entries without collecting them.
func (it *Iterator[K, V]) Count() int {
	count := 0
	for it.Next() {
		count++
	}
	return count
}

// ForEach calls fn for each remaining entry.
// If fn returns false, iteration stops.
func (it *Iterator[K, V]) ForEach(fn func(key K, value V) bool) {
	for it.Next() {
		if !fn(it.Key(), it.Value()) {
			return
		}
	}
}
