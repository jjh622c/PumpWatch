package buffer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"PumpWatch/internal/config"
	"PumpWatch/internal/logging"
	"PumpWatch/internal/models"
)

// BufferManager manages the extended buffer system and integrates with existing code
type BufferManager struct {
	extendedBuffer *ExtendedBuffer
	logger         *logging.Logger
	config         *config.Config

	// Integration settings
	isEnabled           bool
	fallbackToMemory    bool
	bufferDuration      time.Duration
	compressionEnabled  bool

	// Statistics
	stats            ManagerStats
	mu               sync.RWMutex
	ctx              context.Context
	cancel           context.CancelFunc

	// Legacy compatibility mode
	legacyMode       bool
	legacyBuffers    map[string][]models.TradeEvent // fallback storage
}

// ManagerStats provides statistics about the buffer manager
type ManagerStats struct {
	TotalTradesStored   int64
	TotalTradesRetrieved int64
	BufferHits          int64 // Hot buffer hits
	CompressionHits     int64 // Cold buffer hits
	MemoryUsageBytes    int64
	CompressionRatio    float64
	AverageLatency      time.Duration

	// Error tracking
	StorageErrors       int64
	RetrievalErrors     int64
	CompressionErrors   int64
}

// NewBufferManager creates a new buffer manager with configuration
func NewBufferManager(cfg *config.Config) *BufferManager {
	ctx, cancel := context.WithCancel(context.Background())

	logger := logging.GetGlobalLogger()

	bufferDuration := 10 * time.Minute // Default 10-minute buffer
	if cfg.System.ExtendedBufferDuration != "" {
		if duration, err := time.ParseDuration(cfg.System.ExtendedBufferDuration); err == nil {
			bufferDuration = duration
		}
	}

	manager := &BufferManager{
		logger:             logger,
		config:             cfg,
		bufferDuration:     bufferDuration,
		isEnabled:          true, // Enable by default
		fallbackToMemory:   true, // Enable fallback for safety
		compressionEnabled: true,
		legacyBuffers:      make(map[string][]models.TradeEvent),
		ctx:                ctx,
		cancel:             cancel,
	}

	// Initialize extended buffer if enabled
	if manager.isEnabled {
		manager.extendedBuffer = NewExtendedBuffer(bufferDuration)
		logger.Info("🔧 BufferManager: Extended buffer initialized with %v duration", bufferDuration)
	} else {
		logger.Info("🔧 BufferManager: Running in legacy mode")
		manager.legacyMode = true
	}

	// Start background maintenance
	go manager.maintenanceLoop()

	return manager
}

// StoreTradeEvent stores a trade event in the extended buffer
func (bm *BufferManager) StoreTradeEvent(exchange string, trade models.TradeEvent) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	startTime := time.Now()
	defer func() {
		bm.stats.AverageLatency = time.Since(startTime)
	}()

	// Try extended buffer first
	if bm.isEnabled && bm.extendedBuffer != nil {
		err := bm.extendedBuffer.StoreTradeEvent(exchange, trade)
		if err == nil {
			bm.stats.TotalTradesStored++
			return nil
		}

		// Log error and potentially fall back
		bm.logger.Error("🔧 BufferManager: Extended buffer storage failed: %v", err)
		bm.stats.StorageErrors++

		if !bm.fallbackToMemory {
			return fmt.Errorf("extended buffer storage failed: %w", err)
		}
	}

	// Fallback to legacy memory storage
	return bm.storeInLegacyBuffer(exchange, trade)
}

// GetTradeEvents retrieves trade events within the specified time range
func (bm *BufferManager) GetTradeEvents(exchange string, startTime, endTime time.Time) ([]models.TradeEvent, error) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	startRetrieveTime := time.Now()
	defer func() {
		bm.stats.AverageLatency = time.Since(startRetrieveTime)
	}()

	// Try extended buffer first
	if bm.isEnabled && bm.extendedBuffer != nil {
		trades, err := bm.extendedBuffer.GetTradeEvents(exchange, startTime, endTime)
		if err == nil {
			bm.stats.TotalTradesRetrieved += int64(len(trades))

			// Track buffer hit type
			if bm.isHotBufferHit(startTime) {
				bm.stats.BufferHits++
			} else {
				bm.stats.CompressionHits++
			}

			return trades, nil
		}

		// Log error and potentially fall back
		bm.logger.Warn("🔧 BufferManager: Extended buffer retrieval failed: %v", err)
		bm.stats.RetrievalErrors++

		if !bm.fallbackToMemory {
			return nil, fmt.Errorf("extended buffer retrieval failed: %w", err)
		}
	}

	// Fallback to legacy memory retrieval
	return bm.getFromLegacyBuffer(exchange, startTime, endTime)
}

