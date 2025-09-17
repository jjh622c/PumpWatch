package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"PumpWatch/internal/buffer"
	"PumpWatch/internal/config"
	"PumpWatch/internal/logging"
	"PumpWatch/internal/models"
	"PumpWatch/internal/storage"
	"PumpWatch/internal/symbols"
	"PumpWatch/internal/websocket"
)

const (
	TestSymbol  = "SOMI"
	TestName    = "SOMI-Buffer-Test"
	Version     = "buffer-test-1.0"
	WaitMinutes = 1 // 1분 대기 (빠른 테스트)
)

// BufferTestReport: 20분 순환버퍼 테스트 결과
type BufferTestReport struct {
	TestSymbol        string                       `json:"test_symbol"`
	TestStartTime     time.Time                    `json:"test_start_time"`
	TestEndTime       time.Time                    `json:"test_end_time"`
	TestDuration      string                       `json:"test_duration"`
	SignalTime        time.Time                    `json:"signal_time"`
	DataCollectionStart time.Time                  `json:"data_collection_start"`
	DataCollectionEnd   time.Time                  `json:"data_collection_end"`

	// Buffer Testing
	BufferWaitTime    string                       `json:"buffer_wait_time"`
	BufferEnabled     bool                         `json:"buffer_enabled"`
	BufferStats       buffer.CircularBufferStats   `json:"buffer_stats"`
	MemoryAccessTests []MemoryAccessTest           `json:"memory_access_tests"`

	// System Status
	WorkersStarted     int                         `json:"workers_started"`
	WorkersActive      int                         `json:"workers_active"`
	ConnectionStatus   map[string]bool             `json:"connection_status"`

	// Data Collection Results
	DataCollected      bool                        `json:"data_collected"`
	TotalTrades        int                         `json:"total_trades"`
	ExchangeResults    map[string]ExchangeResult   `json:"exchange_results"`

	// Storage Results
	StorageEnabled     bool                        `json:"storage_enabled"`
	FilesCreated       []string                    `json:"files_created"`
	StorageErrors      []string                    `json:"storage_errors"`

	// Performance Results
	DataIntegrityOK    bool                        `json:"data_integrity_ok"`
	PerformanceOK      bool                        `json:"performance_ok"`

	// Final Assessment
	TestSuccess        bool                        `json:"test_success"`
	FailureReasons     []string                    `json:"failure_reasons"`
	Recommendations    []string                    `json:"recommendations"`
}

// MemoryAccessTest: 메모리 접근 성능 테스트 결과
type MemoryAccessTest struct {
	TestName       string        `json:"test_name"`
	AccessTime     time.Duration `json:"access_time"`
	RecordsFound   int          `json:"records_found"`
	TargetLatency  time.Duration `json:"target_latency"`
	TestPassed     bool         `json:"test_passed"`
	TestDetails    string       `json:"test_details"`
}

type ExchangeResult struct {
	Exchange      string `json:"exchange"`
	SpotTrades    int    `json:"spot_trades"`
	FuturesTrades int    `json:"futures_trades"`
	TotalTrades   int    `json:"total_trades"`
	FirstTrade    int64  `json:"first_trade"`
	LastTrade     int64  `json:"last_trade"`
	DataReceived  bool   `json:"data_received"`
	FilesCreated  []string `json:"files_created"`
}

