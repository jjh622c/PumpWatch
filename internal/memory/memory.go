package memory

import (
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"
)

// OrderbookSnapshot 오더북 스냅샷
type OrderbookSnapshot struct {
	Exchange  string     `json:"exchange"`
	Symbol    string     `json:"symbol"`
	Timestamp time.Time  `json:"timestamp"`
	Bids      [][]string `json:"bids"`
	Asks      [][]string `json:"asks"`
}

// TradeData 체결 데이터
type TradeData struct {
	Exchange  string    `json:"exchange"`
	Symbol    string    `json:"symbol"`
	Timestamp time.Time `json:"timestamp"`
	Price     string    `json:"price"`
	Quantity  string    `json:"quantity"`
	Side      string    `json:"side"` // "BUY" or "SELL"
	TradeID   string    `json:"trade_id"`
}

// PumpSignal 펌핑 시그널
type PumpSignal struct {
	Symbol         string    `json:"symbol"`
	Timestamp      time.Time `json:"timestamp"`
	CompositeScore float64   `json:"composite_score"`
	Action         string    `json:"action"`
	Confidence     float64   `json:"confidence"`
	Volume         float64   `json:"volume"`
	PriceChange    float64   `json:"price_change"`
}

// AdvancedPumpSignal 고급 펌핑 시그널
type AdvancedPumpSignal struct {
	PumpSignal
	OrderbookData *OrderbookSnapshot `json:"orderbook_data"`
	TradeHistory  []TradeData        `json:"trade_history"`
	Indicators    map[string]float64 `json:"indicators"`
}

// CompressedTrade 압축된 체결 데이터 (같은 가격의 거래 통합)
type CompressedTrade struct {
	Symbol      string    `json:"symbol"`
	Price       string    `json:"price"`
	TotalVolume float64   `json:"total_volume"`
	BuyVolume   float64   `json:"buy_volume"`
	SellVolume  float64   `json:"sell_volume"`
	TradeCount  int       `json:"trade_count"`
	FirstTime   time.Time `json:"first_time"`
	LastTime    time.Time `json:"last_time"`
	Exchange    string    `json:"exchange"`
}

// SymbolMemory 심볼별 독립 메모리 관리자 (압축 기능 추가)
type SymbolMemory struct {
	symbol              string
	mu                  sync.RWMutex
	orderbooks          []*OrderbookSnapshot
	trades              []*TradeData
	compressedTrades    map[string]*CompressedTrade // price -> compressed trade
	maxOrderbooks       int
	maxTrades           int
	lastCleanup         time.Time
	lastCompression     time.Time
	compressionInterval time.Duration
}

// NewSymbolMemory 새로운 심볼 메모리 생성 (압축 기능 포함)
func NewSymbolMemory(symbol string, maxOrderbooks, maxTrades int, compressionIntervalSeconds int) *SymbolMemory {
	return &SymbolMemory{
		symbol:              symbol,
		orderbooks:          make([]*OrderbookSnapshot, 0, maxOrderbooks),
		trades:              make([]*TradeData, 0, maxTrades),
		compressedTrades:    make(map[string]*CompressedTrade),
		maxOrderbooks:       maxOrderbooks,
		maxTrades:           maxTrades,
		lastCleanup:         time.Now(),
		lastCompression:     time.Now(),
		compressionInterval: time.Duration(compressionIntervalSeconds) * time.Second, // 🔧 config에서 읽어옴
	}
}

// AddOrderbook 오더북 추가 (심볼별 독립 락)
func (sm *SymbolMemory) AddOrderbook(snapshot *OrderbookSnapshot) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 기존 데이터에 추가
	sm.orderbooks = append(sm.orderbooks, snapshot)

	// 최대 개수 제한 (rolling buffer)
	if len(sm.orderbooks) > sm.maxOrderbooks {
		sm.orderbooks = sm.orderbooks[1:] // 가장 오래된 데이터 제거
	}
}

// AddTrade 체결 데이터 추가 (압축 고려)
func (sm *SymbolMemory) AddTrade(trade *TradeData) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 기존 데이터에 추가
	sm.trades = append(sm.trades, trade)

	// 최대 개수 제한 (rolling buffer)
	if len(sm.trades) > sm.maxTrades {
		sm.trades = sm.trades[1:] // 가장 오래된 데이터 제거
	}

	// 일정 시간마다 자동 압축
	if time.Since(sm.lastCompression) > sm.compressionInterval {
		sm.compressOldTrades()
		sm.lastCompression = time.Now()
	}
}

