package websocket

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"noticepumpcatch/internal/cache"
	"noticepumpcatch/internal/latency"
	"noticepumpcatch/internal/logger"
	"noticepumpcatch/internal/memory"
)

// BinanceWebSocket 바이낸스 WebSocket 클라이언트
type BinanceWebSocket struct {
	symbols      []string
	memManager   *memory.Manager     // 기존 메모리 매니저 (통계용)
	cacheManager *cache.CacheManager // 새 캐시 매니저 (실제 데이터 저장)
	logger       *logger.Logger      // 로거 추가
	connections  []*websocket.Conn   // 다중 연결 지원
	dataChannel  chan OrderbookData
	tradeChannel chan TradeData
	workerCount  int
	bufferSize   int
	mu           sync.RWMutex
	isConnected  bool
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup

	// 지연 모니터링
	latencyMonitor *latency.LatencyMonitor

	// 🚀 HFT 수준 실시간 펌핑 감지를 위한 HFT 감지기 추가
	hftDetector interface {
		OnTradeReceivedFromMemory(trade *memory.TradeData)
	}

	// 🔧 하드코딩 제거: config 설정들 추가
	maxSymbolsPerGroup    int
	reportIntervalSeconds int

	// 배치 통계 (성능 최적화)
	batchStats struct {
		mu             sync.Mutex
		orderbookCount int64
		tradeCount     int64
		symbolStats    map[string]int64 // 심볼별 처리 건수
		lastReport     time.Time
		reportInterval time.Duration
	}
}

// OrderbookData 오더북 데이터
type OrderbookData struct {
	Stream string                 `json:"stream"`
	Data   map[string]interface{} `json:"data"`
}

// TradeData 체결 데이터
type TradeData struct {
	Stream string                 `json:"stream"`
	Data   map[string]interface{} `json:"data"`
}

// NewBinanceWebSocket 새 바이낸스 WebSocket 클라이언트 생성
func NewBinanceWebSocket(
	symbols []string,
	memManager *memory.Manager,
	cacheManager *cache.CacheManager, // cacheManager로 변경
	logger *logger.Logger,
	workerCount int,
	bufferSize int,
	latencyMonitor *latency.LatencyMonitor,
	maxSymbolsPerGroup int, // 🔧 config 매개변수 추가
	reportIntervalSeconds int, // 🔧 config 매개변수 추가
	hftDetector interface{ OnTradeReceivedFromMemory(trade *memory.TradeData) }, // 🚀 실시간 펌핑 감지용
) *BinanceWebSocket {
	ctx, cancel := context.WithCancel(context.Background())
	bws := &BinanceWebSocket{
		symbols:               symbols,
		memManager:            memManager,
		cacheManager:          cacheManager, // 제대로 설정
		logger:                logger,       // 로거 주입
		dataChannel:           make(chan OrderbookData, bufferSize),
		tradeChannel:          make(chan TradeData, bufferSize),
		workerCount:           workerCount,
		bufferSize:            bufferSize,
		ctx:                   ctx,
		cancel:                cancel,
		latencyMonitor:        latencyMonitor,
		hftDetector:           hftDetector,           // 🚀 실시간 펌핑 감지 매니저 설정
		maxSymbolsPerGroup:    maxSymbolsPerGroup,    // 🔧 config 값 설정
		reportIntervalSeconds: reportIntervalSeconds, // 🔧 config 값 설정
	}

	// 배치 통계 초기화
	bws.batchStats.symbolStats = make(map[string]int64)
	bws.batchStats.lastReport = time.Now()
	bws.batchStats.reportInterval = time.Duration(reportIntervalSeconds) * time.Second // 🔧 config 값 사용

	// 🔧 고루틴 누수 방지: wg에 추가하고 정리 보장
	bws.wg.Add(1)
	go func() {
		defer bws.wg.Done()
		bws.symbolCountReportRoutine() // 심볼 개수 보고 고루틴 시작
	}()

	return bws
}

