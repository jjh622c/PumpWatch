package buffer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"PumpWatch/internal/models"
)

const (
	// 20분 = 1200초 = 1200 버켓 (1초당 1버켓)
	CircularBufferDuration = 20 * time.Minute
	BucketCount           = int64(CircularBufferDuration / time.Second) // 1200
	HotCacheSeconds       = 120                                         // 2분 핫 캐시
	BatchWriteSize        = 1000                                        // 배치 쓰기 크기
	BatchFlushInterval    = 10 * time.Millisecond                      // 10ms 배치 주기
)

// TimeBucket: 1초 단위 시간 버켓
type TimeBucket struct {
	timestamp int64                            // 버켓 시간 (Unix나노초)
	trades    map[string][]models.TradeEvent // 거래소별 체결 데이터
	mutex     sync.RWMutex                   // 버켓별 동시성 제어
}

// TradeSlice: 핫 캐시용 거래 슬라이스
type TradeSlice struct {
	trades   []models.TradeEvent
	lastUsed int64 // 마지막 접근 시간
}

// WriteRequest: 배치 쓰기 요청
type WriteRequest struct {
	Exchange string
	Trade    models.TradeEvent
}

// CircularBufferStats: 순환 버퍼 통계
type CircularBufferStats struct {
	MemoryUsage    int64   // 메모리 사용량 (바이트)
	TotalEvents    int64   // 총 이벤트 수
	HotEvents      int64   // 핫 캐시 이벤트 수
	ColdEvents     int64   // 콜드 버켓 이벤트 수
	HotCacheHits   int64   // 핫 캐시 히트 수
	ColdBufferHits int64   // 콜드 버퍼 히트 수
	CompressionRate float64 // 압축률 (미사용)
}

// FastAccessManager: 상장 시나리오 최적화
type FastAccessManager struct {
	timeIndex     map[int64]int64               // 시간 -> 버켓 빠른 매핑 (나노초 -> 버켓인덱스)
	exchangeIndex map[string]*ExchangeMetadata // 거래소별 메타데이터
	mutex         sync.RWMutex
}

// ExchangeMetadata: 거래소별 메타데이터
type ExchangeMetadata struct {
	totalTrades   int64
	lastTradeTime int64
	avgTradePer5s float64
}

// CircularTradeBuffer: 20분 순환 버퍼 핵심 구조
type CircularTradeBuffer struct {
	// 시간 기반 버켓 인덱싱 (1초 = 1버켓)
	buckets       []*TimeBucket // 1200개 버켓
	currentBucket int64         // 현재 버켓 인덱스
	startTime     int64         // 버퍼 시작 시간 (Unix나노초)

	// 빠른 접근을 위한 핫 캐시
	hotCache       map[string]*TradeSlice // 최근 2분 데이터
	hotCacheExpiry int64                  // 핫 캐시 만료 시간

	// 동시성 제어
	rwMutex   sync.RWMutex
	writeChan chan WriteRequest // 배치 쓰기 채널

	// Fast Access Manager
	fastAccess *FastAccessManager

	// 성능 통계
	stats CircularBufferStats
	mutex sync.RWMutex // 통계용 뮤텍스

	// 생명주기 관리
	ctx    context.Context
	cancel context.CancelFunc
	closed bool
}