// compressOldTrades 오래된 거래 압축 (internal, 락 이미 보유)
func (sm *SymbolMemory) compressOldTrades() {
	if len(sm.trades) < 10 {
		return // 데이터가 적으면 압축하지 않음
	}

	// 30초 이전 거래만 압축
	cutoffTime := time.Now().Add(-30 * time.Second)

	var toCompress []*TradeData
	var toKeep []*TradeData

	for _, trade := range sm.trades {
		if trade.Timestamp.Before(cutoffTime) {
			toCompress = append(toCompress, trade)
		} else {
			toKeep = append(toKeep, trade)
		}
	}

	if len(toCompress) == 0 {
		return
	}

	// 가격별로 그룹화하여 압축
	priceGroups := make(map[string][]*TradeData)
	for _, trade := range toCompress {
		priceGroups[trade.Price] = append(priceGroups[trade.Price], trade)
	}

	// 압축된 데이터 생성
	for price, trades := range priceGroups {
		if existing, exists := sm.compressedTrades[price]; exists {
			// 기존 압축 데이터 업데이트
			sm.updateCompressedTrade(existing, trades)
		} else {
			// 새 압축 데이터 생성
			sm.compressedTrades[price] = sm.createCompressedTrade(trades)
		}
	}

	// 압축된 거래는 원본에서 제거
	sm.trades = toKeep

	// 압축 로그 (많이 압축된 경우만)
	if len(toCompress) > 20 {
		log.Printf("📦 거래 압축: %s, %d개 → %d개 그룹", sm.symbol, len(toCompress), len(priceGroups))
	}
}

// createCompressedTrade 압축된 거래 생성
func (sm *SymbolMemory) createCompressedTrade(trades []*TradeData) *CompressedTrade {
	if len(trades) == 0 {
		return nil
	}

	compressed := &CompressedTrade{
		Symbol:      sm.symbol,
		Price:       trades[0].Price,
		FirstTime:   trades[0].Timestamp,
		LastTime:    trades[0].Timestamp,
		Exchange:    trades[0].Exchange,
		TradeCount:  0,
		TotalVolume: 0,
		BuyVolume:   0,
		SellVolume:  0,
	}

	// 모든 거래 통합
	for _, trade := range trades {
		quantity := sm.parseFloat(trade.Quantity)
		compressed.TotalVolume += quantity
		compressed.TradeCount++

		if trade.Side == "BUY" {
			compressed.BuyVolume += quantity
		} else {
			compressed.SellVolume += quantity
		}

		if trade.Timestamp.Before(compressed.FirstTime) {
			compressed.FirstTime = trade.Timestamp
		}
		if trade.Timestamp.After(compressed.LastTime) {
			compressed.LastTime = trade.Timestamp
		}
	}

	return compressed
}

// updateCompressedTrade 기존 압축 데이터 업데이트
func (sm *SymbolMemory) updateCompressedTrade(existing *CompressedTrade, newTrades []*TradeData) {
	for _, trade := range newTrades {
		quantity := sm.parseFloat(trade.Quantity)
		existing.TotalVolume += quantity
		existing.TradeCount++

		if trade.Side == "BUY" {
			existing.BuyVolume += quantity
		} else {
			existing.SellVolume += quantity
		}

		if trade.Timestamp.Before(existing.FirstTime) {
			existing.FirstTime = trade.Timestamp
		}
		if trade.Timestamp.After(existing.LastTime) {
			existing.LastTime = trade.Timestamp
		}
	}
}

// parseFloat 문자열을 float64로 변환 (에러 무시)
func (sm *SymbolMemory) parseFloat(s string) float64 {
	// 단순한 파싱 (성능 최적화)
	var result float64
	fmt.Sscanf(s, "%f", &result)
	return result
}

