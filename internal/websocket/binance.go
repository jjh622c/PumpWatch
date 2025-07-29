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
	"os"
	"sync/atomic"
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

	// 🔥 워커 상태 모니터링 추가
	lastWorkerActivity time.Time
	workerHealthCheck  time.Time

	// 🚀 건강성 모니터링 (좀비 연결 방지)
	lastMessageTime     time.Time    // 마지막 메시지 수신 시간
	connectionStartTime time.Time    // 연결 시작 시간 (24시간 타이머용)
	messageCounter      int64        // 메시지 수신 카운터
	healthCheckTicker   *time.Ticker // 건강성 체크 타이머

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

	// 🚀 건강성 모니터링 초기화
	now := time.Now()
	bws.lastMessageTime = now
	bws.connectionStartTime = now
	bws.messageCounter = 0
	bws.healthCheckTicker = time.NewTicker(60 * time.Second) // 60초마다 건강성 체크

	// 🔧 고루틴 누수 방지: wg에 추가하고 정리 보장
	bws.wg.Add(2) // symbolCountReportRoutine + healthCheckRoutine
	go func() {
		defer bws.wg.Done()
		bws.symbolCountReportRoutine(bws.ctx) // 🔥 context 전달
	}()

	go func() {
		defer bws.wg.Done()
		bws.healthCheckRoutine(bws.ctx) // 🚀 건강성 체크 루틴
	}()

	return bws
}

// Connect WebSocket 연결
func (bws *BinanceWebSocket) Connect() error {
	// 🚀 연결 시작 시간 업데이트 (24시간 타이머용)
	bws.connectionStartTime = time.Now()
	bws.messageCounter = 0 // 메시지 카운터 리셋
	log.Printf("🔗 WebSocket 연결 시작... (24시간 타이머 시작)")

	// 🔧 기존 연결 정리
	bws.mu.Lock()
	if bws.isConnected {
		bws.mu.Unlock()
		return fmt.Errorf("이미 연결되어 있습니다")
	}
	bws.connections = make([]*websocket.Conn, 0) // 🔥 새로운 연결 배열 생성
	bws.mu.Unlock()

	log.Printf("🔄 [CONNECT] WebSocket 연결 시작...")

	// 스트림 그룹 생성
	streamGroups := bws.createStreamGroups()
	log.Printf("🔄 [CONNECT] %d개 그룹 생성됨", len(streamGroups))

	// 각 그룹별로 연결
	for i, group := range streamGroups {
		log.Printf("🔄 [CONNECT] 그룹 %d 연결 시도 시작...", i)
		if err := bws.connectToGroup(bws.ctx, group, i); err != nil {
			log.Printf("❌ [CONNECT] 그룹 %d 연결 실패: %v", i, err)
			return fmt.Errorf("그룹 %d 연결 실패: %v", i, err)
		}
		log.Printf("✅ [CONNECT] 그룹 %d 연결 성공", i)
	}

	// 🔥 워커 풀 시작 (컨텍스트 사용)
	log.Printf("🔄 [CONNECT] 워커 풀 시작...")
	bws.startWorkerPool()

	// 🔥 연결 상태 설정
	bws.mu.Lock()
	bws.isConnected = true
	bws.mu.Unlock()

	log.Printf("✅ [CONNECT] WebSocket 연결 완료")
	return nil
}