// NewCircularTradeBuffer: 20분 순환 버퍼 생성
func NewCircularTradeBuffer(parentCtx context.Context) *CircularTradeBuffer {
	ctx, cancel := context.WithCancel(parentCtx)
	now := time.Now().UnixNano()

	// 버켓 초기화
	buckets := make([]*TimeBucket, BucketCount)
	for i := int64(0); i < BucketCount; i++ {
		buckets[i] = &TimeBucket{
			timestamp: now + (i * int64(time.Second)), // 1초 간격
			trades:    make(map[string][]models.TradeEvent),
		}
	}

	buffer := &CircularTradeBuffer{
		buckets:       buckets,
		currentBucket: 0,
		startTime:     now,
		hotCache:      make(map[string]*TradeSlice),
		writeChan:     make(chan WriteRequest, 10), // 작은 채널로 directWrite 유도
		fastAccess: &FastAccessManager{
			timeIndex:     make(map[int64]int64),
			exchangeIndex: make(map[string]*ExchangeMetadata),
		},
		ctx:    ctx,
		cancel: cancel,
	}

	// 배치 쓰기 고루틴 시작
	go buffer.processBatchWrites()

	// 핫 캐시 유지 고루틴 시작
	go buffer.maintainHotCache()

	return buffer
}

// StoreTradeEvent: 거래 이벤트 저장 (배치 쓰기)
func (ctb *CircularTradeBuffer) StoreTradeEvent(exchange string, trade models.TradeEvent) error {
	// 🔍 SOMI 호출 디버깅 (모든 SOMI 데이터)
	if strings.Contains(strings.ToUpper(trade.Symbol), "SOMI") {
		fmt.Printf("🔍 [StoreTradeEvent] Called with %s, Symbol: %s, Closed: %v\n",
			exchange, trade.Symbol, ctb.closed)
	}

	if ctb.closed {
		fmt.Printf("❌ [StoreTradeEvent] Buffer is closed, returning error\n")
		return fmt.Errorf("circular trade buffer is closed")
	}

	// 🔧 임시 수정: 테스트를 위해 직접 쓰기 강제 사용
	return ctb.directWrite(exchange, trade)

	// // 배치 쓰기 채널에 추가
	// select {
	// case ctb.writeChan <- WriteRequest{Exchange: exchange, Trade: trade}:
	// 	return nil
	// case <-ctb.ctx.Done():
	// 	return fmt.Errorf("circular trade buffer is shutting down")
	// default:
	// 	// 채널이 가득 찬 경우 직접 쓰기
	// 	return ctb.directWrite(exchange, trade)
	// }
}

// directWrite: 직접 쓰기 (배치 채널이 가득 찬 경우)
func (ctb *CircularTradeBuffer) directWrite(exchange string, trade models.TradeEvent) error {
	bucketIdx := ctb.GetBucketIndex(trade.Timestamp)

	// 🔍 SOMI 데이터 저장 디버깅 (모든 SOMI 데이터)
	if strings.Contains(strings.ToUpper(trade.Symbol), "SOMI") {
		fmt.Printf("🔍 [DirectWrite] %s SOMI -> Bucket %d (timestamp=%d)\n",
			exchange, bucketIdx, trade.Timestamp)
	}

	bucket := ctb.buckets[bucketIdx]

	bucket.mutex.Lock()
	defer bucket.mutex.Unlock()

	// 거래소별 슬라이스에 추가
	if bucket.trades[exchange] == nil {
		bucket.trades[exchange] = make([]models.TradeEvent, 0, 100)
	}
	bucket.trades[exchange] = append(bucket.trades[exchange], trade)

	// 통계 업데이트
	ctb.mutex.Lock()
	ctb.stats.TotalEvents++
	ctb.mutex.Unlock()

	return nil
}

// processBatchWrites: 배치 쓰기 처리 고루틴
func (ctb *CircularTradeBuffer) processBatchWrites() {
	batch := make([]WriteRequest, 0, BatchWriteSize)
	ticker := time.NewTicker(BatchFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case req := <-ctb.writeChan:
			batch = append(batch, req)
			if len(batch) >= BatchWriteSize {
				ctb.flushBatch(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				ctb.flushBatch(batch)
				batch = batch[:0]
			}
		case <-ctb.ctx.Done():
			// 마지막 배치 플러시
			if len(batch) > 0 {
				ctb.flushBatch(batch)
			}
			return
		}
	}
}

