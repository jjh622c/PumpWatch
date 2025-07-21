package websocket

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"noticepumpcatch/internal/analyzer"
	"noticepumpcatch/internal/memory"

	"github.com/gorilla/websocket"
)

// OrderbookData 오더북 데이터 구조체
type OrderbookData struct {
	Stream string                 `json:"stream"`
	Data   map[string]interface{} `json:"data"`
}

// BinanceWebSocket 바이낸스 WebSocket 클라이언트
type BinanceWebSocket struct {
	conn       *websocket.Conn
	symbols    []string
	memManager *memory.Manager
	analyzer   *analyzer.UltraFastAnalyzer

	// 워커 풀 관련
	workerPool  chan struct{}      // 워커 수 제한
	dataChannel chan OrderbookData // 데이터 처리 채널
	workerCount int                // 워커 수
	ctx         context.Context
	cancel      context.CancelFunc

	// 멀티스트림 최적화
	streamURL      string    // 집계 스트림 URL
	maxStreams     int       // 최대 스트림 수 (바이낸스 제한: 1024)
	streamGroups   []string  // 스트림 그룹 (20개씩 묶음)
	connectionID   string    // 연결 식별자
	reconnectCount int       // 재연결 횟수
	lastHeartbeat  time.Time // 마지막 하트비트
}

// NewBinanceWebSocket 바이낸스 WebSocket 생성
func NewBinanceWebSocket(symbols []string, mm *memory.Manager) *BinanceWebSocket {
	ctx, cancel := context.WithCancel(context.Background())

	bws := &BinanceWebSocket{
		symbols:       symbols,
		memManager:    mm,
		analyzer:      analyzer.NewUltraFastAnalyzer(mm),
		workerCount:   16, // 16개 워커 고루틴 (8코어 16스레드 최적화)
		ctx:           ctx,
		cancel:        cancel,
		maxStreams:    1024, // 바이낸스 최대 스트림 제한
		connectionID:  fmt.Sprintf("conn_%d", time.Now().Unix()),
		lastHeartbeat: time.Now(),
	}

	// 워커 풀 초기화
	bws.workerPool = make(chan struct{}, bws.workerCount)
	bws.dataChannel = make(chan OrderbookData, 1000) // 버퍼 크기 1000

	// 스트림 그룹 생성 (20개씩 묶음)
	bws.createStreamGroups()

	// 워커 고루틴들 시작
	bws.startWorkers()

	return bws
}

// createStreamGroups 스트림 그룹 생성 (20개씩 묶음)
func (bws *BinanceWebSocket) createStreamGroups() {
	bws.streamGroups = make([]string, 0)

	for i := 0; i < len(bws.symbols); i += 20 {
		end := i + 20
		if end > len(bws.symbols) {
			end = len(bws.symbols)
		}

		group := bws.symbols[i:end]
		streams := make([]string, len(group))

		for j, symbol := range group {
			streams[j] = fmt.Sprintf("%susdt@depth20@100ms", strings.ToLower(symbol))
		}

		streamGroup := strings.Join(streams, "/")
		bws.streamGroups = append(bws.streamGroups, streamGroup)
	}

	log.Printf("📊 스트림 그룹 생성: %d개 그룹", len(bws.streamGroups))
}

// startWorkers 워커 고루틴들 시작
func (bws *BinanceWebSocket) startWorkers() {
	for i := 0; i < bws.workerCount; i++ {
		go bws.worker(i)
	}
	log.Printf("🔧 워커 풀 시작: %d개 워커 고루틴", bws.workerCount)
}

// worker 워커 고루틴 (데이터 처리)
func (bws *BinanceWebSocket) worker(id int) {
	for {
		select {
		case <-bws.ctx.Done():
			return
		case data := <-bws.dataChannel:
			// 워커 풀 슬롯 획득
			bws.workerPool <- struct{}{}

			// 데이터 처리 시작 시간 기록
			start := time.Now()

			// 오더북 데이터 처리
			bws.processOrderbookData(data.Stream, data.Data)

			// 워커 풀 슬롯 반환
			<-bws.workerPool

			// 처리 시간 로깅 (100ms 이상인 경우)
			if duration := time.Since(start); duration > 100*time.Millisecond {
				log.Printf("⚠️  워커 %d 처리 지연: %v", id, duration)
			}
		}
	}
}

