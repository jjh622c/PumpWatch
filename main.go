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
	"noticepumpcatch/internal/memory"
	"noticepumpcatch/internal/monitor"
	"noticepumpcatch/internal/signals"
	"noticepumpcatch/internal/storage"
	"noticepumpcatch/internal/triggers"
	"noticepumpcatch/internal/websocket"
)

// Application 메인 애플리케이션 구조체
type Application struct {
	config          *config.Config
	memManager      *memory.Manager
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

// Initialize 컴포넌트 초기화
func (app *Application) Initialize() error {
	log.Printf("🔧 애플리케이션 초기화 시작")

	// 메모리 관리자 생성
	app.memManager = memory.NewManager(
		app.config.Memory.MaxOrderbooksPerSymbol,
		app.config.Memory.MaxTradesPerSymbol,
		1000, // 최대 시그널 수
		app.config.Memory.OrderbookRetentionMinutes,
	)
	log.Printf("✅ 메모리 관리자 생성 완료")

	// 스토리지 관리자 생성
	storageConfig := &storage.StorageConfig{
		BaseDir:       app.config.Storage.BaseDir,
		RetentionDays: app.config.Storage.RetentionDays,
		CompressData:  app.config.Storage.CompressData,
	}
	app.storageManager = storage.NewStorageManager(storageConfig)
	log.Printf("✅ 스토리지 관리자 생성 완료")

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
	log.Printf("✅ 트리거 관리자 생성 완료")

	// 콜백 관리자 생성
	app.callbackManager = callback.NewCallbackManager()
	log.Printf("✅ 콜백 관리자 생성 완료")

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
		signalConfig,
	)
	log.Printf("✅ 시그널 관리자 생성 완료")

	// WebSocket 클라이언트 생성
	app.websocket = websocket.NewBinanceWebSocket(
		app.config.GetSymbols(),
		app.memManager,
		app.config.WebSocket.WorkerCount,
		app.config.WebSocket.BufferSize,
	)
	log.Printf("✅ WebSocket 클라이언트 생성 완료")

	// 성능 모니터 생성
	app.perfMonitor = monitor.NewPerformanceMonitor()
	log.Printf("✅ 성능 모니터 생성 완료")

	log.Printf("✅ 애플리케이션 초기화 완료")
	return nil
}

// Start 애플리케이션 시작
func (app *Application) Start() error {
	log.Printf("🚀 애플리케이션 시작")

	// WebSocket 연결
	log.Printf("🔗 WebSocket 연결 시도 중...")
	if err := app.websocket.Connect(app.ctx); err != nil {
		return err
	}
	log.Printf("✅ WebSocket 연결 성공")

	// 시그널 감지 시작
	app.signalManager.Start()
	log.Printf("✅ 시그널 감지 시작")

	// 모니터링 고루틴 시작
	go app.monitorSystem()

	return nil
}

// Stop 애플리케이션 종료
func (app *Application) Stop() error {
	log.Printf("🛑 애플리케이션 종료 시작")

	// 컨텍스트 취소
	app.cancel()

	// WebSocket 연결 해제
	if app.websocket != nil {
		app.websocket.Disconnect()
	}

	// 종료 대기
	time.Sleep(2 * time.Second)

	log.Printf("👋 애플리케이션 종료 완료")
	return nil
}

// monitorSystem 시스템 모니터링
func (app *Application) monitorSystem() {
	ticker := time.NewTicker(30 * time.Second) // 30초마다 체크
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
	// 메모리 통계
	memStats := app.memManager.GetMemoryStats()
	log.Printf("📊 메모리: 오더북 %v개, 체결 %v개, 시그널 %v개",
		memStats["total_orderbooks"], memStats["total_trades"], memStats["total_signals"])

	// WebSocket 통계
	wsStats := app.websocket.GetWorkerPoolStats()
	log.Printf("🔧 WebSocket: 연결=%v, 오더북버퍼=%v/%v, 체결버퍼=%v/%v",
		wsStats["is_connected"],
		wsStats["data_channel_buffer"], wsStats["data_channel_capacity"],
		wsStats["trade_channel_buffer"], wsStats["trade_channel_capacity"])

	// 성능 통계
	perfStats := app.perfMonitor.GetStats()
	log.Printf("⚡ 성능: 오버플로우 %v회, 지연 %v회",
		perfStats["overflow_count"], perfStats["delay_count"])

	// 트리거 통계
	triggerStats := app.triggerManager.GetStats()
	log.Printf("🚨 트리거: 총 %v개, 오늘 %v개",
		triggerStats.TotalTriggers, triggerStats.DailyTriggerCount)

	// 시그널 통계
	signalStats := app.signalManager.GetSignalStats()
	log.Printf("📈 시그널: 총 %v개, 펌핑 %v개, 평균점수 %.2f",
		signalStats["total_signals"], signalStats["pump_signals"], signalStats["avg_score"])

	// 스토리지 통계
	storageStats := app.storageManager.GetStorageStats()
	log.Printf("💾 스토리지: 시그널 %v개, 오더북 %v개, 체결 %v개, 스냅샷 %v개",
		storageStats["signals_count"], storageStats["orderbooks_count"],
		storageStats["trades_count"], storageStats["snapshots_count"])

	// 콜백 통계
	callbackStats := app.callbackManager.GetCallbackStats()
	log.Printf("📞 콜백: 상장공시 %v개 등록",
		callbackStats["listing_callbacks"])

	log.Printf("---")
}

// TriggerListingSignal 상장공시 신호 트리거 (외부에서 호출)
func (app *Application) TriggerListingSignal(symbol, exchange, source string, confidence float64) {
	app.callbackManager.TriggerListingAnnouncement(symbol, exchange, source, confidence)
	log.Printf("📢 상장공시 신호 수동 트리거: %s (신뢰도: %.2f%%)", symbol, confidence)
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

	// 애플리케이션 생성
	app := NewApplication(cfg)

	// 초기화
	if err := app.Initialize(); err != nil {
		log.Fatalf("❌ 애플리케이션 초기화 실패: %v", err)
	}

	// 시작
	if err := app.Start(); err != nil {
		log.Fatalf("❌ 애플리케이션 시작 실패: %v", err)
	}

	// 시그널 핸들링 (종료 처리)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 메인 루프
	log.Printf("🎯 시스템 실행 중... (Ctrl+C로 종료)")

	// 상장공시 테스트 (5초 후)
	go func() {
		time.Sleep(5 * time.Second)
		app.TriggerListingSignal("TESTUSDT", "binance", "manual_test", 95.0)
	}()

	for {
		select {
		case <-sigChan:
			log.Printf("🛑 종료 신호 수신")
			app.Stop()
			return
		case <-app.ctx.Done():
			log.Printf("🔴 컨텍스트 종료")
			return
		}
	}
}
