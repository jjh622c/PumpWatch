package memory

import (
	"log"
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

// Manager 메모리 관리자
type Manager struct {
	mu sync.RWMutex

	// 오더북 데이터 (rolling buffer)
	orderbooks    map[string][]*OrderbookSnapshot
	maxOrderbooks int

	// 체결 데이터 (rolling buffer)
	trades    map[string][]*TradeData
	maxTrades int

	// 시그널 데이터
	signals    []*AdvancedPumpSignal
	maxSignals int

	// 설정
	retentionMinutes int
	cleanupInterval  time.Duration
}

// NewManager 메모리 관리자 생성
func NewManager(maxOrderbooks, maxTrades, maxSignals, retentionMinutes int) *Manager {
	mm := &Manager{
		orderbooks:       make(map[string][]*OrderbookSnapshot),
		trades:           make(map[string][]*TradeData),
		signals:          make([]*AdvancedPumpSignal, 0),
		maxOrderbooks:    maxOrderbooks,
		maxTrades:        maxTrades,
		maxSignals:       maxSignals,
		retentionMinutes: retentionMinutes,
		cleanupInterval:  time.Duration(retentionMinutes) * time.Minute,
	}

	// 정리 고루틴 시작 (10분마다 실행)
	go mm.cleanupRoutine()

	return mm
}

// AddOrderbook 오더북 추가 (rolling buffer)
func (mm *Manager) AddOrderbook(snapshot *OrderbookSnapshot) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	symbol := snapshot.Symbol

	// 기존 데이터에 추가
	mm.orderbooks[symbol] = append(mm.orderbooks[symbol], snapshot)

	// 최대 개수 제한 (rolling buffer)
	if len(mm.orderbooks[symbol]) > mm.maxOrderbooks {
		mm.orderbooks[symbol] = mm.orderbooks[symbol][1:] // 가장 오래된 데이터 제거
	}

	log.Printf("📊 오더북 저장: %s (총 %d개)", symbol, len(mm.orderbooks[symbol]))
}

// AddTrade 체결 데이터 추가 (rolling buffer)
func (mm *Manager) AddTrade(trade *TradeData) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	symbol := trade.Symbol

	// 기존 데이터에 추가
	mm.trades[symbol] = append(mm.trades[symbol], trade)

	// 최대 개수 제한 (rolling buffer)
	if len(mm.trades[symbol]) > mm.maxTrades {
		mm.trades[symbol] = mm.trades[symbol][1:] // 가장 오래된 데이터 제거
	}

	log.Printf("💰 체결 저장: %s %s@%s (총 %d개)", symbol, trade.Quantity, trade.Price, len(mm.trades[symbol]))
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

	if orderbooks, exists := mm.orderbooks[symbol]; exists {
		return orderbooks
	}
	return []*OrderbookSnapshot{}
}

// GetTrades 특정 심볼의 체결 데이터 조회
func (mm *Manager) GetTrades(symbol string) []*TradeData {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	if trades, exists := mm.trades[symbol]; exists {
		return trades
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

// GetRecentTrades 최근 N개의 체결 조회
func (mm *Manager) GetRecentTrades(symbol string, count int) []*TradeData {
	trades := mm.GetTrades(symbol)
	if len(trades) == 0 {
		return []*TradeData{}
	}

	if count > len(trades) {
		count = len(trades)
	}

	return trades[len(trades)-count:]
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

// GetMemoryStats 메모리 통계 조회
func (mm *Manager) GetMemoryStats() map[string]interface{} {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	totalOrderbooks := 0
	totalTrades := 0

	for _, orderbooks := range mm.orderbooks {
		totalOrderbooks += len(orderbooks)
	}

	for _, trades := range mm.trades {
		totalTrades += len(trades)
	}

	return map[string]interface{}{
		"total_orderbooks":          totalOrderbooks,
		"total_trades":              totalTrades,
		"total_signals":             len(mm.signals),
		"symbols_with_orderbooks":   len(mm.orderbooks),
		"symbols_with_trades":       len(mm.trades),
		"retention_minutes":         mm.retentionMinutes,
		"max_orderbooks_per_symbol": mm.maxOrderbooks,
		"max_trades_per_symbol":     mm.maxTrades,
		"max_signals":               mm.maxSignals,
	}
}

// cleanupRoutine 정리 고루틴 (오래된 데이터 제거)
// cleanupRoutine 정리 고루틴 (10분마다 실행)
func (mm *Manager) cleanupRoutine() {
	ticker := time.NewTicker(10 * time.Minute) // 10분마다 실행
	defer ticker.Stop()

	for range ticker.C {
		mm.cleanup()
	}
}

// cleanup 오래된 데이터 정리 (10분 이상 된 데이터만 제거)
func (mm *Manager) cleanup() {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// 10분 이상 된 데이터만 정리 (최소 2-3분치 데이터는 보관)
	cutoffTime := time.Now().Add(-10 * time.Minute)
	cleanedOrderbooks := 0
	cleanedTrades := 0

	// 오더북 정리
	for symbol, orderbooks := range mm.orderbooks {
		var validOrderbooks []*OrderbookSnapshot
		for _, ob := range orderbooks {
			if ob.Timestamp.After(cutoffTime) {
				validOrderbooks = append(validOrderbooks, ob)
			} else {
				cleanedOrderbooks++
			}
		}
		mm.orderbooks[symbol] = validOrderbooks
	}

	// 체결 정리
	for symbol, trades := range mm.trades {
		var validTrades []*TradeData
		for _, trade := range trades {
			if trade.Timestamp.After(cutoffTime) {
				validTrades = append(validTrades, trade)
			} else {
				cleanedTrades++
			}
		}
		mm.trades[symbol] = validTrades
	}

	if cleanedOrderbooks > 0 || cleanedTrades > 0 {
		log.Printf("🧹 메모리 정리: 오더북 %d개, 체결 %d개 제거 (10분 이상)", cleanedOrderbooks, cleanedTrades)
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

// GetSymbols 저장된 심볼 목록 조회
func (mm *Manager) GetSymbols() []string {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	symbols := make(map[string]bool)

	for symbol := range mm.orderbooks {
		symbols[symbol] = true
	}

	for symbol := range mm.trades {
		symbols[symbol] = true
	}

	result := make([]string, 0, len(symbols))
	for symbol := range symbols {
		result = append(result, symbol)
	}

	return result
}
