package models

import (
	"runtime"
	"strings"
	"time"
)

// CollectionEvent는 업비트 상장공지 시점 기준 -20초 데이터 수집 이벤트
type CollectionEvent struct {
	Symbol      string    `json:"symbol"`       // 상장 심볼 (예: "TIA")
	TriggerTime time.Time `json:"trigger_time"` // 상장공지 시점
	StartTime   time.Time `json:"start_time"`   // -20초 시점
	EndTime     time.Time `json:"end_time"`     // 수집 종료 시점 (TriggerTime)

	// 12개 완전 독립 슬라이스 - "무식하게 때려박기" 메모리 관리
	// 각 슬라이스는 완전히 독립적이므로 Mutex 불필요
	BinanceSpot    []TradeEvent `json:"binance_spot"`
	BinanceFutures []TradeEvent `json:"binance_futures"`
	OKXSpot        []TradeEvent `json:"okx_spot"`
	OKXFutures     []TradeEvent `json:"okx_futures"`
	KuCoinSpot     []TradeEvent `json:"kucoin_spot"`
	KuCoinFutures  []TradeEvent `json:"kucoin_futures"`
	PhemexSpot     []TradeEvent `json:"phemex_spot"`
	PhemexFutures  []TradeEvent `json:"phemex_futures"`
	GateSpot       []TradeEvent `json:"gate_spot"`
	GateFutures    []TradeEvent `json:"gate_futures"`
	BybitSpot      []TradeEvent `json:"bybit_spot"`
	BybitFutures   []TradeEvent `json:"bybit_futures"`

	// 메타데이터
	CollectionComplete bool      `json:"collection_complete"` // 수집 완료 여부
	CreatedAt          time.Time `json:"created_at"`          // 생성 시간
}

// NewCollectionEvent는 "무식하게 때려박기" 초기화를 수행
func NewCollectionEvent(symbol string, triggerTime time.Time) *CollectionEvent {
	const preAllocSize = 50000 // 거래소별 미리 할당할 용량

	return &CollectionEvent{
		Symbol:      symbol,
		TriggerTime: triggerTime,
		StartTime:   triggerTime.Add(-20 * time.Second), // -20초 시점
		EndTime:     triggerTime.Add(20 * time.Second),  // +20초 시점에서 종료 (총 40초)
		CreatedAt:   time.Now(),

		// 12개 독립 슬라이스 미리 할당 - "무식하게 때려박기"
		BinanceSpot:    make([]TradeEvent, 0, preAllocSize),
		BinanceFutures: make([]TradeEvent, 0, preAllocSize),
		OKXSpot:        make([]TradeEvent, 0, preAllocSize),
		OKXFutures:     make([]TradeEvent, 0, preAllocSize),
		KuCoinSpot:     make([]TradeEvent, 0, preAllocSize),
		KuCoinFutures:  make([]TradeEvent, 0, preAllocSize),
		PhemexSpot:     make([]TradeEvent, 0, preAllocSize),
		PhemexFutures:  make([]TradeEvent, 0, preAllocSize),
		GateSpot:       make([]TradeEvent, 0, preAllocSize),
		GateFutures:    make([]TradeEvent, 0, preAllocSize),
		BybitSpot:      make([]TradeEvent, 0, preAllocSize),
		BybitFutures:   make([]TradeEvent, 0, preAllocSize),
	}
}

