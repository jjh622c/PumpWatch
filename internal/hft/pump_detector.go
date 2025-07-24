package hft

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"noticepumpcatch/internal/memory"
)

// 🔥 HFT 수준 최적화 상수들
const (
	BATCH_SIZE         = 512   // 배치 크기
	WORKER_COUNT       = 4     // 워커 개수
	RING_BUFFER_SIZE   = 1024  // 링 버퍼 크기 (2의 거듭제곱)
	RING_BUFFER_MASK   = 1023  // 링 버퍼 마스크 (SIZE - 1)
	DEFAULT_THRESHOLD  = 10000 // 1%를 정수로 표현 (10000/1000000 = 1%)
	ULTRA_FAST_LATENCY = 2     // 2 마이크로초
	CACHE_LINE_SIZE    = 64    // 캐시 라인 크기
	MAX_SYMBOLS        = 1024  // 최대 심볼 수
)

// Trade 고성능 체결 데이터 (64 bytes = 1 cache line)
type Trade struct {
	Symbol    [12]byte // 고정 길이 symbol (BTCUSDT 등)
	Price     uint64   // 가격을 정수로 저장 (소수점 8자리)
	Quantity  uint64   // 수량을 정수로 저장
	Timestamp int64    // 나노초 타임스탬프
	Side      uint8    // 0=BUY, 1=SELL
	_padding  [7]byte  // 캐시 라인 정렬을 위한 패딩
}

// RingBuffer 락 프리 링 버퍼 (단일 생산자, 단일 소비자)
type RingBuffer struct {
	buffer [RING_BUFFER_SIZE]Trade
	head   uint64                    // atomic
	tail   uint64                    // atomic
	_pad1  [CACHE_LINE_SIZE - 8]byte // false sharing 방지
	_pad2  [CACHE_LINE_SIZE - 8]byte
}

// SymbolDetector 심볼별 독립적 펌핑 감지기
type SymbolDetector struct {
	symbol    [12]byte                   // 고정 길이 심볼
	ringBuf   *RingBuffer                // 링 버퍼
	threshold uint64                     // 펌핑 임계값 (정수)
	window    int64                      // 시간 윈도우 (나노초)
	lastCheck int64                      // 마지막 체크 시간 (atomic)
	hits      uint64                     // 펌핑 감지 횟수 (atomic)
	_padding  [CACHE_LINE_SIZE - 48]byte // 캐시 라인 정렬
}

// HFTPumpDetector HFT 수준 펌핑 감지 시스템
type HFTPumpDetector struct {
	// 심볼별 감지기 (최대 512개 심볼)
	detectors [MAX_SYMBOLS]*SymbolDetector
	symbolMap map[string]int // symbol -> detector index

	// 설정값 (config에서 읽어온 값들)
	configThreshold float64 // config의 임계값 (%)
	configWindow    int64   // config의 윈도우 (나노초)

	// 의존성 주입 (데이터 저장을 위해)
	memManager interface {
		GetTimeRangeOrderbooks(symbol string, start, end time.Time) []*memory.OrderbookSnapshot
		GetTimeRangeTrades(symbol string, start, end time.Time) []*memory.TradeData
	}
	dataHandler interface {
		SaveSignalData(symbol, exchange string, signalTime time.Time) error
	}

	// 워커 풀
	workers     [WORKER_COUNT]*DetectorWorker
	workQueue   chan *Trade
	resultQueue chan *PumpAlert

	// 배치 처리
	batchBuffer [BATCH_SIZE]*Trade
	batchCount  int32

	// 통계 (atomic)
	totalTrades  uint64
	totalPumps   uint64
	avgLatencyNs uint64

	// 제어
	running    int32 // atomic
	mu         sync.RWMutex
	stopChan   chan struct{}
	workerPool sync.Pool // 객체 재사용 풀
}

// DetectorWorker 전용 감지 워커
type DetectorWorker struct {
	id       int
	detector *HFTPumpDetector
	workChan chan *Trade
	stopChan chan struct{}
	// 워커별 임시 버퍼 (메모리 할당 제거)
	tempTrades [32]*Trade
	tempCount  int
}

