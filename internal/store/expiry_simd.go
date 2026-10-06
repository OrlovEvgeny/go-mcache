//go:build goexperiment.simd

package store

import "simd"

func markExpired(deadlines []int64, now int64) {
	// Keep the small-input path outside the compiler's SIMD dispatch wrapper.
	if len(deadlines) < 64 || simd.Emulated() {
		markExpiredScalar(deadlines, now)
		return
	}
	markExpiredSIMD(deadlines, now)
}

func markExpiredSIMD(deadlines []int64, now int64) {
	zero := simd.BroadcastInt64s(0)
	current := simd.BroadcastInt64s(now)
	width := current.Len()
	// Four independent vectors amortize loop and slice-bound checks even on
	// 128-bit hardware, where each vector contains just two deadlines.
	for len(deadlines) >= 4*width {
		a := simd.LoadInt64s(deadlines)
		b := simd.LoadInt64s(deadlines[width:])
		c := simd.LoadInt64s(deadlines[2*width:])
		d := simd.LoadInt64s(deadlines[3*width:])
		a.Greater(zero).And(a.Less(current)).ToInt64s().Store(deadlines)
		b.Greater(zero).And(b.Less(current)).ToInt64s().Store(deadlines[width:])
		c.Greater(zero).And(c.Less(current)).ToInt64s().Store(deadlines[2*width:])
		d.Greater(zero).And(d.Less(current)).ToInt64s().Store(deadlines[3*width:])
		deadlines = deadlines[4*width:]
	}
	for len(deadlines) >= width {
		d := simd.LoadInt64s(deadlines)
		d.Greater(zero).And(d.Less(current)).ToInt64s().Store(deadlines)
		deadlines = deadlines[width:]
	}
	markExpiredScalar(deadlines, now)
}
