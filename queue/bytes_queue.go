package queue

import (
	"encoding/binary"
	"log"
	"time"
)

const (
	minimumHeaderSize = 17 // 1 byte blobsize + timestampSizeInBytes + hashSizeInBytes
	leftMarginIndex   = 1
)

type queueError struct {
	message string
}

func (e *queueError) Error() string {
	return e.message
}

var (
	errEmptyQueue       = &queueError{"Empty queue"}
	errInvalidIndex     = &queueError{"Index must be greater than zero. Invalid index."}
	errIndexOutOfBounds = &queueError{"Index out of range"}
)

type BytesQueue struct {
	full         bool
	array        []byte
	capacity     int
	maxCapacity  int
	head         int
	tail         int
	count        int
	rightMargin  int
	headerBuffer []byte
	verbose      bool
}

func getNeededSize(length int) int {
	var header int
	switch {
	case length < 127: // 1<<7-1
		header = 1
	case length < 16382: // 1<<14-2
		header = 2
	case length < 2097149: // 1<<21 -3
		header = 3
	case length < 268435452: // 1<<28 -4
		header = 4
	default:
		header = 5
	}
	return length + header
}

func NewBytesQueue(capacity int, maxCapacity int, verbose bool) *BytesQueue {
	return &BytesQueue{
		array:        make([]byte, capacity),
		capacity:     capacity,
		maxCapacity:  maxCapacity,
		headerBuffer: make([]byte, binary.MaxVarintLen32),
		tail:         leftMarginIndex,
		head:         leftMarginIndex,
		rightMargin:  leftMarginIndex,
		verbose:      verbose,
	}
}

func (q *BytesQueue) Reset() {
	q.tail = leftMarginIndex
	q.head = leftMarginIndex
	q.rightMargin = leftMarginIndex
	q.count = 0
	q.full = false
}

func (q *BytesQueue) Push(data []byte) (int, error) {
	neededSize := getNeededSize(len(data))

	if !q.canInsertAfterTail(neededSize) {
		if q.canInsertBeforeHead(neededSize) {
			q.tail = leftMarginIndex
		} else if q.capacity+neededSize >= q.maxCapacity && q.maxCapacity > 0 {
			return -1, &queueError{"Full queue. Maximum size limit reached."}
		} else {
			q.allocateAdditionalMemory(neededSize)
		}
	}

	index := q.tail

	q.push(data, neededSize)

	return index, nil
}

func (q *BytesQueue) allocateAdditionalMemory(minimum int) {
	start := time.Now()
	if q.capacity < minimum {
		q.capacity += minimum
	}
	q.capacity = q.capacity * 2
	if q.capacity > q.maxCapacity && q.maxCapacity > 0 {
		q.capacity = q.maxCapacity
	}

	oldArray := q.array
	q.array = make([]byte, q.capacity)

	if leftMarginIndex != q.rightMargin {
		copy(q.array, oldArray[:q.rightMargin])

		if q.tail <= q.head {
			if q.tail != q.head {
				q.push(make([]byte, q.head-q.tail), q.head-q.tail)
			}

			q.head = leftMarginIndex
			q.tail = q.rightMargin
		}
	}

	q.full = false

	if q.verbose {
		log.Printf("Allocated new queue in %s; Capacity: %d \n", time.Since(start), q.capacity)
	}
}

func (q *BytesQueue) push(data []byte, len int) {
	headerEntrySize := binary.PutUvarint(q.headerBuffer, uint64(len))
	q.copy(q.headerBuffer, headerEntrySize)

	q.copy(data, len-headerEntrySize)

	if q.tail > q.head {
		q.rightMargin = q.tail
	}
	if q.tail == q.head {
		q.full = true
	}

	q.count++
}

func (q *BytesQueue) copy(data []byte, len int) {
	q.tail += copy(q.array[q.tail:], data[:len])
}

func (q *BytesQueue) Pop() ([]byte, error) {
	data, blockSize, err := q.peek(q.head)
	if err != nil {
		return nil, err
	}

	q.head += blockSize
	q.count--

	if q.head == q.rightMargin {
		q.head = leftMarginIndex
		if q.tail == q.rightMargin {
			q.tail = leftMarginIndex
		}
		q.rightMargin = q.tail
	}

	q.full = false

	return data, nil
}

func (q *BytesQueue) Get(index int) ([]byte, error) {
	data, _, err := q.peek(index)
	return data, err
}

func (q *BytesQueue) CheckGet(index int) error {
	return q.peekCheckErr(index)
}

func (q *BytesQueue) Capacity() int {
	return q.capacity
}

func (q *BytesQueue) Len() int {
	return q.count
}

func (q *BytesQueue) Peek() ([]byte, error) {
	data, _, err := q.peek(q.head)
	return data, err
}

func (q *BytesQueue) peek(index int) ([]byte, int, error) {
	err := q.peekCheckErr(index)
	if err != nil {
		return nil, 0, err
	}

	blockSize, n := binary.Uvarint(q.array[index:])
	return q.array[index+n : index+int(blockSize)], int(blockSize), nil
}

func (q *BytesQueue) peekCheckErr(index int) error {

	if q.count == 0 {
		return errEmptyQueue
	}

	if index <= 0 {
		return errInvalidIndex
	}

	if index >= len(q.array) {
		return errIndexOutOfBounds
	}
	return nil
}

func (q *BytesQueue) canInsertAfterTail(need int) bool {
	if q.full {
		return false
	}
	if q.tail >= q.head {
		return q.capacity-q.tail >= need
	}
	return q.head-q.tail == need || q.head-q.tail >= need+minimumHeaderSize
}

// canInsertBeforeHead returns true if it's possible to insert an entry of size of need before the head of the queue
func (q *BytesQueue) canInsertBeforeHead(need int) bool {
	if q.full {
		return false
	}
	if q.tail >= q.head {
		return q.head-leftMarginIndex == need || q.head-leftMarginIndex >= need+minimumHeaderSize
	}
	return q.head-q.tail == need || q.head-q.tail >= need+minimumHeaderSize
}