// Cleanup 심볼별 데이터 정리 (🔥 압축 데이터 정리 추가)
func (sm *SymbolMemory) Cleanup(orderbookCutoff, tradeCutoff time.Time) (int, int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	cleanedOrderbooks := 0
	cleanedTrades := 0

	// 오더북 정리
	var validOrderbooks []*OrderbookSnapshot
	for _, ob := range sm.orderbooks {
		if ob.Timestamp.After(orderbookCutoff) {
			validOrderbooks = append(validOrderbooks, ob)
		} else {
			cleanedOrderbooks++
		}
	}
	sm.orderbooks = validOrderbooks

	// 체결 정리
	var validTrades []*TradeData
	for _, trade := range sm.trades {
		if trade.Timestamp.After(tradeCutoff) {
			validTrades = append(validTrades, trade)
		} else {
			cleanedTrades++
		}
	}
	sm.trades = validTrades

	// 🔥 치명적 누수 해결: 압축 데이터 정리 추가
	cleanedCompressed := 0
	compressedCutoff := tradeCutoff.Add(-5 * time.Minute) // 압축 데이터는 더 오래 보존

	for price, compressedTrade := range sm.compressedTrades {
		// 너무 오래된 압축 데이터 제거
		if compressedTrade.LastTime.Before(compressedCutoff) {
			delete(sm.compressedTrades, price)
			cleanedCompressed++
		}
	}

	// 🔥 압축 데이터 개수 제한 (가격별 최대 1000개)
	if len(sm.compressedTrades) > 1000 {
		// 시간순으로 정렬하여 가장 오래된 것들 제거
		type timePrice struct {
			price string
			time  time.Time
		}

		var sorted []timePrice
		for price, compressed := range sm.compressedTrades {
			sorted = append(sorted, timePrice{price: price, time: compressed.LastTime})
		}

		// 간단한 시간순 정렬 (최신 1000개만 유지)
		for i := 0; i < len(sorted)-1000; i++ {
			// 가장 오래된 것들부터 제거
			oldestPrice := ""
			oldestTime := time.Now()

			for price, compressed := range sm.compressedTrades {
				if compressed.LastTime.Before(oldestTime) {
					oldestTime = compressed.LastTime
					oldestPrice = price
				}
			}

			if oldestPrice != "" {
				delete(sm.compressedTrades, oldestPrice)
				cleanedCompressed++
			}
		}
	}

	sm.lastCleanup = time.Now()

	// 🔥 압축 정리 로그 (중요한 정리만)
	if cleanedCompressed > 10 {
		log.Printf("🧹 [%s] 압축 데이터 %d개 정리 (메모리 누수 방지)", sm.symbol, cleanedCompressed)
	}

	return cleanedOrderbooks, cleanedTrades + cleanedCompressed
}

// GetStats 심볼별 통계 조회 (압축 데이터 포함)
func (sm *SymbolMemory) GetStats() (int, int) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.orderbooks), len(sm.trades) + len(sm.compressedTrades)
}

// GetCompressedTradeCount 압축된 거래 개수 조회
func (sm *SymbolMemory) GetCompressedTradeCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.compressedTrades)
}

// GetDataForSignal 시그널용 데이터 조회 (±60초)
func (sm *SymbolMemory) GetDataForSignal(signalTime time.Time, beforeSeconds, afterSeconds int) ([]*OrderbookSnapshot, []*TradeData) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	startTime := signalTime.Add(-time.Duration(beforeSeconds) * time.Second)
	endTime := signalTime.Add(time.Duration(afterSeconds) * time.Second)

	var resultOrderbooks []*OrderbookSnapshot
	var resultTrades []*TradeData

	// 오더북 데이터 수집
	for _, ob := range sm.orderbooks {
		if ob.Timestamp.After(startTime) && ob.Timestamp.Before(endTime) {
			resultOrderbooks = append(resultOrderbooks, ob)
		}
	}

	// 체결 데이터 수집
	for _, trade := range sm.trades {
		if trade.Timestamp.After(startTime) && trade.Timestamp.Before(endTime) {
			resultTrades = append(resultTrades, trade)
		}
	}

	return resultOrderbooks, resultTrades
}

// Manager 전체 메모리 관리자 (심볼별 관리)
type Manager struct {
	symbols                   map[string]*SymbolMemory
	signals                   []*AdvancedPumpSignal
	mu                        sync.RWMutex
	maxSignals                int
	orderbookRetentionMinutes float64
	tradeRetentionMinutes     int
	maxOrderbooks             int
	maxTrades                 int
	retentionMinutes          int
	cleanupInterval           time.Duration

	// 🔧 하드코딩 제거: config 설정들 추가
	compressionIntervalSeconds int
	heapWarningMB              float64
	gcThresholdOrderbooks      int
	gcThresholdTrades          int
	maxGoroutines              int
	monitoringIntervalSeconds  int

	// 모니터링 카운터 (누적)
	orderbookCounter int
	tradeCounter     int

	// 처리율 추적 (성능 최적화)
	lastStatsTime      time.Time
	lastOrderbookCount int
	lastTradeCount     int
	orderbookRate      float64 // 초당 오더북 처리율
	tradeRate          float64 // 초당 체결 처리율
}

