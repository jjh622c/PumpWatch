package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"PumpWatch/internal/config"
	"PumpWatch/internal/logging"
	"PumpWatch/internal/models"
	"PumpWatch/internal/storage"
	"PumpWatch/internal/symbols"
	"PumpWatch/internal/websocket"
)

const (
	TestSymbol = "SOMI"
	TestName   = "SOMI-Signal-Test"
	Version    = "test-1.0"
)

// TestReport holds test execution results
type TestReport struct {
	TestSymbol        string                       `json:"test_symbol"`
	TestStartTime     time.Time                    `json:"test_start_time"`
	TestEndTime       time.Time                    `json:"test_end_time"`
	TestDuration      string                       `json:"test_duration"`
	SignalTime        time.Time                    `json:"signal_time"`
	DataCollectionStart time.Time                  `json:"data_collection_start"`
	DataCollectionEnd   time.Time                  `json:"data_collection_end"`
	CollectionDuration  string                     `json:"collection_duration"`

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

	// Final Assessment
	TestSuccess        bool                        `json:"test_success"`
	FailureReasons     []string                    `json:"failure_reasons"`
	Recommendations    []string                    `json:"recommendations"`
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
	fmt.Println("🧪 SOMI 가짜 시그널 테스트 시작")
	fmt.Printf("🎯 테스트 심볼: %s (Somnia)\n", TestSymbol)
	fmt.Println("📋 테스트 시나리오: 1분 후 가짜 상장공고 시그널 → 20초 데이터 수집 → 결과 검증")
	fmt.Println("⏰ 예상 총 소요시간: 약 2분")
	fmt.Println()

	testReport := &TestReport{
		TestSymbol:     TestSymbol,
		TestStartTime:  time.Now(),
		ExchangeResults: make(map[string]ExchangeResult),
		ConnectionStatus: make(map[string]bool),
	}

	// Initialize test environment
	if err := initializeTestEnvironment(); err != nil {
		log.Fatalf("❌ 테스트 환경 초기화 실패: %v", err)
	}

	// Initialize logging
	if err := logging.InitGlobalLogger("somi-test", "info", "logs"); err != nil {
		log.Fatalf("❌ 로깅 초기화 실패: %v", err)
	}
	defer logging.CloseGlobalLogger()

	fmt.Println("✅ 테스트 환경 초기화 완료")

	// Load configuration
	cfg, err := config.Load("cmd/test-somi-signal/test-config.yaml")
	if err != nil {
		log.Fatalf("❌ 설정 로드 실패: %v", err)
	}

	fmt.Println("✅ 테스트 설정 로드 완료")

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

	// Initialize EnhancedTaskManager
	taskManager, err := websocket.NewEnhancedTaskManager(ctx, cfg.Exchanges, symbolsConfig, storageManager)
	if err != nil {
		log.Fatalf("❌ TaskManager 초기화 실패: %v", err)
	}

	fmt.Println("✅ EnhancedTaskManager 초기화 완료")

	// Start WebSocket connections
	if err := taskManager.Start(); err != nil {
		log.Fatalf("❌ WebSocket 시작 실패: %v", err)
	}

	fmt.Println("✅ WebSocket 연결 시작")

	// Wait for 1 minute to stabilize connections
	fmt.Println("⏳ WebSocket 연결 안정화를 위해 60초 대기 중...")
	stabilizationStart := time.Now()

	// Show countdown every 10 seconds
	for i := 60; i > 0; i -= 10 {
		fmt.Printf("⏱️  %d초 남음...\n", i)
		time.Sleep(10 * time.Second)
	}

	stabilizationDuration := time.Since(stabilizationStart)
	fmt.Printf("✅ 연결 안정화 완료 (소요시간: %v)\n", stabilizationDuration)

	// Generate fake SOMI listing signal
	fmt.Printf("🚨 가짜 SOMI 상장공고 시그널 발생! (%s)\n", time.Now().Format("15:04:05"))

	signalTime := time.Now()
	testReport.SignalTime = signalTime
	testReport.DataCollectionStart = signalTime.Add(-20 * time.Second) // -20초부터 수집 시작
	testReport.DataCollectionEnd = signalTime.Add(20 * time.Second)    // +20초까지 수집

	// Simulate Upbit listing announcement for SOMI
	listingEvent := &models.ListingEvent{
		ID:           "test-somi-001",
		Symbol:       TestSymbol,
		Title:        "[신규 거래] SOMI(Somnia) KRW 마켓 추가",
		Markets:      []string{"KRW"},
		AnnouncedAt:  signalTime,
		DetectedAt:   time.Now(),
		NoticeURL:    "https://upbit.com/service_center/notice?id=test-somi",
		TriggerTime:  signalTime,
		IsKRWListing: true,
	}

	// Trigger data collection
	fmt.Println("📊 데이터 수집 시작...")
	fmt.Printf("📍 수집 구간: %s ~ %s (40초간)\n",
		testReport.DataCollectionStart.Format("15:04:05"),
		testReport.DataCollectionEnd.Format("15:04:05"))

	if err := taskManager.StartDataCollection(TestSymbol, signalTime); err != nil {
		fmt.Printf("❌ 데이터 수집 시작 실패: %v\n", err)
		testReport.FailureReasons = append(testReport.FailureReasons,
			fmt.Sprintf("데이터 수집 시작 실패: %v", err))
	}

	// Store listing event metadata
	if err := storageManager.StoreListingEvent(listingEvent); err != nil {
		fmt.Printf("⚠️ 메타데이터 저장 실패: %v\n", err)
		testReport.StorageErrors = append(testReport.StorageErrors,
			fmt.Sprintf("메타데이터 저장 실패: %v", err))
	} else {
		fmt.Println("✅ 상장 메타데이터 저장 완료")
	}

	// Wait for data collection (40 seconds: -20s to +20s)
	fmt.Println("⏳ 데이터 수집 진행 중... (40초)")

	collectionStart := time.Now()
	for i := 40; i > 0; i -= 5 {
		fmt.Printf("⏱️  데이터 수집 %d초 남음...\n", i)
		time.Sleep(5 * time.Second)

		// Show current collection stats every 5 seconds
		showCollectionProgress(taskManager)
	}

	collectionDuration := time.Since(collectionStart)
	fmt.Printf("✅ 데이터 수집 완료 (실제 소요시간: %v)\n", collectionDuration)

	// Wait additional time for data processing
	fmt.Println("⏳ 데이터 처리 및 저장을 위해 추가 10초 대기...")
	time.Sleep(10 * time.Second)

	// Get collection results
	fmt.Println("📊 수집 결과 분석 중...")
	collectResults(taskManager, testReport)

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
	testReport.CollectionDuration = testReport.DataCollectionEnd.Sub(testReport.DataCollectionStart).String()

	// Assess test success
	assessTestResults(testReport)

	// Save test report
	saveTestReport(testReport)

	// Print final results
	printFinalResults(testReport)

	fmt.Println()
	fmt.Println("🧪 SOMI 가짜 시그널 테스트 완료")

	if testReport.TestSuccess {
		fmt.Println("✅ 테스트 성공!")
		os.Exit(0)
	} else {
		fmt.Println("❌ 테스트 실패!")
		os.Exit(1)
	}
}