func main() {
	fmt.Println("🧪 SOMI 20분 순환버퍼 테스트 시작")
	fmt.Printf("🎯 테스트 심볼: %s (Somnia)\n", TestSymbol)
	fmt.Printf("⏰ 버퍼 준비 시간: %d분 대기 (실제 체결데이터 축적)\n", WaitMinutes)
	fmt.Println("📋 테스트 시나리오: 1분 대기 → 가짜 상장공고 → 20분 버퍼에서 데이터 접근 → 성능 검증")
	fmt.Printf("⏱️  예상 총 소요시간: 약 %d분\n", WaitMinutes+2)
	fmt.Println()

	testReport := &BufferTestReport{
		TestSymbol:        TestSymbol,
		TestStartTime:     time.Now(),
		ExchangeResults:   make(map[string]ExchangeResult),
		ConnectionStatus:  make(map[string]bool),
		MemoryAccessTests: make([]MemoryAccessTest, 0),
		BufferWaitTime:    fmt.Sprintf("%dm", WaitMinutes),
	}

	// Initialize test environment
	if err := initializeTestEnvironment(); err != nil {
		log.Fatalf("❌ 테스트 환경 초기화 실패: %v", err)
	}

	// Initialize logging
	if err := logging.InitGlobalLogger("somi-buffer-test", "info", "logs"); err != nil {
		log.Fatalf("❌ 로깅 초기화 실패: %v", err)
	}
	defer logging.CloseGlobalLogger()

	fmt.Println("✅ 테스트 환경 초기화 완료")

	// Load configuration
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.Fatalf("❌ 설정 로드 실패: %v", err)
	}

	fmt.Println("✅ 메인 설정 로드 완료")

	// Load symbols configuration
	symbolsConfig, err := symbols.LoadConfig("config/symbols/symbols.yaml")
	if err != nil {
		log.Fatalf("❌ 심볼 설정 로드 실패: %v", err)
	}

	fmt.Printf("✅ 심볼 설정 로드 완료 (%d 거래소)\n", len(symbolsConfig.Exchanges))

	// Check if SOMI exists in symbol lists
	checkSOMIAvailability(symbolsConfig, testReport)

	// Initialize storage manager with test directory
	storageManager := storage.NewManager(cfg.Storage, cfg.Analysis)
	testReport.StorageEnabled = cfg.Storage.Enabled

	fmt.Printf("✅ 스토리지 매니저 초기화 (활성화: %v)\n", cfg.Storage.Enabled)

	// Context for test execution
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize EnhancedTaskManager with integrated CircularBuffer
	fmt.Println("🔄 EnhancedTaskManager와 통합 20분 순환버퍼 초기화 중...")
	taskManager, err := websocket.NewEnhancedTaskManager(ctx, cfg.Exchanges, symbolsConfig, storageManager)
	if err != nil {
		log.Fatalf("❌ TaskManager 초기화 실패: %v", err)
	}

	// Get the integrated CircularTradeBuffer from the task manager
	circularBuffer := taskManager.GetCircularBuffer()
	testReport.BufferEnabled = true

	fmt.Println("✅ EnhancedTaskManager와 통합 CircularBuffer 초기화 완료")

	// Start WebSocket connections
	if err := taskManager.Start(); err != nil {
		log.Fatalf("❌ WebSocket 시작 실패: %v", err)
	}

	fmt.Println("✅ WebSocket 연결 시작")

	// **핵심**: 1분 대기 (실제 체결데이터 축적)
	fmt.Printf("⏳ 실제 체결데이터 축적을 위해 %d분 대기 중...\n", WaitMinutes)
	fmt.Println("   📊 이 시간 동안 20분 순환버퍼에 실제 체결데이터가 축적됩니다")

	bufferWaitStart := time.Now()

	// Show countdown every 30 seconds
	totalSeconds := WaitMinutes * 60
	for i := totalSeconds; i > 0; i -= 30 {
		minutes := i / 60
		seconds := i % 60
		fmt.Printf("⏱️  %dm %ds 남음... (버퍼에 실제 데이터 축적 중)\n", minutes, seconds)

		// Show buffer stats every minute
		if i%60 == 0 {
			bufferStats := circularBuffer.GetStats()
			fmt.Printf("    📊 현재 버퍼 상태: %d 이벤트, %.1fMB 메모리\n",
				bufferStats.TotalEvents, float64(bufferStats.MemoryUsage)/1024/1024)
		}

		time.Sleep(30 * time.Second)
	}

	bufferWaitDuration := time.Since(bufferWaitStart)
	fmt.Printf("✅ 버퍼 준비 완료 (소요시간: %v)\n", bufferWaitDuration)

	// Get pre-signal buffer stats
	preSignalStats := circularBuffer.GetStats()
	testReport.BufferStats = preSignalStats
	fmt.Printf("📊 시그널 전 버퍼 상태: %d 이벤트, %.1fMB 메모리, %d 핫캐시 히트\n",
		preSignalStats.TotalEvents,
		float64(preSignalStats.MemoryUsage)/1024/1024,
		preSignalStats.HotCacheHits)

	// Generate fake SOMI listing signal
	fmt.Printf("🚨 가짜 SOMI 상장공고 시그널 발생! (%s)\n", time.Now().Format("15:04:05"))

	signalTime := time.Now()
	testReport.SignalTime = signalTime
	testReport.DataCollectionStart = signalTime.Add(-20 * time.Second) // -20초부터 수집 시작
	testReport.DataCollectionEnd = signalTime.Add(20 * time.Second)    // +20초까지 수집

	// **핵심**: 20분 버퍼에서 즉시 데이터 접근 테스트
	fmt.Println("🔍 20분 순환버퍼 메모리 접근 성능 테스트...")
	performMemoryAccessTests(circularBuffer, signalTime, testReport)

	// Simulate Upbit listing announcement for SOMI
	listingEvent := &models.ListingEvent{
		ID:           "test-somi-buffer-001",
		Symbol:       TestSymbol,
		Title:        "[신규 거래] SOMI(Somnia) KRW 마켓 추가 - 버퍼 테스트",
		Markets:      []string{"KRW"},
		AnnouncedAt:  signalTime,
		DetectedAt:   time.Now(),
		NoticeURL:    "https://upbit.com/service_center/notice?id=test-somi-buffer",
		TriggerTime:  signalTime,
		IsKRWListing: true,
	}

	// **핵심**: CircularBuffer를 사용한 CollectionEvent 생성
	fmt.Println("📊 20분 버퍼에서 CollectionEvent 생성...")
	collectionEvent, err := circularBuffer.ToCollectionEvent(TestSymbol, signalTime)
	if err != nil {
		fmt.Printf("❌ CollectionEvent 생성 실패: %v\n", err)
		testReport.FailureReasons = append(testReport.FailureReasons,
			fmt.Sprintf("CollectionEvent 생성 실패: %v", err))
	} else {
		fmt.Println("✅ CollectionEvent 생성 성공")

		// 데이터 무결성 검증
		validateDataIntegrity(collectionEvent, testReport)
	}

	// Store listing event metadata
	if err := storageManager.StoreListingEvent(listingEvent); err != nil {
		fmt.Printf("⚠️ 메타데이터 저장 실패: %v\n", err)
		testReport.StorageErrors = append(testReport.StorageErrors,
			fmt.Sprintf("메타데이터 저장 실패: %v", err))
	} else {
		fmt.Println("✅ 상장 메타데이터 저장 완료")
	}

	// Store CollectionEvent using CircularBuffer data
	if collectionEvent != nil {
		if err := storageManager.StoreCollectionEvent(collectionEvent); err != nil {
			fmt.Printf("⚠️ CollectionEvent 저장 실패: %v\n", err)
			testReport.StorageErrors = append(testReport.StorageErrors,
				fmt.Sprintf("CollectionEvent 저장 실패: %v", err))
		} else {
			fmt.Println("✅ CollectionEvent 저장 완료")
		}
	}

	// Get collection results
	fmt.Println("📊 최종 수집 결과 분석 중...")
	collectResults(taskManager, circularBuffer, testReport)

	// Stop task manager
	if err := taskManager.Stop(); err != nil {
		fmt.Printf("⚠️ TaskManager 정지 중 오류: %v\n", err)
	} else {
		fmt.Println("✅ TaskManager 정지 완료")
	}

	// Verify stored files
	fmt.Println("🔍 저장된 파일 검증 중...")
	verifyStoredFiles(testReport)

	// Generate final report
	testReport.TestEndTime = time.Now()
	testReport.TestDuration = testReport.TestEndTime.Sub(testReport.TestStartTime).String()

	// Assess test success
	assessTestResults(testReport)

	// Save test report
	saveTestReport(testReport)

	// Print final results
	printFinalResults(testReport)

	fmt.Println()
	fmt.Println("🧪 SOMI 20분 순환버퍼 테스트 완료")

	if testReport.TestSuccess {
		fmt.Println("✅ 테스트 성공!")
		os.Exit(0)
	} else {
		fmt.Println("❌ 테스트 실패!")
		os.Exit(1)
	}
}