// NewManager 새 메모리 관리자 생성 (심볼별 관리)
func NewManager(maxOrderbooks, maxTrades, maxSignals, retentionMinutes int, orderbookRetentionMinutes float64,
	compressionIntervalSeconds int, heapWarningMB float64, gcThresholdOrderbooks, gcThresholdTrades, maxGoroutines, monitoringIntervalSeconds int) *Manager {
	log.Printf("�� 메모리 관리자 초기화 시작: 최대 오더북=%d, 체결=%d, 시그널=%d, 오더북보존시간=%.1f분, 체결보존시간=%d분",
		maxOrderbooks, maxTrades, maxSignals, orderbookRetentionMinutes, retentionMinutes)

	mm := &Manager{
		symbols:                   make(map[string]*SymbolMemory),
		signals:                   make([]*AdvancedPumpSignal, 0, maxSignals),
		maxSignals:                maxSignals,
		orderbookRetentionMinutes: orderbookRetentionMinutes,
		tradeRetentionMinutes:     retentionMinutes,
		maxOrderbooks:             maxOrderbooks,
		maxTrades:                 maxTrades,
		retentionMinutes:          retentionMinutes,
		cleanupInterval:           time.Duration(retentionMinutes) * time.Minute,
		// 🔧 하드코딩 제거: config에서 읽어온 값들 설정
		compressionIntervalSeconds: compressionIntervalSeconds,
		heapWarningMB:              heapWarningMB,
		gcThresholdOrderbooks:      gcThresholdOrderbooks,
		gcThresholdTrades:          gcThresholdTrades,
		maxGoroutines:              maxGoroutines,
		monitoringIntervalSeconds:  monitoringIntervalSeconds,
		orderbookCounter:           0,
		tradeCounter:               0,
		// 처리율 추적 초기화
		lastStatsTime:      time.Now(),
		lastOrderbookCount: 0,
		lastTradeCount:     0,
		orderbookRate:      0.0,
		tradeRate:          0.0,
	}

	// 정리 고루틴 시작
	log.Printf("🧹 메모리 정리 고루틴 시작 중...")
	go mm.cleanupRoutine()
	log.Printf("✅ 메모리 정리 고루틴 시작 완료")

	return mm
}

// getOrCreateSymbolMemory 심볼 메모리 조회 또는 생성
func (mm *Manager) getOrCreateSymbolMemory(symbol string) *SymbolMemory {
	mm.mu.RLock()
	if symbolMem, exists := mm.symbols[symbol]; exists {
		mm.mu.RUnlock()
		return symbolMem
	}
	mm.mu.RUnlock()

	// 새로운 심볼 메모리 생성 (Write Lock)
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// 다시 확인 (Double-check locking)
	if symbolMem, exists := mm.symbols[symbol]; exists {
		return symbolMem
	}

	symbolMem := NewSymbolMemory(symbol, mm.maxOrderbooks, mm.maxTrades, mm.compressionIntervalSeconds) // 🔧 config 값 사용
	mm.symbols[symbol] = symbolMem
	log.Printf("🔧 새 심볼 메모리 생성: %s", symbol)
	return symbolMem
}

// AddOrderbook 오더북 추가 (심볼별 분산)
func (mm *Manager) AddOrderbook(snapshot *OrderbookSnapshot) {
	symbolMem := mm.getOrCreateSymbolMemory(snapshot.Symbol)
	symbolMem.AddOrderbook(snapshot)

	// 모니터링 카운터 (전역)
	mm.orderbookCounter++
	if mm.orderbookCounter%1000 == 0 {
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		heapMB := float64(memStats.HeapInuse) / 1024 / 1024
		if heapMB > mm.heapWarningMB { // 🔧 config 값 사용
			log.Printf("📊 오더북 %d회: 힙=%.1fMB, 고루틴=%d개, 심볼=%d개",
				mm.orderbookCounter, heapMB, runtime.NumGoroutine(), len(mm.symbols))
		}
	}
}