// ToCollectionEvent converts buffer data to the legacy CollectionEvent format
func (bm *BufferManager) ToCollectionEvent(symbol string, triggerTime time.Time) (*models.CollectionEvent, error) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	// Try extended buffer first
	if bm.isEnabled && bm.extendedBuffer != nil {
		collectionEvent, err := bm.extendedBuffer.ToCollectionEvent(symbol, triggerTime)
		if err == nil {
			return collectionEvent, nil
		}

		bm.logger.Warn("🔧 BufferManager: Extended buffer collection conversion failed: %v", err)

		if !bm.fallbackToMemory {
			return nil, fmt.Errorf("extended buffer collection conversion failed: %w", err)
		}
	}

	// Fallback to legacy collection event creation
	return bm.createLegacyCollectionEvent(symbol, triggerTime)
}

// EnableExtendedBuffer enables or disables the extended buffer
func (bm *BufferManager) EnableExtendedBuffer(enable bool) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if enable == bm.isEnabled {
		return
	}

	bm.isEnabled = enable
	bm.legacyMode = !enable

	if enable {
		if bm.extendedBuffer == nil {
			bm.extendedBuffer = NewExtendedBuffer(bm.bufferDuration)
			bm.logger.Info("🔧 BufferManager: Extended buffer enabled")
		}
	} else {
		if bm.extendedBuffer != nil {
			bm.extendedBuffer.Close()
			bm.extendedBuffer = nil
			bm.logger.Info("🔧 BufferManager: Extended buffer disabled")
		}
	}
}

// GetStats returns current buffer manager statistics
func (bm *BufferManager) GetStats() ManagerStats {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	stats := bm.stats

	if bm.isEnabled && bm.extendedBuffer != nil {
		extendedStats := bm.extendedBuffer.GetStats()
		stats.MemoryUsageBytes = extendedStats.MemoryUsage
		stats.CompressionRatio = extendedStats.CompressionRate
	} else {
		// Calculate legacy buffer memory usage
		totalTrades := 0
		for _, trades := range bm.legacyBuffers {
			totalTrades += len(trades)
		}
		stats.MemoryUsageBytes = int64(totalTrades * 200) // Approximate 200 bytes per trade
	}

	return stats
}

// Legacy buffer operations
func (bm *BufferManager) storeInLegacyBuffer(exchange string, trade models.TradeEvent) error {
	if _, exists := bm.legacyBuffers[exchange]; !exists {
		bm.legacyBuffers[exchange] = make([]models.TradeEvent, 0)
	}

	// Simple append with size limit
	maxLegacySize := 100000 // Limit legacy buffer to prevent memory issues
	if len(bm.legacyBuffers[exchange]) >= maxLegacySize {
		// Remove oldest 10% when limit is reached
		removeCount := maxLegacySize / 10
		bm.legacyBuffers[exchange] = bm.legacyBuffers[exchange][removeCount:]
	}

	bm.legacyBuffers[exchange] = append(bm.legacyBuffers[exchange], trade)
	bm.stats.TotalTradesStored++

	return nil
}

func (bm *BufferManager) getFromLegacyBuffer(exchange string, startTime, endTime time.Time) ([]models.TradeEvent, error) {
	trades, exists := bm.legacyBuffers[exchange]
	if !exists {
		return []models.TradeEvent{}, nil
	}

	// 🔧 BUG FIX: 나노초를 밀리초로 변환 (TradeEvent.Timestamp는 밀리초)
	startTimestamp := startTime.UnixNano() / 1e6
	endTimestamp := endTime.UnixNano() / 1e6

	var result []models.TradeEvent
	for _, trade := range trades {
		// 🔧 BUG FIX: 밀리초 단위로 비교 (기존: 나노초 비교로 인한 데이터 누락)
		if trade.Timestamp >= startTimestamp && trade.Timestamp <= endTimestamp {
			result = append(result, trade)
		}
	}

	bm.stats.TotalTradesRetrieved += int64(len(result))

	return result, nil
}

