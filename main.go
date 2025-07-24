package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"noticepumpcatch/internal/cache"
	"noticepumpcatch/internal/callback"
	"noticepumpcatch/internal/config"
	"noticepumpcatch/internal/hft"
	"noticepumpcatch/internal/latency"
	"noticepumpcatch/internal/logger"
	"noticepumpcatch/internal/memory"
	"noticepumpcatch/internal/monitor"
	"noticepumpcatch/internal/signals"
	"noticepumpcatch/internal/storage"
	syncmodule "noticepumpcatch/internal/sync"
	"noticepumpcatch/internal/triggers"
	"noticepumpcatch/internal/websocket"
)

// Application 애플리케이션 메인 구조체
type Application struct {
	config          *config.Config
	logger          *logger.Logger
	memManager      *memory.Manager
	cacheManager    *cache.CacheManager // 새 캐시 매니저 추가
	storageManager  *storage.StorageManager
	signalManager   *signals.SignalManager
	triggerManager  *triggers.Manager
	callbackManager *callback.CallbackManager
	websocket       *websocket.BinanceWebSocket
	latencyMonitor  *latency.LatencyMonitor
	symbolSyncer    *syncmodule.SymbolSyncManager
	perfMonitor     *monitor.PerformanceMonitor
	hftDetector     *hft.HFTPumpDetector // 🔥 HFT 수준 펌핑 감지기

	ctx    context.Context
	cancel context.CancelFunc
}