// flushBatch: 배치 데이터 플러시
func (ctb *CircularTradeBuffer) flushBatch(batch []WriteRequest) {
	// 거래소별로 그룹핑
	exchangeGroups := make(map[string][]WriteRequest)
	for _, req := range batch {
		exchangeGroups[req.Exchange] = append(exchangeGroups[req.Exchange], req)
	}

	// 거래소별 병렬 처리
	var wg sync.WaitGroup
	for exchange, reqs := range exchangeGroups {
		wg.Add(1)
		go func(ex string, requests []WriteRequest) {
			defer wg.Done()
			ctb.flushExchangeBatch(ex, requests)
		}(exchange, reqs)
	}
	wg.Wait()
}

// flushExchangeBatch: 특정 거래소 배치 플러시
func (ctb *CircularTradeBuffer) flushExchangeBatch(exchange string, requests []WriteRequest) {
	// 버켓별로 그룹핑
	bucketGroups := make(map[int64][]models.TradeEvent)
	for _, req := range requests {
		bucketIdx := ctb.GetBucketIndex(req.Trade.Timestamp)
		bucketGroups[bucketIdx] = append(bucketGroups[bucketIdx], req.Trade)
	}

	// 버켓별로 쓰기
	for bucketIdx, trades := range bucketGroups {
		bucket := ctb.buckets[bucketIdx]
		bucket.mutex.Lock()

		if bucket.trades[exchange] == nil {
			bucket.trades[exchange] = make([]models.TradeEvent, 0, len(trades))
		}
		bucket.trades[exchange] = append(bucket.trades[exchange], trades...)

		bucket.mutex.Unlock()
	}

	// 통계 업데이트
	ctb.mutex.Lock()
	ctb.stats.TotalEvents += int64(len(requests))
	ctb.mutex.Unlock()
}

// GetBucketIndex: 절대 시간 기준 버켓 인덱스 계산 (동일 timestamp → 동일 bucket 보장)
func (ctb *CircularTradeBuffer) GetBucketIndex(timestamp int64) int64 {
	var seconds int64

	// 입력 형식 자동 감지: 밀리초 vs 나노초
	if timestamp > 1e15 {
		// 나노초 형식 (1e15 이상, 2001년 9월 이후)
		seconds = timestamp / int64(time.Second)
	} else {
		// 밀리초 형식 (1e15 미만)
		seconds = timestamp / 1000
	}

	// 🔍 디버그: 변환 과정
	fmt.Printf("🔍 [GetBucketIndex] timestamp=%d -> seconds=%d -> bucket=%d\n",
		timestamp, seconds, seconds%BucketCount)

	// 1200개 버켓에 순환 매핑 (20분 = 1200초)
	// 동일한 timestamp는 항상 동일한 버켓에 매핑됨
	bucketIndex := seconds % BucketCount

	// 항상 유효한 인덱스 보장 (0 ~ 1199)
	if bucketIndex < 0 {
		bucketIndex += BucketCount
	}

	return bucketIndex
}

// GetTradeEvents: 특정 시간 범위의 거래 데이터 조회
func (ctb *CircularTradeBuffer) GetTradeEvents(exchange string, startTime, endTime time.Time) ([]models.TradeEvent, error) {
	if ctb.closed {
		return nil, fmt.Errorf("circular trade buffer is closed")
	}

	startNano := startTime.UnixNano()
	endNano := endTime.UnixNano()

	// 🔍 디버그 로깅 추가
	fmt.Printf("🔍 [CircularBuffer] GetTradeEvents - Exchange: %s, Range: %s ~ %s\n",
		exchange, startTime.Format("15:04:05"), endTime.Format("15:04:05"))

	// 핫 캐시 확인 먼저
	if trades := ctb.getFromHotCache(exchange, startNano, endNano); trades != nil {
		fmt.Printf("✅ [CircularBuffer] Hot cache hit: %d trades\n", len(trades))
		ctb.mutex.Lock()
		ctb.stats.HotCacheHits++
		ctb.mutex.Unlock()
		return trades, nil
	}

	fmt.Printf("❌ [CircularBuffer] Hot cache miss, trying cold buffer\n")

	// 콜드 버퍼에서 검색
	trades := ctb.getFromColdBuffer(exchange, startNano, endNano)

	fmt.Printf("📊 [CircularBuffer] Cold buffer result: %d trades\n", len(trades))

	ctb.mutex.Lock()
	ctb.stats.ColdBufferHits++
	ctb.mutex.Unlock()

	return trades, nil
}

