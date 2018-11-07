package gomcache

import (
	"fmt"
	"testing"
	"time"
)


//BenchmarkWrite
func BenchmarkWrite(b *testing.B) {
	mcache = StartInstance()

	for i := 0; i < b.N; i++ {
		mcache.SetPointer(fmt.Sprintf("%d", i), i, time.Second*60)
	}
}

//BenchmarkRead
func BenchmarkRead(b *testing.B) {
	for i := 0; i < b.N; i++ {
		mcache.GetPointer(fmt.Sprintf("%d", i))
	}
}

//BenchmarkRW
func BenchmarkRW(b *testing.B) {
	for i := 0; i < b.N; i++ {
		mcache.SetPointer(fmt.Sprintf("%d", i), i, time.Second*60)
		mcache.GetPointer(fmt.Sprintf("%d", i))
	}
}