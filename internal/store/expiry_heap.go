package store

import (
	"container/heap"
	"sync"
)

// expiryItem represents an item in the expiry heap.
type expiryItem struct {
	keyHash  uint64
	expireAt int64
	index    int // Index in the heap slice, maintained by heap.Interface
}

// expiryHeapInternal implements heap.Interface for expiryItems.
type expiryHeapInternal []*expiryItem

func (h expiryHeapInternal) Len() int           { return len(h) }
func (h expiryHeapInternal) Less(i, j int) bool { return h[i].expireAt < h[j].expireAt }
func (h expiryHeapInternal) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *expiryHeapInternal) Push(x any) {
	n := len(*h)
	item := x.(*expiryItem)
	item.index = n
	*h = append(*h, item)
}

func (h *expiryHeapInternal) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // Avoid memory leak
	item.index = -1 // Mark as removed
	*h = old[:n-1]
	return item
}

// ExpiryHeap is a thread-safe min-heap for tracking entry expirations.
// Provides O(log n) insertion, removal, and O(1) peek of next expiration.
type ExpiryHeap struct {
	items    expiryHeapInternal
	keyIndex map[uint64]*expiryItem // keyHash -> heap item for O(1) lookup
	mu       sync.RWMutex
}

// NewExpiryHeap creates a new expiry heap with the given initial capacity.
func NewExpiryHeap(initialCap int) *ExpiryHeap {
	if initialCap < 16 {
		initialCap = 16
	}
	return &ExpiryHeap{
		items:    make(expiryHeapInternal, 0, initialCap),
		keyIndex: make(map[uint64]*expiryItem, initialCap),
	}
}

// Push adds or updates an item in the heap.
// If the keyHash already exists, it updates the expiration time.
// If expireAt <= 0, removes any existing entry (key no longer expires).
// Time complexity: O(log n)
func (h *ExpiryHeap) Push(keyHash uint64, expireAt int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if expireAt <= 0 {
		// Key no longer expires — remove any existing entry
		h.removeLocked(keyHash)
		return
	}

	if existing, ok := h.keyIndex[keyHash]; ok {
		// Update existing item
		existing.expireAt = expireAt
		heap.Fix(&h.items, existing.index)
		return
	}

	// Add new item
	item := &expiryItem{
		keyHash:  keyHash,
		expireAt: expireAt,
	}
	heap.Push(&h.items, item)
	h.keyIndex[keyHash] = item
}

// removeLocked removes an item by keyHash. Must be called with h.mu held.
func (h *ExpiryHeap) removeLocked(keyHash uint64) {
	item, ok := h.keyIndex[keyHash]
	if !ok {
		return
	}
	heap.Remove(&h.items, item.index)
	delete(h.keyIndex, keyHash)
}

// Pop removes and returns the item with the earliest expiration time.
// Returns the keyHash, expireAt, and true if successful, or 0, 0, false if empty.
// Time complexity: O(log n)
func (h *ExpiryHeap) Pop() (uint64, int64, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.items) == 0 {
		return 0, 0, false
	}

	item := heap.Pop(&h.items).(*expiryItem)
	delete(h.keyIndex, item.keyHash)
	return item.keyHash, item.expireAt, true
}

// Remove removes an item from the heap by keyHash.
// Returns true if the item was found and removed.
// Time complexity: O(log n)
func (h *ExpiryHeap) Remove(keyHash uint64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	_, ok := h.keyIndex[keyHash]
	if !ok {
		return false
	}
	h.removeLocked(keyHash)
	return true
}

// PeekExpireAt returns the expiration time of the item that will expire next.
// Returns 0 if the heap is empty.
// Time complexity: O(1)
func (h *ExpiryHeap) PeekExpireAt() int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.items) == 0 {
		return 0
	}
	return h.items[0].expireAt
}

// PopExpired removes and returns all items that have expired before the given time.
// Time complexity: O(k log n) where k is the number of expired items.
func (h *ExpiryHeap) PopExpired(now int64) []uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.items) == 0 {
		return nil
	}

	// Pre-allocate with reasonable estimate
	expired := make([]uint64, 0, 8)

	for len(h.items) > 0 && h.items[0].expireAt <= now {
		item := heap.Pop(&h.items).(*expiryItem)
		delete(h.keyIndex, item.keyHash)
		expired = append(expired, item.keyHash)
	}

	return expired
}

// Len returns the number of items in the heap.
func (h *ExpiryHeap) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.items)
}

// Clear removes all items from the heap.
func (h *ExpiryHeap) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Nil out pointers to allow GC
	for i := range h.items {
		h.items[i] = nil
	}
	h.items = h.items[:0]
	// Replace the map to ensure old entries are freed
	h.keyIndex = make(map[uint64]*expiryItem, len(h.keyIndex))
}
