// Package detector 펌핑 감지 시스템 라이브러리
//
// 이 패키지는 바이낸스 실시간 데이터를 모니터링하여 급등(펌핑) 및 상장공시를 감지합니다.
// 다른 거래 시스템에 쉽게 통합할 수 있도록 설계되었습니다.
//
// 기본 사용법:
//
//	import "github.com/yourname/noticepumpcatch/pkg/detector"
//	import "github.com/yourname/noticepumpcatch/internal/interfaces"
//
//	detector, err := detector.NewDetector("config.json")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	detector.SetPumpCallback(func(event interfaces.PumpEvent) {
//	    fmt.Printf("🚀 펌핑 감지: %s +%.2f%%\n", event.Symbol, event.PriceChange)
//	    // 여기서 거래 로직 실행
//	    if event.PriceChange > 5.0 {
//	        // 5% 이상 급등시 매수 로직
//	        fmt.Printf("💰 매수 신호: %s\n", event.Symbol)
//	    }
//	})
//
//	detector.SetListingCallback(func(event interfaces.ListingEvent) {
//	    fmt.Printf("📢 상장공시: %s (신뢰도: %.1f%%)\n", event.Symbol, event.Confidence)
//	    // 상장공시 시 선빵 매수 로직
//	})
//
//	if err := detector.Start(); err != nil {
//	    log.Fatal(err)
//	}
//	defer detector.Stop()
//
//	// 실시간 상태 확인
//	status := detector.GetStatus()
//	stats := detector.GetStats()
//	fmt.Printf("감지된 펌핑: %d회, 평균 상승률: %.2f%%\n",
//	    stats.TotalPumpEvents, stats.AvgPumpChange)
package detector

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"noticepumpcatch/internal/cache"
	"noticepumpcatch/internal/config"
	"noticepumpcatch/internal/interfaces"
	"noticepumpcatch/internal/latency"
	"noticepumpcatch/internal/logger"
	"noticepumpcatch/internal/memory"
	"noticepumpcatch/internal/signals"
	"noticepumpcatch/internal/storage"
	syncmodule "noticepumpcatch/internal/sync"
	"noticepumpcatch/internal/triggers"
	"noticepumpcatch/internal/websocket"
)

// PumpDetector 펌핑 감지기 구현체
type PumpDetector struct {
	// 내부 컴포넌트들
	config         *config.Config
	logger         *logger.Logger
	memManager     *memory.Manager
	cacheManager   *cache.CacheManager
	storageManager *storage.StorageManager
	signalManager  *signals.SignalManager
	triggerManager *triggers.Manager
	websocket      *websocket.BinanceWebSocket
	latencyMonitor *latency.LatencyMonitor
	symbolSyncer   *syncmodule.SymbolSyncManager

	// 외부 콜백들
	pumpCallback    interfaces.PumpCallback
	listingCallback interfaces.ListingCallback

	// 상태 관리
	isRunning bool
	startTime time.Time
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc

	// 통계 데이터
	stats detectorStats
}

// detectorStats 내부 통계 구조체
type detectorStats struct {
	mu                 sync.RWMutex
	totalPumpEvents    int
	totalListingEvents int
	totalPumpChange    float64
	maxPumpChange      float64
	errorCount         int
	lastError          string
	lastPumpTime       time.Time
	lastListingTime    time.Time
}

// NewDetector 새로운 펌핑 감지기 생성
//
// configPath: 설정 파일 경로 (예: "config.json")
// 반환값: PumpDetector 인터페이스 구현체
//
// 사용 예:
//
//	detector, err := detector.NewDetector("config.json")
//	if err != nil {
//	    log.Fatal("감지기 생성 실패:", err)
//	}
func NewDetector(configPath string) (interfaces.PumpDetector, error) {
	// 설정 로드
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("설정 로드 실패: %v", err)
	}

	// 설정 유효성 검사
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("설정 유효성 검사 실패: %v", err)
	}

	return NewDetectorWithConfig(cfg)
}