// AddTrade 체결 데이터 추가 (심볼별 분산)
func (mm *Manager) AddTrade(trade *TradeData) {
	symbolMem := mm.getOrCreateSymbolMemory(trade.Symbol)
	symbolMem.AddTrade(trade)

	// 모니터링 카운터 (전역)
	mm.tradeCounter++
	if mm.tradeCounter%2000 == 0 {
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		heapMB := float64(memStats.HeapInuse) / 1024 / 1024
		if heapMB > mm.heapWarningMB { // 🔧 config 값 사용
			log.Printf("📊 체결 %d회: 힙=%.1fMB, 고루틴=%d개, 심볼=%d개",
				mm.tradeCounter, heapMB, runtime.NumGoroutine(), len(mm.symbols))
		}
	}
}

// AddSignal 시그널 추가
func (mm *Manager) AddSignal(signal *AdvancedPumpSignal) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.signals = append(mm.signals, signal)

	// 최대 개수 제한
	if len(mm.signals) > mm.maxSignals {
		mm.signals = mm.signals[1:] // 가장 오래된 시그널 제거
	}

	log.Printf("🚨 시그널 저장: %s (점수: %.2f, 액션: %s)", signal.Symbol, signal.CompositeScore, signal.Action)
}

// GetOrderbooks 특정 심볼의 오더북 데이터 조회
func (mm *Manager) GetOrderbooks(symbol string) []*OrderbookSnapshot {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	if symbolMem, exists := mm.symbols[symbol]; exists {
		return symbolMem.orderbooks
	}
	return []*OrderbookSnapshot{}
}

// GetTrades 특정 심볼의 체결 데이터 조회
func (mm *Manager) GetTrades(symbol string) []*TradeData {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	if symbolMem, exists := mm.symbols[symbol]; exists {
		return symbolMem.trades
	}
	return []*TradeData{}
}

// GetRecentOrderbooks 최근 N개의 오더북 조회
func (mm *Manager) GetRecentOrderbooks(symbol string, count int) []*OrderbookSnapshot {
	orderbooks := mm.GetOrderbooks(symbol)
	if len(orderbooks) == 0 {
		return []*OrderbookSnapshot{}
	}

	if count > len(orderbooks) {
		count = len(orderbooks)
	}

	return orderbooks[len(orderbooks)-count:]
}

// GetRecentTrades 최근 N개의 체결 조회 (압축 데이터 포함)
func (mm *Manager) GetRecentTrades(symbol string, count int) []*TradeData {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	symbolMem, exists := mm.symbols[symbol]
	if !exists {
		return []*TradeData{}
	}

	symbolMem.mu.RLock()
	defer symbolMem.mu.RUnlock()

	// 🔧 안전 제한: 요청 개수가 너무 크면 제한
	safeCount := count
	if safeCount > 500 { // 최대 500개로 제한
		safeCount = 500
	}

	// 1단계: 원본 체결 데이터에서 먼저 수집
	trades := symbolMem.trades
	var result []*TradeData

	// 최신 데이터부터 역순으로 수집
	if len(trades) > 0 {
		start := len(trades) - safeCount
		if start < 0 {
			start = 0
		}
		result = append(result, trades[start:]...)
	}

	// 2단계: 부족하면 압축된 데이터에서 보충
	if len(result) < safeCount && len(symbolMem.compressedTrades) > 0 {
		needed := safeCount - len(result)

		// 압축된 데이터를 시간순으로 정렬하여 추가
		var compressedData []*TradeData
		for _, compressed := range symbolMem.compressedTrades {
			// 압축된 데이터를 TradeData 형태로 변환
			tradeData := &TradeData{
				Symbol:    compressed.Symbol,
				Price:     compressed.Price,
				Quantity:  fmt.Sprintf("%.8f", compressed.TotalVolume),
				Side:      "COMPRESSED", // 압축 데이터 표시
				Timestamp: compressed.LastTime,
				Exchange:  compressed.Exchange,
				TradeID:   fmt.Sprintf("compressed_%s", compressed.Price),
			}
			compressedData = append(compressedData, tradeData)
		}

		// 시간순 정렬 (최신순)
		if len(compressedData) > 0 {
			// 간단한 정렬 (최신 데이터만 필요하므로)
			if needed > len(compressedData) {
				needed = len(compressedData)
			}

			// 압축 데이터는 앞쪽에 추가 (시간순)
			result = append(compressedData[:needed], result...)
		}
	}

	// 3단계: 요청된 개수만큼 반환 (최신순 유지)
	if len(result) > safeCount {
		result = result[len(result)-safeCount:]
	}

	return result
}