// Connect WebSocket 연결
func (bws *BinanceWebSocket) Connect(ctx context.Context) error {
	// 첫 번째 스트림 그룹으로 연결
	if len(bws.streamGroups) == 0 {
		return fmt.Errorf("연결할 스트림이 없습니다")
	}

	streamURL := fmt.Sprintf("wss://stream.binance.com:9443/stream?streams=%s", bws.streamGroups[0])

	conn, _, err := websocket.DefaultDialer.Dial(streamURL, nil)
	if err != nil {
		return fmt.Errorf("WebSocket 연결 실패: %v", err)
	}

	bws.conn = conn
	log.Printf("✅ 바이낸스 WebSocket 연결 성공: %s", streamURL)

	// 메시지 처리 고루틴 시작
	go bws.handleMessages(ctx)

	return nil
}

// handleMessages 메시지 처리
func (bws *BinanceWebSocket) handleMessages(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Printf("🔴 WebSocket 연결 종료")
			return
		default:
			var msg map[string]interface{}
			err := bws.conn.ReadJSON(&msg)
			if err != nil {
				log.Printf("❌ 메시지 수신 오류: %v", err)
				return
			}

			// 오더북 데이터를 워커 풀로 전송
			if stream, ok := msg["stream"].(string); ok {
				if data, ok := msg["data"].(map[string]interface{}); ok {
					// 워커 풀에 데이터 전송 (비동기)
					select {
					case bws.dataChannel <- OrderbookData{Stream: stream, Data: data}:
						// 성공적으로 전송됨
					default:
						// 채널이 가득 찬 경우 (버퍼 오버플로우)
						log.Printf("⚠️  데이터 채널 버퍼 오버플로우: %s", stream)
					}
				}
			}
		}
	}
}

// processOrderbookData 오더북 데이터 처리
func (bws *BinanceWebSocket) processOrderbookData(stream string, data map[string]interface{}) {
	// 스트림에서 심볼 추출
	symbol := strings.Replace(stream, "usdt@depth20@100ms", "", 1)
	symbol = strings.ToUpper(symbol)

	// 오더북 데이터 파싱
	bids, ok := data["bids"].([][]interface{})
	if !ok {
		log.Printf("❌ bids 파싱 실패: %s", symbol)
		return
	}

	asks, ok := data["asks"].([][]interface{})
	if !ok {
		log.Printf("❌ asks 파싱 실패: %s", symbol)
		return
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

	// 펌핑 시그널 분석
	signal := bws.analyzer.AnalyzeOrderbook(snapshot)
	if signal != nil {
		bws.memManager.AddSignal(signal)
	}

	// 메모리에 저장
	bws.memManager.AddOrderbook(snapshot)
}

// Close WebSocket 연결 종료
func (bws *BinanceWebSocket) Close() error {
	// 워커 풀 정리
	bws.cancel()

	// WebSocket 연결 종료
	if bws.conn != nil {
		return bws.conn.Close()
	}
	return nil
}

// GetWorkerPoolStats 워커 풀 상태 조회
func (bws *BinanceWebSocket) GetWorkerPoolStats() map[string]interface{} {
	stats := make(map[string]interface{})
	stats["worker_count"] = bws.workerCount
	stats["active_workers"] = len(bws.workerPool)
	stats["data_channel_buffer"] = len(bws.dataChannel)
	stats["data_channel_capacity"] = cap(bws.dataChannel)
	return stats
}

// GetSymbols 심볼 리스트 조회
func (bws *BinanceWebSocket) GetSymbols() []string {
	return bws.symbols
}