// performMemoryAccessTests: 메모리 접근 성능 테스트
func performMemoryAccessTests(circularBuffer *buffer.CircularTradeBuffer, signalTime time.Time, report *BufferTestReport) {
	exchanges := []string{"binance_spot", "binance_futures", "bybit_spot", "bybit_futures", "okx_spot", "okx_futures"}

	for _, exchange := range exchanges {
		// Test 1: 핫 캐시 접근 (최근 2분) - 목표 <100μs
		testHotCacheAccess(circularBuffer, exchange, signalTime, report)

		// Test 2: 상장 시나리오 접근 (-20s ~ +20s) - 목표 <1ms
		testListingScenarioAccess(circularBuffer, exchange, signalTime, report)

		// Test 3: TOSHI 16분 지연 시나리오 - 목표 <1ms
		testTOSHIScenarioAccess(circularBuffer, exchange, signalTime, report)
	}
}

func testHotCacheAccess(circularBuffer *buffer.CircularTradeBuffer, exchange string, signalTime time.Time, report *BufferTestReport) {
	start := time.Now()

	// 최근 2분 데이터 접근
	startTime := signalTime.Add(-2 * time.Minute)
	endTime := signalTime

	trades, err := circularBuffer.GetTradeEvents(exchange, startTime, endTime)
	accessTime := time.Since(start)

	testResult := MemoryAccessTest{
		TestName:      fmt.Sprintf("핫캐시_%s", exchange),
		AccessTime:    accessTime,
		RecordsFound:  len(trades),
		TargetLatency: 100 * time.Microsecond, // 100μs 목표
		TestPassed:    err == nil && accessTime < 100*time.Microsecond,
		TestDetails:   fmt.Sprintf("%d records in %v", len(trades), accessTime),
	}

	report.MemoryAccessTests = append(report.MemoryAccessTests, testResult)

	if testResult.TestPassed {
		fmt.Printf("  ✅ %s 핫캐시 접근: %d개 기록, %v\n", exchange, len(trades), accessTime)
	} else {
		fmt.Printf("  ❌ %s 핫캐시 접근: %d개 기록, %v (목표: <100μs)\n", exchange, len(trades), accessTime)
	}
}

