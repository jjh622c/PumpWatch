package signals

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"noticepumpcatch/internal/memory"
	"noticepumpcatch/internal/storage"
	"noticepumpcatch/internal/triggers"
)

// SignalManager 시그널 관리자
type SignalManager struct {
	memManager      *memory.Manager
	storageManager  *storage.StorageManager
	triggerManager  *triggers.Manager
	dataHandler     *storage.SignalDataHandler
	listingCallback chan ListingSignal
	config          *SignalConfig

	// 🔧 고루틴 누수 방지: 고루틴 관리 추가
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

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
	ctx, cancel := context.WithCancel(context.Background()) // 🔧 고루틴 누수 방지

	sm := &SignalManager{
		memManager:      memManager,
		storageManager:  storageManager,
		triggerManager:  triggerManager,
		dataHandler:     storage.NewSignalDataHandler(storageManager, memManager), // rawManager 제거됨
		listingCallback: make(chan ListingSignal, 100),
		config:          config,
		ctx:             ctx,    // 🔧 고루틴 누수 방지
		cancel:          cancel, // 🔧 고루틴 누수 방지
	}

	// 상장공시 콜백 처리 고루틴 시작
	sm.wg.Add(1) // 🔧 고루틴 누수 방지
	go func() {
		defer sm.wg.Done()
		sm.handleListingSignals()
	}()

	return sm
}

// Start 시그널 감지 시작
func (sm *SignalManager) Start() {
	log.Printf("🚨 시그널 감지 시작")

	// 펌핑 감지 활성화
	if sm.config.PumpDetection.Enabled {
		sm.wg.Add(1) // 🔧 고루틴 누수 방지
		go func() {
			defer sm.wg.Done()
			sm.pumpDetectionRoutine()
		}()
	}

	// 상장공시 감지 활성화
	if sm.config.Listing.Enabled {
		log.Printf("📢 상장공시 감지 활성화")
	}
}

// Stop 시그널 감지 중지 (🔧 고루틴 누수 방지)
func (sm *SignalManager) Stop() {
	log.Printf("🛑 시그널 감지 중지 시작")

	// 컨텍스트 취소로 모든 고루틴 정리
	if sm.cancel != nil {
		sm.cancel()
	}

	// 모든 고루틴 종료 대기 (타임아웃 설정)
	done := make(chan struct{})
	go func() {
		sm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("✅ 시그널 관리자: 모든 고루틴 정리 완료")
	case <-time.After(3 * time.Second):
		log.Printf("⚠️ 시그널 관리자: 고루틴 정리 타임아웃 (3초)")
	}

	// 채널 정리 (논블로킹)
	go func() {
		for {
			select {
			case <-sm.listingCallback:
			default:
				return
			}
		}
	}()

	log.Printf("🛑 시그널 감지 중지 완료")
}

// pumpDetectionRoutine 펌핑 감지 루틴
func (sm *SignalManager) pumpDetectionRoutine() {
	ticker := time.NewTicker(5 * time.Second) // 🔧 성능 최적화: 1초 → 5초로 변경
	defer ticker.Stop()

	for {
		select {
		case <-sm.ctx.Done(): // 🔧 고루틴 누수 방지: context 확인
			return
		case <-ticker.C:
			sm.detectPumpSignals()
		}
	}
}

