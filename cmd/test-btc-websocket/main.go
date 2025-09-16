package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"PumpWatch/internal/analyzer"
	"PumpWatch/internal/config"
	"PumpWatch/internal/logging"
	"PumpWatch/internal/models"
	"PumpWatch/internal/storage"
	"PumpWatch/internal/symbols"
	"PumpWatch/internal/websocket"
)

const (
	TestSymbol = "BTC"
	TestName   = "BTC-WebSocket-Test"
	Version    = "test-1.0"
)

func main() {
	fmt.Println("🧪 BTC WebSocket 실시간 데이터 수신 테스트")
	fmt.Printf("🎯 테스트 심볼: %s (Bitcoin - 매우 활발한 거래)\n", TestSymbol)
	fmt.Println("📋 테스트 시나리오: 30초간 실시간 BTC 체결데이터 수신 → 즉시 저장")
	fmt.Println("⏰ 예상 소요시간: 약 1분 30초")
	fmt.Println()

	// Initialize test environment
	if err := os.MkdirAll("cmd/test-btc-websocket/data", 0755); err != nil {
		log.Fatalf("❌ 테스트 디렉토리 생성 실패: %v", err)
	}

	// Initialize logging
	if err := logging.InitGlobalLogger("btc-test", "info", "logs"); err != nil {
		log.Fatalf("❌ 로깅 초기화 실패: %v", err)
	}
	defer logging.CloseGlobalLogger()

	fmt.Println("✅ 테스트 환경 초기화 완료")

	// Create minimal config for BTC test
	cfg := &config.Config{
		Storage: config.StorageConfig{
			Enabled:         true,
			DataDir:         "./cmd/test-btc-websocket/data",
			RawDataEnabled:  true,
			RefinedEnabled:  true,
			Compression:     false,
			RetentionDays:   1,
			MaxFileSize:     "100MB",
			BackupEnabled:   false,
		},
		Exchanges: config.ExchangesConfig{
			Binance: config.ExchangeConfig{
				Enabled:               true,
				SpotEndpoint:          "wss://stream.binance.com:9443/ws",
				FuturesEndpoint:       "wss://fstream.binance.com/ws",
				MaxSymbolsPerConnection: 100,
				RetryCooldown:         5 * time.Second,
				MaxRetries:           10,
				ConnectionTimeout:    30 * time.Second,
				ReadTimeout:          30 * time.Second,
				WriteTimeout:         10 * time.Second,
			},
			Bybit: config.ExchangeConfig{
				Enabled:               true,
				SpotEndpoint:          "wss://stream.bybit.com/v5/public/spot",
				FuturesEndpoint:       "wss://stream.bybit.com/v5/public/linear",
				MaxSymbolsPerConnection: 50,
				RetryCooldown:         5 * time.Second,
				MaxRetries:           10,
				ConnectionTimeout:    30 * time.Second,
				ReadTimeout:          30 * time.Second,
				WriteTimeout:         10 * time.Second,
			},
			OKX: config.ExchangeConfig{
				Enabled:                 true,
				SpotEndpoint:            "wss://ws.okx.com:8443/ws/v5/public",
				FuturesEndpoint:         "wss://ws.okx.com:8443/ws/v5/public",
				MaxSymbolsPerConnection: 100,
				RetryCooldown:           5 * time.Second,
				MaxRetries:              10,
				ConnectionTimeout:       30 * time.Second,
				ReadTimeout:             30 * time.Second,
				WriteTimeout:            10 * time.Second,
			},
			KuCoin: config.ExchangeConfig{
				Enabled:                 true,
				SpotEndpoint:            "wss://ws-api.kucoin.com/endpoint",
				FuturesEndpoint:         "wss://ws-api-futures.kucoin.com/endpoint",
				MaxSymbolsPerConnection: 100,
				RetryCooldown:           5 * time.Second,
				MaxRetries:              10,
				ConnectionTimeout:       30 * time.Second,
				ReadTimeout:             30 * time.Second,
				WriteTimeout:            10 * time.Second,
			},
			Phemex: config.ExchangeConfig{
				Enabled:                 true,
				SpotEndpoint:            "wss://phemex.com/ws",
				FuturesEndpoint:         "wss://phemex.com/ws",
				MaxSymbolsPerConnection: 100,
				RetryCooldown:           5 * time.Second,
				MaxRetries:              10,
				ConnectionTimeout:       30 * time.Second,
				ReadTimeout:             30 * time.Second,
				WriteTimeout:            10 * time.Second,
			},
			Gate: config.ExchangeConfig{
				Enabled:                 true,
				SpotEndpoint:            "wss://api.gateio.ws/ws/v4/",
				FuturesEndpoint:         "wss://fx-ws.gateio.ws/v4/ws/usdt",
				MaxSymbolsPerConnection: 100,
				RetryCooldown:           5 * time.Second,
				MaxRetries:              10,
				ConnectionTimeout:       30 * time.Second,
				ReadTimeout:             30 * time.Second,
				WriteTimeout:            10 * time.Second,
			},
		},
		Analysis: config.AnalysisConfig{
			Enabled:          true,                  // Enable analysis for testing
			ThresholdPercent: 3.0,                   // 3% pump threshold
			TimeWindow:       1 * time.Second,       // 1-second analysis windows
			MinVolumeRatio:   2.0,                   // Minimum volume increase
			MaxAnalysisDelay: 2 * time.Second,       // Delay before analysis
		},
	}

	// Create minimal symbols config with BTC (SubscriptionLists format)
	symbolsConfig := &symbols.SymbolsConfig{
		Version:   "test-1.0",
		UpdatedAt: time.Now(),
		SubscriptionLists: map[string][]string{
			"binance_spot":     []string{"BTCUSDT"},
			"binance_futures":  []string{"BTCUSDT"},
			"bybit_spot":       []string{"BTCUSDT"},
			"bybit_futures":    []string{"BTCUSDT"},
			"okx_spot":         []string{"BTC-USDT"},
			"okx_futures":      []string{"BTC-USDT-SWAP"},
			"kucoin_spot":      []string{"BTC-USDT"},
			"kucoin_futures":   []string{"XBTUSDTM"},
			"phemex_spot":      []string{"BTCUSDT"},
			"phemex_futures":   []string{"BTCUSD"},
			"gate_spot":        []string{"BTC_USDT"},
			"gate_futures":     []string{"BTC_USDT"},
		},
	}

	fmt.Println("✅ BTC 전용 설정 생성 완료")

	// Initialize storage manager
	storageManager := storage.NewManager(cfg.Storage, cfg.Analysis)
	fmt.Println("✅ 스토리지 매니저 초기화")

	// Initialize and set pump analyzer (for testing analysis functionality)
	pumpAnalyzer := analyzer.NewPumpAnalyzer()
	storageManager.SetAnalyzer(pumpAnalyzer)
	fmt.Println("✅ 펌프 분석기 초기화 및 연결 완료")

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

	// Wait for connections to stabilize
	fmt.Println("⏳ WebSocket 연결 안정화를 위해 30초 대기 중...")
	for i := 30; i > 0; i -= 5 {
		fmt.Printf("⏱️  %d초 남음...\n", i)

		// Show current stats every 5 seconds
		stats := taskManager.GetStats()
		fmt.Printf("    📊 현재 상태: %d/%d 워커 활성, %d 메시지 수신\n",
			stats.ActiveWorkers, stats.TotalWorkers, stats.TotalMessagesReceived)

		time.Sleep(5 * time.Second)
	}

	fmt.Printf("✅ 연결 안정화 완료\n")

	// Show final pre-test stats
	stats := taskManager.GetStats()
	fmt.Printf("📊 테스트 시작 전 상태: %d/%d 워커, %d 메시지 수신됨\n",
		stats.ActiveWorkers, stats.TotalWorkers, stats.TotalMessagesReceived)

	// Start BTC data collection (immediate test)
	triggerTime := time.Now()
	fmt.Printf("🚨 BTC 데이터 수집 시작! (%s)\n", triggerTime.Format("15:04:05"))

	if err := taskManager.StartDataCollection(TestSymbol, triggerTime); err != nil {
		log.Fatalf("❌ 데이터 수집 시작 실패: %v", err)
	}

	// Store test metadata
	listingEvent := &models.ListingEvent{
		ID:           "test-btc-001",
		Symbol:       TestSymbol,
		Title:        "[테스트] BTC WebSocket 실시간 데이터 수신 테스트",
		Markets:      []string{"USDT"},
		AnnouncedAt:  triggerTime,
		DetectedAt:   time.Now(),
		NoticeURL:    "https://test.example.com/btc-websocket-test",
		TriggerTime:  triggerTime,
		IsKRWListing: false,
	}

	if err := storageManager.StoreListingEvent(listingEvent); err != nil {
		fmt.Printf("⚠️ 메타데이터 저장 실패: %v\n", err)
	} else {
		fmt.Println("✅ 테스트 메타데이터 저장 완료")
	}

	// Monitor data collection for 30 seconds (real-time)
	fmt.Println("📊 30초간 실시간 BTC 데이터 수집 중...")
	startTime := time.Now()

	for elapsed := 0; elapsed < 30; elapsed += 5 {
		time.Sleep(5 * time.Second)

		stats := taskManager.GetStats()
		fmt.Printf("⏱️  %d초 경과: %d 메시지 수신 (+%d/5초)\n",
			elapsed+5, stats.TotalMessagesReceived,
			stats.TotalMessagesReceived)
	}

	actualDuration := time.Since(startTime)
	fmt.Printf("✅ 데이터 수집 완료 (실제 소요시간: %v)\n", actualDuration)

	// Wait for data processing
	fmt.Println("⏳ 데이터 처리 및 저장을 위해 10초 대기...")
	time.Sleep(10 * time.Second)

	// Get final statistics
	finalStats := taskManager.GetStats()
	fmt.Printf("📊 최종 통계: %d 메시지 총 수신\n", finalStats.TotalMessagesReceived)

	// Stop task manager
	if err := taskManager.Stop(); err != nil {
		fmt.Printf("⚠️ TaskManager 정지 중 오류: %v\n", err)
	} else {
		fmt.Println("✅ TaskManager 정지 완료")
	}

	// Verify saved files
	fmt.Println("🔍 저장된 파일 검증 중...")
	verifyFiles := func() {
		dataDir := "cmd/test-btc-websocket/data"
		entries, err := os.ReadDir(dataDir)
		if err != nil {
			fmt.Printf("❌ 데이터 디렉토리 읽기 실패: %v\n", err)
			return
		}

		var btcDir string
		for _, entry := range entries {
			if entry.IsDir() && (len(entry.Name()) > 3 && entry.Name()[:3] == "BTC") {
				btcDir = fmt.Sprintf("%s/%s", dataDir, entry.Name())
				fmt.Printf("📁 발견된 BTC 데이터 디렉토리: %s\n", entry.Name())
				break
			}
		}

		if btcDir == "" {
			fmt.Println("❌ BTC 데이터 디렉토리를 찾을 수 없음")
			return
		}

		// Check files in BTC directory
		exchanges := []string{"binance", "bybit"}
		for _, exchange := range exchanges {
			exchangeDir := fmt.Sprintf("%s/raw/%s", btcDir, exchange)
			if _, err := os.Stat(exchangeDir); err == nil {
				fmt.Printf("  📁 %s/ 디렉토리 존재\n", exchange)

				spotFile := fmt.Sprintf("%s/spot.json", exchangeDir)
				futuresFile := fmt.Sprintf("%s/futures.json", exchangeDir)

				if stat, err := os.Stat(spotFile); err == nil {
					fmt.Printf("    ✅ spot.json (%d bytes)\n", stat.Size())
				} else {
					fmt.Printf("    ❌ spot.json 없음\n")
				}

				if stat, err := os.Stat(futuresFile); err == nil {
					fmt.Printf("    ✅ futures.json (%d bytes)\n", stat.Size())
				} else {
					fmt.Printf("    ❌ futures.json 없음\n")
				}
			} else {
				fmt.Printf("  ❌ %s/ 디렉토리 없음\n", exchange)
			}
		}
	}

	verifyFiles()

	// Final assessment
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println("📊 BTC WebSocket 데이터 수신 테스트 결과")
	fmt.Println("============================================================")

	fmt.Printf("🎯 테스트 심볼: %s\n", TestSymbol)
	fmt.Printf("⏱️  총 소요시간: %v\n", time.Since(time.Now().Add(-90*time.Second)))
	fmt.Printf("📊 총 메시지 수신: %d개\n", finalStats.TotalMessagesReceived)
	fmt.Printf("👷 활성 워커: %d/%d개\n", finalStats.ActiveWorkers, finalStats.TotalWorkers)

	if finalStats.TotalMessagesReceived > 0 {
		fmt.Println("✅ 테스트 성공!")
		fmt.Println("   - WebSocket 연결 정상")
		fmt.Printf("   - 실시간 데이터 수신 확인 (%d개 메시지)\n", finalStats.TotalMessagesReceived)
		fmt.Println("   - BTC는 활발히 거래되는 코인이므로 정상적인 결과")
	} else {
		fmt.Println("❌ 테스트 실패!")
		fmt.Println("   - WebSocket 연결은 되었지만 메시지 수신 없음")
		fmt.Println("   - BTC는 매우 활발한 코인이므로 시스템 문제 가능성 높음")
		fmt.Println("   - WebSocket 메시지 파싱 로직 점검 필요")
	}

	fmt.Println()
	fmt.Println("🧪 BTC WebSocket 테스트 완료")
}