func testListingScenarioAccess(circularBuffer *buffer.CircularTradeBuffer, exchange string, signalTime time.Time, report *BufferTestReport) {
	start := time.Now()

	// 상장 시나리오 (-20s ~ +20s) 데이터 접근
	startTime := signalTime.Add(-20 * time.Second)
	endTime := signalTime.Add(20 * time.Second)

	trades, err := circularBuffer.GetTradeEvents(exchange, startTime, endTime)
	accessTime := time.Since(start)

	testResult := MemoryAccessTest{
		TestName:      fmt.Sprintf("상장시나리오_%s", exchange),
		AccessTime:    accessTime,
		RecordsFound:  len(trades),
		TargetLatency: 1 * time.Millisecond, // 1ms 목표
		TestPassed:    err == nil && accessTime < 1*time.Millisecond,
		TestDetails:   fmt.Sprintf("%d records in %v", len(trades), accessTime),
	}

	report.MemoryAccessTests = append(report.MemoryAccessTests, testResult)

	if testResult.TestPassed {
		fmt.Printf("  ✅ %s 상장시나리오: %d개 기록, %v\n", exchange, len(trades), accessTime)
	} else {
		fmt.Printf("  ❌ %s 상장시나리오: %d개 기록, %v (목표: <1ms)\n", exchange, len(trades), accessTime)
	}
}

func testTOSHIScenarioAccess(circularBuffer *buffer.CircularTradeBuffer, exchange string, signalTime time.Time, report *BufferTestReport) {
	start := time.Now()

	// TOSHI 16분 지연 시나리오
	toshiListingTime := signalTime.Add(-16 * time.Minute)
	startTime := toshiListingTime.Add(-20 * time.Second)
	endTime := toshiListingTime.Add(20 * time.Second)

	trades, err := circularBuffer.GetTradeEvents(exchange, startTime, endTime)
	accessTime := time.Since(start)

	testResult := MemoryAccessTest{
		TestName:      fmt.Sprintf("TOSHI시나리오_%s", exchange),
		AccessTime:    accessTime,
		RecordsFound:  len(trades),
		TargetLatency: 1 * time.Millisecond, // 1ms 목표
		TestPassed:    err == nil && accessTime < 1*time.Millisecond,
		TestDetails:   fmt.Sprintf("%d records in %v (16min old)", len(trades), accessTime),
	}

	report.MemoryAccessTests = append(report.MemoryAccessTests, testResult)

	if testResult.TestPassed {
		fmt.Printf("  ✅ %s TOSHI(16분전): %d개 기록, %v\n", exchange, len(trades), accessTime)
	} else {
		fmt.Printf("  ❌ %s TOSHI(16분전): %d개 기록, %v (목표: <1ms)\n", exchange, len(trades), accessTime)
	}
}

