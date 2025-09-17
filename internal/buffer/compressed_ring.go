package buffer

import (
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"PumpWatch/internal/models"
)

// CompressedRing은 8분의 압축 데이터를 저장하는 링 버퍼
type CompressedRing struct {
	blocks   []CompressedBlock // 압축 블록 배열
	capacity int               // 블록 수 (16개 = 8분/30초)
	writePos int               // 현재 쓰기 블록

	// 시간 인덱스
	timeIndex map[int64]int // timestamp → block index

	mu     sync.RWMutex
	closed bool

	// 압축 설정
	blockDuration time.Duration // 블록 지속 시간 (30초)
	compressor    *DeltaCompressor
}

// CompressedBlock은 30초분 데이터의 압축 블록
type CompressedBlock struct {
	// 메타데이터
	StartTime      int64   // 블록 시작 시간
	EndTime        int64   // 블록 종료 시간
	EventCount     int     // 원본 이벤트 수
	CompressedSize int     // 압축 후 크기
	IsEmpty        bool    // 블록이 비어있는지 여부

	// 압축된 데이터
	Data []byte // MessagePack + 델타 압축

	// 빠른 검색용 인덱스
	PriceRange    [2]float64 // [min, max] 가격 범위
	VolumeSum     float64    // 총 거래량
	UniqueSymbols []string   // 거래 심볼들

	mu sync.RWMutex
}

// NewCompressedRing creates a new compressed ring buffer
func NewCompressedRing(capacity int) *CompressedRing {
	blockDuration := 30 * time.Second

	ring := &CompressedRing{
		blocks:        make([]CompressedBlock, capacity),
		capacity:      capacity,
		timeIndex:     make(map[int64]int),
		blockDuration: blockDuration,
		compressor:    NewDeltaCompressor(),
	}

	// Initialize all blocks as empty
	for i := range ring.blocks {
		ring.blocks[i].IsEmpty = true
	}

	return ring
}

// Store adds a single trade event to the appropriate compressed block
func (cr *CompressedRing) Store(trade models.TradeEvent) error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	if cr.closed {
		return fmt.Errorf("compressed ring is closed")
	}

	// Calculate which block this trade belongs to
	blockIndex := cr.getBlockIndex(trade.Timestamp)

	// Get or create the block
	block := &cr.blocks[blockIndex]

	// If this is a new block, initialize it
	if block.IsEmpty {
		block.StartTime = (trade.Timestamp / int64(cr.blockDuration)) * int64(cr.blockDuration)
		block.EndTime = block.StartTime + int64(cr.blockDuration)
		block.IsEmpty = false

		// Update time index
		cr.timeIndex[block.StartTime] = blockIndex
	}

	// For now, store individual trades and compress later
	// This is a simplified approach - in production, we'd batch compress
	trades := []models.TradeEvent{trade}
	return cr.compressAndStoreInBlock(block, trades)
}

// CompressAndStore compresses and stores multiple trade events in appropriate blocks
func (cr *CompressedRing) CompressAndStore(trades []models.TradeEvent, blockStartTime time.Time) error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	if cr.closed {
		return fmt.Errorf("compressed ring is closed")
	}

	if len(trades) == 0 {
		return nil
	}

	// Group trades by block
	blockGroups := make(map[int][]models.TradeEvent)

	for _, trade := range trades {
		blockIndex := cr.getBlockIndex(trade.Timestamp)
		blockGroups[blockIndex] = append(blockGroups[blockIndex], trade)
	}

	// Process each block group
	for blockIndex, blockTrades := range blockGroups {
		block := &cr.blocks[blockIndex]

		// Initialize block if empty
		if block.IsEmpty {
			minTime := blockTrades[0].Timestamp
			block.StartTime = (minTime / int64(cr.blockDuration)) * int64(cr.blockDuration)
			block.EndTime = block.StartTime + int64(cr.blockDuration)
			block.IsEmpty = false

			// Update time index
			cr.timeIndex[block.StartTime] = blockIndex
		}

		// Compress and store
		err := cr.compressAndStoreInBlock(block, blockTrades)
		if err != nil {
			return fmt.Errorf("failed to compress block %d: %w", blockIndex, err)
		}
	}

	return nil
}