// Connect WebSocket 연결
func (bws *BinanceWebSocket) Connect(ctx context.Context) error {
	bws.mu.Lock()
	if bws.isConnected {
		bws.mu.Unlock()
		return fmt.Errorf("이미 연결되어 있습니다")
	}
	bws.mu.Unlock()

	// 스트림 그룹 생성
	streamGroups := bws.createStreamGroups()

	// 각 그룹별로 연결
	for i, group := range streamGroups {
		if err := bws.connectToGroup(ctx, group, i); err != nil {
			return fmt.Errorf("그룹 %d 연결 실패: %v", i, err)
		}
	}

	// 워커 풀 시작
	if bws.logger != nil {
		bws.logger.LogConnection("워커 풀 시작 시도")
	} else {
		log.Printf("🔧 워커 풀 시작 시도")
	}
	bws.startWorkerPool()

	if bws.logger != nil {
		bws.logger.LogConnection("바이낸스 WebSocket 연결 완료")
	} else {
		log.Printf("✅ 바이낸스 WebSocket 연결 완료")
	}
	return nil
}

// Disconnect 연결 해제
func (bws *BinanceWebSocket) Disconnect() {
	bws.mu.Lock()
	if !bws.isConnected {
		bws.mu.Unlock()
		return
	}
	bws.isConnected = false
	bws.mu.Unlock()

	// 🔧 고루틴 누수 방지: 컨텍스트 취소로 모든 고루틴 정리
	if bws.cancel != nil {
		bws.cancel()
	}

	// 모든 연결 닫기
	for _, conn := range bws.connections {
		if conn != nil {
			conn.Close()
		}
	}

	// 🔧 고루틴 누수 방지: 모든 고루틴 종료 대기 (타임아웃 설정)
	done := make(chan struct{})
	go func() {
		bws.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if bws.logger != nil {
			bws.logger.LogConnection("모든 고루틴 정리 완료")
		}
	case <-time.After(5 * time.Second):
		if bws.logger != nil {
			bws.logger.LogError("고루틴 정리 타임아웃 (5초)")
		}
	}

	// 🔥 채널 정리 고루틴 누수 수정: wg에 추가
	bws.wg.Add(1)
	go func() {
		defer bws.wg.Done()
		for {
			select {
			case <-bws.dataChannel:
			case <-bws.tradeChannel:
			default:
				return
			}
		}
	}()

	// 🔥 채널 정리 고루틴 완료 대기
	finalDone := make(chan struct{})
	go func() {
		bws.wg.Wait()
		close(finalDone)
	}()

	select {
	case <-finalDone:
		if bws.logger != nil {
			bws.logger.LogConnection("채널 정리 완료")
		}
	case <-time.After(2 * time.Second):
		if bws.logger != nil {
			bws.logger.LogError("채널 정리 타임아웃 (2초)")
		}
	}

	if bws.logger != nil {
		bws.logger.LogConnection("바이낸스 WebSocket 연결 해제 완료")
	} else {
		log.Printf("🔴 바이낸스 WebSocket 연결 해제 완료")
	}
}

// createStreamGroups 스트림을 그룹으로 나누기 (바이낸스 WebSocket 제한: 1024개 스트림/연결)
func (bws *BinanceWebSocket) createStreamGroups() [][]string {
	// 심볼당 2개 스트림(orderbook + trade)이므로 심볼 기준으로는 maxSymbolsPerGroup개/그룹
	// 심볼당 2개 스트림(orderbook + trade)이므로 심볼 기준으로는 100개/그룹
	maxSymbolsPerGroup := bws.maxSymbolsPerGroup // 🔧 config 값 사용

	var groups [][]string
	var currentGroup []string

	for _, symbol := range bws.symbols {
		currentGroup = append(currentGroup, symbol)

		if len(currentGroup) >= maxSymbolsPerGroup {
			groups = append(groups, currentGroup)
			currentGroup = []string{}
		}
	}

	// 마지막 그룹 추가
	if len(currentGroup) > 0 {
		groups = append(groups, currentGroup)
	}

	return groups
}

// connectToGroup 그룹별 연결

