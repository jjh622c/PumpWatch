package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
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

// Connect WebSocket 연결 (다중 심볼 동시 모니터링)
func (bws *BinanceWebSocket) Connect(ctx context.Context) error {
	log.Printf("🔗 바이낸스 WebSocket 연결 중... (%d개 상장 대기 코인)", len(bws.symbols))

	// 각 심볼별로 개별 WebSocket 연결
	successCount := 0
	for i, symbol := range bws.symbols {
		if err := bws.connectToSymbol(symbol, ctx); err != nil {
			log.Printf("❌ %s 연결 실패: %v", symbol, err)
		} else {
			successCount++
			if i < 10 || i%50 == 0 { // 처음 10개와 50개마다 로깅
				log.Printf("✅ %d/%d %s 연결", i+1, len(bws.symbols), symbol)
			}
		}
		time.Sleep(10 * time.Millisecond) // 연결 간격 조절
	}

	log.Printf("🎯 총 %d/%d 코인 연결 완료", successCount, len(bws.symbols))

	if successCount == 0 {
		return fmt.Errorf("모든 연결 실패")
	}

	return nil
}

// connectToSymbol 개별 심볼 연결
func (bws *BinanceWebSocket) connectToSymbol(symbol string, ctx context.Context) error {
	url := fmt.Sprintf("wss://stream.binance.com/ws/%s@trade", symbol)

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second

	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		return err
	}

	// 개별 고루틴으로 메시지 처리
	go bws.handleSymbolMessages(symbol, conn, ctx)

	return nil
}

// handleSymbolMessages 개별 심볼 메시지 처리
func (bws *BinanceWebSocket) handleSymbolMessages(symbol string, conn *websocket.Conn, ctx context.Context) {
	defer conn.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			var msg map[string]interface{}
			err := conn.ReadJSON(&msg)
			if err != nil {
				return
			}

			// 거래 데이터 처리
			bws.processTradeMessage(msg, symbol)
		}
	}
}

// processTradeMessage 거래 메시지 처리 (펌핑 감지)
func (bws *BinanceWebSocket) processTradeMessage(msg map[string]interface{}, symbol string) {
	// 기존 OrderbookSnapshot을 TradeData로 변경
	if priceStr, ok := msg["p"].(string); ok {
		if qtyStr, qok := msg["q"].(string); qok {
			price, _ := strconv.ParseFloat(priceStr, 64)
			qty, _ := strconv.ParseFloat(qtyStr, 64)

			// 간단한 펌핑 감지 로직
			bws.detectPumpingSignal(symbol, price, qty)
		}
	}
}

// detectPumpingSignal 간단한 펌핑 감지 로직
func (bws *BinanceWebSocket) detectPumpingSignal(symbol string, price, qty float64) {
	// 기본적인 펌핑 감지 (실제로는 더 복잡한 로직 필요)
	// 여기서는 거래량이 큰 경우만 로깅
	if qty > 1000 { // 임시 기준
		log.Printf("📊 %s: 큰 거래 감지 - 가격: $%.6f, 거래량: %.2f", symbol, price, qty)
	}
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
	log.Fatal(http.ListenAndServe(":8081", nil))
}

// UpbitMarket 업비트 마켓 정보
type UpbitMarket struct {
	Market      string `json:"market"`
	KoreanName  string `json:"korean_name"`
	EnglishName string `json:"english_name"`
}

// BinanceSymbol 바이낸스 심볼 정보
type BinanceSymbol struct {
	Symbol string `json:"symbol"`
	Status string `json:"status"`
}

// BinanceExchangeInfo 바이낸스 거래소 정보
type BinanceExchangeInfo struct {
	Symbols []BinanceSymbol `json:"symbols"`
}

// CoinListManager 코인 리스트 관리자
type CoinListManager struct {
	targetCoinsFile string
	mutex           sync.RWMutex
}