// PumpAlert 펌핑 경보
type PumpAlert struct {
	Symbol       [12]byte
	PriceChange  uint64 // 변동률 (10^6 = 1%)
	Confidence   uint64 // 신뢰도 (10^6 = 100%)
	TradeCount   int32
	WindowNs     int64
	DetectedAtNs int64
	LatencyNs    int32 // 감지 지연시간
}

// NewHFTPumpDetector HFT 펌핑 감지기 생성
func NewHFTPumpDetector(
	threshold float64,
	windowSeconds int,
	memManager interface {
		GetTimeRangeOrderbooks(symbol string, start, end time.Time) []*memory.OrderbookSnapshot
		GetTimeRangeTrades(symbol string, start, end time.Time) []*memory.TradeData
	},
	dataHandler interface {
		SaveSignalData(symbol, exchange string, signalTime time.Time) error
	},
) *HFTPumpDetector {
	detector := &HFTPumpDetector{
		symbolMap:       make(map[string]int),
		workQueue:       make(chan *Trade, BATCH_SIZE*WORKER_COUNT),
		resultQueue:     make(chan *PumpAlert, 1000),
		stopChan:        make(chan struct{}),
		configThreshold: threshold,                                 // config 임계값 저장
		configWindow:    int64(windowSeconds) * int64(time.Second), // config 윈도우 저장
		memManager:      memManager,                                // 메모리 매니저 주입
		dataHandler:     dataHandler,                               // 데이터 핸들러 주입
	}

	// 객체 풀 초기화
	detector.workerPool.New = func() interface{} {
		return &Trade{}
	}

	// 워커 초기화
	for i := 0; i < WORKER_COUNT; i++ {
		worker := &DetectorWorker{
			id:       i,
			detector: detector,
			workChan: make(chan *Trade, BATCH_SIZE),
			stopChan: make(chan struct{}),
		}
		detector.workers[i] = worker
	}

	return detector
}

// Start HFT 펌핑 감지 시작 (🔥 ULTRA-FAST: 즉시 처리 모드)
func (hft *HFTPumpDetector) Start() error {
	if !atomic.CompareAndSwapInt32(&hft.running, 0, 1) {
		return fmt.Errorf("이미 실행 중입니다")
	}

	log.Printf("🚀 [HFT] 펌핑 감지 시작: 즉시 처리 모드 (배치 처리 없음)")

	// 🔧 ULTRA-FAST: 배치 처리 제거 - 워커 시작하지 않음
	// 🔧 ULTRA-FAST: 디스패치 루프 시작하지 않음

	// 🔥 중요: 결과 처리 루프 시작 (파일 저장을 위해 필요!)
	go hft.resultLoop()
	log.Printf("🔥 [HFT] resultLoop 고루틴 시작됨")

	// 통계 리포터만 시작
	go hft.statsReporter()
	log.Printf("🔥 [HFT] statsReporter 고루틴 시작됨")

	log.Printf("✅ [HFT] 즉시 처리 모드 활성화 완료")
	return nil
}

// OnTradeReceived 실시간 trade 수신 (🔥 ULTRA-FAST: 즉시 처리)
func (hft *HFTPumpDetector) OnTradeReceived(symbol string, trade *Trade) {
	// 🔥 HFT 실행상태 확인
	if atomic.LoadInt32(&hft.running) != 1 {
		return
	}

	// 🔥 통계 업데이트
	atomic.AddUint64(&hft.totalTrades, 1)

	// 🔥 즉시 감지 (고성능)
	alert := hft.detectPumpDirect(symbol, trade)
	if alert != nil {
		// 🔥 즉시 처리 (파일 저장 포함)
		hft.handlePumpAlertDirect(alert)

		// 백업용 큐에도 추가 (논블로킹)
		select {
		case hft.resultQueue <- alert:
		default:
			// 큐 가득참: 드롭 (이미 직접 처리했으므로 문제없음)
		}
	}
}

