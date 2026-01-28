// Package prefetch provides software prefetching utilities for cache optimization.
package prefetch

import "unsafe"

// PrefetchSlice prefetches the beginning of a slice into L1 cache.
func PrefetchSlice[T any](s []T) {
	if len(s) > 0 {
		PrefetchT0(unsafe.Pointer(&s[0]))
	}
}

// PrefetchSliceRange prefetches multiple cache lines of a slice.
// stride is the number of elements between prefetch points.
func PrefetchSliceRange[T any](s []T, stride int) {
	if stride <= 0 {
		stride = 8 // Default stride for cache line spacing
	}
	for i := 0; i < len(s); i += stride {
		PrefetchT0(unsafe.Pointer(&s[i]))
	}
}

// PrefetchMap prefetches the map header.
// Note: This cannot prefetch actual map buckets as they're internal.
func PrefetchMap[K comparable, V any](m map[K]V) {
	if m != nil {
		// Prefetch the map header
		PrefetchT0(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
	}
}
