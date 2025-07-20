package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// OrderbookSnapshot 오더북 스냅샷 구조체
type OrderbookSnapshot struct {
	Exchange    string     `json:"exchange"`
	Symbol      string     `json:"symbol"`
	Timestamp   time.Time  `json:"timestamp"`
	Bids        [][]string `json:"bids"`
	Asks        [][]string `json:"asks"`
	UpdateID    int64      `json:"updateId,omitempty"`
	SignalScore float64    `json:"signal_score"`
}

// PumpSignal 펌핑 시그널 구조체
type PumpSignal struct {
	Symbol     string    `json:"symbol"`
	Exchange   string    `json:"exchange"`
	Timestamp  time.Time `json:"timestamp"`
	Score      float64   `json:"score"`
	MaxPumpPct float64   `json:"max_pump_pct"`
	Action     string    `json:"action"`
	Reasons    []string  `json:"reasons"`
}

// MemoryManager 메모리 관리자
type MemoryManager struct {
	mu         sync.RWMutex
	orderbooks map[string][]*OrderbookSnapshot // key: exchange_symbol
	signals    []*PumpSignal
	retention  time.Duration // 10분
}

// NewMemoryManager 메모리 관리자 생성
func NewMemoryManager() *MemoryManager {
	return &MemoryManager{
		orderbooks: make(map[string][]*OrderbookSnapshot),
		signals:    make([]*PumpSignal, 0),
		retention:  10 * time.Minute, // 10분간 저장
	}
}

// AddOrderbook 오더북 추가
func (mm *MemoryManager) AddOrderbook(snapshot *OrderbookSnapshot) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	key := fmt.Sprintf("%s_%s", snapshot.Exchange, snapshot.Symbol)
	mm.orderbooks[key] = append(mm.orderbooks[key], snapshot)

	// 메모리 정리 (10분 이전 데이터 삭제)
	cutoff := time.Now().Add(-mm.retention)
	filtered := make([]*OrderbookSnapshot, 0)

	for _, ob := range mm.orderbooks[key] {
		if ob.Timestamp.After(cutoff) {
			filtered = append(filtered, ob)
		}
	}
	mm.orderbooks[key] = filtered
}

// GetRecentOrderbooks 최근 오더북 조회
func (mm *MemoryManager) GetRecentOrderbooks(exchange, symbol string, duration time.Duration) []*OrderbookSnapshot {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	key := fmt.Sprintf("%s_%s", exchange, symbol)
	orderbooks, exists := mm.orderbooks[key]
	if !exists {
		return nil
	}

	cutoff := time.Now().Add(-duration)
	recent := make([]*OrderbookSnapshot, 0)

	for _, ob := range orderbooks {
		if ob.Timestamp.After(cutoff) {
			recent = append(recent, ob)
		}
	}

	return recent
}

// AddSignal 시그널 추가 (중요 시그널만 저장)
func (mm *MemoryManager) AddSignal(signal *PumpSignal) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// 점수 60 이상만 저장 (중요 시그널)
	if signal.Score >= 60 {
		mm.signals = append(mm.signals, signal)

		// 시그널도 10분간만 보관
		cutoff := time.Now().Add(-mm.retention)
		filtered := make([]*PumpSignal, 0)

		for _, s := range mm.signals {
			if s.Timestamp.After(cutoff) {
				filtered = append(filtered, s)
			}
		}
		mm.signals = filtered

		// 디스크에 압축 저장 (중요 시그널만)
		go mm.saveSignalToDisk(signal)
	}
}

// saveSignalToDisk 중요 시그널 디스크 저장
func (mm *MemoryManager) saveSignalToDisk(signal *PumpSignal) {
	// signals 디렉토리 생성
	os.MkdirAll("signals", 0755)

	filename := fmt.Sprintf("signals/%s_%s_%d.json",
		signal.Exchange,
		signal.Symbol,
		signal.Timestamp.Unix())

	data, err := json.MarshalIndent(signal, "", "  ")
	if err != nil {
		log.Printf("시그널 저장 오류: %v", err)
		return
	}

	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		log.Printf("파일 저장 오류: %v", err)
		return
	}

	log.Printf("🔥 중요 시그널 저장: %s (점수: %.1f)", filename, signal.Score)
}