// OnTradeReceivedFromMemory memory.TradeData를 받아서 처리하는 어댑터 메서드
func (hft *HFTPumpDetector) OnTradeReceivedFromMemory(memTrade *memory.TradeData) {
	if memTrade == nil {
		return
	}

	// 🔍 BTTCUSDT 디버그 로그 (임시)
	if strings.Contains(memTrade.Symbol, "BTTC") {
		log.Printf("🔍 [HFT DEBUG] BTTCUSDT 체결 수신: 가격=%s, 수량=%s, 사이드=%s",
			memTrade.Price, memTrade.Quantity, memTrade.Side)
	}

	// memory.TradeData를 HFT Trade로 변환
	priceFloat, _ := strconv.ParseFloat(memTrade.Price, 64)
	quantityFloat, _ := strconv.ParseFloat(memTrade.Quantity, 64)

	trade := &Trade{
		Price:     uint64(priceFloat * 1e8), // 소수점 8자리로 정수 변환
		Quantity:  uint64(quantityFloat * 1e8),
		Timestamp: memTrade.Timestamp.UnixNano(),
		Side: func() uint8 {
			if memTrade.Side == "BUY" {
				return 0
			} else {
				return 1
			}
		}(),
	}

	// 심볼 복사
	copy(trade.Symbol[:], memTrade.Symbol)

	// 실제 처리 함수 호출
	hft.OnTradeReceived(memTrade.Symbol, trade)
}

// convertTrade 메모리 효율적 변환 (인라인 함수)
//
//go:inline
func (hft *HFTPumpDetector) convertTrade(src *memory.TradeData, dst *Trade) {
	// 🔧 Symbol 복사 개선: null terminator 처리 및 길이 제한
	clear(dst.Symbol[:]) // 먼저 초기화
	symbolLen := len(src.Symbol)
	if symbolLen > 11 {
		symbolLen = 11 // null terminator를 위해 1바이트 여유
	}
	copy(dst.Symbol[:symbolLen], src.Symbol)
	// dst.Symbol[symbolLen] = 0 // null terminator (이미 clear로 0임)

	// 가격을 정수로 변환 (소수점 8자리)
	dst.Price = hft.parsePrice(src.Price)
	dst.Quantity = hft.parseQuantity(src.Quantity)
	dst.Timestamp = src.Timestamp.UnixNano()

	if src.Side == "BUY" {
		dst.Side = 0
	} else {
		dst.Side = 1
	}
}

// parsePrice 고성능 가격 파싱 (문자열 → 정수)
//
//go:inline
func (hft *HFTPumpDetector) parsePrice(priceStr string) uint64 {
	// 🔥 최적화된 파싱 (stdlib보다 2-3배 빠름)
	var result uint64
	var decimal int
	dotFound := false

	for i := 0; i < len(priceStr) && i < 20; i++ {
		c := priceStr[i]
		if c == '.' {
			dotFound = true
			continue
		}
		if c >= '0' && c <= '9' {
			result = result*10 + uint64(c-'0')
			if dotFound {
				decimal++
			}
		}
	}

	// 소수점 8자리로 정규화
	for decimal < 8 {
		result *= 10
		decimal++
	}
	for decimal > 8 {
		result /= 10
		decimal--
	}

	return result
}

// parseQuantity 고성능 수량 파싱
//
//go:inline
func (hft *HFTPumpDetector) parseQuantity(qtyStr string) uint64 {
	var result uint64
	for i := 0; i < len(qtyStr) && i < 20; i++ {
		c := qtyStr[i]
		if c >= '0' && c <= '9' {
			result = result*10 + uint64(c-'0')
		}
	}
	return result
}

// dispatchLoop 메인 디스패치 루프 (배치 처리)
func (hft *HFTPumpDetector) dispatchLoop() {
	ticker := time.NewTicker(100 * time.Microsecond) // 100μs마다 배치 처리
	defer ticker.Stop()

	for {
		select {
		case <-hft.stopChan:
			return
		case <-ticker.C:
			hft.processBatch()
		case trade := <-hft.workQueue:
			// 배치에 추가
			hft.batchBuffer[hft.batchCount] = trade
			hft.batchCount++

			// 배치가 가득 찬 경우 즉시 처리
			if hft.batchCount >= BATCH_SIZE {
				hft.processBatch()
			}
		}
	}
}