func (bws *BinanceWebSocket) connectToGroup(ctx context.Context, group []string, groupIndex int) error {
	// 바이낸스 WebSocket API의 올바른 형식으로 수정
	// 여러 스트림을 연결할 때는 /stream 엔드포인트를 사용
	streams := make([]string, 0)
	for _, symbol := range group {
		// 오더북 스트림
		orderbookStream := fmt.Sprintf("%s@depth20@100ms", strings.ToLower(symbol))
		// 체결 스트림
		tradeStream := fmt.Sprintf("%s@trade", strings.ToLower(symbol))

		// 각 스트림을 개별적으로 추가
		streams = append(streams, orderbookStream)
		streams = append(streams, tradeStream)
	}

	// WebSocket URL 생성 - /stream 엔드포인트 사용
	streamParam := strings.Join(streams, "/")
	url := fmt.Sprintf("wss://stream.binance.com:9443/stream?streams=%s", streamParam)

	if bws.logger != nil {
		bws.logger.LogConnection("그룹 %d 연결 시도: %d개 심볼", groupIndex, len(group))
		bws.logger.LogConnection("WebSocket URL 연결 시도: %s", url)
	} else {
		log.Printf("🔗 그룹 %d 연결 시도: %d개 심볼", groupIndex, len(group))
		log.Printf("🔗 WebSocket URL 연결 시도: %s", url)
	}

	// WebSocket 연결에 타임아웃 설정
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		if bws.logger != nil {
			bws.logger.LogError("WebSocket 연결 실패: %v", err)
		} else {
			log.Printf("❌ WebSocket 연결 실패: %v", err)
		}
		return fmt.Errorf("WebSocket 연결 실패: %v", err)
	}

	if bws.logger != nil {
		bws.logger.LogSuccess("WebSocket 연결 성공: %s", url)
	} else {
		log.Printf("✅ WebSocket 연결 성공: %s", url)
	}

	// 연결 설정 간소화
	conn.SetReadLimit(1024 * 1024) // 1MB

	// 다중 연결 목록에 추가
	bws.mu.Lock()
	bws.connections = append(bws.connections, conn)
	bws.isConnected = true
	bws.mu.Unlock()

	// 메시지 처리 고루틴 시작
	bws.wg.Add(1) // 🔧 고루틴 누수 방지
	go func() {
		defer bws.wg.Done()
		bws.handleMessages(ctx, conn, groupIndex)
	}()

	return nil
}