// NewApplication 애플리케이션 생성
func NewApplication(cfg *config.Config) *Application {
	ctx, cancel := context.WithCancel(context.Background())
	return &Application{
		config: cfg,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Initialize 애플리케이션 초기화
func (app *Application) Initialize() error {
	app.logger.LogInfo("애플리케이션 초기화 시작")

	// 메모리 관리자 생성
	app.memManager = memory.NewManager(
		app.config.Memory.MaxOrderbooksPerSymbol,
		app.config.Memory.MaxTradesPerSymbol,
		1000,                                        // 최대 시그널 수
		app.config.Memory.TradeRetentionMinutes,     // 체결 데이터 보존 시간
		app.config.Memory.OrderbookRetentionMinutes, // 오더북 데이터 보존 시간 (0.1분 = 6초)
		// 🔧 하드코딩 제거: config에서 새로 추가된 값들 전달
		app.config.Memory.CompressionIntervalSeconds,
		app.config.Memory.HeapWarningMB,
		app.config.Memory.GCThresholdOrderbooks,
		app.config.Memory.GCThresholdTrades,
		app.config.Memory.MaxGoroutines,
		app.config.Memory.MonitoringIntervalSeconds,
	)
	app.logger.LogSuccess("메모리 관리자 생성 완료")

	// 🚨 핵심: 캐시 매니저 생성 (raw 데이터 관리자 대체)
	var err error
	app.cacheManager, err = cache.NewCacheManager()
	if err != nil {
		app.logger.LogCritical("캐시 매니저 생성 실패: %v", err)
		return fmt.Errorf("캐시 매니저 생성 실패: %v", err)
	}
	app.logger.LogSuccess("캐시 매니저 생성 완료")

	// 지연 모니터링 생성
	app.latencyMonitor = latency.NewLatencyMonitor(
		2.0,  // 2초 이상 경고
		10.0, // 10초 이상 심각
		300,  // 5분(300초)마다 통계
	)
	app.logger.LogSuccess("지연 모니터링 생성 완료")

	// 스토리지 관리자 생성
	storageConfig := &storage.StorageConfig{
		BaseDir:       app.config.Storage.BaseDir,
		RetentionDays: app.config.Storage.RetentionDays,
		CompressData:  app.config.Storage.CompressData,
	}
	app.storageManager = storage.NewStorageManager(storageConfig)
	app.logger.LogSuccess("스토리지 관리자 생성 완료")

	// 트리거 관리자 생성
	triggerConfig := &triggers.TriggerConfig{
		PumpDetection: triggers.PumpDetectionConfig{
			Enabled:              app.config.Triggers.PumpDetection.Enabled,
			MinScore:             app.config.Triggers.PumpDetection.MinScore,
			VolumeThreshold:      app.config.Triggers.PumpDetection.VolumeThreshold,
			PriceChangeThreshold: app.config.Triggers.PumpDetection.PriceChangeThreshold,
			TimeWindowSeconds:    app.config.Triggers.PumpDetection.TimeWindowSeconds,
		},
		Snapshot: triggers.SnapshotConfig{
			PreTriggerSeconds:  app.config.Triggers.Snapshot.PreTriggerSeconds,
			PostTriggerSeconds: app.config.Triggers.Snapshot.PostTriggerSeconds,
			MaxSnapshotsPerDay: app.config.Triggers.Snapshot.MaxSnapshotsPerDay,
		},
	}
	app.triggerManager = triggers.NewManager(triggerConfig, app.memManager)
	app.logger.LogSuccess("트리거 관리자 생성 완료")

	// 콜백 관리자 생성
	app.callbackManager = callback.NewCallbackManager()
	app.logger.LogSuccess("콜백 관리자 생성 완료")

	// 🔥 HFT 펌핑 감지기 생성 (WebSocket 생성 전에 먼저!)
	threshold := app.config.Signals.PumpDetection.PriceChangeThreshold
	windowSeconds := app.config.Signals.PumpDetection.TimeWindowSeconds

	// dataHandler 생성 (signals manager와 동일한 방식)
	dataHandler := storage.NewSignalDataHandler(app.storageManager, app.memManager)

	app.hftDetector = hft.NewHFTPumpDetector(threshold, windowSeconds, app.memManager, dataHandler)
	app.logger.LogSuccess("HFT 펌핑 감지기 생성 완료 (임계값: %.1f%%, 윈도우: %d초)", threshold, windowSeconds)

	// 시그널 관리자 생성 (메모리 기반)
	signalConfig := &signals.SignalConfig{
		PumpDetection: signals.PumpDetectionConfig{
			Enabled:              app.config.Signals.PumpDetection.Enabled,
			MinScore:             app.config.Signals.PumpDetection.MinScore,
			VolumeThreshold:      app.config.Signals.PumpDetection.VolumeThreshold,
			PriceChangeThreshold: app.config.Signals.PumpDetection.PriceChangeThreshold,
			TimeWindowSeconds:    app.config.Signals.PumpDetection.TimeWindowSeconds,
		},
		Listing: signals.ListingConfig{
			Enabled:     app.config.Signals.Listing.Enabled,
			AutoTrigger: app.config.Signals.Listing.AutoTrigger,
		},
	}
	app.signalManager = signals.NewSignalManager(
		app.memManager,
		app.storageManager,
		app.triggerManager,
		signalConfig, // rawManager 제거됨
	)
	app.logger.LogSuccess("시그널 관리자 생성 완료 (메모리 기반)")

	// WebSocket 클라이언트 생성 (심볼 동기화 활성화시 건너뛰기)
	if !app.config.WebSocket.SyncEnabled {
		app.websocket = websocket.NewBinanceWebSocket(
			app.config.GetSymbols(),
			app.memManager,
			app.cacheManager, // 캐시 매니저 주입
			app.logger,       // 로거 주입
			app.config.WebSocket.WorkerCount,
			app.config.WebSocket.BufferSize,
			app.latencyMonitor,
			// 🔧 하드코딩 제거: config에서 새로 추가된 값들 전달
			app.config.WebSocket.MaxSymbolsPerGroup,
			app.config.WebSocket.ReportIntervalSeconds,
			app.hftDetector, // 🚀 이제 제대로 초기화된 HFT 감지기 전달!
		)
		app.logger.LogSuccess("WebSocket 클라이언트 생성 완료")
	} else {
		app.logger.LogInfo("심볼 동기화 활성화 - WebSocket 생성을 나중에 진행")
	}

	// 심볼 동기화 관리자 생성 🚀 중요!
	syncConfig := &syncmodule.Config{
		AutoSyncSymbols:     app.config.WebSocket.AutoSyncSymbols,
		SyncIntervalMinutes: app.config.WebSocket.SyncIntervalMinutes,
		SyncEnabled:         app.config.WebSocket.SyncEnabled,
		EnableUpbitFilter:   app.config.WebSocket.EnableUpbitFilter,
		UpbitSyncMinutes:    app.config.WebSocket.UpbitSyncMinutes,
	}
	app.symbolSyncer = syncmodule.NewSymbolSyncManager(
		syncConfig,
		app.logger,
		app.memManager,
		app.websocket,
		app.latencyMonitor,
	)
	app.logger.LogSuccess("심볼 동기화 관리자 생성 완료")

	// 성능 모니터 생성
	app.perfMonitor = monitor.NewPerformanceMonitor()
	app.logger.LogSuccess("성능 모니터 생성 완료")

	app.logger.LogSuccess("애플리케이션 초기화 완료")
	return nil
}

// Start 애플리케이션 시작
func (app *Application) Start() error {
	app.logger.LogInfo("애플리케이션 시작")

	// 심볼 동기화 시작 🚀 중요!
	if app.config.WebSocket.SyncEnabled {
		app.logger.LogInfo("심볼 동기화 시작...")
		if err := app.symbolSyncer.Start(); err != nil {
			app.logger.LogError("심볼 동기화 시작 실패: %v", err)
			return fmt.Errorf("심볼 동기화 시작 실패: %v", err)
		}
		app.logger.LogSuccess("심볼 동기화 시작 완료")

		// 심볼 동기화 완료 대기 (5초)
		app.logger.LogInfo("심볼 동기화 완료 대기 중...")
		time.Sleep(5 * time.Second)

		// 동기화된 심볼 목록 가져오기
		syncedSymbols := app.symbolSyncer.GetFilteredSymbols()
		app.logger.LogInfo("🔍 심볼 동기화 결과 확인: %d개 심볼 발견", len(syncedSymbols))

		if len(syncedSymbols) > 0 {
			app.logger.LogInfo("✅ 동기화된 심볼 개수: %d개", len(syncedSymbols))

			// 🚨 기존 WebSocket 인스턴스가 있으면 완전히 정리
			if app.websocket != nil {
				app.logger.LogConnection("🔴 기존 WebSocket 연결 해제 및 정리 중...")
				app.websocket.Disconnect()
				app.websocket = nil
				app.logger.LogConnection("✅ 기존 WebSocket 정리 완료")
				// 고루틴 정리를 위한 잠시 대기
				time.Sleep(100 * time.Millisecond)
			}

			app.logger.LogConnection("🔧 동기화된 심볼로 WebSocket 클라이언트 생성 시작...")
			// 동기화된 심볼로 WebSocket 클라이언트 생성
			app.websocket = websocket.NewBinanceWebSocket(
				syncedSymbols, // 동기화된 심볼 사용
				app.memManager,
				app.cacheManager,
				app.logger,
				app.config.WebSocket.WorkerCount,
				app.config.WebSocket.BufferSize,
				app.latencyMonitor,
				// 🔧 하드코딩 제거: config에서 새로 추가된 값들 전달
				app.config.WebSocket.MaxSymbolsPerGroup,
				app.config.WebSocket.ReportIntervalSeconds,
				app.hftDetector, // 🚀 HFT 수준 펌핑 감지기 전달
			)
			app.logger.LogSuccess("✅ WebSocket 클라이언트 생성 완료 (%d개 심볼)", len(syncedSymbols))
		} else {
			app.logger.LogError("⚠️  동기화된 심볼이 없음 - 시스템 중단")
			return fmt.Errorf("동기화된 심볼이 없습니다")
		}
	} else {
		app.logger.LogInfo("심볼 동기화가 비활성화됨 - 설정 파일의 심볼 사용")
	}

	// WebSocket 연결 (동기화 여부와 관계없이 app.websocket이 설정된 후 실행)
	if app.websocket == nil {
		return fmt.Errorf("WebSocket 클라이언트가 초기화되지 않았습니다")
	}

	app.logger.LogConnection("WebSocket 연결 시도 중...")
	if err := app.websocket.Connect(app.ctx); err != nil {
		app.logger.LogError("WebSocket 연결 실패: %v", err)
		return err
	}
	app.logger.LogSuccess("WebSocket 연결 성공")

	// 🔥 HFT 펌핑 감지 시작
	if err := app.hftDetector.Start(); err != nil {
		app.logger.LogError("HFT 펌핑 감지 시작 실패: %v", err)
		return err
	}
	app.logger.LogSuccess("HFT 펌핑 감지 시작 완료")

	// 시그널 감지 시작
	app.signalManager.Start()
	app.logger.LogSuccess("시그널 감지 시작")

	// 시스템 모니터링 시작
	go app.monitorSystem()

	app.logger.LogSuccess("애플리케이션 시작 완료")
	return nil
}

// Stop 애플리케이션 종료
func (app *Application) Stop() error {
	app.logger.LogShutdown("애플리케이션 종료 시작")

	// 컨텍스트 취소
	app.cancel()

	// 🔥 HFT 펌핑 감지 중지
	if app.hftDetector != nil {
		app.hftDetector.Stop()
		app.logger.LogConnection("HFT 펌핑 감지 중지")
	}

	// 시그널 관리자 중지 (🔧 고루틴 누수 방지)
	if app.signalManager != nil {
		app.signalManager.Stop()
		app.logger.LogConnection("시그널 관리자 중지")
	}

	// 심볼 동기화 중지
	if app.symbolSyncer != nil {
		app.symbolSyncer.Stop()
		app.logger.LogConnection("심볼 동기화 중지")
	}

	// WebSocket 연결 해제
	if app.websocket != nil {
		app.websocket.Disconnect()
		app.logger.LogConnection("바이낸스 WebSocket 연결 해제")
	}

	// 캐시 매니저 닫기
	if app.cacheManager != nil {
		app.cacheManager.Close()
		app.logger.LogFile("캐시 매니저 닫기")
	}

	// 로거 닫기
	if app.logger != nil {
		app.logger.Close()
	}

	app.logger.LogGoodbye("애플리케이션 종료 완료")
	return nil
}

// monitorSystem 시스템 모니터링
func (app *Application) monitorSystem() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-app.ctx.Done():
			return
		case <-ticker.C:
			app.printSystemStats()
		}
	}
}

