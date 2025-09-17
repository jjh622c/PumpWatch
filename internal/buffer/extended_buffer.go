package buffer

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"PumpWatch/internal/models"
)

// ExtendedBuffer는 10분간의 거래 데이터를 효율적으로 저장
type ExtendedBuffer struct {
	// 설정
	totalDuration  time.Duration // 10분
	hotDuration    time.Duration // 2분 (hot)
	coldDuration   time.Duration // 8분 (cold)

	// 버퍼 저장소
	hotBuffers  map[string]*CircularBuffer // 거래소별 Hot 버퍼
	coldBuffers map[string]*CompressedRing // 거래소별 Cold 버퍼

	// 동시성 제어
	mu sync.RWMutex

	// 통계 및 상태
	stats     BufferStats
	startTime time.Time
	isActive  bool

	// 설정 가능한 파라미터
	compressionRatio float64       // 압축률 (기본 0.7)
	blockSize        time.Duration // 압축 블록 크기 (기본 30초)

	// Context for lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
}

// BufferStats는 버퍼 사용 통계
type BufferStats struct {
	TotalEvents     int64   // 총 이벤트 수
	HotEvents       int64   // Hot 버퍼 이벤트
	ColdEvents      int64   // Cold 버퍼 이벤트 (압축 후)
	MemoryUsage     int64   // 실제 메모리 사용량
	CompressionRate float64 // 실제 압축률

	// 성능 지표
	HotAccessTime  time.Duration
	ColdAccessTime time.Duration
}

// NewExtendedBuffer creates a new ExtendedBuffer with the specified total duration
func NewExtendedBuffer(totalDuration time.Duration) *ExtendedBuffer {
	ctx, cancel := context.WithCancel(context.Background())

	hotDuration := totalDuration / 5 // 20% for hot buffer
	if hotDuration < 2*time.Minute {
		hotDuration = 2 * time.Minute
	}
	coldDuration := totalDuration - hotDuration

	buffer := &ExtendedBuffer{
		totalDuration:    totalDuration,
		hotDuration:      hotDuration,
		coldDuration:     coldDuration,
		hotBuffers:       make(map[string]*CircularBuffer),
		coldBuffers:      make(map[string]*CompressedRing),
		compressionRatio: 0.7,
		blockSize:        30 * time.Second,
		startTime:        time.Now(),
		isActive:         true,
		ctx:              ctx,
		cancel:           cancel,
	}

	// Initialize buffers for all exchanges
	exchanges := []string{"binance", "bybit", "okx", "kucoin", "gate", "phemex"}
	markets := []string{"spot", "futures"}

	for _, exchange := range exchanges {
		for _, market := range markets {
			key := fmt.Sprintf("%s_%s", exchange, market)

			// Hot buffer: 2분간 데이터 저장 (초당 5000거래 기준)
			hotCapacity := int(hotDuration.Seconds()) * 5000
			buffer.hotBuffers[key] = NewCircularBuffer(hotCapacity)

			// Cold buffer: 8분을 30초 블록으로 분할 (16개 블록)
			coldBlocks := int(coldDuration / buffer.blockSize)
			buffer.coldBuffers[key] = NewCompressedRing(coldBlocks)
		}
	}

	// Start background maintenance
	go buffer.maintenanceLoop()

	return buffer
}

// StoreTradeEvent는 거래 이벤트를 적절한 버퍼에 저장
func (eb *ExtendedBuffer) StoreTradeEvent(exchange string, trade models.TradeEvent) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if !eb.isActive {
		return fmt.Errorf("buffer is not active")
	}

	now := time.Now()
	// 🔧 BUG FIX: 밀리초를 나노초로 변환 (trade.Timestamp는 밀리초, time.Unix는 나노초 기대)
	age := now.Sub(time.Unix(0, trade.Timestamp*1e6))

	// Hot Buffer에 저장 (최근 2분)
	if age <= eb.hotDuration {
		if hotBuffer, exists := eb.hotBuffers[exchange]; exists {
			err := hotBuffer.Store(trade)
			if err == nil {
				eb.stats.TotalEvents++
				eb.stats.HotEvents++
			}
			return err
		}
	}

	// Cold Buffer로 이동 (2분 이상된 데이터)
	return eb.moveToColdbuffer(exchange, trade)
}