// handleMessages 메시지 처리 (각 연결별)
func (bws *BinanceWebSocket) handleMessages(ctx context.Context, conn *websocket.Conn, groupIndex int) {
	log.Printf("🚀 메시지 처리 고루틴 시작 (그룹 %d)", groupIndex)

	for {
		select {
		case <-ctx.Done():
			log.Printf("🔴 WebSocket 연결 종료 (그룹 %d)", groupIndex)
			return
		default:
			var msg map[string]interface{}
			err := conn.ReadJSON(&msg)
			if err != nil {
				log.Printf("❌ 메시지 수신 오류 (그룹 %d): %v", groupIndex, err)
				return
			}

			// 디버깅: 메시지 구조 확인
			if stream, ok := msg["stream"].(string); ok {
				if bws.logger != nil {
					bws.logger.LogWebSocket("메시지 수신: %s", stream)
				} else {
					log.Printf("📨 메시지 수신: %s", stream)
				}

				if data, ok := msg["data"].(map[string]interface{}); ok {
					// 스트림 타입에 따라 분류
					if strings.Contains(stream, "@depth") {
						// 오더북 데이터
						select {
						case bws.dataChannel <- OrderbookData{Stream: stream, Data: data}:
							// 성공적으로 전송됨
						default:
							// 🔧 오버플로우 개선: 경고 출력 빈도 제한
							if bws.logger != nil {
								bws.logger.LogError("오더북 데이터 채널 버퍼 오버플로우: %s", stream)
							} else {
								log.Printf("ERROR: 오더북 데이터 채널 버퍼 오버플로우: %s", stream)
							}

							// 🔥 극한 상황 대비: 채널에서 가장 오래된 데이터 1개 제거 후 새 데이터 추가
							select {
							case <-bws.dataChannel:
								// 오래된 데이터 1개 제거
								select {
								case bws.dataChannel <- OrderbookData{Stream: stream, Data: data}:
									// 새 데이터 추가 성공
								default:
									// 여전히 실패하면 포기
								}
							default:
								// 제거할 데이터도 없으면 포기
							}
						}
					} else if strings.Contains(stream, "@trade") {
						// 🔥 FASTTRACK: 활발한 심볼은 채널 우회하여 직접 처리
						symbol := strings.Replace(stream, "@trade", "", 1)
						symbol = strings.ToUpper(symbol)

						if bws.isFastTrackSymbol(symbol) {
							// 🚀 채널 없이 즉시 처리 (제로 레이턴시)
							bws.processTradeDataDirect(stream, data)
						} else {
							// 🔧 일반 심볼은 채널 경유
							select {
							case bws.tradeChannel <- TradeData{Stream: stream, Data: data}:
								// 성공적으로 전송됨
							default:
								// 🔧 오버플로우 개선: 경고 출력 빈도 제한
								if bws.logger != nil {
									bws.logger.LogError("체결 데이터 채널 버퍼 오버플로우: %s", stream)
								} else {
									log.Printf("ERROR: 체결 데이터 채널 버퍼 오버플로우: %s", stream)
								}

								// 🔥 극한 상황 대비: 채널에서 가장 오래된 데이터 1개 제거 후 새 데이터 추가
								select {
								case <-bws.tradeChannel:
									// 오래된 데이터 1개 제거
									select {
									case bws.tradeChannel <- TradeData{Stream: stream, Data: data}:
										// 새 데이터 추가 성공
									default:
										// 여전히 실패하면 포기
									}
								default:
									// 제거할 데이터도 없으면 포기
								}
							}
						}
					}
				} else {
					if bws.logger != nil {
						bws.logger.LogError("data 필드 파싱 실패: %v", msg)
					} else {
						log.Printf("❌ data 필드 파싱 실패: %v", msg)
					}
				}
			} else {
				if bws.logger != nil {
					bws.logger.LogError("stream 필드 파싱 실패: %v", msg)
				} else {
					log.Printf("❌ stream 필드 파싱 실패: %v", msg)
				}
			}
		}
	}
}

// startWorkerPool 워커 풀 시작 (🔥 고루틴 누수 수정)
func (bws *BinanceWebSocket) startWorkerPool() {
	if bws.logger != nil {
		bws.logger.LogConnection("워커 풀 함수 진입")
	} else {
		log.Printf("🔧 워커 풀 함수 진입")
	}

	// 🔥 고루틴 누수 수정: 오더북 워커들을 wg에 추가
	for i := 0; i < bws.workerCount/2; i++ {
		bws.wg.Add(1)
		go func(workerID int) {
			defer bws.wg.Done()
			bws.orderbookWorker(workerID)
		}(i)
		if bws.logger != nil {
			bws.logger.LogConnection("오더북 워커 %d 시작", i)
		}
	}

	// 🔥 고루틴 누수 수정: 체결 워커들을 wg에 추가
	for i := 0; i < bws.workerCount/2; i++ {
		bws.wg.Add(1)
		go func(workerID int) {
			defer bws.wg.Done()
			bws.tradeWorker(workerID)
		}(i)
		if bws.logger != nil {
			bws.logger.LogConnection("체결 워커 %d 시작", i)
		}
	}

	if bws.logger != nil {
		bws.logger.LogConnection("워커 풀 시작 완료: 오더북 %d개, 체결 %d개", bws.workerCount/2, bws.workerCount/2)
	} else {
		log.Printf("🔧 워커 풀 시작 완료: 오더북 %d개, 체결 %d개", bws.workerCount/2, bws.workerCount/2)
	}
}

// orderbookWorker 오더북 워커
func (bws *BinanceWebSocket) orderbookWorker(id int) {
	for {
		select {
		case <-bws.ctx.Done():
			return
		case data := <-bws.dataChannel:
			bws.processOrderbookData(data.Stream, data.Data)
		}
	}
}