// GetTimeRangeOrderbooks 시간 범위 내 오더북 조회
func (mm *Manager) GetTimeRangeOrderbooks(symbol string, startTime, endTime time.Time) []*OrderbookSnapshot {
	orderbooks := mm.GetOrderbooks(symbol)
	var result []*OrderbookSnapshot

	for _, ob := range orderbooks {
		if ob.Timestamp.After(startTime) && ob.Timestamp.Before(endTime) {
			result = append(result, ob)
		}
	}

	return result
}

// GetTimeRangeTrades 시간 범위 내 체결 조회
func (mm *Manager) GetTimeRangeTrades(symbol string, startTime, endTime time.Time) []*TradeData {
	trades := mm.GetTrades(symbol)
	var result []*TradeData

	for _, trade := range trades {
		if trade.Timestamp.After(startTime) && trade.Timestamp.Before(endTime) {
			result = append(result, trade)
		}
	}

	return result
}

// GetRecentSignals 최근 시그널 조회
func (mm *Manager) GetRecentSignals(count int) []*AdvancedPumpSignal {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	if count > len(mm.signals) {
		count = len(mm.signals)
	}

	if count == 0 {
		return []*AdvancedPumpSignal{}
	}

	return mm.signals[len(mm.signals)-count:]
}

// GetMemoryStats 메모리 통계 조회 (GetSystemStats와 동일 - 호환성 유지)
func (mm *Manager) GetMemoryStats() map[string]interface{} {
	return mm.GetSystemStats()
}

// GetSystemStats 시스템 통계 조회 (압축 정보 포함)
func (mm *Manager) GetSystemStats() map[string]interface{} {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	// 처리율 업데이트 (성능 최적화)
	mm.updateProcessingRates()

	totalOrderbooks := 0
	totalTrades := 0
	totalCompressed := 0

	for _, symbolMem := range mm.symbols {
		totalOrderbooks += len(symbolMem.orderbooks)
		totalTrades += len(symbolMem.trades)
		totalCompressed += len(symbolMem.compressedTrades)
	}

	return map[string]interface{}{
		// 현재 메모리 상태
		"total_orderbooks":        totalOrderbooks,
		"total_trades":            totalTrades,
		"total_compressed_trades": totalCompressed,
		"total_signals":           len(mm.signals),
		"symbols_with_orderbooks": len(mm.symbols),
		"symbols_with_trades":     len(mm.symbols),

		// 누적 처리량 (총 처리한 양)
		"cumulative_orderbooks": mm.orderbookCounter,
		"cumulative_trades":     mm.tradeCounter,

		// 처리율 (초당 처리량)
		"orderbook_rate_per_sec": mm.orderbookRate,
		"trade_rate_per_sec":     mm.tradeRate,

		// 설정 정보
		"retention_minutes":         mm.retentionMinutes,
		"max_orderbooks_per_symbol": mm.maxOrderbooks,
		"max_trades_per_symbol":     mm.maxTrades,
		"compression_efficiency":    mm.calculateCompressionEfficiency(totalTrades, totalCompressed),
	}
}

// calculateCompressionEfficiency 압축 효율성 계산
func (mm *Manager) calculateCompressionEfficiency(rawTrades, compressedTrades int) float64 {
	total := rawTrades + compressedTrades
	if total == 0 {
		return 0.0
	}
	return float64(compressedTrades) / float64(total) * 100.0
}

// updateProcessingRates 처리율 계산 (성능 최적화 - 락 없음)
func (mm *Manager) updateProcessingRates() {
	now := time.Now()
	timeDiff := now.Sub(mm.lastStatsTime).Seconds()

	// 30초 이상 경과시에만 업데이트 (성능 최적화)
	if timeDiff >= 30.0 {
		orderbookDiff := mm.orderbookCounter - mm.lastOrderbookCount
		tradeDiff := mm.tradeCounter - mm.lastTradeCount

		mm.orderbookRate = float64(orderbookDiff) / timeDiff
		mm.tradeRate = float64(tradeDiff) / timeDiff

		mm.lastStatsTime = now
		mm.lastOrderbookCount = mm.orderbookCounter
		mm.lastTradeCount = mm.tradeCounter
	}
}