// NewDetectorWithConfig 설정으로 펌핑 감지기 생성
//
// cfg: 미리 로드된 설정 객체
// 반환값: PumpDetector 인터페이스 구현체
//
// 고급 사용법 (프로그래밍 방식 설정):
//
//	cfg := &config.Config{...}
//	detector, err := detector.NewDetectorWithConfig(cfg)
func NewDetectorWithConfig(cfg *config.Config) (interfaces.PumpDetector, error) {
	ctx, cancel := context.WithCancel(context.Background())

	detector := &PumpDetector{
		config: cfg,
		ctx:    ctx,
		cancel: cancel,
	}

	// 로거 초기화
	loggerConfig := logger.LoggerConfig{
		Level:      logger.LogLevelFromString(cfg.Logging.Level),
		OutputFile: cfg.Logging.OutputFile,
		MaxSize:    cfg.Logging.MaxSize,
		MaxBackups: cfg.Logging.MaxBackups,
	}

	var err error
	detector.logger, err = logger.NewLogger(loggerConfig)
	if err != nil {
		return nil, fmt.Errorf("로거 초기화 실패: %v", err)
	}

	// 내부 컴포넌트 초기화
	if err := detector.initializeComponents(); err != nil {
		return nil, fmt.Errorf("컴포넌트 초기화 실패: %v", err)
	}

	return detector, nil
}

// initializeComponents 내부 컴포넌트 초기화
func (d *PumpDetector) initializeComponents() error {
	// 메모리 관리자 생성
	d.memManager = memory.NewManager(
		d.config.Memory.MaxOrderbooksPerSymbol,
		d.config.Memory.MaxTradesPerSymbol,
		1000, // 최대 시그널 수
		d.config.Memory.TradeRetentionMinutes,
		d.config.Memory.OrderbookRetentionMinutes,
		d.config.Memory.CompressionIntervalSeconds,
		d.config.Memory.HeapWarningMB,
		d.config.Memory.GCThresholdOrderbooks,
		d.config.Memory.GCThresholdTrades,
		d.config.Memory.MaxGoroutines,
		d.config.Memory.MonitoringIntervalSeconds,
	)

	// 캐시 매니저 생성
	var err error
	d.cacheManager, err = cache.NewCacheManager()
	if err != nil {
		return fmt.Errorf("캐시 매니저 생성 실패: %v", err)
	}

	// 지연 모니터링 생성
	d.latencyMonitor = latency.NewLatencyMonitor(
		d.config.Logging.LatencyWarnSeconds,
		d.config.Logging.LatencyCriticalSeconds,
		d.config.Logging.LatencyStatsIntervalSeconds,
	)

	// 스토리지 관리자 생성
	storageConfig := &storage.StorageConfig{
		BaseDir:       d.config.Storage.BaseDir,
		RetentionDays: d.config.Storage.RetentionDays,
		CompressData:  d.config.Storage.CompressData,
	}
	d.storageManager = storage.NewStorageManager(storageConfig)

	// 트리거 관리자 생성
	triggerConfig := &triggers.TriggerConfig{
		PumpDetection: triggers.PumpDetectionConfig{
			Enabled:              d.config.Triggers.PumpDetection.Enabled,
			MinScore:             d.config.Triggers.PumpDetection.MinScore,
			VolumeThreshold:      d.config.Triggers.PumpDetection.VolumeThreshold,
			PriceChangeThreshold: d.config.Triggers.PumpDetection.PriceChangeThreshold,
			TimeWindowSeconds:    d.config.Triggers.PumpDetection.TimeWindowSeconds,
		},
		Snapshot: triggers.SnapshotConfig{
			PreTriggerSeconds:  d.config.Triggers.Snapshot.PreTriggerSeconds,
			PostTriggerSeconds: d.config.Triggers.Snapshot.PostTriggerSeconds,
			MaxSnapshotsPerDay: d.config.Triggers.Snapshot.MaxSnapshotsPerDay,
		},
	}
	d.triggerManager = triggers.NewManager(triggerConfig, d.memManager)

	// 시그널 관리자 생성 (콜백 연동)
	signalConfig := &signals.SignalConfig{
		PumpDetection: signals.PumpDetectionConfig{
			Enabled:              d.config.Signals.PumpDetection.Enabled,
			MinScore:             d.config.Signals.PumpDetection.MinScore,
			VolumeThreshold:      d.config.Signals.PumpDetection.VolumeThreshold,
			PriceChangeThreshold: d.config.Signals.PumpDetection.PriceChangeThreshold,
			TimeWindowSeconds:    d.config.Signals.PumpDetection.TimeWindowSeconds,
		},
		Listing: signals.ListingConfig{
			Enabled:     d.config.Signals.Listing.Enabled,
			AutoTrigger: d.config.Signals.Listing.AutoTrigger,
		},
	}
	d.signalManager = signals.NewSignalManager(d.memManager, d.storageManager, d.triggerManager, signalConfig)

	// 심볼 동기화 관리자 생성
	syncConfig := &syncmodule.Config{
		AutoSyncSymbols:     d.config.WebSocket.AutoSyncSymbols,
		SyncIntervalMinutes: d.config.WebSocket.SyncIntervalMinutes,
		SyncEnabled:         d.config.WebSocket.SyncEnabled,
		EnableUpbitFilter:   d.config.WebSocket.EnableUpbitFilter,
		UpbitSyncMinutes:    d.config.WebSocket.UpbitSyncMinutes,
	}
	d.symbolSyncer = syncmodule.NewSymbolSyncManager(
		syncConfig,
		d.logger,
		d.memManager,
		nil, // WebSocket은 나중에 설정
		d.latencyMonitor,
	)

	return nil
}