// tradeWorker 체결 워커
func (bws *BinanceWebSocket) tradeWorker(id int) {
	for {
		select {
		case <-bws.ctx.Done():
			return
		case data := <-bws.tradeChannel:
			bws.processTradeData(data.Stream, data.Data)
		}
	}
}

// processOrderbookData 오더북 데이터 처리
func (bws *BinanceWebSocket) processOrderbookData(stream string, data map[string]interface{}) {
	// 스트림에서 심볼 추출
	symbol := strings.Replace(stream, "@depth20@100ms", "", 1)
	symbol = strings.ToUpper(symbol)

	// 지연 모니터링
	if bws.latencyMonitor != nil {
		// EventTime 추출 (밀리초 단위)
		if eventTimeRaw, ok := data["E"].(float64); ok {
			eventTime := time.Unix(0, int64(eventTimeRaw)*int64(time.Millisecond))
			latency, isWarning := bws.latencyMonitor.RecordLatency(
				symbol,
				"orderbook",
				eventTime,
				time.Now(),
			)

			if isWarning {
				bws.logger.LogLatency("밀림 감지: symbol=%s, type=orderbook, 거래소 timestamp=%s, 수신 timestamp=%s, latency=%.2f초",
					symbol,
					eventTime.Format("15:04:05.000"),
					time.Now().Format("15:04:05.000"),
					latency,
				)
			}
		}
	}

	// 디버그 로그는 배치 통계로 대체됨 (성능 최적화)

	// 오더북 데이터 파싱 (디버깅)
	if bws.logger != nil {
		bws.logger.LogDebug("%s 데이터 구조 확인: %T", symbol, data["bids"])
	} else {
		log.Printf("🔍 %s 데이터 구조 확인: %T", symbol, data["bids"])
	}

	// bids 파싱: []interface{} -> [][]interface{}
	bidsRaw, ok := data["bids"].([]interface{})
	if !ok {
		log.Printf("❌ bids 파싱 실패: %s (타입: %T)", symbol, data["bids"])
		return
	}

	bids := make([][]interface{}, len(bidsRaw))
	for i, bid := range bidsRaw {
		if bidArray, ok := bid.([]interface{}); ok {
			bids[i] = bidArray
		} else {
			log.Printf("❌ bid 배열 파싱 실패: %s", symbol)
			return
		}
	}

	// asks 파싱: []interface{} -> [][]interface{}
	asksRaw, ok := data["asks"].([]interface{})
	if !ok {
		log.Printf("❌ asks 파싱 실패: %s (타입: %T)", symbol, data["asks"])
		return
	}

	asks := make([][]interface{}, len(asksRaw))
	for i, ask := range asksRaw {
		if askArray, ok := ask.([]interface{}); ok {
			asks[i] = askArray
		} else {
			log.Printf("❌ ask 배열 파싱 실패: %s", symbol)
			return
		}
	}

	// 파싱 성공 로그
	if bws.logger != nil {
		bws.logger.LogDebug("%s 파싱 성공: bids=%d개, asks=%d개", symbol, len(bids), len(asks))
	} else {
		log.Printf("✅ %s 파싱 성공: bids=%d개, asks=%d개", symbol, len(bids), len(asks))
	}

	// 문자열로 변환
	bidsStr := make([][]string, len(bids))
	for i, bid := range bids {
		if len(bid) >= 2 {
			price := fmt.Sprintf("%v", bid[0])
			quantity := fmt.Sprintf("%v", bid[1])
			bidsStr[i] = []string{price, quantity}
		}
	}

	asksStr := make([][]string, len(asks))
	for i, ask := range asks {
		if len(ask) >= 2 {
			price := fmt.Sprintf("%v", ask[0])
			quantity := fmt.Sprintf("%v", ask[1])
			asksStr[i] = []string{price, quantity}
		}
	}

	// 오더북 스냅샷 생성
	snapshot := &memory.OrderbookSnapshot{
		Exchange:  "binance",
		Symbol:    symbol,
		Timestamp: time.Now(),
		Bids:      bidsStr,
		Asks:      asksStr,
	}

	// 캐시 매니저에 저장 (있을 때만)
	if bws.cacheManager != nil {
		if err := bws.cacheManager.AddOrderbook(snapshot); err != nil {
			if bws.logger != nil {
				bws.logger.LogError("캐시 오더북 저장 실패: %s - %v", symbol, err)
			}
		}
	}

	// 기존 메모리 매니저에도 저장 (통계용)
	bws.memManager.AddOrderbook(snapshot)

	// 배치 통계에 추가 (개별 로그 대신)
	// bws.addBatchStats(symbol, "orderbook") // 파일 저장 안하므로 통계 불필요
}

