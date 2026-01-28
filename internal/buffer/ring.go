// Package buffer provides lock-free ring buffer for write coalescing.
package buffer

import (
	"sync/atomic"
	"time"
)

// RingBuffer is a lock-free MPSC (Multi-Producer Single-Consumer) ring buffer.
type RingBuffer[T any] struct {
	data []node[T]
	mask uint64
	head atomic.Uint64 // Consumer reads from head
	tail atomic.Uint64 // Producers write to tail
}

type node[T any] struct {
	value T
	seq   atomic.Uint64
}

// NewRingBuffer creates a new ring buffer with the given capacity.
// Capacity is rounded up to the nearest power of 2.
func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	if capacity <= 0 {
		capacity = 64
	}
	// Round up to power of 2
	capacity--
	capacity |= capacity >> 1
	capacity |= capacity >> 2
	capacity |= capacity >> 4
	capacity |= capacity >> 8
	capacity |= capacity >> 16
	capacity++

	rb := &RingBuffer[T]{
		data: make([]node[T], capacity),
		mask: uint64(capacity - 1),
	}

	// Initialize sequence numbers
	for i := range rb.data {
		rb.data[i].seq.Store(uint64(i))
	}

	return rb
}

// Push attempts to add an item to the buffer.
// Returns true if successful, false if buffer is full.
func (rb *RingBuffer[T]) Push(item T) bool {
	for {
		tail := rb.tail.Load()
		idx := tail & rb.mask
		seq := rb.data[idx].seq.Load()

		if seq == tail {
			// Slot is available
			if rb.tail.CompareAndSwap(tail, tail+1) {
				rb.data[idx].value = item
				rb.data[idx].seq.Store(tail + 1)
				return true
			}
		} else if seq < tail {
			// Buffer is full
			return false
		}
		// Retry - another producer got there first
	}
}

// Pop removes and returns the oldest item from the buffer.
// Returns the item and true if successful, zero value and false if buffer is empty.
func (rb *RingBuffer[T]) Pop() (T, bool) {
	for {
		head := rb.head.Load()
		idx := head & rb.mask
		seq := rb.data[idx].seq.Load()

		if seq == head+1 {
			// Item is available
			if rb.head.CompareAndSwap(head, head+1) {
				value := rb.data[idx].value
				var zero T
				rb.data[idx].value = zero // Clear reference
				rb.data[idx].seq.Store(head + uint64(len(rb.data)))
				return value, true
			}
		} else if seq == head {
			// Buffer is empty
			var zero T
			return zero, false
		}
		// Retry
	}
}

// Len returns the current number of items in the buffer.
// Note: This is approximate due to concurrent operations.
func (rb *RingBuffer[T]) Len() int {
	tail := rb.tail.Load()
	head := rb.head.Load()
	return int(tail - head)
}

// Cap returns the capacity of the buffer.
func (rb *RingBuffer[T]) Cap() int {
	return len(rb.data)
}

// IsEmpty returns true if the buffer is empty.
func (rb *RingBuffer[T]) IsEmpty() bool {
	return rb.Len() == 0
}

// IsFull returns true if the buffer is full.
func (rb *RingBuffer[T]) IsFull() bool {
	return rb.Len() >= len(rb.data)
}

// WriteBuffer provides batched write operations with automatic flushing.
type WriteBuffer[T any] struct {
	ring      *RingBuffer[T]
	flushFn   func([]T)
	batchSize int
	flushCh   chan struct{}
	stopCh    chan struct{}
	doneCh    chan struct{}
}

// NewWriteBuffer creates a new write buffer with automatic flushing.
// flushFn is called with a batch of items when the buffer is flushed.
// batchSize is the maximum number of items to accumulate before flushing.
// flushInterval is the maximum time between flushes.
func NewWriteBuffer[T any](capacity int, batchSize int, flushInterval time.Duration, flushFn func([]T)) *WriteBuffer[T] {
	wb := &WriteBuffer[T]{
		ring:      NewRingBuffer[T](capacity),
		flushFn:   flushFn,
		batchSize: batchSize,
		flushCh:   make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}

	go wb.flushLoop(flushInterval)
	return wb
}

// Push adds an item to the write buffer.
// May trigger a flush if the batch size is reached.
func (wb *WriteBuffer[T]) Push(item T) bool {
	if !wb.ring.Push(item) {
		// Buffer full, trigger flush and retry
		select {
		case wb.flushCh <- struct{}{}:
		default:
		}
		// Spin wait briefly for space
		for i := 0; i < 100; i++ {
			if wb.ring.Push(item) {
				return true
			}
		}
		return false
	}

	// Trigger flush if batch size reached
	if wb.ring.Len() >= wb.batchSize {
		select {
		case wb.flushCh <- struct{}{}:
		default:
		}
	}
	return true
}

// flushLoop runs the background flush goroutine.
func (wb *WriteBuffer[T]) flushLoop(interval time.Duration) {
	defer close(wb.doneCh)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	batch := make([]T, 0, wb.batchSize)

	for {
		select {
		case <-wb.stopCh:
			// Final flush
			wb.flushBatch(&batch)
			return
		case <-ticker.C:
			wb.flushBatch(&batch)
		case <-wb.flushCh:
			wb.flushBatch(&batch)
		}
	}
}

// flushBatch flushes accumulated items.
func (wb *WriteBuffer[T]) flushBatch(batch *[]T) {
	*batch = (*batch)[:0]

	for {
		item, ok := wb.ring.Pop()
		if !ok {
			break
		}
		*batch = append(*batch, item)
		if len(*batch) >= wb.batchSize {
			break
		}
	}

	if len(*batch) > 0 {
		wb.flushFn(*batch)
	}
}

// Flush triggers an immediate flush of the buffer.
func (wb *WriteBuffer[T]) Flush() {
	select {
	case wb.flushCh <- struct{}{}:
	default:
	}
}

// Close stops the write buffer and flushes remaining items.
func (wb *WriteBuffer[T]) Close() {
	close(wb.stopCh)
	<-wb.doneCh
}

// Len returns the current number of pending items.
func (wb *WriteBuffer[T]) Len() int {
	return wb.ring.Len()
}