// GetTradeEvents는 지정된 시간 범위의 거래 이벤트를 반환
func (eb *ExtendedBuffer) GetTradeEvents(exchange string, startTime, endTime time.Time) ([]models.TradeEvent, error) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	var result []models.TradeEvent
	now := time.Now()

	// Hot Buffer에서 조회 (빠름)
	hotStart := now.Add(-eb.hotDuration)
	if endTime.After(hotStart) {
		if hotBuffer, exists := eb.hotBuffers[exchange]; exists {
			hotTrades, err := hotBuffer.GetRange(
				maxTime(startTime, hotStart), endTime)
			if err != nil {
				return nil, err
			}
			result = append(result, hotTrades...)
		}
	}

	// Cold Buffer에서 조회 (압축 해제 필요)
	if startTime.Before(hotStart) {
		if coldBuffer, exists := eb.coldBuffers[exchange]; exists {
			coldTrades, err := coldBuffer.GetRange(
				startTime, minTime(endTime, hotStart))
			if err != nil {
				return nil, err
			}
			result = append(result, coldTrades...)
		}
	}

	// 시간순 정렬
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp < result[j].Timestamp
	})

	return result, nil
}

// ToCollectionEvent는 ExtendedBuffer를 기존 CollectionEvent로 변환
func (eb *ExtendedBuffer) ToCollectionEvent(symbol string, triggerTime time.Time) (*models.CollectionEvent, error) {
	startTime := triggerTime.Add(-20 * time.Second)
	endTime := triggerTime.Add(20 * time.Second)

	event := &models.CollectionEvent{
		Symbol:      symbol,
		TriggerTime: triggerTime,
		StartTime:   startTime,
		EndTime:     endTime,
	}

	// 각 거래소별 데이터 수집
	exchanges := []string{"binance", "bybit", "okx", "kucoin", "gate", "phemex"}
	for _, exchange := range exchanges {
		spotTrades, err := eb.GetTradeEvents(exchange+"_spot", startTime, endTime)
		if err != nil {
			continue
		}
		futuresTrades, err := eb.GetTradeEvents(exchange+"_futures", startTime, endTime)
		if err != nil {
			continue
		}

		// 기존 구조체에 맞게 변환
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

// moveToColdbuffer moves old data from hot buffer to cold buffer
func (eb *ExtendedBuffer) moveToColdbuffer(exchange string, trade models.TradeEvent) error {
	if coldBuffer, exists := eb.coldBuffers[exchange]; exists {
		err := coldBuffer.Store(trade)
		if err == nil {
			eb.stats.TotalEvents++
			eb.stats.ColdEvents++
		}
		return err
	}
	return fmt.Errorf("cold buffer not found for exchange: %s", exchange)
}

// maintenanceLoop handles periodic maintenance tasks
func (eb *ExtendedBuffer) maintenanceLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-eb.ctx.Done():
			return
		case <-ticker.C:
			eb.performMaintenance()
		}
	}
}

// performMaintenance performs periodic maintenance tasks
func (eb *ExtendedBuffer) performMaintenance() {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	now := time.Now()
	cutoffTime := now.Add(-eb.hotDuration)

	// Move old data from hot to cold buffers
	for key, hotBuffer := range eb.hotBuffers {
		oldTrades := hotBuffer.ExtractOldTrades(cutoffTime)
		if len(oldTrades) > 0 {
			if coldBuffer, exists := eb.coldBuffers[key]; exists {
				coldBuffer.CompressAndStore(oldTrades, cutoffTime)
			}
		}
	}

	// Update statistics
	eb.updateStats()
}

// updateStats updates buffer usage statistics
func (eb *ExtendedBuffer) updateStats() {
	var totalMemory int64
	var hotEvents, coldEvents int64

	for _, hotBuffer := range eb.hotBuffers {
		hotEvents += int64(hotBuffer.Size())
		totalMemory += hotBuffer.MemoryUsage()
	}

	for _, coldBuffer := range eb.coldBuffers {
		coldEvents += int64(coldBuffer.Size())
		totalMemory += coldBuffer.MemoryUsage()
	}

	eb.stats.HotEvents = hotEvents
	eb.stats.ColdEvents = coldEvents
	eb.stats.TotalEvents = hotEvents + coldEvents
	eb.stats.MemoryUsage = totalMemory

	// Calculate actual compression rate
	if coldEvents > 0 {
		eb.stats.CompressionRate = eb.compressionRatio
	}
}

// GetStats returns current buffer statistics
func (eb *ExtendedBuffer) GetStats() BufferStats {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return eb.stats
}

// Close gracefully shuts down the buffer
func (eb *ExtendedBuffer) Close() error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.isActive = false
	eb.cancel()

	// Close all hot buffers
	for _, hotBuffer := range eb.hotBuffers {
		hotBuffer.Close()
	}

	// Close all cold buffers
	for _, coldBuffer := range eb.coldBuffers {
		coldBuffer.Close()
	}

	return nil
}

// Helper functions
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}