// printSystemStats 시스템 통계 출력
func (app *Application) printSystemStats() {
	// 상태 요약 데이터 수집
	stats := make(map[string]interface{})

	// 메모리 통계
	memStats := app.memManager.GetSystemStats()
	stats["memory"] = memStats
	app.logger.LogMemory("메모리: 현재 오더북=%v개, 체결=%v개, 시그널=%v개 | 누적: 오더북=%v개, 체결=%v개 | 처리율: %.1f개/초(오더북), %.1f개/초(체결)",
		memStats["total_orderbooks"], memStats["total_trades"], memStats["total_signals"],
		memStats["cumulative_orderbooks"], memStats["cumulative_trades"],
		memStats["orderbook_rate_per_sec"], memStats["trade_rate_per_sec"])

	// WebSocket 통계
	wsStats := app.websocket.GetWorkerPoolStats()
	stats["websocket"] = wsStats
	app.logger.LogWebSocket("WebSocket: 연결=%v, 오더북버퍼=%v/%v, 체결버퍼=%v/%v",
		wsStats["is_connected"],
		wsStats["data_channel_buffer"], wsStats["data_channel_capacity"],
		wsStats["trade_channel_buffer"], wsStats["trade_channel_capacity"])

	// 지연 모니터링 통계 추가 ⭐ 새로 추가
	if app.latencyMonitor != nil {
		latencyOverall := app.latencyMonitor.GetOverallStats()
		stats["latency"] = latencyOverall

		if latencyOverall["total_records"].(int) > 0 {
			app.logger.LogLatencyStats("지연: 평균=%.3f초, 최대=%.3f초, 경고=%d개, 심각=%d개 (총 %d건)",
				latencyOverall["avg_latency"].(float64),
				latencyOverall["max_latency"].(float64),
				latencyOverall["warn_symbols"].(int),
				latencyOverall["critical_symbols"].(int),
				latencyOverall["total_records"].(int))
		} else {
			app.logger.LogLatencyStats("지연: 데이터 수집 중... (아직 기록 없음)")
		}
	}

	// 성능 통계
	perfStats := app.perfMonitor.GetStats()
	stats["performance"] = perfStats
	app.logger.LogPerformance("성능: 오버플로우 %v회, 지연 %v회",
		perfStats["overflow_count"], perfStats["delay_count"])

	// 트리거 통계
	triggerStats := app.triggerManager.GetStats()
	app.logger.LogInfo("트리거: 총 %v개, 오늘 %v개",
		triggerStats.TotalTriggers, triggerStats.DailyTriggerCount)

	// 시그널 통계
	signalStats := app.signalManager.GetSignalStats()
	app.logger.LogInfo("시그널: 총 %v개, 펌핑 %v개, 평균점수 %.2f",
		signalStats["total_signals"], signalStats["pump_signals"], signalStats["avg_score"])

	// 스토리지 통계
	storageStats := app.storageManager.GetStorageStats()
	app.logger.LogInfo("스토리지: 시그널 %v개, 오더북 %v개, 체결 %v개, 스냅샷 %v개",
		storageStats["signals_count"], storageStats["orderbooks_count"],
		storageStats["trades_count"], storageStats["snapshots_count"])

	// 콜백 통계
	callbackStats := app.callbackManager.GetCallbackStats()
	app.logger.LogInfo("콜백: 상장공시 %v개 등록",
		callbackStats["listing_callbacks"])

	// 상태 요약 출력 (콘솔에만)
	app.logger.PrintStatusSummary(stats)
}