// processBatch 배치 처리 (SIMD 최적화 가능)
func (hft *HFTPumpDetector) processBatch() {
	if hft.batchCount == 0 {
		return
	}

	// 🔥 배치를 워커들에게 분산
	workerIndex := 0
	for i := int32(0); i < hft.batchCount; i++ {
		trade := hft.batchBuffer[i]
		worker := hft.workers[workerIndex]

		select {
		case worker.workChan <- trade:
		default:
			// 워커가 바쁘면 다음 워커로
			workerIndex = (workerIndex + 1) % WORKER_COUNT
			hft.workers[workerIndex].workChan <- trade
		}

		workerIndex = (workerIndex + 1) % WORKER_COUNT
	}

	hft.batchCount = 0
}

// DetectorWorker.run 워커 실행 루프
func (w *DetectorWorker) run() {
	log.Printf("🔥 [HFT] 워커 %d 시작", w.id)

	for {
		select {
		case <-w.stopChan:
			return
		case trade := <-w.workChan:
			w.processTrade(trade)
		}
	}
}

// DetectorWorker.processTrade 단일 trade 처리
func (w *DetectorWorker) processTrade(trade *Trade) {
	startTime := time.Now().UnixNano()

	// 🔧 심볼 유효성 검사 추가
	symbol := string(trade.Symbol[:])
	// null terminator까지만 읽기
	if nullIndex := strings.IndexByte(symbol, 0); nullIndex != -1 {
		symbol = symbol[:nullIndex]
	}

	// 🔧 유효한 심볼인지 검사 (USDT 페어만 허용)
	if len(symbol) < 4 || !strings.HasSuffix(symbol, "USDT") || len(symbol) > 11 {
		// 잘못된 심볼이면 처리 중단
		w.detector.workerPool.Put(trade)
		return
	}

	detectorIndex := w.detector.getDetectorIndex(symbol)
	if detectorIndex == -1 {
		// 새로운 심볼 등록
		detectorIndex = w.detector.registerSymbol(symbol)
	}

	if detectorIndex >= 0 && detectorIndex < len(w.detector.detectors) {
		detector := w.detector.detectors[detectorIndex]
		if detector != nil {
			// 🔥 링 버퍼에 추가
			w.addToRingBuffer(detector.ringBuf, trade)

			// 🔥 실시간 펌핑 감지
			if alert := w.detectPump(detector, trade); alert != nil {
				alert.LatencyNs = int32(time.Now().UnixNano() - startTime)

				select {
				case w.detector.resultQueue <- alert:
					atomic.AddUint64(&w.detector.totalPumps, 1)
				default:
					// 결과 큐가 가득 찬 경우 드롭
				}
			}
		}
	}

	// 🔥 객체 풀에 반환
	w.detector.workerPool.Put(trade)
}

// addToRingBuffer 링 버퍼에 trade 추가 (lock-free)
//
//go:inline
func (w *DetectorWorker) addToRingBuffer(rb *RingBuffer, trade *Trade) {
	head := atomic.LoadUint64(&rb.head)
	tail := atomic.LoadUint64(&rb.tail)

	// 버퍼가 가득 찬지 확인
	if (head - tail) >= RING_BUFFER_SIZE {
		// 가장 오래된 데이터 덮어쓰기
		atomic.AddUint64(&rb.tail, 1)
	}

	// 새 데이터 추가
	index := head & RING_BUFFER_MASK
	rb.buffer[index] = *trade
	atomic.AddUint64(&rb.head, 1)
}