// processTradeData 체결 데이터 처리 (🔥 HFT/일반 경로 분리)
func (bws *BinanceWebSocket) processTradeData(stream string, data map[string]interface{}) {
	// 스트림에서 심볼 추출
	symbol := strings.Replace(stream, "@trade", "", 1)
	symbol = strings.ToUpper(symbol)

	// 🚀 ULTRA-FAST PATH: HFT 감지기만 즉시 처리 (최우선)
	timestampMs, ok := data["T"].(float64)
	if ok {
		price, priceOk := data["p"].(string)
		quantity, quantityOk := data["q"].(string)
		side, sideOk := data["m"].(bool)
		tradeID, tradeIDOk := data["t"].(float64)

		if priceOk && quantityOk && sideOk && tradeIDOk && bws.hftDetector != nil {
			sideStr := "SELL"
			if !side {
				sideStr = "BUY"
			}

			// 🔥 HFT 최우선 처리: 즉시 호출 (블로킹 없음)
			hftTrade := &memory.TradeData{
				Exchange:  "binance",
				Symbol:    symbol,
				Timestamp: time.Unix(0, int64(timestampMs)*int64(time.Millisecond)),
				Price:     price,
				Quantity:  quantity,
				Side:      sideStr,
				TradeID:   strconv.FormatInt(int64(tradeID), 10),
			}

			// 🔥 HFT 즉시 처리 (레이턴시 < 1μs)
			if hftDetector, ok := bws.hftDetector.(interface{ OnTradeReceivedFromMemory(trade *memory.TradeData) }); ok {
				hftDetector.OnTradeReceivedFromMemory(hftTrade)
			}
		}
	}

	// 🔥 나머지 무거운 작업들은 별도 고루틴에서 비동기 처리 (HFT에 영향 없음)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("❌ Trade 처리 중 panic 복구: %v", r)
			}
		}()

		// 지연 모니터링 (비동기)
		if bws.latencyMonitor != nil {
			if eventTimeRaw, ok := data["E"].(float64); ok {
				eventTime := time.Unix(0, int64(eventTimeRaw)*int64(time.Millisecond))
				latency, isWarning := bws.latencyMonitor.RecordLatency(symbol, "trade", eventTime, time.Now())
				if isWarning {
					bws.logger.LogLatency("밀림 감지: symbol=%s, type=trade, latency=%.2f초", symbol, latency)
				}
			}
		}

		// 체결 데이터 파싱 (비동기)
		price, ok := data["p"].(string)
		if !ok {
			return
		}
		quantity, ok := data["q"].(string)
		if !ok {
			return
		}
		side, ok := data["m"].(bool)
		if !ok {
			return
		}
		tradeID, ok := data["t"].(float64)
		if !ok {
			return
		}
		timestampMs, ok := data["T"].(float64)
		if !ok {
			return
		}

		sideStr := "SELL"
		if !side {
			sideStr = "BUY"
		}

		trade := &memory.TradeData{
			Exchange:  "binance",
			Symbol:    symbol,
			Timestamp: time.Unix(0, int64(timestampMs)*int64(time.Millisecond)),
			Price:     price,
			Quantity:  quantity,
			Side:      sideStr,
			TradeID:   strconv.FormatInt(int64(tradeID), 10),
		}

		// 🔥 저장 작업 (비동기 - HFT에 영향 없음)
		if bws.cacheManager != nil {
			if err := bws.cacheManager.AddTrade(trade); err != nil {
				// 에러는 무시 (HFT 성능 우선)
			}
		}
		bws.memManager.AddTrade(trade)
	}()
}

