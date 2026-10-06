package store

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestExpiryKernel(t *testing.T) {
	rng := rand.New(rand.NewPCG(12, 34))
	for _, n := range []int{0, 1, 2, 3, 4, 7, 8, 9, 15, 16, 17, 31, 32, 33, 63, 64, 65, 127, 128, 129, 1024} {
		for _, now := range []int64{math.MinInt64, -1, 0, 1, 17, math.MaxInt64} {
			input := make([]int64, n)
			for i := range input {
				input[i] = int64(rng.Uint64())
				switch i % 7 {
				case 0:
					input[i] = now
				case 1:
					input[i] = 0
				case 2:
					input[i] = now - 1
				case 3:
					input[i] = now + 1
				}
			}
			want := slices.Clone(input)
			markExpiredScalar(want, now)
			markExpired(input, now)
			if !slices.Equal(input, want) {
				t.Fatalf("n=%d now=%d: got %v want %v", n, now, input, want)
			}
		}
	}
}

func BenchmarkExpiryKernel(b *testing.B) {
	for _, n := range []int{16, 64, 256, 1024, 16384} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			source := make([]int64, n)
			for i := range source {
				source[i] = int64(i%3) * 100
			}
			input := make([]int64, n)
			for b.Loop() {
				copy(input, source)
				markExpired(input, 150)
			}
		})
	}
}
