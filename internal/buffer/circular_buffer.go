package buffer

import (
	"fmt"
	"sync"
	"time"

	"PumpWatch/internal/models"
)

// CircularBuffer는 최근 2분의 고속 접근용 링 버퍼
type CircularBuffer struct {
	data     []models.TradeEvent // 실제 데이터 저장
	capacity int                 // 버퍼 크기
	writePos int                 // 쓰기 위치
	readPos  int                 // 읽기 위치
	full     bool                // 버퍼 가득참 여부
	size     int                 // 현재 저장된 데이터 수

	// 시간 인덱스 (빠른 시간 기반 검색)
	timeIndex map[int64]int // timestamp → position

	mu     sync.RWMutex
	closed bool
}

// NewCircularBuffer creates a new circular buffer with the specified capacity
func NewCircularBuffer(capacity int) *CircularBuffer {
	return &CircularBuffer{
		data:      make([]models.TradeEvent, capacity),
		capacity:  capacity,
		timeIndex: make(map[int64]int),
	}
}

// Store adds a trade event to the circular buffer
func (cb *CircularBuffer) Store(trade models.TradeEvent) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.closed {
		return fmt.Errorf("circular buffer is closed")
	}

	// Store the trade event
	cb.data[cb.writePos] = trade

	// Update time index for fast time-based lookups
	cb.timeIndex[trade.Timestamp] = cb.writePos

	// Update position
	cb.writePos = (cb.writePos + 1) % cb.capacity

	// Handle buffer full condition
	if cb.writePos == cb.readPos {
		if cb.full {
			// Remove old entry from time index
			oldTrade := cb.data[cb.readPos]
			delete(cb.timeIndex, oldTrade.Timestamp)
			cb.readPos = (cb.readPos + 1) % cb.capacity
		} else {
			cb.full = true
		}
	}

	// Update size
	if !cb.full {
		cb.size++
	}

	return nil
}

// GetRange retrieves trade events within the specified time range
func (cb *CircularBuffer) GetRange(startTime, endTime time.Time) ([]models.TradeEvent, error) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.closed {
		return nil, fmt.Errorf("circular buffer is closed")
	}

	var result []models.TradeEvent
	// 🔧 BUG FIX: 나노초를 밀리초로 변환 (TradeEvent.Timestamp는 밀리초)
	startTimestamp := startTime.UnixNano() / 1e6
	endTimestamp := endTime.UnixNano() / 1e6

	// Iterate through the circular buffer
	pos := cb.readPos
	for i := 0; i < cb.size; i++ {
		trade := cb.data[pos]
		// 🔧 BUG FIX: 밀리초 단위로 비교 (기존: 나노초 비교로 인한 데이터 누락)
		if trade.Timestamp >= startTimestamp && trade.Timestamp <= endTimestamp {
			result = append(result, trade)
		}
		pos = (pos + 1) % cb.capacity
	}

	return result, nil
}

// ExtractOldTrades extracts and removes trades older than the specified cutoff time
func (cb *CircularBuffer) ExtractOldTrades(cutoffTime time.Time) []models.TradeEvent {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.closed || cb.size == 0 {
		return nil
	}

	var oldTrades []models.TradeEvent
	// 🔧 BUG FIX: 나노초를 밀리초로 변환 (TradeEvent.Timestamp는 밀리초)
	cutoffTimestamp := cutoffTime.UnixNano() / 1e6

	// Find and extract old trades
	originalSize := cb.size
	newReadPos := cb.readPos

	for i := 0; i < originalSize; i++ {
		pos := (cb.readPos + i) % cb.capacity
		trade := cb.data[pos]

		// 🔧 BUG FIX: 밀리초 단위로 비교
		if trade.Timestamp < cutoffTimestamp {
			oldTrades = append(oldTrades, trade)
			// Remove from time index
			delete(cb.timeIndex, trade.Timestamp)
			// Clear the data slot
			cb.data[pos] = models.TradeEvent{}
			newReadPos = (pos + 1) % cb.capacity
			cb.size--
		} else {
			break
		}
	}

	// Update read position
	cb.readPos = newReadPos

	// Update full flag
	if cb.size < cb.capacity {
		cb.full = false
	}

	return oldTrades
}

// Size returns the current number of elements in the buffer
func (cb *CircularBuffer) Size() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.size
}