// processTradeDataDirect 활발한 심볼의 체결 데이터를 즉시 처리하는 경로
func (bws *BinanceWebSocket) processTradeDataDirect(stream string, data map[string]interface{}) {
	symbol := strings.Replace(stream, "@trade", "", 1)
	symbol = strings.ToUpper(symbol)

	// 🚀 ULTRA-FAST PATH: HFT 감지기만 즉시 처리 (최우선)
	timestampMs, ok := data["T"].(float64)
	if ok {
		price, priceOk := data["p"].(string)
		quantity, quantityOk := data["q"].(string)
		side, sideOk := data["m"].(bool)
		tradeID, tradeIDOk := data["t"].(float64)

		if priceOk && quantityOk && sideOk && tradeIDOk && bws.hftDetector != nil {
			sideStr := "SELL"
			if !side {
				sideStr = "BUY"
			}

			// 🔥 HFT 최우선 처리: 즉시 호출 (블로킹 없음)
			hftTrade := &memory.TradeData{
				Exchange:  "binance",
				Symbol:    symbol,
				Timestamp: time.Unix(0, int64(timestampMs)*int64(time.Millisecond)),
				Price:     price,
				Quantity:  quantity,
				Side:      sideStr,
				TradeID:   strconv.FormatInt(int64(tradeID), 10),
			}

			// 🔥 HFT 즉시 처리 (레이턴시 < 1μs)
			if hftDetector, ok := bws.hftDetector.(interface{ OnTradeReceivedFromMemory(trade *memory.TradeData) }); ok {
				hftDetector.OnTradeReceivedFromMemory(hftTrade)
			}
		}
	}

	// 🔥 나머지 무거운 작업들은 별도 고루틴에서 비동기 처리 (HFT에 영향 없음)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("❌ Trade 처리 중 panic 복구: %v", r)
			}
		}()

		// 지연 모니터링 (비동기)
		if bws.latencyMonitor != nil {
			if eventTimeRaw, ok := data["E"].(float64); ok {
				eventTime := time.Unix(0, int64(eventTimeRaw)*int64(time.Millisecond))
				latency, isWarning := bws.latencyMonitor.RecordLatency(symbol, "trade", eventTime, time.Now())
				if isWarning {
					bws.logger.LogLatency("밀림 감지: symbol=%s, type=trade, latency=%.2f초", symbol, latency)
				}
			}
		}

		// 체결 데이터 파싱 (비동기)
		price, ok := data["p"].(string)
		if !ok {
			return
		}
		quantity, ok := data["q"].(string)
		if !ok {
			return
		}
		side, ok := data["m"].(bool)
		if !ok {
			return
		}
		tradeID, ok := data["t"].(float64)
		if !ok {
			return
		}
		timestampMs, ok := data["T"].(float64)
		if !ok {
			return
		}

		sideStr := "SELL"
		if !side {
			sideStr = "BUY"
		}

		trade := &memory.TradeData{
			Exchange:  "binance",
			Symbol:    symbol,
			Timestamp: time.Unix(0, int64(timestampMs)*int64(time.Millisecond)),
			Price:     price,
			Quantity:  quantity,
			Side:      sideStr,
			TradeID:   strconv.FormatInt(int64(tradeID), 10),
		}

		// 🔥 저장 작업 (비동기 - HFT에 영향 없음)
		if bws.cacheManager != nil {
			if err := bws.cacheManager.AddTrade(trade); err != nil {
				// 에러는 무시 (HFT 성능 우선)
			}
		}
		bws.memManager.AddTrade(trade)
	}()
}