// getFromHotCache: 핫 캐시에서 데이터 조회
func (ctb *CircularTradeBuffer) getFromHotCache(exchange string, startNano, endNano int64) []models.TradeEvent {
	ctb.rwMutex.RLock()
	defer ctb.rwMutex.RUnlock()

	hotData, exists := ctb.hotCache[exchange]
	if !exists || hotData.lastUsed < time.Now().Add(-HotCacheSeconds*time.Second).UnixNano() {
		return nil
	}

	// 🔧 BUG FIX: 나노초를 밀리초로 변환 (TradeEvent.Timestamp는 밀리초)
	startMilli := startNano / 1e6
	endMilli := endNano / 1e6

	var result []models.TradeEvent
	for _, trade := range hotData.trades {
		// 🔧 BUG FIX: 밀리초 단위로 비교 (기존: 나노초 비교로 인한 데이터 누락)
		if trade.Timestamp >= startMilli && trade.Timestamp <= endMilli {
			result = append(result, trade)
		}
	}

	return result
}

// getFromColdBuffer: 콜드 버퍼에서 데이터 조회
func (ctb *CircularTradeBuffer) getFromColdBuffer(exchange string, startNano, endNano int64) []models.TradeEvent {
	startBucket := ctb.GetBucketIndex(startNano)
	endBucket := ctb.GetBucketIndex(endNano)

	// 🔧 나노초를 밀리초로 변환 (TradeEvent.Timestamp는 밀리초)
	startMilli := startNano / 1e6
	endMilli := endNano / 1e6

	// 🔍 디버그: 버켓 인덱스 확인
	fmt.Printf("🔍 [ColdBuffer] %s: startBucket=%d, endBucket=%d (range: %d ~ %d ms)\n",
		exchange, startBucket, endBucket, startMilli, endMilli)

	var result []models.TradeEvent
	totalTradesInBuckets := 0

	// 버켓 순회 (순환 구조 고려)
	current := startBucket
	for {
		bucket := ctb.buckets[current]
		bucket.mutex.RLock()

		if trades, exists := bucket.trades[exchange]; exists {
			totalTradesInBuckets += len(trades)
			fmt.Printf("🔍 [ColdBuffer] Bucket %d has %d trades for %s\n", current, len(trades), exchange)
			for _, trade := range trades {
				// 🔍 시간 필터링 디버깅 (밀리초 단위로 비교)
				if strings.Contains(strings.ToUpper(trade.Symbol), "SOMI") {
					fmt.Printf("🔍 [TimeFilter] Trade timestamp=%d, range=[%d ~ %d], match=%v\n",
						trade.Timestamp, startMilli, endMilli,
						trade.Timestamp >= startMilli && trade.Timestamp <= endMilli)
				}
				if trade.Timestamp >= startMilli && trade.Timestamp <= endMilli {
					result = append(result, trade)
				}
			}
		}

		bucket.mutex.RUnlock()

		if current == endBucket {
			break
		}
		current = (current + 1) % BucketCount
	}

	fmt.Printf("🔍 [ColdBuffer] %s: Total trades in buckets=%d, filtered result=%d\n",
		exchange, totalTradesInBuckets, len(result))

	return result
}