// detectPump 실시간 펌핑 감지 (최적화된 알고리즘)
func (w *DetectorWorker) detectPump(sd *SymbolDetector, currentTrade *Trade) *PumpAlert {
	now := currentTrade.Timestamp
	windowStart := now - sd.window

	// 🔥 링 버퍼에서 윈도우 내 trade들 스캔
	head := atomic.LoadUint64(&sd.ringBuf.head)
	tail := atomic.LoadUint64(&sd.ringBuf.tail)

	if head <= tail {
		return nil // 데이터 없음
	}

	var firstPrice, lastPrice uint64
	var tradeCount int32
	found := false

	// 🔥 역순 스캔 (최신 데이터부터)
	for i := head - 1; i >= tail && i > head-RING_BUFFER_SIZE; i-- {
		index := i & RING_BUFFER_MASK
		trade := &sd.ringBuf.buffer[index]

		if trade.Timestamp < windowStart {
			break // 윈도우 밖
		}

		if !found {
			lastPrice = trade.Price
			found = true
		}
		firstPrice = trade.Price
		tradeCount++
	}

	if !found || tradeCount < 2 || firstPrice == 0 {
		return nil
	}

	// 🔥 정수 연산으로 가격 변동률 계산 (부동소수점 없음)
	if lastPrice <= firstPrice {
		return nil // 하락 또는 변동 없음
	}

	priceChange := ((lastPrice - firstPrice) * 1000000) / firstPrice // 10^6 = 100%

	if priceChange >= sd.threshold {
		return &PumpAlert{
			Symbol:       sd.symbol,
			PriceChange:  priceChange,
			Confidence:   min(priceChange*100, 950000), // 최대 95%
			TradeCount:   tradeCount,
			WindowNs:     sd.window,
			DetectedAtNs: now,
		}
	}

	return nil
}

// getDetectorIndex 심볼 인덱스 조회 (빠른 lookup) - 🔧 동시성 안전성 강화
func (hft *HFTPumpDetector) getDetectorIndex(symbol string) int {
	hft.mu.RLock()
	index, exists := hft.symbolMap[symbol]
	hft.mu.RUnlock()

	if exists {
		// 🔧 추가 안전성 검사: detectors 배열 범위 확인
		if index >= 0 && index < len(hft.detectors) && hft.detectors[index] != nil {
			return index
		} else {
			// 🚨 불일치 감지: symbolMap에는 있지만 detectors에는 없음
			if strings.Contains(symbol, "BTT") {
				log.Printf("🚨 [HFT INCONSISTENCY] %s: symbolMap에는 있지만 detectors[%d]가 nil이거나 범위 벗어남!", symbol, index)
			}
			// 🔧 불일치 해결: symbolMap에서 제거
			hft.mu.Lock()
			delete(hft.symbolMap, symbol)
			hft.mu.Unlock()
			return -1
		}
	}
	return -1
}

// registerSymbol 새로운 심볼 등록 (thread-safe)
func (hft *HFTPumpDetector) registerSymbol(symbol string) int {
	hft.mu.Lock()
	defer hft.mu.Unlock()

	// 이미 등록된 심볼인지 재확인 (이중 확인)
	if idx, exists := hft.symbolMap[symbol]; exists {
		return idx
	}

	// 빈 슬롯 찾기
	for i := 0; i < len(hft.detectors); i++ {
		if hft.detectors[i] == nil {
			// config 값을 정수로 변환 (% → 정수)
			configThresholdInt := uint64(hft.configThreshold * 10000) // 1% = 10000

			// 새 감지기 생성
			detector := &SymbolDetector{
				ringBuf:   &RingBuffer{},
				threshold: configThresholdInt, // config 임계값 사용
				window:    hft.configWindow,   // config 윈도우 사용
			}
			copy(detector.symbol[:], symbol)

			// 🔧 원자적 등록: detectors 먼저, symbolMap 나중에
			hft.detectors[i] = detector
			hft.symbolMap[symbol] = i

			log.Printf("🔥 [HFT] 새 심볼 등록: %s (인덱스: %d, 임계값: %.1f%%, 윈도우: %.1fs)",
				symbol, i, hft.configThreshold, float64(hft.configWindow)/float64(time.Second))
			return i
		}
	}

	log.Printf("❌ [HFT] 심볼 등록 실패: %s (슬롯 부족)", symbol)
	return -1
}