// cleanupRoutine 정리 고루틴 (심볼별 분산 정리)
func (mm *Manager) cleanupRoutine() {
	cleanupInterval := 10 * time.Second // 10초마다 실행
	log.Printf("🧹 메모리 정리 고루틴 진입 (%v마다 실행, 오더북=%.1f분, 체결=%d분 보존)",
		cleanupInterval, mm.orderbookRetentionMinutes, mm.tradeRetentionMinutes)

	// 🧹 즉시 한 번 정리 실행
	log.Printf("🧹 초기 메모리 정리 실행...")
	mm.cleanup()

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	log.Printf("🧹 메모리 정리 타이머 시작 (%v 간격)", cleanupInterval)

	for range ticker.C {
		log.Printf("🧹 정기 메모리 정리 시작...")
		mm.cleanup()
	}
}

// cleanup 심볼별 분산 정리 (락 경합 대폭 감소)
func (mm *Manager) cleanup() {
	cleanupStart := time.Now()

	// 서로 다른 보존 시간 설정
	orderbookCutoff := time.Now().Add(-time.Duration(mm.orderbookRetentionMinutes*60) * time.Second)
	tradeCutoff := time.Now().Add(-time.Duration(mm.tradeRetentionMinutes) * time.Minute)
	signalCutoff := time.Now().Add(-time.Duration(mm.retentionMinutes) * time.Minute)

	cleanedOrderbooks := 0
	cleanedTrades := 0
	cleanedSignals := 0

	// GC 통계 수집 (정리 전)
	var memStatsBefore runtime.MemStats
	runtime.ReadMemStats(&memStatsBefore)

	// 1단계: 심볼 목록 수집 (빠른 락)
	mm.mu.RLock()
	symbolsToClean := make([]string, 0, len(mm.symbols))
	totalOrderbooksBefore := 0
	totalTradesBefore := 0

	for symbol, symbolMem := range mm.symbols {
		symbolsToClean = append(symbolsToClean, symbol)
		obCount, tradeCount := symbolMem.GetStats()
		totalOrderbooksBefore += obCount
		totalTradesBefore += tradeCount
	}
	mm.mu.RUnlock()

	log.Printf("🧹 메모리 정리 시작: 오더북cutoff=%s, 체결cutoff=%s, 현재 오더북=%d개, 체결=%d개",
		orderbookCutoff.Format("15:04:05"), tradeCutoff.Format("15:04:05"),
		totalOrderbooksBefore, totalTradesBefore)

	// 2단계: 심볼별 병렬 정리 (락 없음 - 각 심볼이 독립적으로 락 관리)
	for _, symbol := range symbolsToClean {
		mm.mu.RLock()
		symbolMem, exists := mm.symbols[symbol]
		mm.mu.RUnlock()

		if exists {
			cleanedOB, cleanedTR := symbolMem.Cleanup(orderbookCutoff, tradeCutoff)
			cleanedOrderbooks += cleanedOB
			cleanedTrades += cleanedTR
		}
	}

	// 3단계: 시그널 정리 (전역 락 필요)
	mm.mu.Lock()
	var validSignals []*AdvancedPumpSignal
	for _, signal := range mm.signals {
		if signal.Timestamp.After(signalCutoff) {
			validSignals = append(validSignals, signal)
		} else {
			cleanedSignals++
		}
	}
	mm.signals = validSignals
	mm.mu.Unlock()

	// 4단계: 최종 통계 수집
	mm.mu.RLock()
	totalOrderbooksAfter := 0
	totalTradesAfter := 0
	for _, symbolMem := range mm.symbols {
		obCount, tradeCount := symbolMem.GetStats()
		totalOrderbooksAfter += obCount
		totalTradesAfter += tradeCount
	}
	symbolCount := len(mm.symbols)
	mm.mu.RUnlock()

	totalCleanupTime := time.Since(cleanupStart)

	// GC 통계 수집 (정리 후)
	var memStatsAfter runtime.MemStats
	runtime.ReadMemStats(&memStatsAfter)

	// 정리 결과 로그 출력
	log.Printf("🧹 메모리 정리 완료 (오더북=%.1f분, 체결=%d분): 오더북 %d→%d개(-%d), 체결 %d→%d개(-%d), 시그널 -%d개 [총시간: %.2fms]",
		mm.orderbookRetentionMinutes, mm.tradeRetentionMinutes,
		totalOrderbooksBefore, totalOrderbooksAfter, cleanedOrderbooks,
		totalTradesBefore, totalTradesAfter, cleanedTrades,
		cleanedSignals,
		float64(totalCleanupTime.Nanoseconds())/1000000)

	// GC 및 메모리 정보
	gcRuns := memStatsAfter.NumGC - memStatsBefore.NumGC
	heapMB := float64(memStatsAfter.HeapInuse) / 1024 / 1024
	gcPauseTotalNs := memStatsAfter.PauseTotalNs - memStatsBefore.PauseTotalNs

	if gcRuns > 0 || heapMB > 30 || cleanedOrderbooks > 10 || cleanedTrades > 50 {
		log.Printf("🧹 GC 정보: 실행횟수=+%d, 힙메모리=%.1fMB, GC일시정지=%.2fms, 고루틴=%d개, 심볼=%d개",
			gcRuns, heapMB, float64(gcPauseTotalNs)/1000000, runtime.NumGoroutine(), symbolCount)
	}

	// 메모리 사용량 경고
	if symbolCount > 0 {
		avgOrderbooks := totalOrderbooksAfter / symbolCount
		avgTrades := totalTradesAfter / symbolCount

		if avgOrderbooks > mm.maxOrderbooks/2 || avgTrades > mm.maxTrades/2 {
			log.Printf("⚠️  메모리 사용량 높음: 오더북 평균=%d개, 체결 평균=%d개", avgOrderbooks, avgTrades)
		}
	}

	// 강제 GC 실행 (임계값 대폭 감소)
	if cleanedOrderbooks > mm.gcThresholdOrderbooks || cleanedTrades > mm.gcThresholdTrades || heapMB > mm.heapWarningMB*10 { // 🔧 config 값 사용
		log.Printf("🧹 강제 GC 실행 중 (오더북: %d개, 체결: %d개, 힙: %.1fMB)...",
			cleanedOrderbooks, cleanedTrades, heapMB)
		runtime.GC()

		// GC 후 메모리 상태 확인
		var memStatsAfterGC runtime.MemStats
		runtime.ReadMemStats(&memStatsAfterGC)
		heapAfterMB := float64(memStatsAfterGC.HeapInuse) / 1024 / 1024
		log.Printf("🧹 강제 GC 완료: %.1fMB → %.1fMB (%.1fMB 절약)", heapMB, heapAfterMB, heapMB-heapAfterMB)
	}
}