// Disconnect WebSocket 연결 해제
func (bws *BinanceWebSocket) Disconnect() error {
	bws.mu.Lock()
	defer bws.mu.Unlock()

	if !bws.isConnected {
		return nil
	}

	log.Printf("🔄 WebSocket 연결 해제 시작...")

	// 🚀 건강성 모니터링 정리
	if bws.healthCheckTicker != nil {
		bws.healthCheckTicker.Stop()
		log.Printf("🩺 건강성 모니터링 타이머 정리 완료")
	}

	// Context 취소로 모든 고루틴에 종료 신호
	if bws.cancel != nil {
		bws.cancel()
	}

	// 모든 연결 닫기
	for i, conn := range bws.connections {
		if conn != nil {
			log.Printf("🔴 [DISCONNECT] 연결 %d 닫는 중...", i)
			conn.Close()
		}
	}

	// 🔥 채널 완전 드레인 (타임아웃 증가)
	log.Printf("🔴 [DISCONNECT] 채널 정리 시작...")
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("❌ [DISCONNECT] 채널 정리 중 panic: %v", r)
			}
		}()

		drainCount := 0
		for {
			select {
			case <-bws.dataChannel:
				drainCount++
			case <-bws.tradeChannel:
				drainCount++
			case <-time.After(500 * time.Millisecond): // 🔥 타임아웃 증가
				log.Printf("🔴 [DISCONNECT] 채널 정리 완료 (%d개 드레인)", drainCount)
				return
			}
		}
	}()

	// 🔧 고루틴 누수 방지: 모든 고루틴 종료 대기 (타임아웃 설정)
	log.Printf("🔴 [DISCONNECT] 고루틴 종료 대기...")
	done := make(chan struct{})
	go func() {
		bws.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("✅ [DISCONNECT] 모든 고루틴 정리 완료")
	case <-time.After(8 * time.Second): // 🔥 타임아웃 증가
		log.Printf("⚠️ [DISCONNECT] 고루틴 정리 타임아웃 (8초)")
	}

	// 🔥 컨텍스트 재생성 (재연결을 위해)
	bws.mu.Lock()
	bws.ctx, bws.cancel = context.WithCancel(context.Background())
	bws.mu.Unlock()
	log.Printf("🔄 [DISCONNECT] 새 컨텍스트 생성 완료")

	log.Printf("✅ [DISCONNECT] WebSocket 연결 해제 완료")
	return nil
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