// detectPumpSignals 펌핑 신호 감지
func (sm *SignalManager) detectPumpSignals() {
	symbols := sm.memManager.GetSymbols()

	// 🔧 성능 최적화: 로깅 대폭 축소 (30초마다 한 번만)
	now := time.Now()
	if now.Second()%30 == 0 {
		log.Printf("🔍 [DEBUG] 펌핑 감지: 심볼 %d개, 임계값 %.1f%%",
			len(symbols), sm.config.PumpDetection.PriceChangeThreshold)
	}

	for i, symbol := range symbols {
		// 🔧 성능 최적화: 디버그 로깅을 더욱 축소 (첫 3개 심볼만, 그리고 30초마다만)
		showDebug := i < 3 && now.Second()%30 == 0
		
		if showDebug {
			trades := sm.memManager.GetRecentTrades(symbol, 50) // 100 → 50으로 축소
			log.Printf("🔍 [DEBUG] 심볼 %s: 최근 체결 %d개", symbol, len(trades))
		}
		// 🔧 적응형 시간 윈도우: 충분한 체결 데이터 확보
		// 1. 기본 시간 윈도우 시도
		baseWindow := sm.config.PumpDetection.TimeWindowSeconds
		maxWindow := baseWindow * 3 // 🔧 성능 최적화: 5배 → 3배로 축소

		var filteredTrades []*memory.TradeData
		currentWindow := baseWindow

		// 충분한 체결 데이터가 있을 때까지 시간 윈도우 확장
		for currentWindow <= maxWindow {
			// 🔧 성능 최적화: 안전한 데이터 조회 (더욱 축소)
			tradeCount := currentWindow * 30 // 50 → 30으로 축소
			if tradeCount > 200 {            // 500 → 200으로 축소
				tradeCount = 200
			}
			trades := sm.memManager.GetRecentTrades(symbol, tradeCount)

			if len(trades) < 3 { // 5 → 3으로 축소
				break // 전체 체결 데이터가 너무 적음
			}

			// 현재 시간 윈도우로 필터링
			filteredTrades = sm.filterTradesByTimeWindow(trades, currentWindow)

			// 🎯 최소 2개 체결 확보되면 진행
			if len(filteredTrades) >= 2 {
				break
			}

			// 시간 윈도우 확장 (1초 → 2초 → 3초...)
			currentWindow++
		}

		// 🚨 충분한 체결 데이터 없으면 스킵
		if len(filteredTrades) < 2 {
			continue
		}

		// 체결 데이터 기반 가격 변동 계산
		priceChangePercent := sm.calculatePriceChangeFromTrades(filteredTrades)

		// 🔧 성능 최적화: 디버깅 로그를 더욱 축소 (중요한 것만)
		if showDebug && priceChangePercent > 0.1 { // 0.1% 이상 변동시만 로그
			log.Printf("🔍 [DEBUG] 심볼 %s: 가격변동 %.3f%%, 체결수 %d개, 윈도우 %d초",
				symbol, priceChangePercent, len(filteredTrades), currentWindow)
		}

		// 🎯 시간 윈도우 보정: 확장된 시간에 비례해서 임계값 조정
		adjustedThreshold := sm.config.PumpDetection.PriceChangeThreshold
		if currentWindow > baseWindow {
			// 시간이 늘어난 만큼 임계값도 비례 증가
			adjustedThreshold = adjustedThreshold * (float64(currentWindow) / float64(baseWindow))
		}

		// 🎯 핵심: 조정된 임계값 이상 상승 시에만 시그널 발생
		if priceChangePercent >= adjustedThreshold {
			// 🚨 펌핑 시그널 감지 로그 (확장 윈도우 정보 포함)
			windowInfo := ""
			if currentWindow > baseWindow {
				windowInfo = fmt.Sprintf(" (확장: %d초→%d초)", baseWindow, currentWindow)
			}

			log.Printf("🚨 [PUMP DETECTED] %s: +%.2f%% (%d초간 실제 체결 기준%s, 임계값: %.1f%%, 체결: %d건)",
				symbol, priceChangePercent, currentWindow, windowInfo,
				adjustedThreshold, len(filteredTrades))

			// 현재 가격 정보 (최신 체결 가격)
			if len(filteredTrades) > 0 {
				latestTrade := filteredTrades[len(filteredTrades)-1]
				latestPrice, _ := parseFloat(latestTrade.Price)
				log.Printf("📊 [PUMP INFO] %s: 최신체결가=%.8f, 체결량=%s, 매수/매도=%s",
					symbol, latestPrice, latestTrade.Quantity, latestTrade.Side)
			}

			// 오더북 데이터도 수집 (참고용)
			orderbooks := sm.memManager.GetRecentOrderbooks(symbol, 60)
			log.Printf("💾 [PUMP SAVE] %s: 체결 %d건, 오더북 %d건 데이터 수집", symbol, len(filteredTrades), len(orderbooks))

			// 펌핑 신호 생성 (체결 데이터 기반)
			signal := sm.createTradeBasedPumpSignal(symbol, priceChangePercent, orderbooks, filteredTrades)

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
				"trade_count":  len(filteredTrades),
				"time_window":  currentWindow,
				"threshold":    adjustedThreshold,
			}
			sm.triggerManager.TriggerPumpDetection(symbol, priceChangePercent, signal.Confidence, metadata)

			log.Printf("🚨 펌핑 감지: %s (%d초간 실제 체결: +%.2f%%, 임계값: %.1f%%)",
				symbol, currentWindow, priceChangePercent, adjustedThreshold)
		}
	}
}