// validateDataIntegrity: 데이터 무결성 검증
func validateDataIntegrity(event *models.CollectionEvent, report *BufferTestReport) {
	fmt.Println("🔍 데이터 무결성 검증 중...")

	totalTrades := len(event.BinanceSpot) + len(event.BinanceFutures) +
		len(event.BybitSpot) + len(event.BybitFutures) +
		len(event.OKXSpot) + len(event.OKXFutures) +
		len(event.KuCoinSpot) + len(event.KuCoinFutures) +
		len(event.GateSpot) + len(event.GateFutures)

	report.TotalTrades = totalTrades
	report.DataCollected = totalTrades > 0

	// 심볼 필터링 검증 (SOMI 관련 거래만 포함되었는지)
	somiTradesFound := 0
	nonSomiTradesFound := 0

	allTrades := [][]models.TradeEvent{
		event.BinanceSpot, event.BinanceFutures,
		event.BybitSpot, event.BybitFutures,
		event.OKXSpot, event.OKXFutures,
		event.KuCoinSpot, event.KuCoinFutures,
		event.GateSpot, event.GateFutures,
	}

	for _, trades := range allTrades {
		for _, trade := range trades {
			if containsSOMI(trade.Symbol) {
				somiTradesFound++
			} else {
				nonSomiTradesFound++
			}
		}
	}

	report.DataIntegrityOK = nonSomiTradesFound == 0 // 모든 거래가 SOMI 관련이어야 함

	if report.DataIntegrityOK {
		fmt.Printf("  ✅ 심볼 필터링 정상: %d SOMI 거래, %d 무관 거래\n", somiTradesFound, nonSomiTradesFound)
	} else {
		fmt.Printf("  ❌ 심볼 필터링 문제: %d SOMI 거래, %d 무관 거래 (필터링 안됨)\n", somiTradesFound, nonSomiTradesFound)
		report.FailureReasons = append(report.FailureReasons,
			fmt.Sprintf("심볼 필터링 실패: %d개 무관 거래 포함", nonSomiTradesFound))
	}

	// 시간 범위 검증
	expectedStart := event.TriggerTime.Add(-20 * time.Second)
	expectedEnd := event.TriggerTime.Add(20 * time.Second)

	fmt.Printf("  📍 예상 시간 범위: %s ~ %s\n",
		expectedStart.Format("15:04:05"), expectedEnd.Format("15:04:05"))
	fmt.Printf("  📊 수집된 데이터: 총 %d개 거래\n", totalTrades)
}

func initializeTestEnvironment() error {
	// Create test directories
	testDirs := []string{
		"cmd/test-somi-buffer/data",
		"cmd/test-somi-buffer/logs",
	}

	for _, dir := range testDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("디렉토리 생성 실패 %s: %w", dir, err)
		}
	}

	return nil
}

func checkSOMIAvailability(symbolsConfig *symbols.SymbolsConfig, report *BufferTestReport) {
	fmt.Println("🔍 SOMI 심볼 가용성 확인 중...")

	somiFound := false
	exchanges := []string{"binance", "bybit", "okx", "kucoin", "gate", "phemex"}

	for _, exchange := range exchanges {
		if exchangeConfig, exists := symbolsConfig.Exchanges[exchange]; exists {
			spotFound := false
			futuresFound := false

			// Check spot symbols
			for _, symbol := range exchangeConfig.SpotSymbols {
				if containsSOMI(symbol) {
					spotFound = true
					break
				}
			}

			// Check futures symbols
			for _, symbol := range exchangeConfig.FuturesSymbols {
				if containsSOMI(symbol) {
					futuresFound = true
					break
				}
			}

			if spotFound || futuresFound {
				somiFound = true
				report.ConnectionStatus[exchange] = true
				fmt.Printf("  ✅ %s: Spot=%v, Futures=%v\n", exchange, spotFound, futuresFound)
			} else {
				report.ConnectionStatus[exchange] = false
				fmt.Printf("  ❌ %s: SOMI 심볼 없음\n", exchange)
			}
		}
	}

	if somiFound {
		fmt.Printf("✅ SOMI 심볼 확인 완료 (발견된 거래소: %d개)\n", countTrueValues(report.ConnectionStatus))
	} else {
		fmt.Println("⚠️ 경고: 어떤 거래소에서도 SOMI 심볼을 찾을 수 없음")
		report.FailureReasons = append(report.FailureReasons, "SOMI 심볼이 구독 목록에 없음")
	}
}