// GetMemoryStats 메모리 상태 조회
func (mm *MemoryManager) GetMemoryStats() map[string]interface{} {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	stats := make(map[string]interface{})

	totalOrderbooks := 0
	for key, orderbooks := range mm.orderbooks {
		stats[key] = len(orderbooks)
		totalOrderbooks += len(orderbooks)
	}

	stats["total_orderbooks"] = totalOrderbooks
	stats["total_signals"] = len(mm.signals)
	stats["retention_minutes"] = int(mm.retention.Minutes())

	return stats
}

// BinanceWebSocket 바이낸스 WebSocket 클라이언트
type BinanceWebSocket struct {
	conn       *websocket.Conn
	symbols    []string
	memManager *MemoryManager
	analyzer   *UltraFastAnalyzer
}

// UltraFastAnalyzer 초고속 분석기
type UltraFastAnalyzer struct {
	memManager *MemoryManager
}

// NewUltraFastAnalyzer 분석기 생성
func NewUltraFastAnalyzer(mm *MemoryManager) *UltraFastAnalyzer {
	return &UltraFastAnalyzer{memManager: mm}
}

// AnalyzeOrderbook 오더북 분석 (1초 내 완료)
func (ufa *UltraFastAnalyzer) AnalyzeOrderbook(snapshot *OrderbookSnapshot) *PumpSignal {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		if duration > 100*time.Millisecond {
			log.Printf("⚠️  분석 시간 초과: %v", duration)
		}
	}()

	// 최근 1초간 데이터 조회
	recent := ufa.memManager.GetRecentOrderbooks(snapshot.Exchange, snapshot.Symbol, 1*time.Second)

	if len(recent) < 2 {
		return nil // 분석에 충분한 데이터 없음
	}

	// 간단한 펌핑 분석
	signal := &PumpSignal{
		Symbol:    snapshot.Symbol,
		Exchange:  snapshot.Exchange,
		Timestamp: snapshot.Timestamp,
		Reasons:   make([]string, 0),
	}

	// 스프레드 분석
	if len(snapshot.Bids) > 0 && len(snapshot.Asks) > 0 {
		// 스프레드 압축 감지 등의 로직
		signal.Score += 20
		signal.Reasons = append(signal.Reasons, "오더북 활성화")
	}

	// 시그널 등급 결정
	if signal.Score >= 80 {
		signal.Action = "즉시 매수"
	} else if signal.Score >= 60 {
		signal.Action = "빠른 매수"
	} else if signal.Score >= 40 {
		signal.Action = "신중 매수"
	} else {
		signal.Action = "대기"
	}

	return signal
}

// NewBinanceWebSocket 바이낸스 WebSocket 클라이언트 생성
func NewBinanceWebSocket(symbols []string, mm *MemoryManager) *BinanceWebSocket {
	return &BinanceWebSocket{
		symbols:    symbols,
		memManager: mm,
		analyzer:   NewUltraFastAnalyzer(mm),
	}
}

// Connect WebSocket 연결
func (bws *BinanceWebSocket) Connect(ctx context.Context) error {
	// 바이낸스 WebSocket URL 구성
	url := "wss://stream.binance.com:9443/ws/"

	// 여러 심볼 구독
	streams := make([]string, 0)
	for _, symbol := range bws.symbols {
		streams = append(streams, fmt.Sprintf("%s@depth5@100ms", symbol))
	}

	log.Printf("🔗 바이낸스 WebSocket 연결 중... (%d 심볼)", len(bws.symbols))

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		return fmt.Errorf("WebSocket 연결 실패: %v", err)
	}

	bws.conn = conn

	// 구독 메시지 전송
	subscribeMsg := map[string]interface{}{
		"method": "SUBSCRIBE",
		"params": streams,
		"id":     1,
	}

	if err := conn.WriteJSON(subscribeMsg); err != nil {
		return fmt.Errorf("구독 메시지 전송 실패: %v", err)
	}

	log.Printf("✅ 바이낸스 WebSocket 연결 성공!")

	// 메시지 수신 고루틴
	go bws.handleMessages(ctx)

	return nil
}

