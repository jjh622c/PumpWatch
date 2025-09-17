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
	TestSymbol  = "BTC"
	TestName    = "BTC-Buffer-Test"
	Version     = "buffer-test-1.0"
	WaitMinutes = 1 // 1분 대기 후 테스트
)

type BufferTestReport struct {
	TestSymbol       string            `json:"test_symbol"`
	TestStartTime    time.Time         `json:"test_start_time"`
	TestEndTime      time.Time         `json:"test_end_time"`
	BufferStartTime  time.Time         `json:"buffer_start_time"`
	BufferEndTime    time.Time         `json:"buffer_end_time"`
	ConnectionStatus map[string]bool   `json:"connection_status"`
	WorkersConnected int               `json:"workers_connected"`
	TotalSymbols     int               `json:"total_symbols"`
	CollectedTrades  int               `json:"collected_trades"`
	DataIntegrityOK  bool              `json:"data_integrity_ok"`
	PerformanceMS    map[string]int64  `json:"performance_ms"`
	StorageErrors    []string          `json:"storage_errors"`
	FailureReasons   []string          `json:"failure_reasons"`
	Recommendations  []string          `json:"recommendations"`
	Success          bool              `json:"success"`
}

func main() {
	fmt.Println("🧪 BTC 20분 순환버퍼 테스트 시작")
	fmt.Printf("🎯 테스트 심볼: %s (Bitcoin)\n", TestSymbol)
	fmt.Printf("⏰ 버퍼 준비 시간: %d분 대기 (실제 체결데이터 축적)\n", WaitMinutes)
	fmt.Println("📋 테스트 시나리오: 1분 대기 → 가짜 상장공고 → 20분 버퍼에서 데이터 접근 → 성능 검증")
	fmt.Println("⏱️  예상 총 소요시간: 약 3분")
	fmt.Println()

	var testReport BufferTestReport
	testReport.TestSymbol = TestSymbol
	testReport.TestStartTime = time.Now()
	testReport.ConnectionStatus = make(map[string]bool)
	testReport.PerformanceMS = make(map[string]int64)

	// Initialize logging
	if err := logging.InitLogger("debug", TestName, Version); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	fmt.Println("✅ 테스트 환경 초기화 완료")

	// Load main configuration
	mainConfig, err := config.LoadConfig("config/config.yaml")
	if err != nil {
		log.Fatalf("Failed to load main config: %v", err)
	}
	fmt.Println("✅ 메인 설정 로드 완료")

	// Load symbol configuration
	symbolsConfig, err := symbols.LoadSymbolsConfig("config/symbols/symbols.yaml")
	if err != nil {
		log.Fatalf("Failed to load symbols config: %v", err)
	}
	fmt.Printf("✅ 심볼 설정 로드 완료 (%d 거래소)\n", len(symbolsConfig.Exchanges))

	// Check if BTC exists in symbol lists
	checkBTCAvailability(symbolsConfig, &testReport)

	// Initialize 20-minute circular buffer
	fmt.Println("🔄 20분 순환버퍼 초기화 중...")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	circularBuffer, err := buffer.NewCircularTradeBuffer(ctx)
	if err != nil {
		log.Fatalf("Failed to create circular buffer: %v", err)
	}
	defer circularBuffer.Close()
	fmt.Println("✅ 20분 순환버퍼 초기화 완료")

	// Initialize storage manager
	fmt.Println("✅ PumpAnalyzer enabled - will be initialized externally")
	storageManager, err := storage.NewManager(mainConfig.Storage, true)
	if err != nil {
		log.Fatalf("Failed to initialize storage manager: %v", err)
	}
	defer storageManager.Close()
	fmt.Printf("✅ 스토리지 매니저 초기화 (활성화: %t)\n", storageManager != nil)

	// Initialize EnhancedTaskManager
	fmt.Println("✅ EnhancedTaskManager 초기화 중...")
	taskManager, err := websocket.NewEnhancedTaskManager(
		ctx,
		mainConfig.Exchanges,
		symbolsConfig,
		storageManager,
		circularBuffer,
	)
	if err != nil {
		log.Fatalf("Failed to initialize EnhancedTaskManager: %v", err)
	}
	defer taskManager.Close()
	fmt.Println("✅ EnhancedTaskManager 초기화 완료")

	testReport.BufferStartTime = time.Now()

	// Start collecting data
	fmt.Printf("🚀 데이터 수집 시작 (EnhancedTaskManager)...\n")
	if err := taskManager.Start(); err != nil {
		log.Fatalf("Failed to start task manager: %v", err)
	}

	// Wait for buffer to accumulate real trading data
	fmt.Printf("⏳ %d분간 실제 거래 데이터 축적 중", WaitMinutes)
	for i := 0; i < WaitMinutes*60; i += 10 {
		time.Sleep(10 * time.Second)
		fmt.Print(".")
	}
	fmt.Println(" 완료!")

	// Generate fake BTC listing signal
	fmt.Printf("🚨 가짜 %s 상장공고 시그널 발생! (%s)\n", TestSymbol, time.Now().Format("15:04:05"))

	testReport.BufferEndTime = time.Now()

	// Stop data collection
	fmt.Println("⏹️  데이터 수집 중단 중...")
	cancel()
	fmt.Println("✅ 데이터 수집 중단 완료")

	// Simulate Upbit listing announcement for BTC
	triggerTime := time.Now()
	listingEvent := models.ListingEvent{
		Symbol:       TestSymbol,
		Title:        "[신규 거래] BTC(Bitcoin) KRW 마켓 추가 - 버퍼 테스트",
		Markets:      []string{"KRW"},
		AnnouncedAt:  triggerTime,
		DetectedAt:   triggerTime,
		NoticeURL:    "https://upbit.com/service_center/notice?id=test-btc-buffer",
		TriggerTime:  triggerTime,
		IsKRWListing: true,
	}

	// Test buffer access performance and data integrity
	fmt.Println("🔍 20분 순환버퍼 성능 및 데이터 무결성 검증 중...")

	startTime := time.Now()
	collectionEvent, err := circularBuffer.ToCollectionEvent(TestSymbol, triggerTime)
	accessLatency := time.Since(startTime)

	if err != nil {
		testReport.FailureReasons = append(testReport.FailureReasons, fmt.Sprintf("Buffer access failed: %v", err))
		fmt.Printf("❌ 버퍼 접근 실패: %v\n", err)
	} else {
		testReport.PerformanceMS["buffer_access"] = accessLatency.Milliseconds()
		fmt.Printf("⚡ 버퍼 접근 성능: %v\n", accessLatency)

		// Count collected trades and validate data integrity
		validateBufferData(collectionEvent, &testReport)
	}

	// Save data using storage manager
	fmt.Println("💾 데이터 저장 중...")
	if collectionEvent != nil {
		saveStartTime := time.Now()

		if err := storageManager.SaveCollectionEvent(&listingEvent, collectionEvent); err != nil {
			testReport.StorageErrors = append(testReport.StorageErrors, err.Error())
			fmt.Printf("❌ 저장 실패: %v\n", err)
		} else {
			saveLatency := time.Since(saveStartTime)
			testReport.PerformanceMS["storage_save"] = saveLatency.Milliseconds()
			fmt.Printf("✅ 저장 완료 (소요시간: %v)\n", saveLatency)

			// Verify stored files
			verifyStoredFiles(&testReport)
		}
	}

	fmt.Println("🧪 BTC 20분 순환버퍼 테스트 완료")
	fmt.Println()

	// Generate test report
	testReport.TestEndTime = time.Now()
	generateTestReport(&testReport)

	fmt.Println("📊 BTC 20분 순환버퍼 테스트 최종 결과")
	fmt.Printf("✅ 성공 여부: %t\n", testReport.Success)
	fmt.Printf("📈 수집된 거래: %d건\n", testReport.CollectedTrades)
	fmt.Printf("⚡ 버퍼 접근 성능: %dms\n", testReport.PerformanceMS["buffer_access"])
	fmt.Printf("💾 저장 성능: %dms\n", testReport.PerformanceMS["storage_save"])
	fmt.Printf("🔒 데이터 무결성: %t\n", testReport.DataIntegrityOK)

	if len(testReport.FailureReasons) > 0 {
		fmt.Println("❌ 실패 사유:")
		for _, reason := range testReport.FailureReasons {
			fmt.Printf("  - %s\n", reason)
		}
	}

	if len(testReport.Recommendations) > 0 {
		fmt.Println("💡 권장사항:")
		for _, rec := range testReport.Recommendations {
			fmt.Printf("  - %s\n", rec)
		}
	}
}