func containsSOMI(symbol string) bool {
	// Check various SOMI formats: SOMI, SOMIUSDT, SOMI-USDT, SOMI_USDT, sSOMIUSDT
	somiFormats := []string{"SOMI", "somi", "SOMIUSDT", "SOMI-USDT", "SOMI_USDT", "sSOMIUSDT"}
	for _, format := range somiFormats {
		if symbol == format {
			return true
		}
	}
	return false
}

func countTrueValues(m map[string]bool) int {
	count := 0
	for _, v := range m {
		if v {
			count++
		}
	}
	return count
}

func collectResults(taskManager *websocket.EnhancedTaskManager, circularBuffer *buffer.CircularTradeBuffer, report *BufferTestReport) {
	// Get task manager stats
	stats := taskManager.GetStats()
	report.WorkersStarted = stats.TotalWorkers
	report.WorkersActive = stats.ActiveWorkers

	fmt.Printf("📊 워커 상태: %d/%d 활성\n", report.WorkersActive, report.WorkersStarted)
	fmt.Printf("📊 총 메시지 수신: %d개\n", stats.TotalMessagesReceived)

	// Get final buffer stats
	finalBufferStats := circularBuffer.GetStats()
	report.BufferStats = finalBufferStats

	fmt.Printf("📊 최종 버퍼 상태: %d 총이벤트, %d 핫이벤트, %d 콜드이벤트\n",
		finalBufferStats.TotalEvents, finalBufferStats.HotEvents, finalBufferStats.ColdEvents)
	fmt.Printf("📊 버퍼 메모리: %.1fMB, 핫캐시히트: %d, 콜드버퍼히트: %d\n",
		float64(finalBufferStats.MemoryUsage)/1024/1024,
		finalBufferStats.HotCacheHits, finalBufferStats.ColdBufferHits)

	// Collect exchange-specific results
	exchanges := []string{"binance", "bybit", "okx", "kucoin", "gate", "phemex"}
	for _, exchange := range exchanges {
		if exchangeStats, exists := stats.ExchangeStats[exchange]; exists {
			result := ExchangeResult{
				Exchange:      exchange,
				TotalTrades:   int(exchangeStats.TotalMessages),
				DataReceived:  exchangeStats.TotalMessages > 0,
			}
			report.ExchangeResults[exchange] = result

			if result.DataReceived {
				fmt.Printf("  ✅ %s: %d 메시지 수신\n", exchange, result.TotalTrades)
			} else {
				fmt.Printf("  ❌ %s: 데이터 없음\n", exchange)
			}
		}
	}

	// Performance assessment
	passedTests := 0
	totalTests := len(report.MemoryAccessTests)

	for _, test := range report.MemoryAccessTests {
		if test.TestPassed {
			passedTests++
		}
	}

	if totalTests > 0 {
		report.PerformanceOK = float64(passedTests)/float64(totalTests) >= 0.8 // 80% 통과 기준
		fmt.Printf("📊 성능 테스트: %d/%d 통과 (%.1f%%)\n",
			passedTests, totalTests, float64(passedTests)/float64(totalTests)*100)
	}
}

