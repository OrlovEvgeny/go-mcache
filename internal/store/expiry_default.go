//go:build !goexperiment.simd

package store

func markExpired(deadlines []int64, now int64) { markExpiredScalar(deadlines, now) }