func validateBufferData(collectionEvent *models.CollectionEvent, report *BufferTestReport) {
	if collectionEvent == nil {
		report.FailureReasons = append(report.FailureReasons, "CollectionEvent is nil")
		return
	}

	totalTrades := 0
	btcTradesFound := 0
	nonBtcTradesFound := 0

	// Count trades from all exchanges
	for exchange, trades := range map[string][]models.TradeEvent{
		"binance_spot":    collectionEvent.BinanceSpot,
		"binance_futures": collectionEvent.BinanceFutures,
		"bybit_spot":      collectionEvent.BybitSpot,
		"bybit_futures":   collectionEvent.BybitFutures,
		"okx_spot":        collectionEvent.OKXSpot,
		"okx_futures":     collectionEvent.OKXFutures,
		"kucoin_spot":     collectionEvent.KuCoinSpot,
		"kucoin_futures":  collectionEvent.KuCoinFutures,
		"gate_spot":       collectionEvent.GateSpot,
		"gate_futures":    collectionEvent.GateFutures,
		"phemex_spot":     collectionEvent.PhemexSpot,
		"phemex_futures":  collectionEvent.PhemexFutures,
	} {
		totalTrades += len(trades)

		// 심볼 필터링 검증 (BTC 관련 거래만 포함되었는지)
		for _, trade := range trades {
			if containsBTC(trade.Symbol) {
				btcTradesFound++
			} else {
				nonBtcTradesFound++
				fmt.Printf("  ⚠️ %s에서 비BTC 거래 발견: %s\n", exchange, trade.Symbol)
			}
		}
	}

	report.CollectedTrades = totalTrades
	report.DataIntegrityOK = nonBtcTradesFound == 0 // 모든 거래가 BTC 관련이어야 함

	if report.DataIntegrityOK {
		fmt.Printf("  ✅ 심볼 필터링 정상: %d BTC 거래, %d 무관 거래\n", btcTradesFound, nonBtcTradesFound)
	} else {
		fmt.Printf("  ❌ 심볼 필터링 문제: %d BTC 거래, %d 무관 거래 (필터링 안됨)\n", btcTradesFound, nonBtcTradesFound)
	}

	// Performance evaluation
	if totalTrades == 0 {
		report.FailureReasons = append(report.FailureReasons, "No trades collected during buffer period")
		report.Recommendations = append(report.Recommendations, "BTC가 실제로 거래되고 있는지 확인 필요")
	} else if totalTrades < 10 {
		report.Recommendations = append(report.Recommendations, "BTC 거래량이 예상보다 적음 - 더 긴 수집 시간 고려")
	}
}