// connectToGroup 특정 그룹에 연결
func (bws *BinanceWebSocket) connectToGroup(ctx context.Context, group []string, groupIndex int) error {
	// 🔥 기존 연결이 있으면 정리
	bws.mu.Lock()
	if groupIndex < len(bws.connections) && bws.connections[groupIndex] != nil {
		bws.connections[groupIndex].Close()
		bws.connections[groupIndex] = nil
	}
	bws.mu.Unlock()

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

	log.Printf("🔗 [그룹 %d] 연결 시도: %d개 심볼, %d개 스트림", groupIndex, len(group), len(streams))

	// 🚨 바이낸스 API 제한 대응: 연결 재시도 전략
	maxRetries := 3
	baseDelay := 5 * time.Second

	var conn *websocket.Conn
	var err error
	var attempt int

	for attempt = 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// 지수 백오프: 5초, 10초, 20초
			delay := baseDelay * time.Duration(1<<attempt)
			log.Printf("🔄 [그룹 %d] 연결 재시도 %d/%d (대기: %v) - 바이낸스 API 제한 고려",
				groupIndex, attempt+1, maxRetries, delay)

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return fmt.Errorf("연결 중단됨")
			}
		}

		// WebSocket 연결에 타임아웃 설정
		dialer := websocket.Dialer{
			HandshakeTimeout: 15 * time.Second, // 10초 → 15초로 확장
		}
		conn, _, err = dialer.Dial(url, nil)
		if err == nil {
			break // 연결 성공
		}

		log.Printf("❌ [그룹 %d] 연결 시도 %d 실패: %v", groupIndex, attempt+1, err)

		// 429 (Rate Limit) 또는 특정 오류 패턴 감지
		if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "too many requests") {
			log.Printf("🚨 [API LIMIT] 바이낸스 연결 제한 감지 - 더 긴 대기 시간 적용")
			baseDelay = 30 * time.Second // 30초로 확장
		}
	}

	if err != nil {
		log.Printf("❌ [그룹 %d] 모든 연결 시도 실패 (%d회): %v", groupIndex, maxRetries, err)
		log.Printf("🚨 [API LIMIT] 바이낸스 제한: IP당 5분마다 300회 연결, 단일 연결당 1024 스트림")
		log.Printf("💡 [TIP] 현재 %d개 스트림으로 단일 연결 최적화됨", len(streams))
		return fmt.Errorf("WebSocket 연결 실패 (모든 재시도 소진): %v", err)
	}

	log.Printf("✅ [그룹 %d] WebSocket 연결 성공 (시도 횟수: %d회)", groupIndex, attempt+1)

	// 연결 설정 개선
	conn.SetReadLimit(2 * 1024 * 1024) // 2MB (기존 1MB에서 증가)

	// 🏓 바이낸스 WebSocket 핑퐁 핸들러 (공식 스펙 준수)
	// 서버에서 3분마다 ping을 보내면 동일한 payload로 pong 응답
	conn.SetPingHandler(func(appData string) error {
		log.Printf("🏓 [그룹 %d] 서버 Ping 수신 → Pong 응답", groupIndex)
		// 바이낸스 요구사항: 동일한 payload로 pong 응답
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(10*time.Second))
	})

	// 🚪 연결 종료 핸들러
	conn.SetCloseHandler(func(code int, text string) error {
		log.Printf("🚪 [그룹 %d] WebSocket 연결 종료됨: 코드=%d, 메시지=%s", groupIndex, code, text)
		return nil
	})

	// 다중 연결 목록에 추가
	bws.mu.Lock()
	// 🔥 연결 배열 크기 조정
	if len(bws.connections) <= groupIndex {
		newConnections := make([]*websocket.Conn, groupIndex+1)
		copy(newConnections, bws.connections)
		bws.connections = newConnections
	}
	bws.connections[groupIndex] = conn
	bws.mu.Unlock()

	log.Printf("✅ [그룹 %d] WebSocket 연결 완료 - 메시지 처리 고루틴 시작", groupIndex)

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

	// 🔥 디버깅: 메시지 수신 카운터 추가
	messageCount := 0
	lastMessageTime := time.Now()

	// 🔥 ReadJSON 타임아웃 설정 (60초로 확장)
	readTimeout := 60 * time.Second

	for {
		select {
		case <-ctx.Done():
			log.Printf("🔴 WebSocket 연결 종료 (그룹 %d) - 총 %d개 메시지 처리", groupIndex, messageCount)
			return
		default:
			// 🔥 ReadJSON 타임아웃 설정
			conn.SetReadDeadline(time.Now().Add(readTimeout))

			var msg map[string]interface{}
			err := conn.ReadJSON(&msg)
			if err != nil {
				// 🔥 WebSocket 오류 발생시 안전한 전체 재시작
				log.Printf("❌ [CRITICAL] WebSocket 연결 오류 (그룹 %d): %v", groupIndex, err)
				log.Printf("🔄 [RESTART] 안전을 위해 프로그램을 재시작합니다...")
				log.Printf("📊 [STATS] 총 %d개 메시지 처리 완료", messageCount)

				// 🚨 즉시 graceful shutdown 시작
				go func() {
					// 잠시 대기하여 로그가 출력되도록 함
					time.Sleep(1 * time.Second)
					log.Printf("🛑 [SHUTDOWN] 프로그램 종료 시작...")
					os.Exit(1) // exit code 1로 재시작 신호
				}()
				return
			}

			// 🔥 메시지 수신 성공
			messageCount++
			lastMessageTime = time.Now()

			// 🚀 건강성 모니터링 업데이트
			atomic.AddInt64(&bws.messageCounter, 1)
			bws.lastMessageTime = lastMessageTime

			// 🔥 ReadJSON 타임아웃 해제
			conn.SetReadDeadline(time.Time{})

			// 🔥 간소화: 5000개마다만 상태 로그 출력
			if messageCount%5000 == 0 {
				log.Printf("📨 [그룹 %d] %dk개 메시지 처리됨 (마지막: %v)",
					groupIndex, messageCount/1000, lastMessageTime.Format("15:04:05"))
			}

			// 디버깅: 메시지 구조 확인
			if stream, ok := msg["stream"].(string); ok {
				// 🔥 메시지 수신 로그 간소화 (1000개마다만 출력)
				if messageCount%1000 == 0 {
					log.Printf("📨 [그룹 %d] %d천개 메시지 처리 중... (스트림: %s)",
						groupIndex, messageCount/1000, stream)
				}

				if data, ok := msg["data"].(map[string]interface{}); ok {
					// 스트림 타입에 따라 분류
					if strings.Contains(stream, "@depth20") {
						// 🔧 일반 오더북 데이터는 채널 경유
						select {
						case bws.dataChannel <- OrderbookData{Stream: stream, Data: data}:
							// 🔥 채널 전송 성공 로그 (디버깅용)
							if messageCount%10000 == 0 {
								log.Printf("✅ [채널] 오더북 데이터 전송 성공 (그룹 %d, 메시지 %d)", groupIndex, messageCount)
							}
						default:
							// 🔧 오버플로우 개선: 경고 출력 빈도 제한
							log.Printf("❌ [오버플로우] 오더북 채널 가득참 (그룹 %d): %s", groupIndex, stream)

							// 🔥 극한 상황 대비: 채널에서 가장 오래된 데이터 1개 제거 후 새 데이터 추가
							select {
							case <-bws.dataChannel:
								// 오래된 데이터 1개 제거
								select {
								case bws.dataChannel <- OrderbookData{Stream: stream, Data: data}:
									log.Printf("🔄 [복구] 오더북 채널 복구 성공 (그룹 %d)", groupIndex)
								default:
									log.Printf("❌ [실패] 오더북 채널 복구 실패 (그룹 %d)", groupIndex)
								}
							default:
								log.Printf("❌ [비어있음] 오더북 채널이 비어있음 (그룹 %d)", groupIndex)
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
								// 🔥 채널 전송 성공 로그 (디버깅용)
								if messageCount%5000 == 0 {
									log.Printf("✅ [채널] 체결 데이터 전송 성공 (그룹 %d, 메시지 %d)", groupIndex, messageCount)
								}
							default:
								log.Printf("❌ [오버플로우] 체결 채널 가득참 (그룹 %d): %s", groupIndex, stream)

								// 🔥 극한 상황 대비: 채널에서 가장 오래된 데이터 1개 제거 후 새 데이터 추가
								select {
								case <-bws.tradeChannel:
									// 오래된 데이터 1개 제거
									select {
									case bws.tradeChannel <- TradeData{Stream: stream, Data: data}:
										log.Printf("🔄 [복구] 체결 채널 복구 성공 (그룹 %d)", groupIndex)
									default:
										log.Printf("❌ [실패] 체결 채널 복구 실패 (그룹 %d)", groupIndex)
									}
								default:
									log.Printf("❌ [비어있음] 체결 채널이 비어있음 (그룹 %d)", groupIndex)
								}
							}
						}
					}
				} else {
					log.Printf("❌ [파싱] data 필드 파싱 실패 (그룹 %d): %v", groupIndex, msg)
				}
			} else {
				log.Printf("❌ [파싱] stream 필드 파싱 실패 (그룹 %d): %v", groupIndex, msg)
			}
		}
	}
}