// GetSnapshotData 트리거 발생 시점 주변 데이터 조회
func (mm *Manager) GetSnapshotData(symbol string, triggerTime time.Time, preSeconds, postSeconds int) map[string]interface{} {
	startTime := triggerTime.Add(-time.Duration(preSeconds) * time.Second)
	endTime := triggerTime.Add(time.Duration(postSeconds) * time.Second)

	orderbooks := mm.GetTimeRangeOrderbooks(symbol, startTime, endTime)
	trades := mm.GetTimeRangeTrades(symbol, startTime, endTime)

	return map[string]interface{}{
		"symbol":          symbol,
		"trigger_time":    triggerTime,
		"start_time":      startTime,
		"end_time":        endTime,
		"orderbooks":      orderbooks,
		"trades":          trades,
		"orderbook_count": len(orderbooks),
		"trade_count":     len(trades),
	}
}

// GetSymbols 현재 관리 중인 심볼 목록 반환
func (mm *Manager) GetSymbols() []string {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	symbols := make([]string, 0, len(mm.symbols))
	for symbol := range mm.symbols {
		symbols = append(symbols, symbol)
	}
	return symbols
}

// InitializeSymbol 새 심볼 초기화
func (mm *Manager) InitializeSymbol(symbol string) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// 심볼이 이미 존재하는지 확인
	if _, exists := mm.symbols[symbol]; !exists {
		// 새 심볼에 대한 빈 슬라이스 초기화
		mm.symbols[symbol] = NewSymbolMemory(symbol, mm.maxOrderbooks, mm.maxTrades, mm.compressionIntervalSeconds) // 🔧 config 값 사용
		log.Printf("심볼 초기화 완료: %s", symbol)
	}
}

// CleanupSymbol 심볼 데이터 정리
func (mm *Manager) CleanupSymbol(symbol string) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// 심볼 메모리 삭제
	delete(mm.symbols, symbol)

	log.Printf("심볼 정리 완료: %s", symbol)
}