// handleMessages 메시지 처리
func (bws *BinanceWebSocket) handleMessages(ctx context.Context) {
	defer bws.conn.Close()

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

			// 오더북 데이터 처리
			if stream, ok := msg["stream"].(string); ok {
				if data, ok := msg["data"].(map[string]interface{}); ok {
					go bws.processOrderbookData(stream, data)
				}
			}
		}
	}
}

// processOrderbookData 오더북 데이터 처리
func (bws *BinanceWebSocket) processOrderbookData(stream string, data map[string]interface{}) {
	start := time.Now()

	// 심볼 추출
	symbol := ""
	if s, ok := data["s"].(string); ok {
		symbol = s
	}

	// 오더북 스냅샷 생성
	snapshot := &OrderbookSnapshot{
		Exchange:  "binance",
		Symbol:    symbol,
		Timestamp: time.Now(),
	}

	// bids 처리
	if bids, ok := data["b"].([]interface{}); ok {
		snapshot.Bids = make([][]string, len(bids))
		for i, bid := range bids {
			if bidArray, ok := bid.([]interface{}); ok && len(bidArray) >= 2 {
				snapshot.Bids[i] = []string{
					bidArray[0].(string), // price
					bidArray[1].(string), // quantity
				}
			}
		}
	}

	// asks 처리
	if asks, ok := data["a"].([]interface{}); ok {
		snapshot.Asks = make([][]string, len(asks))
		for i, ask := range asks {
			if askArray, ok := ask.([]interface{}); ok && len(askArray) >= 2 {
				snapshot.Asks[i] = []string{
					askArray[0].(string), // price
					askArray[1].(string), // quantity
				}
			}
		}
	}

	// 메모리에 저장
	bws.memManager.AddOrderbook(snapshot)

	// 초고속 분석
	if signal := bws.analyzer.AnalyzeOrderbook(snapshot); signal != nil {
		bws.memManager.AddSignal(signal)

		if signal.Score >= 60 {
			log.Printf("🚀 강한 시그널: %s %.1f점 - %s",
				signal.Symbol, signal.Score, signal.Action)
		}
	}

	// 성능 모니터링
	duration := time.Since(start)
	if duration > 50*time.Millisecond {
		log.Printf("⚠️  처리 시간 초과: %s - %v", symbol, duration)
	}
}

// Close WebSocket 연결 종료
func (bws *BinanceWebSocket) Close() error {
	if bws.conn != nil {
		return bws.conn.Close()
	}
	return nil
}

// 메모리 상태 모니터링 고루틴
func monitorMemory(mm *MemoryManager) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		stats := mm.GetMemoryStats()
		log.Printf("📊 메모리 상태: 오더북 %d개, 시그널 %d개, 보관시간 %d분",
			stats["total_orderbooks"],
			stats["total_signals"],
			stats["retention_minutes"])
	}
}

// HTTP API 서버 (상태 조회용)
func startHTTPServer(mm *MemoryManager) {
	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		stats := mm.GetMemoryStats()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})

	log.Printf("🌐 HTTP 서버 시작: http://localhost:8080/status")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func main() {
	log.Printf("🚀 초고속 펌핑 분석 시스템 시작")
	log.Printf("⚡ Go 고성능 WebSocket 오더북 수집기")

	// 메모리 관리자 생성
	memManager := NewMemoryManager()

	// 모니터링할 심볼들
	symbols := []string{"btcusdt", "ethusdt", "solusdt", "adausdt", "dotusdt"}

	// 컨텍스트 생성
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 바이낸스 WebSocket 클라이언트 생성
	binanceWS := NewBinanceWebSocket(symbols, memManager)

	// WebSocket 연결
	if err := binanceWS.Connect(ctx); err != nil {
		log.Fatalf("❌ 연결 실패: %v", err)
	}
	defer binanceWS.Close()

	// 메모리 모니터링 고루틴 시작
	go monitorMemory(memManager)

	// HTTP 서버 시작 (상태 조회용)
	go startHTTPServer(memManager)

	// 시그널 대기
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Printf("✅ 시스템 준비 완료! Ctrl+C로 종료")
	log.Printf("📊 모니터링: http://localhost:8080/status")
	log.Printf("💾 중요 시그널 저장: ./signals/ 디렉토리")

	<-sigChan
	log.Printf("🔴 시스템 종료 중...")
	cancel()
	time.Sleep(1 * time.Second)
	log.Printf("✅ 시스템 종료 완료")
}