// GetRange retrieves trade events within the specified time range
func (cr *CompressedRing) GetRange(startTime, endTime time.Time) ([]models.TradeEvent, error) {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	if cr.closed {
		return nil, fmt.Errorf("compressed ring is closed")
	}

	var result []models.TradeEvent
	// 🔧 BUG FIX: 나노초를 밀리초로 변환 (TradeEvent.Timestamp는 밀리초)
	startTimestamp := startTime.UnixNano() / 1e6
	endTimestamp := endTime.UnixNano() / 1e6

	// Find relevant blocks
	for i, block := range cr.blocks {
		if block.IsEmpty {
			continue
		}

		// 🔧 BUG FIX: 블록 시간도 밀리초 단위로 비교 (블록 시간은 trade.Timestamp 기반으로 생성됨)
		if block.EndTime >= startTimestamp && block.StartTime <= endTimestamp {
			blockTrades, err := cr.decompressBlock(&cr.blocks[i])
			if err != nil {
				continue // Skip blocks that can't be decompressed
			}

			// Filter trades within the exact time range
			for _, trade := range blockTrades {
				// 🔧 BUG FIX: 밀리초 단위로 비교 (기존: 나노초 비교로 인한 데이터 누락)
				if trade.Timestamp >= startTimestamp && trade.Timestamp <= endTimestamp {
					result = append(result, trade)
				}
			}
		}
	}

	// Sort by timestamp
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp < result[j].Timestamp
	})

	return result, nil
}

// Size returns the total number of compressed blocks
func (cr *CompressedRing) Size() int {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	count := 0
	for _, block := range cr.blocks {
		if !block.IsEmpty {
			count++
		}
	}
	return count
}

// MemoryUsage returns the approximate memory usage in bytes
func (cr *CompressedRing) MemoryUsage() int64 {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	var totalMemory int64

	for _, block := range cr.blocks {
		if !block.IsEmpty {
			totalMemory += int64(block.CompressedSize)
			totalMemory += int64(len(block.UniqueSymbols) * 20) // Approximate symbol storage
		}
	}

	// Add index memory
	totalMemory += int64(len(cr.timeIndex) * 16)

	return totalMemory
}

// Close gracefully shuts down the compressed ring
func (cr *CompressedRing) Close() {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	cr.closed = true

	// Clear all blocks
	for i := range cr.blocks {
		cr.blocks[i] = CompressedBlock{IsEmpty: true}
	}

	cr.timeIndex = make(map[int64]int)
}

// getBlockIndex calculates which block index a timestamp should go to
func (cr *CompressedRing) getBlockIndex(timestamp int64) int {
	blockTime := timestamp / int64(cr.blockDuration)
	return int(blockTime) % cr.capacity
}