// maintainHotCache: 핫 캐시 유지 고루틴
func (ctb *CircularTradeBuffer) maintainHotCache() {
	ticker := time.NewTicker(10 * time.Second) // 10초마다 갱신
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ctb.updateHotCache()
		case <-ctb.ctx.Done():
			return
		}
	}
}

// updateHotCache: 핫 캐시 업데이트
func (ctb *CircularTradeBuffer) updateHotCache() {
	now := time.Now().UnixNano()
	hotStart := now - HotCacheSeconds*int64(time.Second)

	ctb.rwMutex.Lock()
	defer ctb.rwMutex.Unlock()

	// 활성 거래소 목록 (실제 구현에서는 동적으로 가져와야 함)
	activeExchanges := []string{"binance_spot", "binance_futures", "bybit_spot", "bybit_futures",
		"okx_spot", "okx_futures", "kucoin_spot", "kucoin_futures", "gate_spot", "gate_futures"}

	for _, exchange := range activeExchanges {
		trades := ctb.getRecentTrades(exchange, hotStart, now)
		if len(trades) > 0 {
			ctb.hotCache[exchange] = &TradeSlice{
				trades:   trades,
				lastUsed: now,
			}
		}
	}

	// 통계 업데이트
	ctb.mutex.Lock()
	ctb.stats.HotEvents = ctb.countHotCacheEvents()
	ctb.mutex.Unlock()
}

// getRecentTrades: 최근 거래 데이터 수집
func (ctb *CircularTradeBuffer) getRecentTrades(exchange string, startNano, endNano int64) []models.TradeEvent {
	startBucket := ctb.GetBucketIndex(startNano)
	endBucket := ctb.GetBucketIndex(endNano)

	var result []models.TradeEvent

	current := startBucket
	for {
		bucket := ctb.buckets[current]
		bucket.mutex.RLock()

		if trades, exists := bucket.trades[exchange]; exists {
			for _, trade := range trades {
				if trade.Timestamp >= startNano && trade.Timestamp <= endNano {
					result = append(result, trade)
				}
			}
		}

		bucket.mutex.RUnlock()

		if current == endBucket {
			break
		}

		// 안전한 순환 인덱스 계산
		current = (current + 1) % BucketCount
		if current < 0 {
			current = (current + BucketCount) % BucketCount
		}
	}

	return result
}

// countHotCacheEvents: 핫 캐시 이벤트 수 계산
func (ctb *CircularTradeBuffer) countHotCacheEvents() int64 {
	var count int64
	for _, slice := range ctb.hotCache {
		count += int64(len(slice.trades))
	}
	return count
}

// GetStats: 통계 조회
func (ctb *CircularTradeBuffer) GetStats() CircularBufferStats {
	ctb.mutex.RLock()
	defer ctb.mutex.RUnlock()

	// 메모리 사용량 계산
	stats := ctb.stats
	stats.MemoryUsage = ctb.calculateMemoryUsage()
	stats.ColdEvents = stats.TotalEvents - stats.HotEvents

	return stats
}

// calculateMemoryUsage: 메모리 사용량 계산
func (ctb *CircularTradeBuffer) calculateMemoryUsage() int64 {
	// 기본 구조체 크기
	baseMemory := int64(8 * 1024) // 8KB 기본 구조

	// 버켓 메모리 (대략적 계산)
	bucketMemory := int64(BucketCount) * 1024 // 1KB per bucket

	// 데이터 메모리 (200바이트 per 거래)
	dataMemory := ctb.stats.TotalEvents * 200

	// 핫 캐시 메모리
	hotCacheMemory := ctb.stats.HotEvents * 200

	return baseMemory + bucketMemory + dataMemory + hotCacheMemory
}