// isFastTrackSymbol 활발한 심볼인지 확인
func (bws *BinanceWebSocket) isFastTrackSymbol(symbol string) bool {
	// 여기에 활발한 심볼 목록을 추가합니다.
	// 예: "BTCUSDT", "ETHUSDT", "BNBUSDT" 등
	// 이 목록은 실제 거래량이나 활발도를 기준으로 설정해야 합니다.
	// 현재는 간단히 몇 가지 예시를 포함합니다.
	fastTrackSymbols := map[string]bool{
		"BTCUSDT":   true,
		"ETHUSDT":   true,
		"BNBUSDT":   true,
		"XRPUSDT":   true,
		"ADAUSDT":   true,
		"DOTUSDT":   true,
		"SOLUSDT":   true,
		"AVAXUSDT":  true,
		"MATICUSDT": true,
		"LTCUSDT":   true,
		"LINKUSDT":  true,
		"UNIUSDT":   true,
		"XMRUSDT":   true,
		"ZECUSDT":   true,
		"ATOMUSDT":  true,
		"ETCUSDT":   true,
		"XTZUSDT":   true,
		"YFIUSDT":   true,
		"MKRUSDT":   true,
		"SNXUSDT":   true,
		"CRVUSDT":   true,
		"AAVEUSDT":  true,
		"MIMUSDT":   true,
		"SUSHIUSDT": true,
		"WAVESUSDT": true,
		"RUNEUSDT":  true,
		"TRXUSDT":   true,
		"NEARUSDT":  true,
		"FTMUSDT":   true,
		"HBARUSDT":  true,
		"XEMUSDT":   true,
		"CELOUSDT":  true,
		"ENJUSDT":   true,
		"KAVAUSDT":  true,
		"GRTUSDT":   true,
		"ZRXUSDT":   true,
		"BATUSDT":   true,
		"KNCUSDT":   true,
		"RENUSDT":   true,
		"SNMUSDT":   true,
		"WOOUSDT":   true,
		"ZILUSDT":   true,
		"CELRUSDT":  true,
		"OCEANUSDT": true,
		"1INCHUSDT": true,
		"LRCUSDT":   true,
		"KSMUSDT":   true,
		"NEXUSDT":   true,
		"CEEKUSDT":  true,
		"ANKRUSDT":  true,
		"OGNUSDT":   true,
		"MFTUSDT":   true,
		"DENTUSDT":  true,
		"BANDUSDT":  true,
		"ZENUSDT":   true,
		"STORJUSDT": true,
		"BTTCUSDT":  true,
	}

	return fastTrackSymbols[symbol]
}

// symbolCountReportRoutine 구독 중인 심볼 개수 보고 고루틴
func (bws *BinanceWebSocket) symbolCountReportRoutine() {
	ticker := time.NewTicker(bws.batchStats.reportInterval)
	defer ticker.Stop()

	log.Printf("🎯 WebSocket 심볼 보고 고루틴 시작 (인스턴스: %p)", bws)

	for {
		select {
		case <-bws.ctx.Done():
			log.Printf("🔴 WebSocket 심볼 보고 고루틴 종료 (인스턴스: %p)", bws)
			return
		case <-ticker.C:
			bws.reportSymbolCount()
		}
	}
}

// reportSymbolCount 구독 중인 심볼 개수 보고
func (bws *BinanceWebSocket) reportSymbolCount() {
	symbolCount := len(bws.symbols)
	streamCount := symbolCount * 2

	// 로거가 있으면 로거 사용, 없으면 log.Printf 사용 (중복 제거)
	if bws.logger != nil {
		bws.logger.LogStatus("🔗 WebSocket 구독 중: %d개 심볼 (%d개 스트림) [인스턴스: %p]", symbolCount, streamCount, bws)
	} else {
		log.Printf("2025/07/22 %s STATUS: 🔗 WebSocket 구독 중: %d개 심볼 (%d개 스트림) [인스턴스: %p]",
			time.Now().Format("15:04:05"), symbolCount, streamCount, bws)
	}
}

// GetWorkerPoolStats 워커 풀 통계 조회
func (bws *BinanceWebSocket) GetWorkerPoolStats() map[string]interface{} {
	return map[string]interface{}{
		"worker_count":           bws.workerCount,
		"active_workers":         bws.workerCount, // 간단한 구현
		"data_channel_capacity":  bws.bufferSize,
		"data_channel_buffer":    len(bws.dataChannel),
		"trade_channel_capacity": bws.bufferSize,
		"trade_channel_buffer":   len(bws.tradeChannel),
		"is_connected":           bws.isConnected,
	}
}