// Capacity returns the maximum capacity of the buffer
func (cb *CircularBuffer) Capacity() int {
	return cb.capacity
}

// IsFull returns whether the buffer is full
func (cb *CircularBuffer) IsFull() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.full
}

// IsEmpty returns whether the buffer is empty
func (cb *CircularBuffer) IsEmpty() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.size == 0
}

// MemoryUsage returns the approximate memory usage in bytes
func (cb *CircularBuffer) MemoryUsage() int64 {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	// Approximate memory usage calculation
	// Each TradeEvent is approximately 200 bytes
	const tradeEventSize = 200

	baseMemory := int64(cb.capacity * tradeEventSize) // Main data array
	indexMemory := int64(len(cb.timeIndex) * 16)      // Time index (8 bytes key + 8 bytes value)

	return baseMemory + indexMemory
}

// GetLatestTrade returns the most recently added trade event
func (cb *CircularBuffer) GetLatestTrade() (models.TradeEvent, error) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.closed {
		return models.TradeEvent{}, fmt.Errorf("circular buffer is closed")
	}

	if cb.size == 0 {
		return models.TradeEvent{}, fmt.Errorf("buffer is empty")
	}

	// Get the last written position
	lastPos := cb.writePos - 1
	if lastPos < 0 {
		lastPos = cb.capacity - 1
	}

	return cb.data[lastPos], nil
}

// GetOldestTrade returns the oldest trade event in the buffer
func (cb *CircularBuffer) GetOldestTrade() (models.TradeEvent, error) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.closed {
		return models.TradeEvent{}, fmt.Errorf("circular buffer is closed")
	}

	if cb.size == 0 {
		return models.TradeEvent{}, fmt.Errorf("buffer is empty")
	}

	return cb.data[cb.readPos], nil
}

// FindByTimestamp finds a trade event by its timestamp (fast lookup using index)
func (cb *CircularBuffer) FindByTimestamp(timestamp int64) (models.TradeEvent, bool) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.closed {
		return models.TradeEvent{}, false
	}

	pos, exists := cb.timeIndex[timestamp]
	if !exists {
		return models.TradeEvent{}, false
	}

	return cb.data[pos], true
}

// GetTimeRange returns the time range of data currently in the buffer
func (cb *CircularBuffer) GetTimeRange() (time.Time, time.Time, error) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.closed {
		return time.Time{}, time.Time{}, fmt.Errorf("circular buffer is closed")
	}

	if cb.size == 0 {
		return time.Time{}, time.Time{}, fmt.Errorf("buffer is empty")
	}

	oldestTrade := cb.data[cb.readPos]

	lastPos := cb.writePos - 1
	if lastPos < 0 {
		lastPos = cb.capacity - 1
	}
	newestTrade := cb.data[lastPos]

	// 🔧 BUG FIX: 밀리초를 나노초로 변환 (trade.Timestamp는 밀리초, time.Unix는 나노초 기대)
	oldestTime := time.Unix(0, oldestTrade.Timestamp*1e6)
	newestTime := time.Unix(0, newestTrade.Timestamp*1e6)

	return oldestTime, newestTime, nil
}

// Clear removes all data from the buffer
func (cb *CircularBuffer) Clear() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.writePos = 0
	cb.readPos = 0
	cb.size = 0
	cb.full = false
	cb.timeIndex = make(map[int64]int)

	// Clear all data slots
	for i := range cb.data {
		cb.data[i] = models.TradeEvent{}
	}
}

// Close gracefully shuts down the circular buffer
func (cb *CircularBuffer) Close() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.closed = true
	cb.Clear()
}

// GetStats returns statistics about the circular buffer
func (cb *CircularBuffer) GetStats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	stats := map[string]interface{}{
		"capacity":     cb.capacity,
		"size":         cb.size,
		"full":         cb.full,
		"empty":        cb.size == 0,
		"memory_usage": cb.MemoryUsage(),
		"closed":       cb.closed,
	}

	if cb.size > 0 {
		oldestTime, newestTime, err := cb.GetTimeRange()
		if err == nil {
			stats["oldest_timestamp"] = oldestTime
			stats["newest_timestamp"] = newestTime
			stats["time_span_seconds"] = newestTime.Sub(oldestTime).Seconds()
		}
	}

	return stats
}