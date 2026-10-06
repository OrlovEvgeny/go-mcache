package mcache

import (
	"testing"
	"time"
)

// Measure sustained sequential overwrites separately from retained-memory
// diagnostics, which include cache construction and forced GC in their time.
func BenchmarkTTLOverwriteSerial(b *testing.B) {
	c := NewCache[int, int]()
	defer c.Close()
	for i := range 10000 {
		c.Set(i, i, time.Hour)
	}
	i := 0
	for b.Loop() {
		c.Set(i%10000, i, time.Hour)
		i++
	}
}