// compressAndStoreInBlock compresses trades and stores them in the specified block
func (cr *CompressedRing) compressAndStoreInBlock(block *CompressedBlock, trades []models.TradeEvent) error {
	if len(trades) == 0 {
		return nil
	}

	block.mu.Lock()
	defer block.mu.Unlock()

	// If block already has data, decompress first, merge, then recompress
	var allTrades []models.TradeEvent
	if block.CompressedSize > 0 {
		existingTrades, err := cr.decompressBlockUnsafe(block)
		if err != nil {
			return fmt.Errorf("failed to decompress existing data: %w", err)
		}
		allTrades = append(allTrades, existingTrades...)
	}
	allTrades = append(allTrades, trades...)

	// Sort by timestamp
	sort.Slice(allTrades, func(i, j int) bool {
		return allTrades[i].Timestamp < allTrades[j].Timestamp
	})

	// Compress the combined data
	compressedData, err := cr.compressor.Compress(allTrades)
	if err != nil {
		return fmt.Errorf("compression failed: %w", err)
	}

	// Update block metadata
	block.Data = compressedData
	block.CompressedSize = len(compressedData)
	block.EventCount = len(allTrades)

	// Calculate price range and volume sum
	if len(allTrades) > 0 {
		// Convert first trade price to float64
		firstPrice, err := strconv.ParseFloat(allTrades[0].Price, 64)
		if err != nil {
			return fmt.Errorf("failed to parse price: %w", err)
		}

		minPrice := firstPrice
		maxPrice := firstPrice
		var volumeSum float64
		uniqueSymbols := make(map[string]bool)

		for _, trade := range allTrades {
			// Convert price and quantity strings to float64
			price, err := strconv.ParseFloat(trade.Price, 64)
			if err != nil {
				continue // Skip invalid prices
			}

			quantity, err := strconv.ParseFloat(trade.Quantity, 64)
			if err != nil {
				continue // Skip invalid quantities
			}

			if price < minPrice {
				minPrice = price
			}
			if price > maxPrice {
				maxPrice = price
			}
			volumeSum += quantity
			uniqueSymbols[trade.Symbol] = true
		}

		block.PriceRange = [2]float64{minPrice, maxPrice}
		block.VolumeSum = volumeSum

		// Convert unique symbols map to slice
		block.UniqueSymbols = make([]string, 0, len(uniqueSymbols))
		for symbol := range uniqueSymbols {
			block.UniqueSymbols = append(block.UniqueSymbols, symbol)
		}
	}

	return nil
}

// decompressBlock decompresses a block and returns the trade events
func (cr *CompressedRing) decompressBlock(block *CompressedBlock) ([]models.TradeEvent, error) {
	block.mu.RLock()
	defer block.mu.RUnlock()

	return cr.decompressBlockUnsafe(block)
}

// decompressBlockUnsafe decompresses a block without locking (internal use)
func (cr *CompressedRing) decompressBlockUnsafe(block *CompressedBlock) ([]models.TradeEvent, error) {
	if block.IsEmpty || block.CompressedSize == 0 {
		return nil, nil
	}

	return cr.compressor.Decompress(block.Data)
}

// GetStats returns statistics about the compressed ring
func (cr *CompressedRing) GetStats() map[string]interface{} {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	activeBlocks := 0
	totalEvents := 0
	totalCompressedSize := 0
	totalOriginalSize := 0

	for _, block := range cr.blocks {
		if !block.IsEmpty {
			activeBlocks++
			totalEvents += block.EventCount
			totalCompressedSize += block.CompressedSize
			// Approximate original size (200 bytes per event)
			totalOriginalSize += block.EventCount * 200
		}
	}

	compressionRatio := 0.0
	if totalOriginalSize > 0 {
		compressionRatio = float64(totalCompressedSize) / float64(totalOriginalSize)
	}

	return map[string]interface{}{
		"capacity":           cr.capacity,
		"active_blocks":      activeBlocks,
		"total_events":       totalEvents,
		"compressed_size":    totalCompressedSize,
		"original_size":      totalOriginalSize,
		"compression_ratio":  compressionRatio,
		"memory_usage":       cr.MemoryUsage(),
		"block_duration_sec": cr.blockDuration.Seconds(),
		"closed":             cr.closed,
	}
}

// GetBlockInfo returns information about a specific block
func (cr *CompressedRing) GetBlockInfo(blockIndex int) map[string]interface{} {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	if blockIndex < 0 || blockIndex >= cr.capacity {
		return map[string]interface{}{"error": "invalid block index"}
	}

	block := cr.blocks[blockIndex]

	info := map[string]interface{}{
		"block_index":     blockIndex,
		"is_empty":        block.IsEmpty,
		"start_time":      time.Unix(0, block.StartTime),
		"end_time":        time.Unix(0, block.EndTime),
		"event_count":     block.EventCount,
		"compressed_size": block.CompressedSize,
		"price_range":     block.PriceRange,
		"volume_sum":      block.VolumeSum,
		"unique_symbols":  block.UniqueSymbols,
	}

	return info
}