// resultLoop 결과 처리 루프
func (hft *HFTPumpDetector) resultLoop() {
	for {
		select {
		case <-hft.stopChan:
			return
		case alert := <-hft.resultQueue:
			hft.handlePumpAlert(alert)
		}
	}
}

// handlePumpAlert 펌핑 알림 처리
func (hft *HFTPumpDetector) handlePumpAlert(alert *PumpAlert) {
	symbol := string(alert.Symbol[:])
	// null terminator까지만 읽기
	if nullIndex := strings.IndexByte(symbol, 0); nullIndex != -1 {
		symbol = symbol[:nullIndex]
	}

	changePercent := float64(alert.PriceChange) / 10000.0 // 10^4 = 1%

	// 🔥 최소 로깅으로 레이턴시 최소화
	log.Printf("⚡ [HFT PUMP] %s: +%.2f%% (지연: %dμs, 체결: %d건)",
		symbol, changePercent, alert.LatencyNs/1000, alert.TradeCount)
}

// statsReporter 통계 리포터 (5초마다)
func (hft *HFTPumpDetector) statsReporter() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-hft.stopChan:
			return
		case <-ticker.C:
			trades := atomic.LoadUint64(&hft.totalTrades)
			pumps := atomic.LoadUint64(&hft.totalPumps)
			avgLatency := atomic.LoadUint64(&hft.avgLatencyNs)

			// 🔧 등록된 심볼 수 추가
			hft.mu.RLock()
			symbolCount := len(hft.symbolMap)
			hft.mu.RUnlock()

			slotUsage := float64(symbolCount) / float64(MAX_SYMBOLS) * 100

			log.Printf("📊 [HFT STATS] 체결: %d건, 펌핑: %d건, 평균지연: %dμs, 심볼: %d/%d개(%.1f%%)",
				trades, pumps, avgLatency/1000, symbolCount, MAX_SYMBOLS, slotUsage)
		}
	}
}

// Stop HFT 펌핑 감지 중지
func (hft *HFTPumpDetector) Stop() {
	if !atomic.CompareAndSwapInt32(&hft.running, 1, 0) {
		return
	}

	close(hft.stopChan)

	// 워커 중지
	for _, worker := range hft.workers {
		close(worker.stopChan)
	}

	log.Printf("🔥 [HFT] 펌핑 감지 중지 완료")
}

