package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"noticepumpcatch/internal/callback"
	"noticepumpcatch/internal/config"
	"noticepumpcatch/internal/logger"
	"noticepumpcatch/internal/memory"
	"noticepumpcatch/internal/monitor"
	"noticepumpcatch/internal/raw"
	"noticepumpcatch/internal/signals"
	"noticepumpcatch/internal/storage"
	"noticepumpcatch/internal/triggers"
	"noticepumpcatch/internal/websocket"
)

// Application 애플리케이션 메인 구조체
type Application struct {
	config          *config.Config
	logger          *logger.Logger
	memManager      *memory.Manager
	rawManager      *raw.RawManager // raw 데이터 관리자 추가
	storageManager  *storage.StorageManager
	signalManager   *signals.SignalManager
	triggerManager  *triggers.Manager
	callbackManager *callback.CallbackManager
	websocket       *websocket.BinanceWebSocket
	perfMonitor     *monitor.PerformanceMonitor

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
		1000, // 최대 시그널 수
		app.config.Memory.OrderbookRetentionMinutes,
	)
	app.logger.LogSuccess("메모리 관리자 생성 완료")

	// 🚨 핵심: raw 데이터 관리자 생성
	app.rawManager = raw.NewRawManager(
		"data/raw",     // raw 데이터 저장 경로
		8192,           // 버퍼 크기 (8KB)
		false,          // 압축 사용 안함 (성능 우선)
		app.memManager, // 메모리 관리자 주입
	)
	app.logger.LogSuccess("raw 데이터 관리자 생성 완료")

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

	// 시그널 관리자 생성
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
		app.rawManager, // raw 데이터 관리자 주입
		signalConfig,
	)
	app.logger.LogSuccess("시그널 관리자 생성 완료")

	// WebSocket 클라이언트 생성
	app.websocket = websocket.NewBinanceWebSocket(
		app.config.GetSymbols(),
		app.memManager,
		app.rawManager, // raw 데이터 관리자 주입
		app.logger,     // 로거 주입
		app.config.WebSocket.WorkerCount,
		app.config.WebSocket.BufferSize,
		nil, // 재연결 설정 제거
	)
	app.logger.LogSuccess("WebSocket 클라이언트 생성 완료")

	// 성능 모니터 생성
	app.perfMonitor = monitor.NewPerformanceMonitor()
	app.logger.LogSuccess("성능 모니터 생성 완료")

	app.logger.LogSuccess("애플리케이션 초기화 완료")
	return nil
}

// Start 애플리케이션 시작
func (app *Application) Start() error {
	app.logger.LogInfo("애플리케이션 시작")

	// WebSocket 연결
	app.logger.LogConnection("WebSocket 연결 시도 중...")
	if err := app.websocket.Connect(app.ctx); err != nil {
		app.logger.LogError("WebSocket 연결 실패: %v", err)
		return err
	}
	app.logger.LogSuccess("WebSocket 연결 성공")

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

	// WebSocket 연결 해제
	if app.websocket != nil {
		app.websocket.Disconnect()
		app.logger.LogConnection("바이낸스 WebSocket 연결 해제")
	}

	// raw 데이터 관리자 닫기
	if app.rawManager != nil {
		app.rawManager.Close()
		app.logger.LogFile("raw 데이터 관리자 닫기")
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
	memStats := app.memManager.GetMemoryStats()
	stats["memory"] = memStats
	app.logger.LogMemory("메모리: 오더북 %v개, 체결 %v개, 시그널 %v개",
		memStats["total_orderbooks"], memStats["total_trades"], memStats["total_signals"])

	// WebSocket 통계
	wsStats := app.websocket.GetWorkerPoolStats()
	stats["websocket"] = wsStats
	app.logger.LogWebSocket("WebSocket: 연결=%v, 오더북버퍼=%v/%v, 체결버퍼=%v/%v",
		wsStats["is_connected"],
		wsStats["data_channel_buffer"], wsStats["data_channel_capacity"],
		wsStats["trade_channel_buffer"], wsStats["trade_channel_capacity"])

	// 성능 통계
	perfStats := app.perfMonitor.GetStats()
	stats["performance"] = perfStats
	app.logger.LogPerformance("성능: 오버플로우 %v회, 지연 %v회",
		perfStats["overflow_count"], perfStats["delay_count"])

	// 트리거 통계
	triggerStats := app.triggerManager.GetStats()
	app.logger.LogTrigger("트리거: 총 %v개, 오늘 %v개",
		triggerStats.TotalTriggers, triggerStats.DailyTriggerCount)

	// 시그널 통계
	signalStats := app.signalManager.GetSignalStats()
	app.logger.LogSignal("시그널: 총 %v개, 펌핑 %v개, 평균점수 %.2f",
		signalStats["total_signals"], signalStats["pump_signals"], signalStats["avg_score"])

	// 스토리지 통계
	storageStats := app.storageManager.GetStorageStats()
	app.logger.LogStorage("스토리지: 시그널 %v개, 오더북 %v개, 체결 %v개, 스냅샷 %v개",
		storageStats["signals_count"], storageStats["orderbooks_count"],
		storageStats["trades_count"], storageStats["snapshots_count"])

	// 콜백 통계
	callbackStats := app.callbackManager.GetCallbackStats()
	app.logger.LogCallback("콜백: 상장공시 %v개 등록",
		callbackStats["listing_callbacks"])

	// 상태 요약 출력 (콘솔에만)
	app.logger.PrintStatusSummary(stats)
}

// TriggerListingSignal 상장공시 신호 트리거 (외부에서 호출)
func (app *Application) TriggerListingSignal(symbol, exchange, source string, confidence float64) {
	app.callbackManager.TriggerListingAnnouncement(symbol, exchange, source, confidence)
	app.logger.LogCallback("상장공시 신호 수동 트리거: %s (신뢰도: %.2f%%)", symbol, confidence)
}

func main() {
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

	// 상장공시 테스트 (5초 후)
	go func() {
		time.Sleep(5 * time.Second)
		app.TriggerListingSignal("TESTUSDT", "binance", "manual_test", 95.0)
	}()

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
