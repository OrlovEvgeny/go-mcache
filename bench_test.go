package mcache

import (
	"fmt"
	"testing"
)

//BenchmarkWrite
func BenchmarkWrite(b *testing.B) {
	mcache = New()

	for i := 0; i < b.N; i++ {
		mcache.Set(fmt.Sprintf("%d", i), i, TTL_FOREVER)
	}
}

//BenchmarkRead
func BenchmarkRead(b *testing.B) {
	for i := 0; i < b.N; i++ {
		mcache.Get(fmt.Sprintf("%d", i))
	}
}

//BenchmarkRW
func BenchmarkRW(b *testing.B) {
	for i := 0; i < b.N; i++ {
		mcache.Set(fmt.Sprintf("%d", i), i, TTL_FOREVER)
		mcache.Get(fmt.Sprintf("%d", i))
	}
}
