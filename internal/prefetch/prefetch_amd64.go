//go:build amd64

package prefetch

import "unsafe"

// PrefetchT0 prefetches data into all cache levels (L1, L2, L3).
// This is the most aggressive prefetch hint, suitable when data will be
// accessed very soon.
//
//go:noescape
func PrefetchT0(addr unsafe.Pointer)

// PrefetchT1 prefetches data into L2 and L3 caches.
// Use when data will be accessed soon but not immediately.
//
//go:noescape
func PrefetchT1(addr unsafe.Pointer)

// PrefetchT2 prefetches data into L3 cache only.
// Use for data that will be accessed in the near future.
//
//go:noescape
func PrefetchT2(addr unsafe.Pointer)

// PrefetchNTA prefetches data with non-temporal hint.
// Data is fetched minimizing cache pollution - useful for streaming access patterns.
//
//go:noescape
func PrefetchNTA(addr unsafe.Pointer)

// PrefetchW prefetches data for writing.
// Brings cache line into exclusive state, useful before a write operation.
//
//go:noescape
func PrefetchW(addr unsafe.Pointer)