// calculatePriceChangeInWindow 시간 윈도우 내 가격 변동율 계산 (기존 calculateOneSecondPriceChange)
func (sm *SignalManager) calculatePriceChangeInWindow(orderbooks []*memory.OrderbookSnapshot) float64 {
	if len(orderbooks) < 2 {
		return 0
	}

	// 시간 윈도우 시작과 끝의 오더북 중간가 비교
	first := orderbooks[0]
	last := orderbooks[len(orderbooks)-1]

	if len(first.Bids) == 0 || len(first.Asks) == 0 || len(last.Bids) == 0 || len(last.Asks) == 0 {
		return 0
	}

	// 중간가 계산 (에러 처리 추가)
	firstBid, err1 := parseFloat(first.Bids[0][0])
	firstAsk, err2 := parseFloat(first.Asks[0][0])
	if err1 != nil || err2 != nil {
		return 0 // 파싱 실패 시 변동 없음으로 처리
	}
	firstMid := (firstBid + firstAsk) / 2

	lastBid, err3 := parseFloat(last.Bids[0][0])
	lastAsk, err4 := parseFloat(last.Asks[0][0])
	if err3 != nil || err4 != nil {
		return 0 // 파싱 실패 시 변동 없음으로 처리
	}
	lastMid := (lastBid + lastAsk) / 2

	if firstMid == 0 {
		return 0
	}

	// 시간 윈도우 내 가격 변동율 계산 (양수만 반환)
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
	for {
		select {
		case <-sm.ctx.Done(): // 🔧 고루틴 누수 방지: context 확인
			return
		case signal := <-sm.listingCallback:
			sm.processListingSignal(signal)
		}
	}
}

// processListingSignal 상장공시 신호 처리 (🔧 고루틴 누수 방지를 위해 분리)
func (sm *SignalManager) processListingSignal(signal ListingSignal) {
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

// filterTradesByTimeWindow 시간 윈도우 내의 체결 데이터 필터링
func (sm *SignalManager) filterTradesByTimeWindow(trades []*memory.TradeData, windowSeconds int) []*memory.TradeData {
	if len(trades) == 0 {
		return []*memory.TradeData{}
	}

	cutoffTime := time.Now().Add(-time.Duration(windowSeconds) * time.Second)
	var filteredTrades []*memory.TradeData

	for _, trade := range trades {
		if trade.Timestamp.After(cutoffTime) {
			filteredTrades = append(filteredTrades, trade)
		}
	}

	return filteredTrades
}

// calculatePriceChangeFromTrades 체결 데이터에서 가격 변동율 계산
func (sm *SignalManager) calculatePriceChangeFromTrades(trades []*memory.TradeData) float64 {
	if len(trades) < 2 {
		return 0
	}

	// 시간순 정렬되어 있다고 가정하고 첫 번째와 마지막 체결 가격 비교
	firstTrade := trades[0]
	lastTrade := trades[len(trades)-1]

	// 가격 파싱 (에러 처리 포함)
	firstPrice, err1 := parseFloat(firstTrade.Price)
	lastPrice, err2 := parseFloat(lastTrade.Price)

	if err1 != nil || err2 != nil || firstPrice == 0 {
		return 0
	}

	// 가격 변동율 계산 (양수만 반환)
	changePercent := ((lastPrice - firstPrice) / firstPrice) * 100
	if changePercent < 0 {
		return 0 // 하락은 무시
	}

	return changePercent
}

// createTradeBasedPumpSignal 체결 기반 펌핑 시그널 생성
func (sm *SignalManager) createTradeBasedPumpSignal(
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
		Volume:         sm.calculateVolumeFromTrades(trades),
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
		OrderbookData: nil, // 체결 기반이므로 오더북은 선택적
		TradeHistory:  recentTrades,
		Indicators: map[string]float64{
			"volume_change":  pumpSignal.Volume,
			"price_change":   pumpSignal.PriceChange,
			"trade_count":    float64(len(trades)),
			"avg_trade_size": sm.calculateAvgTradeSize(trades),
		},
	}

	// 오더북 데이터가 있으면 추가
	if len(orderbooks) > 0 {
		advancedSignal.OrderbookData = orderbooks[len(orderbooks)-1]
		if advancedSignal.OrderbookData != nil {
			advancedSignal.Indicators["orderbook_imbalance"] = sm.calculateOrderbookImbalance(advancedSignal.OrderbookData)
		}
	}

	return advancedSignal
}

// calculateVolumeFromTrades 체결 데이터에서 거래량 계산
func (sm *SignalManager) calculateVolumeFromTrades(trades []*memory.TradeData) float64 {
	if len(trades) < 10 {
		return 0
	}

	// 최근 절반과 이전 절반 거래량 비교
	mid := len(trades) / 2
	recent := trades[mid:]
	previous := trades[:mid]

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

// calculateAvgTradeSize 평균 거래 크기 계산
func (sm *SignalManager) calculateAvgTradeSize(trades []*memory.TradeData) float64 {
	if len(trades) == 0 {
		return 0
	}

	totalVolume := 0.0
	validTrades := 0

	for _, trade := range trades {
		if qty, err := parseFloat(trade.Quantity); err == nil {
			totalVolume += qty
			validTrades++
		}
	}

	if validTrades == 0 {
		return 0
	}

	return totalVolume / float64(validTrades)
}
