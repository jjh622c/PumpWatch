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

	"noticepumpcatch/internal/memory"
)

// BinanceWebSocket 바이낸스 WebSocket 클라이언트
type BinanceWebSocket struct {
	symbols      []string
	memManager   *memory.Manager
	conn         *websocket.Conn
	dataChannel  chan OrderbookData
	tradeChannel chan TradeData
	workerCount  int
	bufferSize   int
	mu           sync.RWMutex
	isConnected  bool
	ctx          context.Context
	cancel       context.CancelFunc
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

// NewBinanceWebSocket 바이낸스 WebSocket 클라이언트 생성
func NewBinanceWebSocket(symbols []string, memManager *memory.Manager, workerCount, bufferSize int) *BinanceWebSocket {
	ctx, cancel := context.WithCancel(context.Background())

	return &BinanceWebSocket{
		symbols:      symbols,
		memManager:   memManager,
		dataChannel:  make(chan OrderbookData, bufferSize),
		tradeChannel: make(chan TradeData, bufferSize),
		workerCount:  workerCount,
		bufferSize:   bufferSize,
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Connect WebSocket 연결
func (bws *BinanceWebSocket) Connect(ctx context.Context) error {
	bws.mu.Lock()
	defer bws.mu.Unlock()

	if bws.isConnected {
		return fmt.Errorf("이미 연결되어 있습니다")
	}

	// 스트림 그룹 생성
	streamGroups := bws.createStreamGroups()

	// 각 그룹별로 연결
	for i, group := range streamGroups {
		if err := bws.connectToGroup(ctx, group, i); err != nil {
			return fmt.Errorf("그룹 %d 연결 실패: %v", i, err)
		}
	}

	// 워커 풀 시작
	bws.startWorkerPool()

	log.Printf("✅ 바이낸스 WebSocket 연결 완료 (%d개 그룹)", len(streamGroups))
	return nil
}

// Disconnect 연결 해제
func (bws *BinanceWebSocket) Disconnect() error {
	bws.mu.Lock()
	defer bws.mu.Unlock()

	if !bws.isConnected {
		return nil
	}

	bws.cancel()

	if bws.conn != nil {
		bws.conn.Close()
	}

	bws.isConnected = false
	log.Printf("🔴 바이낸스 WebSocket 연결 해제")
	return nil
}

// createStreamGroups 스트림 그룹 생성
func (bws *BinanceWebSocket) createStreamGroups() [][]string {
	const maxStreamsPerGroup = 200 // 바이낸스 제한

	var groups [][]string
	var currentGroup []string

	for _, symbol := range bws.symbols {
		currentGroup = append(currentGroup, symbol)

		if len(currentGroup) >= maxStreamsPerGroup {
			groups = append(groups, currentGroup)
			currentGroup = []string{}
		}
	}

	if len(currentGroup) > 0 {
		groups = append(groups, currentGroup)
	}

	return groups
}

// connectToGroup 그룹별 연결
func (bws *BinanceWebSocket) connectToGroup(ctx context.Context, group []string, groupIndex int) error {
	// 스트림 URL 생성
	streams := make([]string, len(group))
	for j, symbol := range group {
		// 오더북 스트림
		orderbookStream := fmt.Sprintf("%s@depth20@100ms", strings.ToLower(symbol))
		// 체결 스트림
		tradeStream := fmt.Sprintf("%s@trade", strings.ToLower(symbol))

		// 두 스트림을 하나의 URL로 결합
		streams[j] = orderbookStream + "/" + tradeStream
	}

	// WebSocket URL 생성
	streamParam := strings.Join(streams, "/")
	url := fmt.Sprintf("wss://stream.binance.com:9443/stream?streams=%s", streamParam)

	log.Printf("🔗 그룹 %d 연결 시도: %d개 심볼", groupIndex, len(group))

	// WebSocket 연결
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return fmt.Errorf("WebSocket 연결 실패: %v", err)
	}

	// 연결 설정
	conn.SetReadLimit(1024 * 1024) // 1MB
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	bws.conn = conn
	bws.isConnected = true

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

			// 디버깅: 메시지 구조 확인
			if stream, ok := msg["stream"].(string); ok {
				log.Printf("📨 메시지 수신: %s", stream)
				if data, ok := msg["data"].(map[string]interface{}); ok {
					// 스트림 타입에 따라 분류
					if strings.Contains(stream, "@depth") {
						// 오더북 데이터
						select {
						case bws.dataChannel <- OrderbookData{Stream: stream, Data: data}:
							// 성공적으로 전송됨
						default:
							log.Printf("⚠️  오더북 데이터 채널 버퍼 오버플로우: %s", stream)
						}
					} else if strings.Contains(stream, "@trade") {
						// 체결 데이터
						select {
						case bws.tradeChannel <- TradeData{Stream: stream, Data: data}:
							// 성공적으로 전송됨
						default:
							log.Printf("⚠️  체결 데이터 채널 버퍼 오버플로우: %s", stream)
						}
					}
				} else {
					log.Printf("❌ data 필드 파싱 실패: %v", msg)
				}
			} else {
				log.Printf("❌ stream 필드 파싱 실패: %v", msg)
			}
		}
	}
}