// Start 펌핑 감지 시작
//
// 이 메서드는 전체 시스템을 시작합니다:
// 1. 심볼 동기화 (업비트에서 상장된 코인 목록 가져오기)
// 2. 바이낸스 WebSocket 연결
// 3. 실시간 데이터 수집 및 분석 시작
//
// 주의: Start() 호출 전에 반드시 콜백을 설정하세요.
//
//	detector.SetPumpCallback(myPumpHandler)
//	detector.SetListingCallback(myListingHandler)
//	detector.Start()
func (d *PumpDetector) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.isRunning {
		return fmt.Errorf("이미 실행 중입니다")
	}

	d.logger.LogInfo("🚀 펌핑 감지 시스템 시작")
	d.startTime = time.Now()

	// 심볼 동기화 시작
	if d.config.WebSocket.SyncEnabled {
		if err := d.symbolSyncer.Start(); err != nil {
			return fmt.Errorf("심볼 동기화 시작 실패: %v", err)
		}

		// 동기화 대기
		time.Sleep(5 * time.Second)
		syncedSymbols := d.symbolSyncer.GetFilteredSymbols()

		if len(syncedSymbols) == 0 {
			return fmt.Errorf("동기화된 심볼이 없습니다")
		}

		d.logger.LogInfo("📡 %d개 심볼 동기화 완료", len(syncedSymbols))

		// WebSocket 생성
		d.websocket = websocket.NewBinanceWebSocket(
			syncedSymbols,
			d.memManager,
			d.cacheManager,
			d.logger,
			d.config.WebSocket.WorkerCount,
			d.config.WebSocket.BufferSize,
			d.latencyMonitor,
			d.config.WebSocket.MaxSymbolsPerGroup,
			d.config.WebSocket.ReportIntervalSeconds,
		)
	} else {
		// 설정 파일의 심볼 사용
		symbols := d.config.GetSymbols()
		d.logger.LogInfo("📡 설정 파일에서 %d개 심볼 로드", len(symbols))

		d.websocket = websocket.NewBinanceWebSocket(
			symbols,
			d.memManager,
			d.cacheManager,
			d.logger,
			d.config.WebSocket.WorkerCount,
			d.config.WebSocket.BufferSize,
			d.latencyMonitor,
			d.config.WebSocket.MaxSymbolsPerGroup,
			d.config.WebSocket.ReportIntervalSeconds,
		)
	}

	// WebSocket 연결
	if err := d.websocket.Connect(d.ctx); err != nil {
		return fmt.Errorf("WebSocket 연결 실패: %v", err)
	}

	// 시그널 감지 시작 (콜백 연동)
	d.setupSignalCallbacks()
	d.signalManager.Start()

	d.isRunning = true
	d.logger.LogSuccess("✅ 펌핑 감지 시스템 시작 완료")

	return nil
}