// startWorkerPool 워커 풀 시작 (🔥 고루틴 누수 수정)
func (bws *BinanceWebSocket) startWorkerPool() {
	// 🚨 중복 워커 생성 방지: 이미 워커가 있으면 생성하지 않음
	if len(bws.dataChannel) == cap(bws.dataChannel) && len(bws.tradeChannel) == cap(bws.tradeChannel) {
		// 채널이 가득 차 있다면 이미 워커가 동작 중일 가능성
		if bws.logger != nil {
			bws.logger.LogConnection("워커 풀 이미 실행 중 - 건너뛰기")
		}
		return
	}

	if bws.logger != nil {
		bws.logger.LogConnection("워커 풀 시작 (%d개)", bws.workerCount)
	}

	// 🔥 고루틴 누수 수정: 오더북 워커들을 wg에 추가
	for i := 0; i < bws.workerCount/2; i++ {
		bws.wg.Add(1)
		go func(workerID int) {
			defer bws.wg.Done()
			log.Printf("🔧 [WORKER] 오더북 워커 %d 시작", workerID)
			bws.orderbookWorker(bws.ctx, workerID) // 🔥 context 전달
		}(i)
	}

	// 🔥 고루틴 누수 수정: 체결 워커들을 wg에 추가
	for i := 0; i < bws.workerCount/2; i++ {
		bws.wg.Add(1)
		go func(workerID int) {
			defer bws.wg.Done()
			log.Printf("🔧 [WORKER] 체결 워커 %d 시작", workerID)
			bws.tradeWorker(bws.ctx, workerID) // 🔥 context 전달
		}(i)
	}

	if bws.logger != nil {
		bws.logger.LogConnection("워커 풀 시작 완료: 오더북 %d개, 체결 %d개", bws.workerCount/2, bws.workerCount/2)
	}
}