func verifyStoredFiles(report *BufferTestReport) {
	testDataDir := "data"

	// Look for SOMI data directory
	entries, err := os.ReadDir(testDataDir)
	if err != nil {
		report.StorageErrors = append(report.StorageErrors,
			fmt.Sprintf("데이터 디렉토리 읽기 실패: %v", err))
		return
	}

	var somiDir string
	for _, entry := range entries {
		if entry.IsDir() && containsString(entry.Name(), "SOMI") {
			somiDir = filepath.Join(testDataDir, entry.Name())
			fmt.Printf("📁 발견된 SOMI 데이터 디렉토리: %s\n", entry.Name())
			break
		}
	}

	if somiDir == "" {
		report.StorageErrors = append(report.StorageErrors, "SOMI 데이터 디렉토리가 생성되지 않음")
		fmt.Println("❌ SOMI 데이터 디렉토리를 찾을 수 없음")
		return
	}

	// Check for metadata.json
	metadataPath := filepath.Join(somiDir, "metadata.json")
	if fileExists(metadataPath) {
		report.FilesCreated = append(report.FilesCreated, "metadata.json")
		fmt.Println("  ✅ metadata.json")
	} else {
		fmt.Println("  ❌ metadata.json 없음")
	}

	// Check for raw data directory and files
	rawDir := filepath.Join(somiDir, "raw")
	if _, err := os.Stat(rawDir); err == nil {
		fmt.Println("  📁 raw/ 디렉토리 존재")

		// Check exchange directories
		exchanges := []string{"binance", "bybit", "okx", "kucoin", "gate", "phemex"}
		for _, exchange := range exchanges {
			exchangeDir := filepath.Join(rawDir, exchange)
			if _, err := os.Stat(exchangeDir); err == nil {
				fmt.Printf("    📁 %s/ 디렉토리 존재\n", exchange)

				// Check for spot.json and futures.json
				spotFile := filepath.Join(exchangeDir, "spot.json")
				futuresFile := filepath.Join(exchangeDir, "futures.json")

				if fileExists(spotFile) {
					report.FilesCreated = append(report.FilesCreated, fmt.Sprintf("%s/spot.json", exchange))
					fmt.Printf("      ✅ spot.json\n")
				}

				if fileExists(futuresFile) {
					report.FilesCreated = append(report.FilesCreated, fmt.Sprintf("%s/futures.json", exchange))
					fmt.Printf("      ✅ futures.json\n")
				}

				if !fileExists(spotFile) && !fileExists(futuresFile) {
					fmt.Printf("      ❌ 데이터 파일 없음\n")
				}
			}
		}
	} else {
		report.StorageErrors = append(report.StorageErrors, "raw/ 디렉토리가 생성되지 않음")
		fmt.Println("  ❌ raw/ 디렉토리 없음")
	}
}

func assessTestResults(report *BufferTestReport) {
	report.TestSuccess = true

	// Check buffer functionality
	if !report.BufferEnabled {
		report.TestSuccess = false
		report.FailureReasons = append(report.FailureReasons, "20분 순환버퍼가 활성화되지 않음")
	}

	// Check if workers started
	if report.WorkersStarted == 0 {
		report.TestSuccess = false
		report.FailureReasons = append(report.FailureReasons, "워커가 시작되지 않음")
	}

	// Check performance
	if !report.PerformanceOK {
		report.TestSuccess = false
		report.FailureReasons = append(report.FailureReasons, "성능 요구사항 미달 (80% 미만 통과)")
		report.Recommendations = append(report.Recommendations, "메모리 접근 성능 최적화 필요")
	}

	// Check data integrity
	if !report.DataIntegrityOK {
		report.TestSuccess = false
		report.FailureReasons = append(report.FailureReasons, "데이터 무결성 실패")
		report.Recommendations = append(report.Recommendations, "심볼 필터링 로직 점검 필요")
	}

	// Check if any data was collected
	if !report.DataCollected {
		report.TestSuccess = false
		report.FailureReasons = append(report.FailureReasons, "데이터 수집되지 않음")
		report.Recommendations = append(report.Recommendations, "SOMI가 실제로 거래되고 있는지 확인 필요")
	}

	// Check if storage worked
	if report.StorageEnabled && len(report.FilesCreated) == 0 {
		report.TestSuccess = false
		report.FailureReasons = append(report.FailureReasons, "저장된 파일이 없음")
		report.Recommendations = append(report.Recommendations, "Storage Manager 초기화 및 권한 확인 필요")
	}

	// Check for storage errors
	if len(report.StorageErrors) > 0 {
		report.TestSuccess = false
		report.Recommendations = append(report.Recommendations, "Storage 설정 및 디렉토리 권한 점검 필요")
	}
}