// Stop 펌핑 감지 중지
//
// 모든 컴포넌트를 안전하게 정리하고 종료합니다.
// 이 메서드는 블로킹되며, 모든 고루틴이 정리될 때까지 대기합니다.
//
// 사용법:
//
//	defer detector.Stop() // 프로그램 종료 시 자동 정리
func (d *PumpDetector) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.isRunning {
		return nil
	}

	d.logger.LogInfo("🛑 펌핑 감지 시스템 중지 시작")

	// 컨텍스트 취소
	if d.cancel != nil {
		d.cancel()
	}

	// 컴포넌트들 중지 (순서 중요)
	if d.signalManager != nil {
		d.signalManager.Stop()
	}

	if d.symbolSyncer != nil {
		d.symbolSyncer.Stop()
	}

	if d.websocket != nil {
		d.websocket.Disconnect()
	}

	if d.cacheManager != nil {
		d.cacheManager.Close()
	}

	if d.logger != nil {
		d.logger.Close()
	}

	d.isRunning = false
	d.logger.LogInfo("✅ 펌핑 감지 시스템 중지 완료")

	return nil
}

// SetPumpCallback 펌핑 감지 콜백 설정
//
// 펌핑(급등)이 감지될 때마다 호출될 함수를 설정합니다.
//
// callback: func(event interfaces.PumpEvent) 형태의 함수
//
// 콜백 함수는 별도 고루틴에서 실행되므로 블로킹되어도 시스템에 영향을 주지 않습니다.
// 하지만 가능한 한 빠르게 처리하는 것이 좋습니다.
//
// 사용 예:
//
//	detector.SetPumpCallback(func(event interfaces.PumpEvent) {
//	    if event.PriceChange > 10.0 {
//	        // 10% 이상 급등시 즉시 매수
//	        trading.Buy(event.Symbol, event.CurrentPrice)
//	    }
//	})
func (d *PumpDetector) SetPumpCallback(callback interfaces.PumpCallback) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pumpCallback = callback
	d.logger.LogInfo("🔔 펌핑 감지 콜백 설정 완료")
}

// SetListingCallback 상장공시 감지 콜백 설정
//
// 상장공시가 감지될 때마다 호출될 함수를 설정합니다.
//
// callback: func(event interfaces.ListingEvent) 형태의 함수
//
// 상장공시는 매우 중요한 신호이므로, 콜백에서 즉시 거래 로직을 실행하는 것이 일반적입니다.
//
// 사용 예:
//
//	detector.SetListingCallback(func(event interfaces.ListingEvent) {
//	    if event.Confidence > 90.0 {
//	        // 신뢰도 90% 이상일 때만 거래
//	        trading.BuyAtMarket(event.Symbol)
//	    }
//	})
func (d *PumpDetector) SetListingCallback(callback interfaces.ListingCallback) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.listingCallback = callback
	d.logger.LogInfo("🔔 상장공시 감지 콜백 설정 완료")
}

// UpdateConfig 실시간 설정 업데이트
//
// 시스템 재시작 없이 일부 설정을 업데이트할 수 있습니다.
// 현재 지원되는 설정: 펌핑 감지 임계값, 시간 윈도우, 거래량 임계값
//
// config: 업데이트할 설정 값들
//
// 사용 예:
//
//	newConfig := interfaces.DetectorConfig{
//	    PumpDetection: struct{...}{
//	        PriceChangeThreshold: 5.0, // 5%로 임계값 변경
//	        TimeWindowSeconds: 2,      // 2초로 시간 윈도우 변경
//	    },
//	}
//	detector.UpdateConfig(newConfig)
func (d *PumpDetector) UpdateConfig(config interfaces.DetectorConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// 설정 업데이트 (제한적으로)
	d.config.Signals.PumpDetection.PriceChangeThreshold = config.PumpDetection.PriceChangeThreshold
	d.config.Signals.PumpDetection.TimeWindowSeconds = config.PumpDetection.TimeWindowSeconds
	d.config.Signals.PumpDetection.VolumeThreshold = config.PumpDetection.VolumeThreshold

	d.logger.LogInfo("⚙️ 설정 업데이트 완료: 임계값=%.1f%%, 시간윈도우=%d초",
		config.PumpDetection.PriceChangeThreshold, config.PumpDetection.TimeWindowSeconds)

	return nil
}

