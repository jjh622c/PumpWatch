package signals

import (
	"fmt"
	"log"
	"sync"
	"time"

	"noticepumpcatch/internal/memory"
	"noticepumpcatch/internal/storage"
	"noticepumpcatch/internal/triggers"
)

// SignalManager 시그널 관리자 (메모리 기반)
type SignalManager struct {
	memManager     *memory.Manager
	storageManager *storage.StorageManager
	triggerManager *triggers.Manager
	dataHandler    *storage.SignalDataHandler // 시그널 데이터 저장 핸들러 (메모리 기반)

	// 상장공시 콜백 채널
	listingCallback chan ListingSignal

	// 설정
	config *SignalConfig

	mu sync.RWMutex
}

// SignalConfig 시그널 설정
type SignalConfig struct {
	PumpDetection PumpDetectionConfig `json:"pump_detection"`
	Listing       ListingConfig       `json:"listing"`
}

// PumpDetectionConfig 펌핑 감지 설정
type PumpDetectionConfig struct {
	Enabled              bool    `json:"enabled"`
	MinScore             float64 `json:"min_score"`
	VolumeThreshold      float64 `json:"volume_threshold"`
	PriceChangeThreshold float64 `json:"price_change_threshold"`
	TimeWindowSeconds    int     `json:"time_window_seconds"`
}

// ListingConfig 상장공시 설정
type ListingConfig struct {
	Enabled     bool `json:"enabled"`
	AutoTrigger bool `json:"auto_trigger"`
}