// AddTrade는 거래소별 독립 슬라이스에 거래 데이터 추가 (Mutex 없음)
func (ce *CollectionEvent) AddTrade(trade TradeEvent) {
	// 시간 필터링: -20초 ~ +20초 범위 (총 40초) 수집
	if trade.Timestamp < ce.StartTime.UnixMilli() || trade.Timestamp > ce.EndTime.UnixMilli() {
		return
	}

	// 심볼 필터링: 상장 공고된 심볼과 일치하는 거래만 수집
	if !ce.isTargetSymbol(trade.Symbol) {
		return
	}

	// 거래소별 독립 슬라이스에 직접 추가 - 동시성 문제 없음
	switch trade.Exchange {
	case "binance":
		if trade.MarketType == "spot" {
			ce.BinanceSpot = append(ce.BinanceSpot, trade)
		} else {
			ce.BinanceFutures = append(ce.BinanceFutures, trade)
		}
	case "okx":
		if trade.MarketType == "spot" {
			ce.OKXSpot = append(ce.OKXSpot, trade)
		} else {
			ce.OKXFutures = append(ce.OKXFutures, trade)
		}
	case "kucoin":
		if trade.MarketType == "spot" {
			ce.KuCoinSpot = append(ce.KuCoinSpot, trade)
		} else {
			ce.KuCoinFutures = append(ce.KuCoinFutures, trade)
		}
	case "phemex":
		if trade.MarketType == "spot" {
			ce.PhemexSpot = append(ce.PhemexSpot, trade)
		} else {
			ce.PhemexFutures = append(ce.PhemexFutures, trade)
		}
	case "gate":
		if trade.MarketType == "spot" {
			ce.GateSpot = append(ce.GateSpot, trade)
		} else {
			ce.GateFutures = append(ce.GateFutures, trade)
		}
	case "bybit":
		if trade.MarketType == "spot" {
			ce.BybitSpot = append(ce.BybitSpot, trade)
		} else {
			ce.BybitFutures = append(ce.BybitFutures, trade)
		}
	}
}

// GetTotalTradeCount는 수집된 전체 거래 수 반환
func (ce *CollectionEvent) GetTotalTradeCount() int {
	return len(ce.BinanceSpot) + len(ce.BinanceFutures) +
		len(ce.OKXSpot) + len(ce.OKXFutures) +
		len(ce.KuCoinSpot) + len(ce.KuCoinFutures) +
		len(ce.PhemexSpot) + len(ce.PhemexFutures) +
		len(ce.GateSpot) + len(ce.GateFutures) +
		len(ce.BybitSpot) + len(ce.BybitFutures)
}

// GetExchangeStats는 거래소별 수집 통계 반환
func (ce *CollectionEvent) GetExchangeStats() map[string]ExchangeStats {
	return map[string]ExchangeStats{
		"binance": {
			SpotTrades:    len(ce.BinanceSpot),
			FuturesTrades: len(ce.BinanceFutures),
			FirstTrade:    ce.getFirstTradeTime(ce.BinanceSpot, ce.BinanceFutures),
			LastTrade:     ce.getLastTradeTime(ce.BinanceSpot, ce.BinanceFutures),
			Connected:     len(ce.BinanceSpot)+len(ce.BinanceFutures) > 0,
		},
		"okx": {
			SpotTrades:    len(ce.OKXSpot),
			FuturesTrades: len(ce.OKXFutures),
			FirstTrade:    ce.getFirstTradeTime(ce.OKXSpot, ce.OKXFutures),
			LastTrade:     ce.getLastTradeTime(ce.OKXSpot, ce.OKXFutures),
			Connected:     len(ce.OKXSpot)+len(ce.OKXFutures) > 0,
		},
		"kucoin": {
			SpotTrades:    len(ce.KuCoinSpot),
			FuturesTrades: len(ce.KuCoinFutures),
			FirstTrade:    ce.getFirstTradeTime(ce.KuCoinSpot, ce.KuCoinFutures),
			LastTrade:     ce.getLastTradeTime(ce.KuCoinSpot, ce.KuCoinFutures),
			Connected:     len(ce.KuCoinSpot)+len(ce.KuCoinFutures) > 0,
		},
		"phemex": {
			SpotTrades:    len(ce.PhemexSpot),
			FuturesTrades: len(ce.PhemexFutures),
			FirstTrade:    ce.getFirstTradeTime(ce.PhemexSpot, ce.PhemexFutures),
			LastTrade:     ce.getLastTradeTime(ce.PhemexSpot, ce.PhemexFutures),
			Connected:     len(ce.PhemexSpot)+len(ce.PhemexFutures) > 0,
		},
		"gate": {
			SpotTrades:    len(ce.GateSpot),
			FuturesTrades: len(ce.GateFutures),
			FirstTrade:    ce.getFirstTradeTime(ce.GateSpot, ce.GateFutures),
			LastTrade:     ce.getLastTradeTime(ce.GateSpot, ce.GateFutures),
			Connected:     len(ce.GateSpot)+len(ce.GateFutures) > 0,
		},
		"bybit": {
			SpotTrades:    len(ce.BybitSpot),
			FuturesTrades: len(ce.BybitFutures),
			FirstTrade:    ce.getFirstTradeTime(ce.BybitSpot, ce.BybitFutures),
			LastTrade:     ce.getLastTradeTime(ce.BybitSpot, ce.BybitFutures),
			Connected:     len(ce.BybitSpot)+len(ce.BybitFutures) > 0,
		},
	}
}