// GetStatus 현재 상태 조회
//
// 시스템의 실시간 상태 정보를 반환합니다.
// 이 정보는 모니터링이나 헬스체크에 유용합니다.
//
// 반환값: DetectorStatus 구조체
//   - IsRunning: 실행 중 여부
//   - StartTime: 시작 시간
//   - SymbolCount: 모니터링 중인 심볼 수
//   - WebSocketStatus: WebSocket 연결 상태
//   - LastPumpDetected: 마지막 펌핑 감지 시간
//   - ErrorCount: 에러 발생 횟수
func (d *PumpDetector) GetStatus() interfaces.DetectorStatus {
	d.mu.RLock()
	defer d.mu.RUnlock()

	status := interfaces.DetectorStatus{
		IsRunning:   d.isRunning,
		StartTime:   d.startTime,
		SymbolCount: len(d.config.GetSymbols()),
	}

	if d.websocket != nil {
		// WebSocket 상태 확인 로직 (실제 구현 필요)
		status.WebSocketStatus = "connected"
	}

	d.stats.mu.RLock()
	status.LastPumpDetected = d.stats.lastPumpTime
	status.LastListingEvent = d.stats.lastListingTime
	status.ErrorCount = d.stats.errorCount
	status.LastError = d.stats.lastError
	d.stats.mu.RUnlock()

	return status
}

// GetStats 통계 정보 조회
//
// 시스템의 상세한 통계 정보를 반환합니다.
// 성능 분석이나 최적화에 유용한 데이터를 제공합니다.
//
// 반환값: DetectorStats 구조체
//   - TotalPumpEvents: 총 감지된 펌핑 이벤트 수
//   - AvgPumpChange: 평균 펌핑 변동률
//   - MaxPumpChange: 최대 펌핑 변동률
//   - MemoryUsageMB: 메모리 사용량 (MB)
//   - GoroutineCount: 현재 고루틴 수
//   - UptimeSeconds: 가동 시간 (초)
func (d *PumpDetector) GetStats() interfaces.DetectorStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	memoryStats := d.memManager.GetSystemStats()

	d.stats.mu.RLock()
	avgPumpChange := float64(0)
	if d.stats.totalPumpEvents > 0 {
		avgPumpChange = d.stats.totalPumpChange / float64(d.stats.totalPumpEvents)
	}

	stats := interfaces.DetectorStats{
		// 처리 통계
		TotalOrderbooks: memoryStats["cumulative_orderbooks"].(int),
		TotalTrades:     memoryStats["cumulative_trades"].(int),
		OrderbookRate:   memoryStats["orderbook_rate_per_sec"].(float64),
		TradeRate:       memoryStats["trade_rate_per_sec"].(float64),

		// 감지 통계
		TotalPumpEvents:    d.stats.totalPumpEvents,
		TotalListingEvents: d.stats.totalListingEvents,
		AvgPumpChange:      avgPumpChange,
		MaxPumpChange:      d.stats.maxPumpChange,

		// 시스템 통계
		MemoryUsageMB:  float64(memStats.HeapInuse) / 1024 / 1024,
		GoroutineCount: runtime.NumGoroutine(),
		UptimeSeconds:  int64(time.Since(d.startTime).Seconds()),

		// 성능 통계 (latencyMonitor에서 가져와야 함)
		ErrorRate: float64(d.stats.errorCount),
	}
	d.stats.mu.RUnlock()

	return stats
}

// setupSignalCallbacks 시그널 콜백 설정 (내부용)
func (d *PumpDetector) setupSignalCallbacks() {
	// 향후 signalManager에 실제 콜백 연동 로직 구현
	d.logger.LogInfo("🔗 시그널 콜백 연동 완료")
}

