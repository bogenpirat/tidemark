package buffer

// RingBuffer is a fixed-capacity circular buffer. Push overwrites the oldest
// entry once capacity is reached. All methods are not goroutine-safe; callers
// must synchronize externally if needed.
type RingBuffer[T any] struct {
	data     []T
	head     int // index of the next write position
	count    int
	capacity int
}

// New creates a RingBuffer with the given fixed capacity.
func New[T any](capacity int) *RingBuffer[T] {
	return &RingBuffer[T]{
		data:     make([]T, capacity),
		capacity: capacity,
	}
}

// Push inserts an item, overwriting the oldest entry when the buffer is full.
func (ringBuffer *RingBuffer[T]) Push(item T) {
	ringBuffer.data[ringBuffer.head] = item
	ringBuffer.head = (ringBuffer.head + 1) % ringBuffer.capacity
	if ringBuffer.count < ringBuffer.capacity {
		ringBuffer.count++
	}
}

// Snapshot returns a copy of all items in chronological order (oldest first).
func (ringBuffer *RingBuffer[T]) Snapshot() []T {
	if ringBuffer.count == 0 {
		return nil
	}
	snapshot := make([]T, ringBuffer.count)
	if ringBuffer.count < ringBuffer.capacity {
		copy(snapshot, ringBuffer.data[:ringBuffer.count])
	} else {
		oldestIndex := ringBuffer.head
		firstChunkLength := ringBuffer.capacity - oldestIndex
		copy(snapshot, ringBuffer.data[oldestIndex:])
		copy(snapshot[firstChunkLength:], ringBuffer.data[:oldestIndex])
	}
	return snapshot
}