// Cleanup은 30초 후 완전한 메모리 해제를 수행
func (ce *CollectionEvent) Cleanup() {
	// 모든 슬라이스를 명시적으로 nil 처리
	ce.BinanceSpot = nil
	ce.BinanceFutures = nil
	ce.OKXSpot = nil
	ce.OKXFutures = nil
	ce.KuCoinSpot = nil
	ce.KuCoinFutures = nil
	ce.PhemexSpot = nil
	ce.PhemexFutures = nil
	ce.GateSpot = nil
	ce.GateFutures = nil
	ce.BybitSpot = nil
	ce.BybitFutures = nil

	// 강제 가비지 컬렉션
	runtime.GC()
}

// 헬퍼 메서드들
func (ce *CollectionEvent) getFirstTradeTime(spot, futures []TradeEvent) int64 {
	var firstTime int64 = 0

	if len(spot) > 0 {
		firstTime = spot[0].Timestamp
	}
	if len(futures) > 0 && (firstTime == 0 || futures[0].Timestamp < firstTime) {
		firstTime = futures[0].Timestamp
	}

	return firstTime
}

func (ce *CollectionEvent) getLastTradeTime(spot, futures []TradeEvent) int64 {
	var lastTime int64 = 0

	if len(spot) > 0 {
		lastTime = spot[len(spot)-1].Timestamp
	}
	if len(futures) > 0 && futures[len(futures)-1].Timestamp > lastTime {
		lastTime = futures[len(futures)-1].Timestamp
	}

	return lastTime
}

// isTargetSymbol은 거래 심볼이 상장 공고된 타겟 심볼과 일치하는지 확인
func (ce *CollectionEvent) isTargetSymbol(tradeSymbol string) bool {
	targetSymbol := strings.ToUpper(ce.Symbol)
	tradeSymbol = strings.ToUpper(tradeSymbol)

	// 1. 정확한 일치 (SOMI == SOMI)
	if targetSymbol == tradeSymbol {
		return true
	}

	// 2. USDT 페어 매칭 (SOMI -> SOMIUSDT)
	if tradeSymbol == targetSymbol+"USDT" {
		return true
	}

	// 3. 거래소별 구분자 포함 형식 매칭
	// SOMI -> SOMI-USDT (KuCoin, OKX)
	if tradeSymbol == targetSymbol+"-USDT" {
		return true
	}

	// SOMI -> SOMI_USDT (Gate.io)
	if tradeSymbol == targetSymbol+"_USDT" {
		return true
	}

	// 4. 역방향 매칭: 거래소 형식에서 기본 심볼 추출
	// SOMIUSDT -> SOMI
	if strings.HasSuffix(tradeSymbol, "USDT") {
		baseSymbol := strings.TrimSuffix(tradeSymbol, "USDT")
		baseSymbol = strings.TrimSuffix(baseSymbol, "-")  // SOMI-USDT -> SOMI
		baseSymbol = strings.TrimSuffix(baseSymbol, "_")  // SOMI_USDT -> SOMI
		if baseSymbol == targetSymbol {
			return true
		}
	}

	// 5. Phemex spot 형식 (sSOMIUSDT -> SOMI)
	if strings.HasPrefix(tradeSymbol, "S") && len(tradeSymbol) > 1 {
		phemexSymbol := strings.TrimPrefix(tradeSymbol, "S")
		if strings.HasSuffix(phemexSymbol, "USDT") {
			baseSymbol := strings.TrimSuffix(phemexSymbol, "USDT")
			if baseSymbol == targetSymbol {
				return true
			}
		}
	}

	return false
}