// ListingSignal 상장공시 신호
type ListingSignal struct {
	Symbol     string                 `json:"symbol"`
	Exchange   string                 `json:"exchange"`
	Timestamp  time.Time              `json:"timestamp"`
	Confidence float64                `json:"confidence"`
	Source     string                 `json:"source"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// ListingCallback 상장공시 콜백 인터페이스
type ListingCallback interface {
	OnListingAnnouncement(signal ListingSignal)
}

// NewSignalManager 시그널 관리자 생성 (메모리 기반)
func NewSignalManager(
	memManager *memory.Manager,
	storageManager *storage.StorageManager,
	triggerManager *triggers.Manager,
	config *SignalConfig,
) *SignalManager {
	sm := &SignalManager{
		memManager:      memManager,
		storageManager:  storageManager,
		triggerManager:  triggerManager,
		dataHandler:     storage.NewSignalDataHandler(storageManager, memManager), // rawManager 제거됨
		listingCallback: make(chan ListingSignal, 100),
		config:          config,
	}

	// 상장공시 콜백 처리 고루틴 시작
	go sm.handleListingSignals()

	return sm
}

// Start 시그널 감지 시작
func (sm *SignalManager) Start() {
	log.Printf("🚨 시그널 감지 시작")

	// 펌핑 감지 활성화
	if sm.config.PumpDetection.Enabled {
		go sm.pumpDetectionRoutine()
	}

	// 상장공시 감지 활성화
	if sm.config.Listing.Enabled {
		log.Printf("📢 상장공시 감지 활성화")
	}
}

// pumpDetectionRoutine 펌핑 감지 루틴
func (sm *SignalManager) pumpDetectionRoutine() {
	ticker := time.NewTicker(1 * time.Second) // 1초마다 체크
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sm.detectPumpSignals()
		}
	}
}

// detectPumpSignals 펌핑 신호 감지
func (sm *SignalManager) detectPumpSignals() {
	symbols := sm.memManager.GetSymbols()

	for _, symbol := range symbols {
		// 최근 1초간 오더북 데이터 가져오기
		orderbooks := sm.memManager.GetRecentOrderbooks(symbol, 1) // 1초 데이터만
		if len(orderbooks) < 2 {
			continue
		}

		// 1초 내 가격 변동 계산
		priceChangePercent := sm.calculateOneSecondPriceChange(orderbooks)

		// 🎯 핵심: config에서 설정한 임계값 이상 상승 시에만 시그널 발생
		if priceChangePercent >= sm.config.PumpDetection.PriceChangeThreshold {
			// 🚨 펌핑 시그널 감지 로그 (상세 정보)
			log.Printf("🚨 [PUMP DETECTED] %s: +%.2f%% (1초간 상승, 임계값: %.1f%%)", symbol, priceChangePercent, sm.config.PumpDetection.PriceChangeThreshold)

			// 현재 가격 정보 추가
			if len(orderbooks) > 0 && len(orderbooks[len(orderbooks)-1].Bids) > 0 && len(orderbooks[len(orderbooks)-1].Asks) > 0 {
				currentBid, _ := parseFloat(orderbooks[len(orderbooks)-1].Bids[0][0])
				currentAsk, _ := parseFloat(orderbooks[len(orderbooks)-1].Asks[0][0])
				currentMid := (currentBid + currentAsk) / 2
				log.Printf("📊 [PUMP INFO] %s: 현재가=%.8f, 매수=%.8f, 매도=%.8f",
					symbol, currentMid, currentBid, currentAsk)
			}

			// 최근 체결 데이터 가져오기 (±60초 저장용)
			trades := sm.memManager.GetRecentTrades(symbol, 60)
			log.Printf("💾 [PUMP SAVE] %s: 체결 %d건 데이터 수집", symbol, len(trades))

			// 펌핑 신호 생성
			signal := sm.createSimplePumpSignal(symbol, priceChangePercent, orderbooks, trades)

			// 메모리에 저장
			sm.memManager.AddSignal(signal)
			log.Printf("📝 [PUMP MEMORY] %s: 시그널 메모리 저장 완료", symbol)

			// 스토리지에 저장 (기존 시그널 저장)
			if err := sm.storageManager.SaveSignal(signal); err != nil {
				log.Printf("❌ [PUMP ERROR] %s: 시그널 저장 실패 - %v", symbol, err)
			} else {
				log.Printf("✅ [PUMP STORAGE] %s: 시그널 파일 저장 완료", symbol)
			}

			// 🚨 핵심: 시그널 발생 시 ±5초 범위 데이터 즉시 저장
			if err := sm.dataHandler.SavePumpSignalData(signal); err != nil {
				log.Printf("❌ [PUMP ERROR] %s: 데이터 저장 실패 - %v", symbol, err)
			} else {
				log.Printf("✅ [PUMP DATA] %s: ±5초 데이터 저장 완료", symbol)
			}

			// 트리거 발생
			metadata := map[string]interface{}{
				"price_change": priceChangePercent,
				"confidence":   signal.Confidence,
				"action":       signal.Action,
			}
			sm.triggerManager.TriggerPumpDetection(symbol, priceChangePercent, signal.Confidence, metadata)

			log.Printf("🚨 펌핑 감지: %s (1초 상승: +%.2f%%)", symbol, priceChangePercent)
		}
	}
}

// calculateOneSecondPriceChange 1초 가격 변동율 계산
func (sm *SignalManager) calculateOneSecondPriceChange(orderbooks []*memory.OrderbookSnapshot) float64 {
	if len(orderbooks) < 2 {
		return 0
	}

	// 1초 전과 현재 오더북의 중간가 비교
	first := orderbooks[0]
	last := orderbooks[len(orderbooks)-1]

	if len(first.Bids) == 0 || len(first.Asks) == 0 || len(last.Bids) == 0 || len(last.Asks) == 0 {
		return 0
	}

	// 중간가 계산
	firstBid, _ := parseFloat(first.Bids[0][0])
	firstAsk, _ := parseFloat(first.Asks[0][0])
	firstMid := (firstBid + firstAsk) / 2

	lastBid, _ := parseFloat(last.Bids[0][0])
	lastAsk, _ := parseFloat(last.Asks[0][0])
	lastMid := (lastBid + lastAsk) / 2

	if firstMid == 0 {
		return 0
	}

	// 1초간 가격 변동율 계산 (양수만 반환)
	changePercent := ((lastMid - firstMid) / firstMid) * 100
	if changePercent < 0 {
		return 0 // 하락은 무시
	}

	return changePercent
}

// calculateVolumeChange 거래량 변화율 계산 (기존 메서드 유지)
func (sm *SignalManager) calculateVolumeChange(trades []*memory.TradeData) float64 {
	if len(trades) < 20 {
		return 0
	}

	// 최근 10개와 이전 10개 거래량 비교
	recent := trades[len(trades)-10:]
	previous := trades[len(trades)-20 : len(trades)-10]

	recentVolume := 0.0
	for _, trade := range recent {
		if qty, err := parseFloat(trade.Quantity); err == nil {
			recentVolume += qty
		}
	}

	previousVolume := 0.0
	for _, trade := range previous {
		if qty, err := parseFloat(trade.Quantity); err == nil {
			previousVolume += qty
		}
	}

	if previousVolume == 0 {
		return 0
	}

	return ((recentVolume - previousVolume) / previousVolume) * 100
}

// calculateOrderbookImbalance 오더북 불균형 계산 (기존 메서드 유지)
func (sm *SignalManager) calculateOrderbookImbalance(orderbook *memory.OrderbookSnapshot) float64 {
	if len(orderbook.Bids) == 0 || len(orderbook.Asks) == 0 {
		return 0
	}

	bidVolume := 0.0
	askVolume := 0.0

	// 상위 5개 호가의 거래량 합계
	for i := 0; i < 5 && i < len(orderbook.Bids); i++ {
		if qty, err := parseFloat(orderbook.Bids[i][1]); err == nil {
			bidVolume += qty
		}
	}

	for i := 0; i < 5 && i < len(orderbook.Asks); i++ {
		if qty, err := parseFloat(orderbook.Asks[i][1]); err == nil {
			askVolume += qty
		}
	}

	if bidVolume == 0 && askVolume == 0 {
		return 0
	}

	totalVolume := bidVolume + askVolume
	imbalance := (bidVolume - askVolume) / totalVolume

	// 0-1 범위로 정규화
	if imbalance < 0 {
		imbalance = -imbalance
	}

	return imbalance
}

// createPumpSignal 펌핑 시그널 생성 (기존 메서드를 새 메서드로 수정)
func (sm *SignalManager) createPumpSignal(
	symbol string,
	score float64,
	orderbooks []*memory.OrderbookSnapshot,
	trades []*memory.TradeData,
) *memory.AdvancedPumpSignal {
	// 새로운 단순 펌핑 시그널로 리다이렉트
	return sm.createSimplePumpSignal(symbol, score, orderbooks, trades)
}

// calculatePriceChange 가격 변화율 계산 (기존 메서드 유지)
func (sm *SignalManager) calculatePriceChange(orderbooks []*memory.OrderbookSnapshot) float64 {
	if len(orderbooks) < 2 {
		return 0
	}

	// 첫 번째와 마지막 오더북의 중간가 비교
	first := orderbooks[0]
	last := orderbooks[len(orderbooks)-1]

	if len(first.Bids) == 0 || len(first.Asks) == 0 || len(last.Bids) == 0 || len(last.Asks) == 0 {
		return 0
	}

	// 중간가 계산
	firstBid, _ := parseFloat(first.Bids[0][0])
	firstAsk, _ := parseFloat(first.Asks[0][0])
	firstMid := (firstBid + firstAsk) / 2

	lastBid, _ := parseFloat(last.Bids[0][0])
	lastAsk, _ := parseFloat(last.Asks[0][0])
	lastMid := (lastBid + lastAsk) / 2

	if firstMid == 0 {
		return 0
	}

	return ((lastMid - firstMid) / firstMid) * 100
}

// createSimplePumpSignal 단순 펌핑 시그널 생성 (1초 가격 변동 기준)
func (sm *SignalManager) createSimplePumpSignal(
	symbol string,
	priceChangePercent float64,
	orderbooks []*memory.OrderbookSnapshot,
	trades []*memory.TradeData,
) *memory.AdvancedPumpSignal {
	// 기본 펌핑 시그널
	pumpSignal := memory.PumpSignal{
		Symbol:         symbol,
		Timestamp:      time.Now(),
		CompositeScore: priceChangePercent, // 점수를 가격 변동율로 설정
		Action:         sm.determineAction(priceChangePercent),
		Confidence:     sm.calculateConfidence(priceChangePercent),
		Volume:         sm.calculateVolumeChange(trades),
		PriceChange:    priceChangePercent, // 가격 변동율 직접 저장
	}

	// 최근 10개 체결 데이터 변환
	recentTrades := make([]memory.TradeData, 0, 10)
	start := len(trades) - 10
	if start < 0 {
		start = 0
	}
	for i := start; i < len(trades); i++ {
		recentTrades = append(recentTrades, *trades[i])
	}

	// 고급 펌핑 시그널
	advancedSignal := &memory.AdvancedPumpSignal{
		PumpSignal:    pumpSignal,
		OrderbookData: orderbooks[len(orderbooks)-1],
		TradeHistory:  recentTrades,
		Indicators: map[string]float64{
			"volume_change":       pumpSignal.Volume,
			"price_change":        pumpSignal.PriceChange,
			"orderbook_imbalance": sm.calculateOrderbookImbalance(orderbooks[len(orderbooks)-1]),
		},
	}

	return advancedSignal
}

// determineAction 액션 결정 (단순화 - 데이터 수집용)
func (sm *SignalManager) determineAction(score float64) string {
	return "PUMP_DETECTED" // 액션 결정은 외부에서 수행
}

// calculateConfidence 신뢰도 계산
func (sm *SignalManager) calculateConfidence(score float64) float64 {
	// 점수에 비례하여 신뢰도 계산 (최대 95%)
	confidence := score * 0.95
	if confidence > 95 {
		confidence = 95
	}
	return confidence
}

// handleListingSignals 상장공시 신호 처리
func (sm *SignalManager) handleListingSignals() {
	for signal := range sm.listingCallback {
		log.Printf("📢 상장공시 신호 수신: %s (신뢰도: %.2f%%)", signal.Symbol, signal.Confidence)

		// 🚨 핵심: 상장공시 시그널 발생 시 ±60초 범위 데이터 즉시 저장
		if err := sm.dataHandler.SaveListingSignalData(signal.Symbol, signal.Timestamp); err != nil {
			log.Printf("❌ 상장공시 데이터 저장 실패: %v", err)
		}

		// ±60초 데이터 수집 (기존 로직 유지)
		triggerTime := signal.Timestamp
		startTime := triggerTime.Add(-60 * time.Second)
		endTime := triggerTime.Add(60 * time.Second)

		// 오더북 데이터 수집 (±60초)
		orderbooks := sm.memManager.GetTimeRangeOrderbooks(signal.Symbol, startTime, endTime)

		// 체결 데이터 수집 (±60초)
		trades := sm.memManager.GetTimeRangeTrades(signal.Symbol, startTime, endTime)

		log.Printf("📊 상장공시 데이터 수집: %s (오더북 %d개, 체결 %d개)",
			signal.Symbol, len(orderbooks), len(trades))

		// 트리거 발생 (스냅샷 저장 포함)
		metadata := map[string]interface{}{
			"exchange":     signal.Exchange,
			"source":       signal.Source,
			"confidence":   signal.Confidence,
			"orderbooks":   len(orderbooks),
			"trades":       len(trades),
			"trigger_time": triggerTime,
		}

		sm.triggerManager.TriggerListingAnnouncement(
			signal.Symbol, signal.Confidence, metadata,
		)
	}
}

// TriggerListingSignal 상장공시 신호 트리거 (외부에서 호출)
func (sm *SignalManager) TriggerListingSignal(signal ListingSignal) {
	select {
	case sm.listingCallback <- signal:
		// 성공적으로 전송됨
		log.Printf("✅ 상장공시 신호 전송: %s", signal.Symbol)
	default:
		log.Printf("⚠️  상장공시 신호 버퍼 가득참: %s", signal.Symbol)
	}
}

// RegisterListingCallback 상장공시 콜백 등록
func (sm *SignalManager) RegisterListingCallback(callback ListingCallback) {
	// 이 메서드는 외부 콜백 관리자를 통해 처리됨
	log.Printf("📝 상장공시 콜백 등록 요청: %T", callback)
}

// GetSignalStats 시그널 통계 조회
func (sm *SignalManager) GetSignalStats() map[string]interface{} {
	recentSignals := sm.memManager.GetRecentSignals(100)

	stats := map[string]interface{}{
		"total_signals":   len(recentSignals),
		"pump_signals":    0,
		"listing_signals": 0,
		"avg_score":       0.0,
		"avg_confidence":  0.0,
	}

	if len(recentSignals) > 0 {
		totalScore := 0.0
		totalConfidence := 0.0

		for _, signal := range recentSignals {
			totalScore += signal.CompositeScore
			totalConfidence += signal.Confidence

			if signal.CompositeScore > 0 {
				stats["pump_signals"] = stats["pump_signals"].(int) + 1
			}
		}

		stats["avg_score"] = totalScore / float64(len(recentSignals))
		stats["avg_confidence"] = totalConfidence / float64(len(recentSignals))
	}

	return stats
}

// parseFloat 문자열을 float64로 변환
func parseFloat(s string) (float64, error) {
	var result float64
	_, err := fmt.Sscanf(s, "%f", &result)
	return result, err
}
