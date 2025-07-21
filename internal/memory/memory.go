package memory

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// OrderbookSnapshot 오더북 스냅샷
type OrderbookSnapshot struct {
	Exchange    string     `json:"exchange"`
	Symbol      string     `json:"symbol"`
	Timestamp   time.Time  `json:"timestamp"`
	Bids        [][]string `json:"bids"`
	Asks        [][]string `json:"asks"`
	UpdateID    int64      `json:"updateId,omitempty"`
	SignalScore float64    `json:"signal_score"`
}

// PumpSignal 펌핑 시그널
type PumpSignal struct {
	Symbol     string    `json:"symbol"`
	Exchange   string    `json:"exchange"`
	Timestamp  time.Time `json:"timestamp"`
	Score      float64   `json:"score"`
	MaxPumpPct float64   `json:"max_pump_pct"`
	Action     string    `json:"action"`
	Reasons    []string  `json:"reasons"`
}

// AdvancedPumpSignal 고도화된 펌핑 시그널
type AdvancedPumpSignal struct {
	Symbol    string    `json:"symbol"`
	Exchange  string    `json:"exchange"`
	Timestamp time.Time `json:"timestamp"`

	// 멀티지표 점수
	VolumeScore    float64 `json:"volume_score"`    // 거래량 점수
	PriceScore     float64 `json:"price_score"`     // 가격 변동 점수
	PatternScore   float64 `json:"pattern_score"`   // 패턴 점수
	CrossScore     float64 `json:"cross_score"`     // 타거래소 대비 점수
	CompositeScore float64 `json:"composite_score"` // 종합 점수

	// 상세 데이터
	VolumeChange       float64 `json:"volume_change"`       // 거래량 변화율
	PriceChange        float64 `json:"price_change"`        // 가격 변화율
	TradeAmount        float64 `json:"trade_amount"`        // 거래대금
	SpreadChange       float64 `json:"spread_change"`       // 스프레드 변화
	OrderBookImbalance float64 `json:"orderbook_imbalance"` // 오더북 불균형

	// 액션 권장
	Action     string  `json:"action"`     // 즉시매수/빠른매수/신중매수/대기
	Confidence float64 `json:"confidence"` // 신뢰도 (0-100)
	RiskLevel  string  `json:"risk_level"` // 위험도 (LOW/MEDIUM/HIGH)

	// 백테스팅 데이터
	HistoricalPatterns []string `json:"historical_patterns"` // 과거 패턴
	SuccessRate        float64  `json:"success_rate"`        // 성공률
}

// Manager 메모리 관리자
type Manager struct {
	mu         sync.RWMutex
	orderbooks map[string][]*OrderbookSnapshot // key: exchange_symbol
	signals    []*PumpSignal
	retention  time.Duration // 10분
}

// NewManager 메모리 관리자 생성
func NewManager() *Manager {
	return &Manager{
		orderbooks: make(map[string][]*OrderbookSnapshot),
		signals:    make([]*PumpSignal, 0),
		retention:  10 * time.Minute,
	}
}

// AddOrderbook 오더북 추가
func (mm *Manager) AddOrderbook(snapshot *OrderbookSnapshot) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	key := fmt.Sprintf("%s_%s", snapshot.Exchange, snapshot.Symbol)

	// 기존 오더북에 추가
	mm.orderbooks[key] = append(mm.orderbooks[key], snapshot)

	// 보관 시간 초과 데이터 정리
	cutoff := time.Now().Add(-mm.retention)
	var validOrderbooks []*OrderbookSnapshot
	for _, ob := range mm.orderbooks[key] {
		if ob.Timestamp.After(cutoff) {
			validOrderbooks = append(validOrderbooks, ob)
		}
	}
	mm.orderbooks[key] = validOrderbooks

	// 펌핑 시그널 분석
	if snapshot.SignalScore >= 60 {
		log.Printf("🚀 펌핑 시그널 감지: %s (점수: %.2f)", key, snapshot.SignalScore)
	}
}

// GetRecentOrderbooks 최근 오더북 조회
func (mm *Manager) GetRecentOrderbooks(exchange, symbol string, duration time.Duration) []*OrderbookSnapshot {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	key := fmt.Sprintf("%s_%s", exchange, symbol)
	orderbooks, exists := mm.orderbooks[key]
	if !exists {
		return []*OrderbookSnapshot{}
	}

	cutoff := time.Now().Add(-duration)
	var recentOrderbooks []*OrderbookSnapshot

	for _, ob := range orderbooks {
		if ob.Timestamp.After(cutoff) {
			recentOrderbooks = append(recentOrderbooks, ob)
		}
	}

	return recentOrderbooks
}

// AddSignal 시그널 추가
func (mm *Manager) AddSignal(signal *PumpSignal) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.signals = append(mm.signals, signal)

	// 중요 시그널은 디스크에 저장
	if signal.Score >= 80 {
		mm.saveSignalToDisk(signal)
	}

	log.Printf("📊 시그널 저장: %s (점수: %.2f, 액션: %s)",
		signal.Symbol, signal.Score, signal.Action)
}

// saveSignalToDisk 시그널을 디스크에 저장
func (mm *Manager) saveSignalToDisk(signal *PumpSignal) {
	// signals 디렉토리 생성
	signalsDir := "signals"
	if err := os.MkdirAll(signalsDir, 0755); err != nil {
		log.Printf("❌ 디렉토리 생성 실패: %v", err)
		return
	}

	// 파일명 생성 (날짜_시간_심볼.json)
	filename := fmt.Sprintf("%s_%s_%s.json",
		signal.Timestamp.Format("20060102"),
		signal.Timestamp.Format("150405"),
		signal.Symbol)

	filepath := filepath.Join(signalsDir, filename)

	// JSON으로 저장
	data, err := json.MarshalIndent(signal, "", "  ")
	if err != nil {
		log.Printf("❌ JSON 마샬링 실패: %v", err)
		return
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		log.Printf("❌ 파일 저장 실패: %v", err)
		return
	}

	log.Printf("💾 중요 시그널 저장: %s", filepath)
}

// GetMemoryStats 메모리 상태 조회
func (mm *Manager) GetMemoryStats() map[string]interface{} {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	stats := make(map[string]interface{})

	totalOrderbooks := 0
	for key, orderbooks := range mm.orderbooks {
		stats[key] = len(orderbooks)
		totalOrderbooks += len(orderbooks)
	}

	stats["total_orderbooks"] = totalOrderbooks
	stats["total_signals"] = len(mm.signals)
	stats["retention_minutes"] = int(mm.retention.Minutes())

	return stats
}

// GetRecentSignals 최근 시그널 조회
func (mm *Manager) GetRecentSignals(limit int) []*AdvancedPumpSignal {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	// 기존 PumpSignal을 AdvancedPumpSignal로 변환
	advancedSignals := make([]*AdvancedPumpSignal, 0)

	for i := len(mm.signals) - 1; i >= 0 && len(advancedSignals) < limit; i-- {
		signal := mm.signals[i]

		// PumpSignal을 AdvancedPumpSignal로 변환
		advancedSignal := &AdvancedPumpSignal{
			Symbol:         signal.Symbol,
			Exchange:       signal.Exchange,
			Timestamp:      signal.Timestamp,
			CompositeScore: signal.Score,
			PriceChange:    signal.MaxPumpPct,
			VolumeChange:   0, // 기본값
			Action:         signal.Action,
		}

		advancedSignals = append(advancedSignals, advancedSignal)
	}

	return advancedSignals
}
