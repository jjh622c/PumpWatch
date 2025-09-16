package models

import "time"

// PumpEvent는 탐지된 펌프 패턴을 나타내는 모델
type PumpEvent struct {
	// 기본 정보
	Exchange   string `json:"exchange"`    // "binance", "okx", etc.
	MarketType string `json:"market_type"` // "spot" | "futures"
	Symbol     string `json:"symbol"`      // "BTCUSDT"

	// 시간 정보 (Unix milliseconds)
	StartTime    int64 `json:"start_time"`    // 펌프 시작 시간
	EndTime      int64 `json:"end_time"`      // 펌프 종료 시간
	Duration     int64 `json:"duration_ms"`   // 지속 시간 (밀리초)
	TriggerTime  int64 `json:"trigger_time"`  // 상장공고 시점
	RelativeTime int64 `json:"relative_time"` // 공고 대비 상대시간 (초)

	// 가격 정보 (string으로 정밀도 유지)
	StartPrice  string `json:"start_price"`  // 펀프 시작 가격
	PeakPrice   string `json:"peak_price"`   // 최고 가격
	EndPrice    string `json:"end_price"`    // 펀프 종료 가격
	PriceChange string `json:"price_change"` // 가격 변동률 (%)
	PeakChange  string `json:"peak_change"`  // 최고점 대비 변동률 (%)

	// 거래량 정보
	Volume     string `json:"volume"`      // 해당 구간 총 거래량
	QuoteVol   string `json:"quote_vol"`   // 견적 자산 거래량
	TradeCount int    `json:"trade_count"` // 총 체결 건수
	AvgPrice   string `json:"avg_price"`   // 평균 체결 가격

	// 펌프 분석 메타데이터
	PumpType     string  `json:"pump_type"`     // "sudden", "gradual", "spike"
	Confidence   float64 `json:"confidence"`    // 신뢰도 (0-1)
	Intensity    float64 `json:"intensity"`     // 강도 (가격변동률 + 거래량 기반)
	VolumeRatio  float64 `json:"volume_ratio"`  // 평소 대비 거래량 배수
	SpreadImpact float64 `json:"spread_impact"` // 호가 스프레드 영향

	// 추가 분석 데이터
	TradeEvents []TradeEvent `json:"trade_events,omitempty"` // 해당 구간 거래 내역
	CreatedAt   time.Time    `json:"created_at"`             // 분석 생성 시간
}

// NewPumpEvent는 기본 PumpEvent를 생성
func NewPumpEvent(exchange, marketType, symbol string, triggerTime int64) *PumpEvent {
	return &PumpEvent{
		Exchange:    exchange,
		MarketType:  marketType,
		Symbol:      symbol,
		TriggerTime: triggerTime,
		CreatedAt:   time.Now(),
		TradeEvents: make([]TradeEvent, 0),
	}
}

// SetTimeRange는 펌프 시간 범위를 설정
func (pe *PumpEvent) SetTimeRange(startTime, endTime int64) {
	pe.StartTime = startTime
	pe.EndTime = endTime
	pe.Duration = endTime - startTime
	pe.RelativeTime = (startTime - pe.TriggerTime) / 1000 // milliseconds to seconds
}

// SetPriceRange는 펌프 가격 범위를 설정
func (pe *PumpEvent) SetPriceRange(startPrice, peakPrice, endPrice string) {
	pe.StartPrice = startPrice
	pe.PeakPrice = peakPrice
	pe.EndPrice = endPrice

	// 가격 변동률 계산
	pe.PriceChange = calculatePercentChange(startPrice, endPrice)
	pe.PeakChange = calculatePercentChange(startPrice, peakPrice)
}

// AddTradeEvents는 펌프 구간의 거래 내역을 추가
func (pe *PumpEvent) AddTradeEvents(trades []TradeEvent) {
	pe.TradeEvents = trades
	pe.TradeCount = len(trades)

	// 거래량 및 평균가 계산
	pe.calculateVolumeStats()
}

// CalculateIntensity는 펌프 강도를 계산 (가격변동 + 거래량 기반)
func (pe *PumpEvent) CalculateIntensity() {
	priceChangeFloat := parseFloat(pe.PriceChange)
	volumeRatioFloat := pe.VolumeRatio

	// 강도 = (가격변동률 * 0.7) + (거래량배수 * 0.3)
	pe.Intensity = (priceChangeFloat * 0.7) + (volumeRatioFloat * 0.3)
}

// IsSignificant는 유의미한 펌프인지 판단
func (pe *PumpEvent) IsSignificant(minPriceChange float64, minDuration int64) bool {
	priceChange := parseFloat(pe.PriceChange)
	return priceChange >= minPriceChange && pe.Duration >= minDuration
}

// GetTimeWindow는 펌프가 발생한 시간 윈도우를 반환 (상장공고 기준)
func (pe *PumpEvent) GetTimeWindow() string {
	relTime := pe.RelativeTime
	switch {
	case relTime <= -15:
		return "early_pump" // -20초 ~ -15초
	case relTime <= -10:
		return "mid_pump" // -15초 ~ -10초
	case relTime <= -5:
		return "late_pump" // -10초 ~ -5초
	case relTime <= 0:
		return "final_pump" // -5초 ~ 0초
	default:
		return "post_pump" // 0초 이후
	}
}

// 내부 헬퍼 메서드들
func (pe *PumpEvent) calculateVolumeStats() {
	if len(pe.TradeEvents) == 0 {
		return
	}

	totalVolume := 0.0
	totalQuoteVol := 0.0
	totalValue := 0.0

	for _, trade := range pe.TradeEvents {
		qty := parseFloat(trade.Quantity)
		price := parseFloat(trade.Price)

		totalVolume += qty
		totalQuoteVol += qty * price
		totalValue += price * qty
	}

	pe.Volume = formatFloat(totalVolume)
	pe.QuoteVol = formatFloat(totalQuoteVol)

	if totalVolume > 0 {
		pe.AvgPrice = formatFloat(totalValue / totalVolume)
	}
}

// PumpSummary는 여러 펌프 이벤트의 요약 정보
type PumpSummary struct {
	Symbol          string `json:"symbol"`
	TriggerTime     int64  `json:"trigger_time"`
	CollectionStart int64  `json:"collection_start"`
	CollectionEnd   int64  `json:"collection_end"`

	TotalPumps      int            `json:"total_pumps"`
	ExchangePumps   map[string]int `json:"exchange_pumps"`    // 거래소별 펌프 수
	MarketTypePumps map[string]int `json:"market_type_pumps"` // 현물/선물별 펌프 수
	TimeWindowPumps map[string]int `json:"time_window_pumps"` // 시간대별 펌프 수

	MaxPriceChange  string `json:"max_price_change"`  // 최대 가격 변동률
	MaxPumpExchange string `json:"max_pump_exchange"` // 최대 펌프 거래소
	FirstPumpTime   int64  `json:"first_pump_time"`   // 첫 펌프 시간
	LastPumpTime    int64  `json:"last_pump_time"`    // 마지막 펌프 시간

	AvgIntensity float64 `json:"avg_intensity"` // 평균 펌프 강도
	TotalVolume  string  `json:"total_volume"`  // 총 거래량

	CreatedAt time.Time `json:"created_at"`
}
