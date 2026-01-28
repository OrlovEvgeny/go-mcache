package buffer

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRingBufferBasic(t *testing.T) {
	rb := NewRingBuffer[int](8)

	// Test Push and Pop
	if !rb.Push(1) {
		t.Error("Push should succeed on empty buffer")
	}
	if !rb.Push(2) {
		t.Error("Push should succeed")
	}
	if !rb.Push(3) {
		t.Error("Push should succeed")
	}

	// Test Len
	if rb.Len() != 3 {
		t.Errorf("Expected Len=3, got %d", rb.Len())
	}

	// Test Pop (FIFO order)
	val, ok := rb.Pop()
	if !ok || val != 1 {
		t.Errorf("Expected 1, got %d, ok=%v", val, ok)
	}

	val, ok = rb.Pop()
	if !ok || val != 2 {
		t.Errorf("Expected 2, got %d, ok=%v", val, ok)
	}

	val, ok = rb.Pop()
	if !ok || val != 3 {
		t.Errorf("Expected 3, got %d, ok=%v", val, ok)
	}

	// Pop on empty
	_, ok = rb.Pop()
	if ok {
		t.Error("Pop should return false on empty buffer")
	}
}

func TestRingBufferFull(t *testing.T) {
	rb := NewRingBuffer[int](4) // Capacity will be 4

	// Fill buffer
	for i := 0; i < 4; i++ {
		if !rb.Push(i) {
			t.Errorf("Push %d should succeed", i)
		}
	}

	// Buffer should be full
	if !rb.IsFull() {
		t.Error("Buffer should be full")
	}

	// Push should fail
	if rb.Push(99) {
		t.Error("Push should fail on full buffer")
	}

	// Pop one and push should work
	rb.Pop()
	if !rb.Push(99) {
		t.Error("Push should succeed after Pop")
	}
}

func TestRingBufferEmpty(t *testing.T) {
	rb := NewRingBuffer[int](8)

	if !rb.IsEmpty() {
		t.Error("New buffer should be empty")
	}

	rb.Push(1)
	if rb.IsEmpty() {
		t.Error("Buffer with item should not be empty")
	}

	rb.Pop()
	if !rb.IsEmpty() {
		t.Error("Buffer after Pop should be empty")
	}
}

func TestRingBufferCap(t *testing.T) {
	rb := NewRingBuffer[int](10) // Will round up to 16
	if rb.Cap() != 16 {
		t.Errorf("Expected capacity 16, got %d", rb.Cap())
	}

	rb2 := NewRingBuffer[int](8)
	if rb2.Cap() != 8 {
		t.Errorf("Expected capacity 8, got %d", rb2.Cap())
	}
}

func TestRingBufferConcurrent(t *testing.T) {
	rb := NewRingBuffer[int](1024)

	const producers = 4
	const itemsPerProducer = 1000

	var wg sync.WaitGroup
	var produced atomic.Int64
	var consumed atomic.Int64

	// Producers
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < itemsPerProducer; i++ {
				val := id*itemsPerProducer + i
				for !rb.Push(val) {
					// Spin wait for space
				}
				produced.Add(1)
			}
		}(p)
	}

	// Consumer
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				// Drain remaining
				for {
					if _, ok := rb.Pop(); !ok {
						return
					}
					consumed.Add(1)
				}
			default:
				if _, ok := rb.Pop(); ok {
					consumed.Add(1)
				}
			}
		}
	}()

	wg.Wait()
	close(done)
	time.Sleep(10 * time.Millisecond) // Allow consumer to drain

	t.Logf("Produced: %d, Consumed: %d", produced.Load(), consumed.Load())
}

func TestWriteBuffer(t *testing.T) {
	var items []int
	var mu sync.Mutex

	wb := NewWriteBuffer[int](64, 10, time.Millisecond, func(batch []int) {
		mu.Lock()
		items = append(items, batch...)
		mu.Unlock()
	})

	// Push items
	for i := 0; i < 50; i++ {
		wb.Push(i)
	}

	// Wait for flush
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	count := len(items)
	mu.Unlock()

	if count < 50 {
		t.Errorf("Expected at least 50 items flushed, got %d", count)
	}

	wb.Close()
}

func TestWriteBufferFlush(t *testing.T) {
	var flushed atomic.Int32

	wb := NewWriteBuffer[int](64, 10, time.Hour, func(batch []int) {
		flushed.Add(int32(len(batch)))
	})

	// Push items
	for i := 0; i < 5; i++ {
		wb.Push(i)
	}

	// Force flush
	wb.Flush()
	time.Sleep(10 * time.Millisecond)

	if flushed.Load() != 5 {
		t.Errorf("Expected 5 items flushed, got %d", flushed.Load())
	}

	wb.Close()
}

func TestWriteBufferClose(t *testing.T) {
	var flushed atomic.Int32

	wb := NewWriteBuffer[int](64, 100, time.Hour, func(batch []int) {
		flushed.Add(int32(len(batch)))
	})

	// Push items
	for i := 0; i < 20; i++ {
		wb.Push(i)
	}

	// Close should flush remaining
	wb.Close()

	if flushed.Load() < 20 {
		t.Errorf("Expected at least 20 items flushed on Close, got %d", flushed.Load())
	}
}

func BenchmarkRingBufferPush(b *testing.B) {
	rb := NewRingBuffer[int](1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Push(i)
		if rb.IsFull() {
			rb.Pop()
		}
	}
}

func BenchmarkRingBufferPushPop(b *testing.B) {
	rb := NewRingBuffer[int](1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Push(i)
		rb.Pop()
	}
}

func BenchmarkRingBufferConcurrent(b *testing.B) {
	rb := NewRingBuffer[int](1024)

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				rb.Push(i)
			} else {
				rb.Pop()
			}
			i++
		}
	})
}

func BenchmarkWriteBufferPush(b *testing.B) {
	wb := NewWriteBuffer[int](1024, 64, 10*time.Microsecond, func(batch []int) {
		// No-op
	})
	defer wb.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wb.Push(i)
	}
}