func main() {
	// 🛡️ 패닉 복구 메커니즘 설정
	defer func() {
		if r := recover(); r != nil {
			log.Printf("🚨 [PANIC RECOVERY] 시스템 패닉 발생: %v", r)
			log.Printf("🔄 [PANIC RECOVERY] 시스템 재시작을 권장합니다")

			// 10초 대기 후 종료 (재시작 가능하도록)
			time.Sleep(10 * time.Second)
			os.Exit(1)
		}
	}()

	log.Printf("🚀 NoticePumpCatch 시스템 시작")

	// 설정 로드
	cfg, err := config.LoadConfig("")
	if err != nil {
		log.Fatalf("❌ 설정 로드 실패: %v", err)
	}

	// 설정 유효성 검사
	if err := cfg.Validate(); err != nil {
		log.Fatalf("❌ 설정 유효성 검사 실패: %v", err)
	}

	// 로거 초기화 (새로운 구조)
	loggerConfig := logger.LoggerConfig{
		Level:      logger.LogLevelFromString(cfg.Logging.Level),
		OutputFile: cfg.Logging.OutputFile,
		MaxSize:    cfg.Logging.MaxSize,
		MaxBackups: cfg.Logging.MaxBackups,
	}

	appLogger, err := logger.NewLogger(loggerConfig)
	if err != nil {
		log.Fatalf("❌ 로거 초기화 실패: %v", err)
	}
	defer appLogger.Close()

	appLogger.LogSuccess("로거 초기화 완료")
	appLogger.LogSuccess("설정 로드 완료")

	// 애플리케이션 생성
	app := NewApplication(cfg)
	app.logger = appLogger // 로거 주입

	appLogger.LogSuccess("애플리케이션 생성 완료")

	// 초기화
	if err := app.Initialize(); err != nil {
		appLogger.LogError("애플리케이션 초기화 실패: %v", err)
		log.Fatalf("❌ 애플리케이션 초기화 실패: %v", err)
	}

	// 시작
	if err := app.Start(); err != nil {
		appLogger.LogError("애플리케이션 시작 실패: %v", err)
		log.Fatalf("❌ 애플리케이션 시작 실패: %v", err)
	}

	// 시그널 핸들링 (종료 처리)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 메인 루프
	appLogger.LogInfo("시스템 실행 중... (Ctrl+C로 종료)")

	// 상장공시 테스트 제거 (시그널1 미사용)
	// go func() {
	//	time.Sleep(5 * time.Second)
	//	app.TriggerListingSignal("TESTUSDT", "binance", "manual_test", 95.0)
	// }()

	// 시스템 준비 완료
	app.logger.LogSuccess("✅ 시스템 준비 완료! Ctrl+C로 종료")

	// 🔍 시스템 모니터링 시작 (장시간 실행용)
	go app.startSystemMonitoring()

	for {
		select {
		case <-sigChan:
			appLogger.LogShutdown("종료 신호 수신")
			app.Stop()
			return
		case <-app.ctx.Done():
			appLogger.LogConnection("컨텍스트 종료")
			return
		}
	}
}