// startWorkerPool 워커 풀 시작
func (bws *BinanceWebSocket) startWorkerPool() {
	// 오더북 워커
	for i := 0; i < bws.workerCount/2; i++ {
		go bws.orderbookWorker(i)
	}

	// 체결 워커
	for i := 0; i < bws.workerCount/2; i++ {
		go bws.tradeWorker(i)
	}

	log.Printf("🔧 워커 풀 시작: 오더북 %d개, 체결 %d개", bws.workerCount/2, bws.workerCount/2)
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

	log.Printf("📊 오더북 데이터 처리: %s -> %s", stream, symbol)

	// 오더북 데이터 파싱 (디버깅)
	log.Printf("🔍 %s 데이터 구조 확인: %T", symbol, data["bids"])

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
	log.Printf("✅ %s 파싱 성공: bids=%d개, asks=%d개", symbol, len(bids), len(asks))

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

	// 메모리 관리자에 저장
	bws.memManager.AddOrderbook(snapshot)
}

// processTradeData 체결 데이터 처리
func (bws *BinanceWebSocket) processTradeData(stream string, data map[string]interface{}) {
	// 스트림에서 심볼 추출
	symbol := strings.Replace(stream, "@trade", "", 1)
	symbol = strings.ToUpper(symbol)

	log.Printf("💰 체결 데이터 처리: %s -> %s", stream, symbol)

	// 체결 데이터 파싱
	price, ok := data["p"].(string)
	if !ok {
		log.Printf("❌ 가격 파싱 실패: %s", symbol)
		return
	}

	quantity, ok := data["q"].(string)
	if !ok {
		log.Printf("❌ 수량 파싱 실패: %s", symbol)
		return
	}

	side, ok := data["m"].(bool)
	if !ok {
		log.Printf("❌ 매수/매도 파싱 실패: %s", symbol)
		return
	}

	tradeID, ok := data["t"].(float64)
	if !ok {
		log.Printf("❌ 거래 ID 파싱 실패: %s", symbol)
		return
	}

	// 타임스탬프 파싱
	timestampMs, ok := data["T"].(float64)
	if !ok {
		log.Printf("❌ 타임스탬프 파싱 실패: %s", symbol)
		return
	}

	// 매수/매도 문자열 변환
	sideStr := "SELL"
	if !side {
		sideStr = "BUY"
	}

	// 체결 데이터 생성
	trade := &memory.TradeData{
		Exchange:  "binance",
		Symbol:    symbol,
		Timestamp: time.Unix(0, int64(timestampMs)*int64(time.Millisecond)),
		Price:     price,
		Quantity:  quantity,
		Side:      sideStr,
		TradeID:   strconv.FormatInt(int64(tradeID), 10),
	}

	// 메모리 관리자에 저장
	bws.memManager.AddTrade(trade)

	log.Printf("✅ %s 체결 저장: %s %s@%s", symbol, sideStr, quantity, price)
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