func checkBTCAvailability(symbolsConfig *symbols.SymbolsConfig, report *BufferTestReport) {
	fmt.Println("🔍 BTC 심볼 가용성 확인 중...")

	for _, exchange := range []string{"binance", "bybit", "okx", "kucoin", "gate", "phemex"} {
		found := false

		if exchangeConfig, exists := symbolsConfig.Exchanges[exchange]; exists {
			// Check spot symbols
			for _, symbol := range exchangeConfig.Spot.Symbols {
				if containsBTC(symbol) {
					found = true
					break
				}
			}

			// Check futures symbols if not found in spot
			if !found {
				for _, symbol := range exchangeConfig.Futures.Symbols {
					if containsBTC(symbol) {
						found = true
						break
					}
				}
			}

			if found {
				fmt.Printf("  ✅ %s: BTC 심볼 발견\n", exchange)
			} else {
				fmt.Printf("  ❌ %s: BTC 심볼 없음\n", exchange)
			}
		} else {
			fmt.Printf("  ❌ %s: BTC 심볼 없음\n", exchange)
		}

		report.ConnectionStatus[exchange] = found
	}

	if countTrueValues(report.ConnectionStatus) > 0 {
		fmt.Printf("✅ BTC 심볼 확인 완료 (발견된 거래소: %d개)\n", countTrueValues(report.ConnectionStatus))
	} else {
		fmt.Println("⚠️ 경고: 어떤 거래소에서도 BTC 심볼을 찾을 수 없음")
		report.FailureReasons = append(report.FailureReasons, "BTC 심볼이 구독 목록에 없음")
	}
}