// orderbookWorker 오더북 워커
func (bws *BinanceWebSocket) orderbookWorker(ctx context.Context, id int) {
	processedCount := 0

	for {
		select {
		case <-ctx.Done():
			log.Printf("🔴 오더북 워커 %d 컨텍스트 종료", id)
			return
		case data := <-bws.dataChannel:
			// 🔥 워커 활동 시간 업데이트
			bws.mu.Lock()
			bws.lastWorkerActivity = time.Now()
			bws.mu.Unlock()

			processedCount++
			// 🔥 간소화: 1000개마다만 로그 출력
			if processedCount%1000 == 0 {
				log.Printf("🔧 [WORKER] 오더북 워커 %d: %dk개 처리", id, processedCount/1000)
			}
			bws.processOrderbookData(data.Stream, data.Data)
		}
	}
}

// tradeWorker 체결 워커
func (bws *BinanceWebSocket) tradeWorker(ctx context.Context, id int) {
	processedCount := 0

	for {
		select {
		case <-ctx.Done():
			log.Printf("🔴 체결 워커 %d 컨텍스트 종료", id)
			return
		case data := <-bws.tradeChannel:
			// 🔥 워커 활동 시간 업데이트
			bws.mu.Lock()
			bws.lastWorkerActivity = time.Now()
			bws.mu.Unlock()

			processedCount++
			// 🔥 간소화: 500개마다만 로그 출력
			if processedCount%500 == 0 {
				log.Printf("🔧 [WORKER] 체결 워커 %d: %d개 처리", id, processedCount)
			}
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

			// 🔥 메모리와 캐시에 저장 (동기식으로 변경하여 고루틴 누수 방지)
			if bws.cacheManager != nil {
				bws.cacheManager.AddTrade(hftTrade) // 에러 무시
			}
			bws.memManager.AddTrade(hftTrade)
		}
	}

	// 🔥 지연 모니터링은 별도 함수로 분리하여 비동기 처리
	bws.processLatencyAsync(symbol, data)
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

			// 🔥 메모리와 캐시에 저장 (동기식으로 변경하여 고루틴 누수 방지)
			if bws.cacheManager != nil {
				bws.cacheManager.AddTrade(hftTrade) // 에러 무시
			}
			bws.memManager.AddTrade(hftTrade)
		}
	}

	// 🔥 지연 모니터링은 별도 함수로 분리하여 비동기 처리
	bws.processLatencyAsync(symbol, data)
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
func (bws *BinanceWebSocket) symbolCountReportRoutine(ctx context.Context) {
	ticker := time.NewTicker(bws.batchStats.reportInterval)
	defer ticker.Stop()

	log.Printf("🎯 WebSocket 심볼 보고 고루틴 시작 (인스턴스: %p)", bws)

	for {
		select {
		case <-ctx.Done():
			log.Printf("🔴 WebSocket 심볼 보고 고루틴 컨텍스트 종료 (인스턴스: %p)", bws)
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

// processLatencyAsync 지연 모니터링 비동기 처리 (고루틴 누수 방지)
func (bws *BinanceWebSocket) processLatencyAsync(symbol string, data map[string]interface{}) {
	// 🔥 고루틴 생성 제거 - 동기 처리로 변경
	defer func() {
		if r := recover(); r != nil {
			if bws.logger != nil {
				bws.logger.LogError("지연 모니터링 중 panic: %v", r)
			}
		}
	}()

	// 🔥 컨텍스트 체크 추가
	select {
	case <-bws.ctx.Done():
		return // 시스템 종료 중이면 지연 모니터링 중단
	default:
	}

	if bws.latencyMonitor != nil {
		if eventTimeRaw, ok := data["E"].(float64); ok {
			eventTime := time.Unix(0, int64(eventTimeRaw)*int64(time.Millisecond))
			bws.latencyMonitor.RecordLatency(symbol, "trade", eventTime, time.Now())
		}
	}
}

// healthCheckRoutine 연결 건강성 모니터링 (좀비 연결 방지)
func (bws *BinanceWebSocket) healthCheckRoutine(ctx context.Context) {
	log.Printf("🩺 WebSocket 건강성 모니터링 시작 (60초 간격)")

	for {
		select {
		case <-ctx.Done():
			log.Printf("🔴 건강성 모니터링 컨텍스트 종료")
			bws.healthCheckTicker.Stop()
			return
		case <-bws.healthCheckTicker.C:
			now := time.Now()

			// 🚨 메시지 수신 중단 체크 (2분간 메시지 없음)
			timeSinceLastMessage := now.Sub(bws.lastMessageTime)
			if timeSinceLastMessage > 2*time.Minute {
				log.Printf("🚨 [CRITICAL] 좀비 연결 감지: %.1f분간 메시지 없음 (초당 수천개 메시지가 정상)", timeSinceLastMessage.Minutes())
				log.Printf("📊 [STATS] 총 %d개 메시지 처리 후 중단됨", bws.messageCounter)
				log.Printf("🔍 [DIAGNOSTIC] 연결 시작: %s, 마지막 메시지: %s",
					bws.connectionStartTime.Format("2006-01-02 15:04:05"),
					bws.lastMessageTime.Format("2006-01-02 15:04:05"))
				log.Printf("🔗 [CONNECTION] 활성 연결 수: %d개, 심볼 수: %d개", len(bws.connections), len(bws.symbols))
				log.Printf("⚠️ [API LIMIT] 바이낸스 제한: IP당 5분마다 300회 연결 제한 - 재연결 신중 진행")
				log.Printf("🔄 [RESTART] 좀비 연결 제거를 위해 프로그램을 재시작합니다...")

				// 🚨 즉시 graceful shutdown 시작
				go func() {
					time.Sleep(1 * time.Second)
					log.Printf("🛑 [SHUTDOWN] 좀비 연결 감지로 인한 종료...")
					os.Exit(1) // exit code 1로 재시작 신호
				}()
				return
			}

			// 🕐 24시간 연결 시간 체크 (바이낸스 강제 종료 전 선제적 재연결)
			connectionUptime := now.Sub(bws.connectionStartTime)
			if connectionUptime > 23*time.Hour+30*time.Minute { // 23.5시간 후 선제적 재연결
				log.Printf("🔄 [PREEMPTIVE] 24시간 연결 유지로 인한 선제적 재시작")
				log.Printf("📊 [STATS] 연결 시간: %.1f시간, 처리 메시지: %d개", connectionUptime.Hours(), bws.messageCounter)
				log.Printf("💡 [BINANCE] 바이낸스 24시간 자동 종료 전 선제적 대응")
				log.Printf("⚠️ [API LIMIT] 재연결 시 바이낸스 IP 제한 (5분마다 300회) 고려됨")
				log.Printf("🔄 [RESTART] 바이낸스 강제 종료 전 선제적 재시작...")

				// 🚨 즉시 graceful shutdown 시작
				go func() {
					time.Sleep(1 * time.Second)
					log.Printf("🛑 [SHUTDOWN] 24시간 타이머로 인한 종료...")
					os.Exit(1) // exit code 1로 재시작 신호
				}()
				return
			}

			// 🩺 주기적 건강성 리포트 (10분마다)
			if int(timeSinceLastMessage.Minutes())%10 == 0 && timeSinceLastMessage.Seconds() < 60 {
				log.Printf("🩺 [HEALTH] 연결 정상: 최근 메시지 %.1f분 전, 연결 시간 %.1f시간",
					timeSinceLastMessage.Minutes(), connectionUptime.Hours())
			}
		}
	}
}

// 🔥 재연결 로직 제거 - 이제 오류시 안전한 재시작 사용
// attemptReconnection과 fullReconnection 함수들은 제거됨
// WebSocket 오류 발생시 즉시 프로그램을 재시작하여 깔끔한 상태로 복구

// isFastTrackSymbol 활발한 심볼인지 확인