// startSystemMonitoring 시스템 모니터링 시작 (장시간 실행용)
func (app *Application) startSystemMonitoring() {
	app.logger.LogInfo("🔍 시스템 모니터링 시작 (5분 간격)")

	ticker := time.NewTicker(5 * time.Minute) // 5분마다 체크
	defer ticker.Stop()

	lastDataTime := time.Now()

	for {
		select {
		case <-ticker.C:
			app.performSystemHealthCheck(&lastDataTime)
		}
	}
}

// performSystemHealthCheck 시스템 상태 체크
func (app *Application) performSystemHealthCheck(lastDataTime *time.Time) {
	// 🛡️ 패닉 복구로 감싸기
	defer func() {
		if r := recover(); r != nil {
			app.logger.LogError("🚨 [HEALTH CHECK PANIC] 모니터링 중 패닉 발생: %v", r)
		}
	}()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	heapMB := float64(m.HeapInuse) / 1024 / 1024
	goroutines := runtime.NumGoroutine()
	gcRuns := m.NumGC

	// 📊 기본 상태 로그 (더 상세한 정보)
	app.logger.LogInfo("🔍 [HEALTH] 힙=%.1fMB, 고루틴=%d개, GC실행=%d회",
		heapMB, goroutines, gcRuns)

	// ⚠️ 메모리 사용량 경고 (2GB 이상)
	if heapMB > 2048 {
		app.logger.LogError("🚨 [MEMORY ALERT] 메모리 사용량 과다: %.1fMB (임계치: 2048MB)", heapMB)

		// 강제 GC 실행
		runtime.GC()
		app.logger.LogInfo("🗑️ [MEMORY] 강제 GC 실행")
	}

	// ⚠️ 고루틴 수 경고 (50개 이상)
	if goroutines > 50 {
		app.logger.LogError("🚨 [GOROUTINE ALERT] 고루틴 수 과다: %d개 (임계치: 50개)", goroutines)
	}

	// 📡 WebSocket 연결 상태 체크
	if app.websocket != nil {
		symbols := app.memManager.GetSymbols()
		app.logger.LogInfo("📡 [WEBSOCKET] 상태: 연결됨, 심볼=%d개", len(symbols))

		// 🔍 데이터 수집 상태 체크 (여러 심볼 확인)
		if len(symbols) > 0 {
			activeSymbols := 0

			// 처음 5개 심볼을 체크
			checkCount := 5
			if len(symbols) < checkCount {
				checkCount = len(symbols)
			}

			for i := 0; i < checkCount; i++ {
				symbol := symbols[i]
				recentTrades := app.memManager.GetRecentTrades(symbol, 1)         // 1분
				recentOrderbooks := app.memManager.GetRecentOrderbooks(symbol, 1) // 1분

				if len(recentTrades) > 0 || len(recentOrderbooks) > 0 {
					activeSymbols++
				}
			}

			if activeSymbols > 0 {
				*lastDataTime = time.Now()
				app.logger.LogInfo("✅ [DATA] 수집 정상: %d/%d 심볼 활성", activeSymbols, checkCount)
			} else {
				timeSinceLastData := time.Since(*lastDataTime)
				if timeSinceLastData > 5*time.Minute {
					app.logger.LogError("🚨 [DATA ALERT] 수집 중단 감지: %.1f분간 데이터 없음",
						timeSinceLastData.Minutes())

					// TODO: 자동 재연결 로직 추가 가능
					app.logger.LogInfo("🔄 [AUTO RECOVERY] WebSocket 재연결이 필요할 수 있습니다")
				}
			}
		}
	} else {
		app.logger.LogError("🚨 [WEBSOCKET ALERT] 연결 끊어짐")
	}

	// 🗂️ 저장된 시그널 수 확인
	signals := app.memManager.GetRecentSignals(100)
	if len(signals) > 0 {
		app.logger.LogInfo("📈 [SIGNALS] %d개 시그널 저장됨 (최근 100개 기준)", len(signals))
	} else {
		app.logger.LogInfo("📈 [SIGNALS] 저장된 시그널 없음")
	}

	// 📁 디스크 공간 체크
	app.checkDiskSpace()
}

// checkDiskSpace 디스크 공간 체크
func (app *Application) checkDiskSpace() {
	defer func() {
		if r := recover(); r != nil {
			app.logger.LogError("🚨 [DISK CHECK PANIC] 디스크 체크 중 패닉: %v", r)
		}
	}()

	// 간단한 파일 생성 테스트로 디스크 상태 확인
	testFile := "data/test_write.tmp"
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		app.logger.LogError("🚨 [DISK ALERT] 디스크 쓰기 실패: %v", err)
	} else {
		os.Remove(testFile) // 테스트 파일 삭제
		app.logger.LogInfo("💾 [DISK] 쓰기 가능 상태")
	}
}