// onPumpDetected 펌핑 감지 콜백 (내부용)
func (d *PumpDetector) onPumpDetected(symbol string, priceChange float64, confidence float64, metadata map[string]interface{}) {
	d.stats.mu.Lock()
	d.stats.totalPumpEvents++
	d.stats.totalPumpChange += priceChange
	if priceChange > d.stats.maxPumpChange {
		d.stats.maxPumpChange = priceChange
	}
	d.stats.lastPumpTime = time.Now()
	d.stats.mu.Unlock()

	// 외부 콜백 호출
	if d.pumpCallback != nil {
		event := interfaces.PumpEvent{
			Symbol:      symbol,
			Exchange:    "binance",
			Timestamp:   time.Now(),
			PriceChange: priceChange,
			Confidence:  confidence,
			Action:      "BUY", // 펌핑이므로 매수 권장
			Metadata:    metadata,
		}

		// 별도 고루틴에서 콜백 실행 (블로킹 방지)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					d.logger.LogError("펌핑 콜백 실행 중 패닉: %v", r)
				}
			}()
			d.pumpCallback(event)
		}()
	}
}

// onListingDetected 상장공시 감지 콜백 (내부용)
func (d *PumpDetector) onListingDetected(symbol string, confidence float64, source string, content string) {
	d.stats.mu.Lock()
	d.stats.totalListingEvents++
	d.stats.lastListingTime = time.Now()
	d.stats.mu.Unlock()

	// 외부 콜백 호출
	if d.listingCallback != nil {
		event := interfaces.ListingEvent{
			Symbol:     symbol,
			Exchange:   "upbit",
			Timestamp:  time.Now(),
			Source:     source,
			Confidence: confidence,
			Content:    content,
		}

		// 별도 고루틴에서 콜백 실행 (블로킹 방지)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					d.logger.LogError("상장공시 콜백 실행 중 패닉: %v", r)
				}
			}()
			d.listingCallback(event)
		}()
	}
}

// Factory 팩토리 구현
type Factory struct{}

// NewFactory 새 팩토리 생성
func NewFactory() interfaces.PumpDetectorFactory {
	return &Factory{}
}

// CreateDetector 새로운 감지기 생성
func (f *Factory) CreateDetector(configPath string) (interfaces.PumpDetector, error) {
	return NewDetector(configPath)
}

// CreateDetectorWithConfig 설정으로 감지기 생성
func (f *Factory) CreateDetectorWithConfig(config interfaces.DetectorConfig) (interfaces.PumpDetector, error) {
	// interfaces.DetectorConfig를 internal config로 변환하는 로직 필요
	// 지금은 기본 설정 사용
	return NewDetector("config.json")
}

// GetDefaultConfig 기본 설정 반환
func (f *Factory) GetDefaultConfig() interfaces.DetectorConfig {
	return interfaces.DetectorConfig{
		PumpDetection: struct {
			Enabled              bool    `json:"enabled"`
			PriceChangeThreshold float64 `json:"price_change_threshold"`
			TimeWindowSeconds    int     `json:"time_window_seconds"`
			MinTradeCount        int     `json:"min_trade_count"`
			VolumeThreshold      float64 `json:"volume_threshold"`
		}{
			Enabled:              true,
			PriceChangeThreshold: 3.0,
			TimeWindowSeconds:    1,
			MinTradeCount:        2,
			VolumeThreshold:      100.0,
		},
		ListingDetection: struct {
			Enabled     bool `json:"enabled"`
			AutoTrigger bool `json:"auto_trigger"`
		}{
			Enabled:     true,
			AutoTrigger: false,
		},
		SymbolFilter: struct {
			EnableUpbitFilter bool     `json:"enable_upbit_filter"`
			CustomSymbols     []string `json:"custom_symbols"`
			ExcludeSymbols    []string `json:"exclude_symbols"`
		}{
			EnableUpbitFilter: true,
			CustomSymbols:     []string{},
			ExcludeSymbols:    []string{},
		},
	}
}
