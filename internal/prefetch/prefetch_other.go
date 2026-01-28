//go:build !amd64

package prefetch

import "unsafe"

// PrefetchT0 is a no-op on non-amd64 platforms.
func PrefetchT0(addr unsafe.Pointer) {}

// PrefetchT1 is a no-op on non-amd64 platforms.
func PrefetchT1(addr unsafe.Pointer) {}

// PrefetchT2 is a no-op on non-amd64 platforms.
func PrefetchT2(addr unsafe.Pointer) {}

// PrefetchNTA is a no-op on non-amd64 platforms.
func PrefetchNTA(addr unsafe.Pointer) {}

// PrefetchW is a no-op on non-amd64 platforms.
func PrefetchW(addr unsafe.Pointer) {}
