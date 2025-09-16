package models

import "strings"

// TradeEvent는 모든 거래소의 체결 데이터를 통합한 표준 모델
type TradeEvent struct {
	// 메타데이터
	Exchange   string `json:"exchange"`    // "binance", "okx", "kucoin", "phemex", "gate", "bybit"
	MarketType string `json:"market_type"` // "spot" | "futures"
	Symbol     string `json:"symbol"`      // "BTCUSDT", "BTC-USDT" 등

	// 거래 데이터
	TradeID  string `json:"trade_id"` // 거래소별 고유 ID
	Price    string `json:"price"`    // "50000.00" (string으로 정밀도 유지)
	Quantity string `json:"quantity"` // "0.1" (string으로 정밀도 유지)
	Side     string `json:"side"`     // "buy" | "sell"

	// 타임스탬프 (모두 Unix milliseconds)
	Timestamp   int64 `json:"timestamp"`    // 거래 발생 시간
	CollectedAt int64 `json:"collected_at"` // 수집 시점
	TriggerTime int64 `json:"trigger_time"` // 상장공고 시점

	// 추가 메타데이터 (선택사항)
	IsBuyerMaker bool   `json:"is_buyer_maker,omitempty"` // 매수자가 메이커인지 여부
	QuoteQty     string `json:"quote_qty,omitempty"`      // 견적 수량
}

// NewTradeEvent는 TradeEvent를 생성하고 수집 시점을 자동 설정
func NewTradeEvent(exchange, marketType, symbol string) *TradeEvent {
	return &TradeEvent{
		Exchange:    exchange,
		MarketType:  marketType,
		Symbol:      symbol,
		CollectedAt: GetCurrentUnixMilli(),
	}
}

// IsValidTimeRange는 해당 거래가 수집 대상 시간 범위에 있는지 확인
func (te *TradeEvent) IsValidTimeRange(startTime, endTime int64) bool {
	return te.Timestamp >= startTime && te.Timestamp <= endTime
}

// GetRelativeTime은 상장공고 시점 대비 상대 시간(초)을 반환
func (te *TradeEvent) GetRelativeTime() int64 {
	if te.TriggerTime == 0 {
		return 0
	}
	return (te.Timestamp - te.TriggerTime) / 1000 // milliseconds to seconds
}

// SetTriggerTime은 상장공고 시점을 설정
func (te *TradeEvent) SetTriggerTime(triggerTime int64) {
	te.TriggerTime = triggerTime
}

// Normalize는 거래소별 데이터 형식을 표준화
func (te *TradeEvent) Normalize() {
	// Symbol 표준화 (모든 거래소에서 BTCUSDT 형식으로)
	te.Symbol = normalizeSymbol(te.Exchange, te.Symbol)

	// Side 표준화 (모든 거래소에서 "buy"/"sell"로)
	te.Side = normalizeSide(te.Exchange, te.Side)
}

// 거래소별 심볼 표준화 함수
func normalizeSymbol(exchange, symbol string) string {
	switch exchange {
	case "okx":
		// BTC-USDT -> BTCUSDT
		return normalizeOKXSymbol(symbol)
	case "kucoin":
		// BTC-USDT -> BTCUSDT
		return normalizeKuCoinSymbol(symbol)
	case "phemex":
		// sBTCUSDT -> BTCUSDT (spot)
		// BTCUSD -> BTCUSDT (futures)
		return normalizePhemexSymbol(symbol)
	case "gate":
		// BTC_USDT -> BTCUSDT
		return normalizeGateSymbol(symbol)
	case "binance", "bybit":
		// 이미 BTCUSDT 형식
		return symbol
	default:
		return symbol
	}
}

// 거래소별 거래 방향 표준화 함수
func normalizeSide(exchange, side string) string {
	switch exchange {
	case "binance", "okx", "kucoin", "gate", "bybit":
		// 대부분 "buy"/"sell" 사용
		return side
	case "phemex":
		// "Buy"/"Sell" -> "buy"/"sell"
		if side == "Buy" {
			return "buy"
		} else if side == "Sell" {
			return "sell"
		}
		return side
	default:
		return side
	}
}

// 거래소별 심볼 표준화 구현 함수들
func normalizeOKXSymbol(symbol string) string {
	// BTC-USDT -> BTCUSDT
	// BTC-USDT-SWAP -> BTCUSDT
	if len(symbol) > 8 && symbol[len(symbol)-5:] == "-SWAP" {
		symbol = symbol[:len(symbol)-5] // SWAP 제거
	}
	return strings.ReplaceAll(symbol, "-", "")
}

func normalizeKuCoinSymbol(symbol string) string {
	// BTC-USDT -> BTCUSDT
	// XBTUSDTM -> BTCUSDT (futures)
	if strings.HasSuffix(symbol, "M") && len(symbol) > 6 {
		// futures 심볼 처리
		if strings.HasPrefix(symbol, "XBT") {
			return "BTC" + symbol[3:len(symbol)-1]
		}
	}
	return strings.ReplaceAll(symbol, "-", "")
}

func normalizePhemexSymbol(symbol string) string {
	// sBTCUSDT -> BTCUSDT (spot)
	// BTCUSD -> BTCUSDT (futures)
	if strings.HasPrefix(symbol, "s") && len(symbol) > 1 {
		return symbol[1:] // 's' 접두사 제거
	}
	if strings.HasSuffix(symbol, "USD") && !strings.HasSuffix(symbol, "USDT") {
		return symbol + "T" // BTCUSD -> BTCUSDT
	}
	return symbol
}

func normalizeGateSymbol(symbol string) string {
	// BTC_USDT -> BTCUSDT
	return strings.ReplaceAll(symbol, "_", "")
}