func saveTestReport(report *BufferTestReport) {
	reportPath := fmt.Sprintf("cmd/test-somi-buffer/SOMI-buffer-test-report-%s.json",
		time.Now().Format("20060102-150405"))

	file, err := os.Create(reportPath)
	if err != nil {
		fmt.Printf("⚠️ 리포트 저장 실패: %v\n", err)
		return
	}
	defer file.Close()

	// Write detailed JSON report
	fmt.Fprintf(file, "{\n")
	fmt.Fprintf(file, "  \"test_symbol\": \"%s\",\n", report.TestSymbol)
	fmt.Fprintf(file, "  \"test_success\": %v,\n", report.TestSuccess)
	fmt.Fprintf(file, "  \"test_duration\": \"%s\",\n", report.TestDuration)
	fmt.Fprintf(file, "  \"buffer_wait_time\": \"%s\",\n", report.BufferWaitTime)
	fmt.Fprintf(file, "  \"buffer_enabled\": %v,\n", report.BufferEnabled)
	fmt.Fprintf(file, "  \"workers_active\": %d,\n", report.WorkersActive)
	fmt.Fprintf(file, "  \"total_trades\": %d,\n", report.TotalTrades)
	fmt.Fprintf(file, "  \"data_collected\": %v,\n", report.DataCollected)
	fmt.Fprintf(file, "  \"data_integrity_ok\": %v,\n", report.DataIntegrityOK)
	fmt.Fprintf(file, "  \"performance_ok\": %v,\n", report.PerformanceOK)
	fmt.Fprintf(file, "  \"files_created\": %d,\n", len(report.FilesCreated))
	fmt.Fprintf(file, "  \"storage_errors\": %d,\n", len(report.StorageErrors))
	fmt.Fprintf(file, "  \"memory_tests_passed\": %d,\n", countPassedTests(report.MemoryAccessTests))
	fmt.Fprintf(file, "  \"buffer_memory_mb\": %.1f\n", float64(report.BufferStats.MemoryUsage)/1024/1024)
	fmt.Fprintf(file, "}\n")

	fmt.Printf("📄 테스트 리포트 저장: %s\n", reportPath)
}

func countPassedTests(tests []MemoryAccessTest) int {
	count := 0
	for _, test := range tests {
		if test.TestPassed {
			count++
		}
	}
	return count
}

func printFinalResults(report *BufferTestReport) {
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println("📊 SOMI 20분 순환버퍼 테스트 최종 결과")
	fmt.Println("============================================================")

	fmt.Printf("🎯 테스트 심볼: %s\n", report.TestSymbol)
	fmt.Printf("⏱️  총 소요시간: %s\n", report.TestDuration)
	fmt.Printf("⏰ 버퍼 대기시간: %s\n", report.BufferWaitTime)
	fmt.Printf("🔄 버퍼 활성화: %v\n", report.BufferEnabled)
	fmt.Printf("💾 버퍼 메모리: %.1f MB\n", float64(report.BufferStats.MemoryUsage)/1024/1024)
	fmt.Printf("👷 활성 워커: %d개\n", report.WorkersActive)
	fmt.Printf("📊 수집 데이터: %d개\n", report.TotalTrades)
	fmt.Printf("🚀 성능 테스트: %d/%d 통과\n", countPassedTests(report.MemoryAccessTests), len(report.MemoryAccessTests))
	fmt.Printf("🔍 데이터 무결성: %v\n", report.DataIntegrityOK)
	fmt.Printf("💾 생성된 파일: %d개\n", len(report.FilesCreated))

	if len(report.StorageErrors) > 0 {
		fmt.Printf("⚠️ Storage 오류: %d개\n", len(report.StorageErrors))
	}

	fmt.Println()

	if report.TestSuccess {
		fmt.Println("✅ 테스트 성공!")
		fmt.Println("   - 20분 순환버퍼 정상 동작")
		fmt.Println("   - 메모리 접근 성능 요구사항 충족")
		fmt.Println("   - 데이터 무결성 확인")
		fmt.Println("   - WebSocket 연결 및 데이터 수집 완료")
		fmt.Println("   - 파일 저장 완료")
	} else {
		fmt.Println("❌ 테스트 실패!")
		for _, reason := range report.FailureReasons {
			fmt.Printf("   - %s\n", reason)
		}

		if len(report.Recommendations) > 0 {
			fmt.Println()
			fmt.Println("💡 개선 권장사항:")
			for _, rec := range report.Recommendations {
				fmt.Printf("   - %s\n", rec)
			}
		}
	}
}

// Helper functions
func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}