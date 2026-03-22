package policy

// Victim represents an entry selected for eviction.
type Victim[K comparable] struct {
	Key     K
	KeyHash uint64
}

// Policer is the interface for admission and eviction policies.
// Generic over K to enable exact-key identity (no hash collision ambiguity).
type Policer[K comparable] interface {
	// Add attempts to add a key with the given cost.
	// Returns (victims to evict, whether the item should be added).
	Add(key K, keyHash uint64, cost int64) (victims []Victim[K], added bool)

	// Access records an access to a key (hit).
	// Uses keyHash for frequency estimation (TinyLFU).
	Access(keyHash uint64)

	// Has checks if a key is tracked by the policy.
	Has(key K) bool

	// Del removes a key from the policy.
	Del(key K, keyHash uint64)

	// Update updates the cost of an existing key.
	Update(key K, keyHash uint64, cost int64)

	// Cost returns the current total cost.
	Cost() int64

	// NumEntries returns the current number of entries.
	NumEntries() int64

	// Clear resets the policy to its initial state.
	Clear()

	// SetMaxCost updates the maximum cost limit.
	SetMaxCost(maxCost int64)

	// SetMaxEntries updates the maximum entries limit.
	SetMaxEntries(maxEntries int64)
}