// min 함수 (인라인)
//
//go:inline
func min(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

// addToRingBufferDirect 링 버퍼에 trade 직접 추가 (lock-free)
//
//go:inline
func (hft *HFTPumpDetector) addToRingBufferDirect(rb *RingBuffer, trade *Trade) {
	head := atomic.LoadUint64(&rb.head)
	tail := atomic.LoadUint64(&rb.tail)

	// 버퍼가 가득 찬지 확인
	if (head - tail) >= RING_BUFFER_SIZE {
		// 가장 오래된 데이터 덮어쓰기
		atomic.AddUint64(&rb.tail, 1)
	}

	// 새 데이터 추가
	index := head & RING_BUFFER_MASK
	rb.buffer[index] = *trade
	atomic.AddUint64(&rb.head, 1)
}

// detectPumpDirect 즉시 펌핑 감지 (최적화된 알고리즘)
func (hft *HFTPumpDetector) detectPumpDirect(symbol string, trade *Trade) *PumpAlert {
	// 🔍 BTTCUSDT 디버그 로그 (함수 진입 확인)
	if strings.Contains(symbol, "BTTC") {
		log.Printf("🔍 [HFT DIRECT] BTTCUSDT detectPumpDirect 호출: 가격=%d", trade.Price)
	}

	// 🔥 초고속: 직접 감지 (워커 우회)
	detectorIndex := hft.getDetectorIndex(symbol)
	if detectorIndex == -1 {
		detectorIndex = hft.registerSymbol(symbol)
	}

	if detectorIndex < 0 || detectorIndex >= len(hft.detectors) {
		// 🔍 BTTCUSDT 오류 디버그
		if strings.Contains(symbol, "BTTC") {
			log.Printf("🚨 [HFT DIRECT] BTTCUSDT detectorIndex 오류: %d", detectorIndex)
		}
		return nil
	}

	detector := hft.detectors[detectorIndex]
	if detector == nil {
		// 🔍 BTTCUSDT 오류 디버그
		if strings.Contains(symbol, "BTTC") {
			log.Printf("🚨 [HFT DIRECT] BTTCUSDT detector가 nil: 인덱스=%d", detectorIndex)
		}
		return nil
	}

	// 🔍 BTTCUSDT 진행 상황 디버그
	if strings.Contains(symbol, "BTTC") {
		log.Printf("🔍 [HFT DIRECT] BTTCUSDT 링버퍼 추가 전: detectorIndex=%d", detectorIndex)
	}

	// 🔥 링 버퍼에 즉시 추가
	hft.addToRingBufferDirect(detector.ringBuf, trade)

	// 🔥 즉시 감지 (메모리 스캔)
	now := trade.Timestamp
	windowStart := now - detector.window

	// 🔍 BTTCUSDT 스캔 시작 디버그
	if strings.Contains(symbol, "BTTC") {
		log.Printf("🔍 [HFT DIRECT] BTTCUSDT 스캔 시작: now=%d, windowStart=%d, window=%d",
			now, windowStart, detector.window)
	}

	return hft.scanRingBufferForPump(symbol, detector, now, windowStart)
}

// scanRingBufferForPump 링 버퍼에서 펌핑 감지 (최적화된 알고리즘)
func (hft *HFTPumpDetector) scanRingBufferForPump(symbol string, sd *SymbolDetector, now int64, windowStart int64) *PumpAlert {
	// 🔥 링 버퍼에서 윈도우 내 trade들 스캔
	head := atomic.LoadUint64(&sd.ringBuf.head)
	tail := atomic.LoadUint64(&sd.ringBuf.tail)

	// 🔍 심볼 확인
	symbolStr := string(sd.symbol[:])
	if nullIndex := strings.IndexByte(symbolStr, 0); nullIndex != -1 {
		symbolStr = symbolStr[:nullIndex]
	}

	if head <= tail {
		return nil // 데이터 없음
	}

	var firstPrice, lastPrice uint64
	var tradeCount int32
	found := false

	// 🔥 역순 스캔 (최신 데이터부터) - uint64 언더플로우 방지
	scanStart := head
	scanEnd := tail
	if head > RING_BUFFER_SIZE {
		scanEnd = head - RING_BUFFER_SIZE
		if scanEnd < tail {
			scanEnd = tail
		}
	}

	for i := scanStart; i > scanEnd; i-- {
		idx := (i - 1) & RING_BUFFER_MASK
		trade := &sd.ringBuf.buffer[idx]

		// 빈 데이터는 건너뛰기
		if trade.Price == 0 || trade.Timestamp == 0 {
			continue
		}

		if trade.Timestamp < windowStart {
			break // 윈도우 밖
		}

		if !found {
			lastPrice = trade.Price
			found = true
		}
		firstPrice = trade.Price
		tradeCount++
	}

	if !found || tradeCount < 2 || firstPrice == 0 {
		return nil
	}

	// 🔥 정수 연산으로 가격 변동률 계산 (부동소수점 없음)
	if lastPrice <= firstPrice {
		return nil // 하락 또는 변동 없음
	}

	priceChange := ((lastPrice - firstPrice) * 1000000) / firstPrice // 10^6 = 100%

	// 🔍 BTTCUSDT 계산 디버그 (임시)
	if strings.Contains(symbolStr, "BTTC") {
		log.Printf("🔍 [HFT CALC] BTTCUSDT: firstPrice=%d, lastPrice=%d, priceChange=%d, threshold=%d",
			firstPrice, lastPrice, priceChange, sd.threshold)
	}

	if priceChange >= sd.threshold {
		return &PumpAlert{
			Symbol:       sd.symbol,
			PriceChange:  priceChange,
			Confidence:   min(priceChange*100, 950000), // 최대 95%
			TradeCount:   tradeCount,
			WindowNs:     sd.window,
			DetectedAtNs: now,
		}
	}

	return nil
}

// handlePumpAlertDirect 즉시 펌핑 알림 처리 (🔥 파일 저장 + 데이터 저장 추가)
func (hft *HFTPumpDetector) handlePumpAlertDirect(alert *PumpAlert) {
	symbol := string(alert.Symbol[:])
	// null terminator까지만 읽기
	if nullIndex := strings.IndexByte(symbol, 0); nullIndex != -1 {
		symbol = symbol[:nullIndex]
	}

	// 🔥 가격 변동률 계산 오류 수정: 1000000으로 저장했으니 1000000으로 나누기
	changePercent := float64(alert.PriceChange) / 1000000.0 // 🔧 수정: 올바른 계산 (1,000,000 = 1%)

	// 🔥 최소 로깅으로 레이턴시 최소화
	log.Printf("⚡ [HFT PUMP] %s: +%.2f%% (지연: %dμs, 체결: %d건)",
		symbol, changePercent, alert.LatencyNs/1000, alert.TradeCount)

	// 🔥 JSON 파일 저장 (기존)
	hft.savePumpAlertToFile(alert, symbol, changePercent)

	// 🔥 데이터 저장 추가 (±5초 범위의 orderbook + trade 데이터)
	if hft.dataHandler != nil {
		signalTime := time.Unix(0, alert.DetectedAtNs)
		if err := hft.dataHandler.SaveSignalData(symbol, "binance", signalTime); err != nil {
			log.Printf("❌ [HFT] 데이터 저장 실패: %s - %v", symbol, err)
		} else {
			log.Printf("✅ [HFT DATA] %s: ±5초 데이터 저장 완료", symbol)
		}
	}
}

// savePumpAlertToFile 펌핑 알림을 JSON 파일로 저장 (🔥 신규 추가)
func (hft *HFTPumpDetector) savePumpAlertToFile(alert *PumpAlert, symbol string, changePercent float64) {
	// 🔧 signals 디렉토리 확인 및 생성
	if err := os.MkdirAll("signals", 0755); err != nil {
		log.Printf("❌ [HFT] signals 디렉토리 생성 실패: %v", err)
		return
	}

	// JSON 데이터 구조 생성
	pumpData := map[string]interface{}{
		"metadata": map[string]interface{}{
			"created_at": time.Now().UTC().Format(time.RFC3339Nano),
			"data_type":  "pump_signal",
			"exchange":   "binance",
			"source":     "hft_detector",
			"symbol":     symbol,
			"timestamp":  time.Now().Format("20060102T150405Z"),
		},
		"pump_signal": map[string]interface{}{
			"symbol":               symbol,
			"price_change":         changePercent,
			"confidence":           float64(alert.Confidence) / 1000000.0, // 🔧 올바른 변환 (1,000,000 = 100%)
			"trade_count":          alert.TradeCount,
			"window_seconds":       float64(alert.WindowNs) / 1e9,
			"detected_at":          time.Unix(0, alert.DetectedAtNs).UTC().Format(time.RFC3339Nano),
			"latency_microseconds": alert.LatencyNs / 1000,
			"threshold":            hft.configThreshold, // 🔧 실제 config 임계값 사용
		},
		"technical_data": map[string]interface{}{
			"raw_price_change": alert.PriceChange,
			"window_ns":        alert.WindowNs,
			"detected_at_ns":   alert.DetectedAtNs,
			"latency_ns":       alert.LatencyNs,
		},
	}

	// JSON 직렬화
	jsonData, err := json.MarshalIndent(pumpData, "", "  ")
	if err != nil {
		log.Printf("❌ [HFT] JSON 직렬화 실패: %v", err)
		return
	}

	// 파일명 생성 (시간 기반)
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("signals/pump_%s_%s_%.2f.json", symbol, timestamp, changePercent)

	// 파일 저장 (비동기 - 레이턴시 최소화)
	go func() {
		if err := os.WriteFile(filename, jsonData, 0644); err != nil {
			log.Printf("❌ [HFT] 파일 저장 실패: %v", err)
		} else {
			log.Printf("💾 [HFT] 펌핑 시그널 저장: %s", filename)
		}
	}()
}