func initializeTestEnvironment() error {
	// Create test directories
	testDirs := []string{
		"cmd/test-somi-signal/data",
		"cmd/test-somi-signal/logs",
	}

	for _, dir := range testDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("디렉토리 생성 실패 %s: %w", dir, err)
		}
	}

	return nil
}

func checkSOMIAvailability(symbolsConfig *symbols.SymbolsConfig, report *TestReport) {
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
	// Check various SOMI formats: SOMI, SOMIUSDT, SOMI-USDT, SOMI_USDT
	somiFormats := []string{"SOMI", "somi", "SOMIUSDT", "SOMI-USDT", "SOMI_USDT"}
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

func showCollectionProgress(taskManager *websocket.EnhancedTaskManager) {
	// This would ideally get real-time stats from task manager
	// For now, just show a progress indicator
	fmt.Printf("    📈 수집 진행률: WebSocket 연결 활성, 데이터 수신 대기 중...\n")
}

func collectResults(taskManager *websocket.EnhancedTaskManager, report *TestReport) {
	// Get task manager stats
	stats := taskManager.GetStats()
	report.WorkersStarted = stats.TotalWorkers
	report.WorkersActive = stats.ActiveWorkers
	report.TotalTrades = int(stats.TotalMessagesReceived) // Approximation

	fmt.Printf("📊 워커 상태: %d/%d 활성\n", report.WorkersActive, report.WorkersStarted)
	fmt.Printf("📊 총 메시지 수신: %d개\n", stats.TotalMessagesReceived)

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

	report.DataCollected = report.TotalTrades > 0
}

func verifyStoredFiles(report *TestReport) {
	testDataDir := "cmd/test-somi-signal/data"

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

func assessTestResults(report *TestReport) {
	report.TestSuccess = true

	// Check if workers started
	if report.WorkersStarted == 0 {
		report.TestSuccess = false
		report.FailureReasons = append(report.FailureReasons, "워커가 시작되지 않음")
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

func saveTestReport(report *TestReport) {
	reportPath := fmt.Sprintf("cmd/test-somi-signal/SOMI-test-report-%s.json",
		time.Now().Format("20060102-150405"))

	file, err := os.Create(reportPath)
	if err != nil {
		fmt.Printf("⚠️ 리포트 저장 실패: %v\n", err)
		return
	}
	defer file.Close()

	// Write JSON report (simple formatting)
	fmt.Fprintf(file, "{\n")
	fmt.Fprintf(file, "  \"test_symbol\": \"%s\",\n", report.TestSymbol)
	fmt.Fprintf(file, "  \"test_success\": %v,\n", report.TestSuccess)
	fmt.Fprintf(file, "  \"test_duration\": \"%s\",\n", report.TestDuration)
	fmt.Fprintf(file, "  \"workers_active\": %d,\n", report.WorkersActive)
	fmt.Fprintf(file, "  \"total_trades\": %d,\n", report.TotalTrades)
	fmt.Fprintf(file, "  \"data_collected\": %v,\n", report.DataCollected)
	fmt.Fprintf(file, "  \"files_created\": %d,\n", len(report.FilesCreated))
	fmt.Fprintf(file, "  \"storage_errors\": %d\n", len(report.StorageErrors))
	fmt.Fprintf(file, "}\n")

	fmt.Printf("📄 테스트 리포트 저장: %s\n", reportPath)
}

func printFinalResults(report *TestReport) {
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println("📊 SOMI 가짜 시그널 테스트 최종 결과")
	fmt.Println("============================================================")

	fmt.Printf("🎯 테스트 심볼: %s\n", report.TestSymbol)
	fmt.Printf("⏱️  총 소요시간: %s\n", report.TestDuration)
	fmt.Printf("👷 활성 워커: %d개\n", report.WorkersActive)
	fmt.Printf("📊 수집 데이터: %d개\n", report.TotalTrades)
	fmt.Printf("💾 생성된 파일: %d개\n", len(report.FilesCreated))

	if len(report.StorageErrors) > 0 {
		fmt.Printf("⚠️ Storage 오류: %d개\n", len(report.StorageErrors))
	}

	fmt.Println()

	if report.TestSuccess {
		fmt.Println("✅ 테스트 성공!")
		fmt.Println("   - WebSocket 연결 정상")
		fmt.Println("   - 데이터 수집 완료")
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