// ToCollectionEvent: CollectionEvent로 변환 (레거시 호환)
func (ctb *CircularTradeBuffer) ToCollectionEvent(symbol string, triggerTime time.Time) (*models.CollectionEvent, error) {
	startTime := triggerTime.Add(-20 * time.Second)
	endTime := triggerTime.Add(20 * time.Second)

	event := &models.CollectionEvent{
		Symbol:      symbol,
		TriggerTime: triggerTime,
		StartTime:   startTime,
		EndTime:     endTime,
	}

	// 거래소별 데이터 수집
	exchanges := []struct {
		key    string
		target *[]models.TradeEvent
	}{
		{"binance_spot", &event.BinanceSpot},
		{"binance_futures", &event.BinanceFutures},
		{"bybit_spot", &event.BybitSpot},
		{"bybit_futures", &event.BybitFutures},
		{"okx_spot", &event.OKXSpot},
		{"okx_futures", &event.OKXFutures},
		{"kucoin_spot", &event.KuCoinSpot},
		{"kucoin_futures", &event.KuCoinFutures},
		{"gate_spot", &event.GateSpot},
		{"gate_futures", &event.GateFutures},
	}

	for _, exchange := range exchanges {
		trades, err := ctb.GetTradeEvents(exchange.key, startTime, endTime)
		if err != nil {
			return nil, fmt.Errorf("failed to get trades for %s: %w", exchange.key, err)
		}

		// 심볼 필터링 적용
		var filteredTrades []models.TradeEvent
		for _, trade := range trades {
			if ctb.isTargetSymbol(symbol, trade.Symbol) {
				filteredTrades = append(filteredTrades, trade)
			}
		}

		*exchange.target = filteredTrades
	}

	return event, nil
}

// isTargetSymbol: 심볼 매칭 로직 (CLAUDE.md에서 완전히 해결된 로직 사용)
func (ctb *CircularTradeBuffer) isTargetSymbol(targetSymbol, tradeSymbol string) bool {
	// 대소문자 통일
	target := strings.ToUpper(targetSymbol)
	trade := strings.ToUpper(tradeSymbol)

	// 1. 정확한 일치
	if target == trade {
		return true
	}

	// 2. USDT 페어 매칭
	if trade == target+"USDT" {
		return true
	}

	// 3. 거래소별 구분자 포함 형식 매칭
	if trade == target+"-USDT" || trade == target+"_USDT" {
		return true
	}

	// 4. Phemex spot 형식 (sSOMIUSDT -> SOMI)
	if strings.HasPrefix(trade, "S") && len(trade) > 1 {
		phemexSymbol := strings.TrimPrefix(trade, "S")
		if strings.HasSuffix(phemexSymbol, "USDT") {
			baseSymbol := strings.TrimSuffix(phemexSymbol, "USDT")
			if baseSymbol == target {
				return true
			}
		}
	}

	return false
}

// HandleTOSHIScenario: TOSHI 16분 지연 시나리오 처리
func (ctb *CircularTradeBuffer) HandleTOSHIScenario(triggerTime time.Time) (*models.CollectionEvent, error) {
	// 16분 전 상장 시점
	listingTime := triggerTime.Add(-16 * time.Minute)

	// -20초 ~ +20초 데이터 즉시 접근 (O(1))
	return ctb.ToCollectionEvent("TOSHI", listingTime)
}

// HandleNormalListing: 일반 상장 즉시 감지 처리
func (ctb *CircularTradeBuffer) HandleNormalListing(symbol string, triggerTime time.Time) (*models.CollectionEvent, error) {
	// 핫 캐시에서 초고속 접근
	return ctb.ToCollectionEvent(symbol, triggerTime)
}

// Close: 순환 버퍼 종료
func (ctb *CircularTradeBuffer) Close() error {
	if ctb.closed {
		return nil
	}

	ctb.closed = true
	ctb.cancel()

	// 채널 닫기
	close(ctb.writeChan)

	// 메모리 정리
	ctb.rwMutex.Lock()
	ctb.hotCache = make(map[string]*TradeSlice)
	ctb.rwMutex.Unlock()

	return nil
}