package cache

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"noticepumpcatch/internal/memory"

	"github.com/dgraph-io/ristretto"
)

// CacheManager 고성능 메모리 캐시 매니저 (단순화 버전)
type CacheManager struct {
	tradeCache     *ristretto.Cache
	orderbookCache *ristretto.Cache

	ttl      time.Duration
	stopChan chan struct{}
}

// CacheKey 캐시 키 구조체
type CacheKey struct {
	Symbol    string
	Timestamp int64  // Unix nanoseconds
	Type      string // "trade" or "orderbook"
}

// String 캐시 키를 문자열로 변환
func (ck CacheKey) String() string {
	return fmt.Sprintf("%s:%s:%d", ck.Symbol, ck.Type, ck.Timestamp)
}

// NewCacheManager 새 캐시 매니저 생성 (단순화)
func NewCacheManager() (*CacheManager, error) {
	// 체결 데이터 캐시 (3MB, 2분 TTL)
	tradeCache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 30000,   // 3만개 카운터
		MaxCost:     3145728, // 3MB
		BufferItems: 64,      // 버퍼 크기
		Metrics:     true,    // 메트릭 활성화
	})
	if err != nil {
		return nil, fmt.Errorf("체결 캐시 생성 실패: %v", err)
	}

	// 오더북 데이터 캐시 (7MB, 2분 TTL)
	orderbookCache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 30000,   // 3만개 카운터
		MaxCost:     7340032, // 7MB
		BufferItems: 64,      // 버퍼 크기
		Metrics:     true,    // 메트릭 활성화
	})
	if err != nil {
		return nil, fmt.Errorf("오더북 캐시 생성 실패: %v", err)
	}

	cm := &CacheManager{
		tradeCache:     tradeCache,
		orderbookCache: orderbookCache,
		ttl:            2 * time.Minute, // 2분 TTL
		stopChan:       make(chan struct{}),
	}

	log.Printf("🚀 캐시 매니저 초기화 완료 (체결: 3MB, 오더북: 7MB, TTL: 2분)")
	return cm, nil
}

// AddTrade 체결 데이터를 캐시에 추가 (단순화)
func (cm *CacheManager) AddTrade(trade *memory.TradeData) error {
	// JSON 직렬화
	data, err := json.Marshal(trade)
	if err != nil {
		return fmt.Errorf("체결 데이터 직렬화 실패: %v", err)
	}

	// 캐시 키 생성
	key := CacheKey{
		Symbol:    trade.Symbol,
		Timestamp: trade.Timestamp.UnixNano(),
		Type:      "trade",
	}

	keyStr := key.String()

	// 캐시에 저장 (데이터 크기를 cost로 사용, TTL 적용)
	cost := int64(len(data))
	if !cm.tradeCache.SetWithTTL(keyStr, data, cost, cm.ttl) {
		// 캐시가 가득 찬 경우 무시 (Ristretto가 자동으로 관리)
		return nil
	}

	return nil
}

// AddOrderbook 오더북 데이터를 캐시에 추가 (단순화)
func (cm *CacheManager) AddOrderbook(orderbook *memory.OrderbookSnapshot) error {
	// JSON 직렬화
	data, err := json.Marshal(orderbook)
	if err != nil {
		return fmt.Errorf("오더북 데이터 직렬화 실패: %v", err)
	}

	// 캐시 키 생성
	key := CacheKey{
		Symbol:    orderbook.Symbol,
		Timestamp: orderbook.Timestamp.UnixNano(),
		Type:      "orderbook",
	}

	keyStr := key.String()

	// 캐시에 저장 (데이터 크기를 cost로 사용, TTL 적용)
	cost := int64(len(data))
	if !cm.orderbookCache.SetWithTTL(keyStr, data, cost, cm.ttl) {
		// 캐시가 가득 찬 경우 무시 (Ristretto가 자동으로 관리)
		return nil
	}

	return nil
}

// GetRecentTrades 최근 체결 데이터 조회 (단순화 - 브루트포스)
func (cm *CacheManager) GetRecentTrades(symbol string, duration time.Duration) ([]*memory.TradeData, error) {
	// 현재 시간에서 duration만큼 뒤로 가서 시작 시간 계산
	now := time.Now()
	startTime := now.Add(-duration)

	var trades []*memory.TradeData

	// 1초 간격으로 키를 생성해서 캐시에서 조회 (브루트포스)
	for t := startTime; t.Before(now); t = t.Add(time.Millisecond * 100) { // 100ms 간격
		key := CacheKey{
			Symbol:    symbol,
			Timestamp: t.UnixNano(),
			Type:      "trade",
		}

		if data, found := cm.tradeCache.Get(key.String()); found {
			if jsonData, ok := data.([]byte); ok {
				var trade memory.TradeData
				if err := json.Unmarshal(jsonData, &trade); err == nil {
					trades = append(trades, &trade)
				}
			}
		}
	}

	return trades, nil
}

// GetRecentOrderbooks 최근 오더북 데이터 조회 (단순화 - 브루트포스)
func (cm *CacheManager) GetRecentOrderbooks(symbol string, duration time.Duration) ([]*memory.OrderbookSnapshot, error) {
	// 현재 시간에서 duration만큼 뒤로 가서 시작 시간 계산
	now := time.Now()
	startTime := now.Add(-duration)

	var orderbooks []*memory.OrderbookSnapshot

	// 1초 간격으로 키를 생성해서 캐시에서 조회 (브루트포스)
	for t := startTime; t.Before(now); t = t.Add(time.Millisecond * 100) { // 100ms 간격
		key := CacheKey{
			Symbol:    symbol,
			Timestamp: t.UnixNano(),
			Type:      "orderbook",
		}

		if data, found := cm.orderbookCache.Get(key.String()); found {
			if jsonData, ok := data.([]byte); ok {
				var orderbook memory.OrderbookSnapshot
				if err := json.Unmarshal(jsonData, &orderbook); err == nil {
					orderbooks = append(orderbooks, &orderbook)
				}
			}
		}
	}

	return orderbooks, nil
}

// GetCacheStats 캐시 통계 조회 (단순화)
func (cm *CacheManager) GetCacheStats() map[string]interface{} {
	tradeMetrics := cm.tradeCache.Metrics
	orderbookMetrics := cm.orderbookCache.Metrics

	return map[string]interface{}{
		"trade_cache": map[string]interface{}{
			"hits":         tradeMetrics.Hits(),
			"misses":       tradeMetrics.Misses(),
			"keys_added":   tradeMetrics.KeysAdded(),
			"keys_evicted": tradeMetrics.KeysEvicted(),
			"cost_added":   tradeMetrics.CostAdded(),
			"cost_evicted": tradeMetrics.CostEvicted(),
		},
		"orderbook_cache": map[string]interface{}{
			"hits":         orderbookMetrics.Hits(),
			"misses":       orderbookMetrics.Misses(),
			"keys_added":   orderbookMetrics.KeysAdded(),
			"keys_evicted": orderbookMetrics.KeysEvicted(),
			"cost_added":   orderbookMetrics.CostAdded(),
			"cost_evicted": orderbookMetrics.CostEvicted(),
		},
	}
}

// Close 캐시 매니저 종료 (단순화)
func (cm *CacheManager) Close() {
	if cm.tradeCache != nil {
		cm.tradeCache.Close()
	}

	if cm.orderbookCache != nil {
		cm.orderbookCache.Close()
	}

	log.Printf("��️ 캐시 매니저 종료 완료")
}