func containsBTC(symbol string) bool {
	// Check various BTC formats: BTC, BTCUSDT, BTC-USDT, BTC_USDT, etc.
	btcFormats := []string{"BTC", "btc", "BTCUSDT", "BTC-USDT", "BTC_USDT", "BTCUSDTM"}

	for _, format := range btcFormats {
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

func verifyStoredFiles(report *BufferTestReport) {
	// Look for BTC data directory
	dataDir := "data"

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		report.StorageErrors = append(report.StorageErrors, fmt.Sprintf("Cannot read data directory: %v", err))
		fmt.Printf("❌ 데이터 디렉토리 읽기 실패: %v\n", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() && containsString(entry.Name(), "BTC") {
			dataPath := filepath.Join(dataDir, entry.Name())
			fmt.Printf("📁 발견된 BTC 데이터 디렉토리: %s\n", entry.Name())
			// 디렉토리 구조 및 파일 내용 검증
			verifyDataDirectory(dataPath, report)
			_ = dataPath
			return
		}
	}

	report.StorageErrors = append(report.StorageErrors, "BTC 데이터 디렉토리가 생성되지 않음")
	fmt.Println("❌ BTC 데이터 디렉토리를 찾을 수 없음")
}

func containsString(str, substr string) bool {
	return len(str) >= len(substr) &&
		   (str[:len(substr)] == substr || str[len(str)-len(substr):] == substr)
}

func generateTestReport(report *BufferTestReport) {
	// Determine overall success
	report.Success = len(report.FailureReasons) == 0 &&
	                len(report.StorageErrors) == 0 &&
	                report.CollectedTrades > 0 &&
	                report.DataIntegrityOK

	// Performance analysis
	if bufferLatency, exists := report.PerformanceMS["buffer_access"]; exists {
		if bufferLatency > 100 {
			report.Recommendations = append(report.Recommendations, "버퍼 접근 성능이 목표(100ms) 초과 - 최적화 필요")
		}
	}

	if saveLatency, exists := report.PerformanceMS["storage_save"]; exists {
		if saveLatency > 1000 {
			report.Recommendations = append(report.Recommendations, "저장 성능이 목표(1초) 초과 - I/O 최적화 필요")
		}
	}

	// Save report to file
	timestamp := time.Now().Format("20060102-150405")
	reportPath := fmt.Sprintf("cmd/test-btc-buffer/BTC-buffer-test-report-%s.json", timestamp)

	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
		fmt.Printf("❌ 리포트 디렉토리 생성 실패: %v\n", err)
		return
	}

	// JSON 형태로 리포트 저장
	fmt.Printf("📋 테스트 리포트 저장: %s\n", reportPath)
	saveReportAsJSON(report, reportPath)
}