func (bm *BufferManager) createLegacyCollectionEvent(symbol string, triggerTime time.Time) (*models.CollectionEvent, error) {
	startTime := triggerTime.Add(-20 * time.Second)
	endTime := triggerTime.Add(20 * time.Second)

	event := &models.CollectionEvent{
		Symbol:      symbol,
		TriggerTime: triggerTime,
		StartTime:   startTime,
		EndTime:     endTime,
	}

	// Collect data from legacy buffers
	exchanges := []string{"binance", "bybit", "okx", "kucoin", "gate", "phemex"}
	for _, exchange := range exchanges {
		spotTrades, _ := bm.getFromLegacyBuffer(exchange+"_spot", startTime, endTime)
		futuresTrades, _ := bm.getFromLegacyBuffer(exchange+"_futures", startTime, endTime)

		switch exchange {
		case "binance":
			event.BinanceSpot = spotTrades
			event.BinanceFutures = futuresTrades
		case "bybit":
			event.BybitSpot = spotTrades
			event.BybitFutures = futuresTrades
		case "okx":
			event.OKXSpot = spotTrades
			event.OKXFutures = futuresTrades
		case "kucoin":
			event.KuCoinSpot = spotTrades
			event.KuCoinFutures = futuresTrades
		case "gate":
			event.GateSpot = spotTrades
			event.GateFutures = futuresTrades
		case "phemex":
			event.PhemexSpot = spotTrades
			event.PhemexFutures = futuresTrades
		}
	}

	return event, nil
}

// Helper methods
func (bm *BufferManager) isHotBufferHit(requestTime time.Time) bool {
	hotBufferThreshold := 2 * time.Minute // Hot buffer duration
	return time.Since(requestTime) <= hotBufferThreshold
}

func (bm *BufferManager) maintenanceLoop() {
	ticker := time.NewTicker(60 * time.Second) // Maintenance every minute
	defer ticker.Stop()

	for {
		select {
		case <-bm.ctx.Done():
			return
		case <-ticker.C:
			bm.performMaintenance()
		}
	}
}

func (bm *BufferManager) performMaintenance() {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	// Clean up old legacy buffer data
	if bm.legacyMode {
		cutoffTime := time.Now().Add(-bm.bufferDuration)
		// 🔧 BUG FIX: 나노초를 밀리초로 변환 (TradeEvent.Timestamp는 밀리초)
		cutoffTimestamp := cutoffTime.UnixNano() / 1e6

		for exchange, trades := range bm.legacyBuffers {
			var filteredTrades []models.TradeEvent
			for _, trade := range trades {
				// 🔧 BUG FIX: 밀리초 단위로 비교
				if trade.Timestamp >= cutoffTimestamp {
					filteredTrades = append(filteredTrades, trade)
				}
			}
			bm.legacyBuffers[exchange] = filteredTrades
		}
	}

	// Update statistics
	if bm.isEnabled && bm.extendedBuffer != nil {
		extendedStats := bm.extendedBuffer.GetStats()
		bm.stats.MemoryUsageBytes = extendedStats.MemoryUsage
		bm.stats.CompressionRatio = extendedStats.CompressionRate
	}

	// Log periodic statistics
	if bm.stats.TotalTradesStored > 0 {
		errorRate := float64(bm.stats.StorageErrors+bm.stats.RetrievalErrors) / float64(bm.stats.TotalTradesStored) * 100
		bm.logger.Info("🔧 BufferManager Stats: %.2fMB memory, %.1f%% compression, %.2f%% error rate",
			float64(bm.stats.MemoryUsageBytes)/1024/1024,
			bm.stats.CompressionRatio*100,
			errorRate)
	}
}

// Close gracefully shuts down the buffer manager
func (bm *BufferManager) Close() error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	bm.cancel()

	if bm.extendedBuffer != nil {
		err := bm.extendedBuffer.Close()
		if err != nil {
			bm.logger.Error("🔧 BufferManager: Error closing extended buffer: %v", err)
		}
	}

	// Clear legacy buffers
	bm.legacyBuffers = make(map[string][]models.TradeEvent)

	bm.logger.Info("🔧 BufferManager: Shutdown complete")

	return nil
}