// NewCoinListManager 새로운 코인 리스트 관리자 생성
func NewCoinListManager() *CoinListManager {
	return &CoinListManager{
		targetCoinsFile: "target_coins.json",
	}
}

// fetchUpbitMarkets 업비트 마켓 정보 가져오기
func (clm *CoinListManager) fetchUpbitMarkets() ([]UpbitMarket, error) {
	log.Printf("📡 업비트 마켓 정보 가져오는 중...")

	resp, err := http.Get("https://api.upbit.com/v1/market/all")
	if err != nil {
		return nil, fmt.Errorf("업비트 API 호출 실패: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("응답 읽기 실패: %v", err)
	}

	var markets []UpbitMarket
	if err := json.Unmarshal(body, &markets); err != nil {
		return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
	}

	log.Printf("✅ 업비트 마켓 %d개 조회 완료", len(markets))
	return markets, nil
}

// fetchBinanceSymbols 바이낸스 심볼 정보 가져오기
func (clm *CoinListManager) fetchBinanceSymbols() ([]BinanceSymbol, error) {
	log.Printf("📡 바이낸스 심볼 정보 가져오는 중...")

	resp, err := http.Get("https://api.binance.com/api/v3/exchangeInfo")
	if err != nil {
		return nil, fmt.Errorf("바이낸스 API 호출 실패: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("응답 읽기 실패: %v", err)
	}

	var exchangeInfo BinanceExchangeInfo
	if err := json.Unmarshal(body, &exchangeInfo); err != nil {
		return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
	}

	log.Printf("✅ 바이낸스 심볼 %d개 조회 완료", len(exchangeInfo.Symbols))
	return exchangeInfo.Symbols, nil
}

// calculateTargetCoins 상장 대기 코인 계산
func (clm *CoinListManager) calculateTargetCoins() ([]string, error) {
	// 업비트 마켓 정보 가져오기
	upbitMarkets, err := clm.fetchUpbitMarkets()
	if err != nil {
		return nil, fmt.Errorf("업비트 마켓 조회 실패: %v", err)
	}

	// 바이낸스 심볼 정보 가져오기
	binanceSymbols, err := clm.fetchBinanceSymbols()
	if err != nil {
		return nil, fmt.Errorf("바이낸스 심볼 조회 실패: %v", err)
	}

	// 업비트 KRW 마켓 코인들 추출
	upbitKRWCoins := make(map[string]bool)
	for _, market := range upbitMarkets {
		if strings.HasPrefix(market.Market, "KRW-") {
			coin := strings.Replace(market.Market, "KRW-", "", 1)
			upbitKRWCoins[coin] = true
		}
	}

	// 바이낸스 USDT 페어 중 업비트 미상장 코인들 필터링
	var targetCoins []string
	for _, symbol := range binanceSymbols {
		if symbol.Status == "TRADING" && strings.HasSuffix(symbol.Symbol, "USDT") {
			// USDT 제거해서 베이스 심볼 추출
			baseCoin := strings.Replace(symbol.Symbol, "USDT", "", 1)

			// 특수 케이스 처리 (1000PEPE -> PEPE)
			if strings.HasPrefix(baseCoin, "1000") {
				baseCoin = strings.Replace(baseCoin, "1000", "", 1)
			}

			// 업비트에 상장되지 않은 코인만 추가
			if !upbitKRWCoins[baseCoin] {
				targetCoins = append(targetCoins, strings.ToLower(symbol.Symbol))
			}
		}
	}

	log.Printf("🎯 상장 대기 코인 계산 완료: %d개", len(targetCoins))
	log.Printf("📊 업비트 KRW 마켓: %d개, 바이낸스 USDT 페어: %d개", len(upbitKRWCoins), len(targetCoins))

	return targetCoins, nil
}

// saveTargetCoins 상장 대기 코인 목록을 파일에 저장
func (clm *CoinListManager) saveTargetCoins(coins []string) error {
	clm.mutex.Lock()
	defer clm.mutex.Unlock()

	data, err := json.MarshalIndent(coins, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON 생성 실패: %v", err)
	}

	if err := os.WriteFile(clm.targetCoinsFile, data, 0644); err != nil {
		return fmt.Errorf("파일 저장 실패: %v", err)
	}

	log.Printf("💾 상장 대기 코인 목록 저장 완료: %s", clm.targetCoinsFile)
	return nil
}

// loadTargetCoins 파일에서 상장 대기 코인 목록 읽기
func (clm *CoinListManager) loadTargetCoins() ([]string, error) {
	clm.mutex.RLock()
	defer clm.mutex.RUnlock()

	data, err := os.ReadFile(clm.targetCoinsFile)
	if err != nil {
		return nil, fmt.Errorf("파일 읽기 실패: %v", err)
	}

	var coins []string
	if err := json.Unmarshal(data, &coins); err != nil {
		return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
	}

	log.Printf("📂 파일에서 상장 대기 코인 %d개 로드", len(coins))
	return coins, nil
}

// getTargetCoins 상장 대기 코인 목록 가져오기 (API 우선, 실패시 파일)
func (clm *CoinListManager) getTargetCoins() []string {
	log.Printf("🔄 상장 대기 코인 목록 업데이트 중...")

	// API로 최신 정보 가져오기 시도
	coins, err := clm.calculateTargetCoins()
	if err != nil {
		log.Printf("⚠️ API 조회 실패: %v", err)
		log.Printf("📂 백업 파일에서 로드 시도...")

		// 파일에서 읽기 시도
		if backupCoins, fileErr := clm.loadTargetCoins(); fileErr == nil {
			log.Printf("✅ 백업 파일에서 %d개 코인 로드 성공", len(backupCoins))
			return backupCoins
		} else {
			log.Printf("❌ 백업 파일 로드도 실패: %v", fileErr)
			log.Printf("🔧 기본 코인 목록 사용")
			return clm.getDefaultCoins()
		}
	}

	// API 성공시 파일에 저장
	if saveErr := clm.saveTargetCoins(coins); saveErr != nil {
		log.Printf("⚠️ 파일 저장 실패: %v", saveErr)
	}

	return coins
}

// getDefaultCoins 기본 코인 목록 (최후 백업)
func (clm *CoinListManager) getDefaultCoins() []string {
	return []string{
		"arbusdt", "opusdt", "ldousdt", "wldusdt", "strkusdt",
		"gmxusdt", "magicusdt", "joeusdt", "avaxusdt", "dotusdt",
	}
}

// calculateUnlistedCoins 기존 함수를 새로운 시스템으로 교체
func calculateUnlistedCoins() []string {
	coinManager := NewCoinListManager()
	return coinManager.getTargetCoins()
}

func main() {
	log.Printf("🚀 초고속 펌핑 분석 시스템 시작")
	log.Printf("⚡ Go 고성능 WebSocket 오더북 수집기")

	// 메모리 관리자 생성
	memManager := NewMemoryManager()

	// 상장 대기 코인들을 자동 계산
	symbols := calculateUnlistedCoins()

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

	// HTTP 서버 시작 (상태 조회용) - 테스트용 비활성화
	// go startHTTPServer(memManager)

	// 시그널 대기
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Printf("✅ 시스템 준비 완료! Ctrl+C로 종료")
	log.Printf("📊 모니터링: http://localhost:8080/status")
	log.Printf("💾 중요 시그널 저장: ./signals/ 디렉토리")

	// 30초 자동 테스트 후 종료
	go func() {
		time.Sleep(30 * time.Second)
		log.Printf("⏰ 30초 테스트 완료 - 자동 종료")
		sigChan <- syscall.SIGTERM
	}()

	<-sigChan
	log.Printf("🔴 시스템 종료 중...")
	cancel()
	time.Sleep(1 * time.Second)
	log.Printf("✅ 시스템 종료 완료")
}
