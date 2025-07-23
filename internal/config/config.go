package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Config 전체 설정 구조체
type Config struct {
	Server       ServerConfig       `json:"server"`
	WebSocket    WebSocketConfig    `json:"websocket"`
	Memory       MemoryConfig       `json:"memory"`
	Storage      StorageConfig      `json:"storage"`
	Signals      SignalsConfig      `json:"signals"`
	Triggers     TriggersConfig     `json:"triggers"`
	Snapshot     SnapshotConfig     `json:"snapshot"`
	Notification NotificationConfig `json:"notification"`
	Logging      LoggingConfig      `json:"logging"`
}

// ServerConfig HTTP 서버 설정
type ServerConfig struct {
	Port int    `json:"port"`
	Host string `json:"host"`
}

// WebSocketConfig WebSocket 연결 설정
type WebSocketConfig struct {
	Symbols             []string      `json:"symbols"`
	ReconnectInterval   time.Duration `json:"reconnect_interval"`
	HeartbeatInterval   time.Duration `json:"heartbeat_interval"`
	WorkerCount         int           `json:"worker_count"`
	BufferSize          int           `json:"buffer_size"`
	AutoSyncSymbols     bool          `json:"auto_sync_symbols"`
	SyncIntervalMinutes int           `json:"sync_interval_minutes"`
	SyncEnabled         bool          `json:"sync_enabled"`
	EnableUpbitFilter   bool          `json:"enable_upbit_filter"`
	UpbitSyncMinutes    int           `json:"upbit_sync_minutes"`
	// 자동 재연결 설정
	MaxReconnectAttempts int           `json:"max_reconnect_attempts"` // 최대 재연결 시도 횟수
	ReconnectBackoff     time.Duration `json:"reconnect_backoff"`      // 재연결 백오프 간격
	MaxBackoff           time.Duration `json:"max_backoff"`            // 최대 백오프 시간
	KeepAliveInterval    time.Duration `json:"keep_alive_interval"`    // PING/PONG 간격
	ReadTimeout          time.Duration `json:"read_timeout"`           // 읽기 타임아웃
	WriteTimeout         time.Duration `json:"write_timeout"`
	// 🔧 하드코딩 제거: WebSocket 관련 설정들 추가
	MaxSymbolsPerGroup    int `json:"max_symbols_per_group"`
	ReportIntervalSeconds int `json:"report_interval_seconds"`
	MessageTimeoutSeconds int `json:"message_timeout_seconds"`
}

// MemoryConfig 메모리 관리 설정
type MemoryConfig struct {
	OrderbookRetentionMinutes float64 `json:"orderbook_retention_minutes"`
	TradeRetentionMinutes     int     `json:"trade_retention_minutes"`
	MaxOrderbooksPerSymbol    int     `json:"max_orderbooks_per_symbol"`
	MaxTradesPerSymbol        int     `json:"max_trades_per_symbol"`
	CleanupIntervalMinutes    int     `json:"cleanup_interval_minutes"`
	// 🔧 하드코딩 제거: 새로운 설정들 추가
	CompressionIntervalSeconds int     `json:"compression_interval_seconds"`
	HeapWarningMB              float64 `json:"heap_warning_mb"`
	GCThresholdOrderbooks      int     `json:"gc_threshold_orderbooks"`
	GCThresholdTrades          int     `json:"gc_threshold_trades"`
	MaxGoroutines              int     `json:"max_goroutines"`
	MonitoringIntervalSeconds  int     `json:"monitoring_interval_seconds"`
}

// StorageConfig 스토리지 설정
type StorageConfig struct {
	BaseDir       string `json:"base_dir"`
	RetentionDays int    `json:"retention_days"`
	CompressData  bool   `json:"compress_data"`
}

// SignalsConfig 시그널 설정
type SignalsConfig struct {
	PumpDetection PumpDetectionConfig `json:"pump_detection"`
	Listing       ListingConfig       `json:"listing"`
}

// PumpDetectionConfig 펌핑 감지 설정
type PumpDetectionConfig struct {
	Enabled              bool    `json:"enabled"`
	MinScore             float64 `json:"min_score"`
	VolumeThreshold      float64 `json:"volume_threshold"`
	PriceChangeThreshold float64 `json:"price_change_threshold"`
	TimeWindowSeconds    int     `json:"time_window_seconds"`
}

// ListingConfig 상장공시 설정
type ListingConfig struct {
	Enabled     bool `json:"enabled"`
	AutoTrigger bool `json:"auto_trigger"`
}

// TriggersConfig 트리거 설정
type TriggersConfig struct {
	PumpDetection PumpDetectionConfig   `json:"pump_detection"`
	Snapshot      SnapshotTriggerConfig `json:"snapshot"`
}

// SnapshotTriggerConfig 스냅샷 트리거 설정
type SnapshotTriggerConfig struct {
	PreTriggerSeconds  int `json:"pre_trigger_seconds"`   // 트리거 발생 전 저장할 시간
	PostTriggerSeconds int `json:"post_trigger_seconds"`  // 트리거 발생 후 저장할 시간
	MaxSnapshotsPerDay int `json:"max_snapshots_per_day"` // 일일 최대 스냅샷 수
}

// SnapshotConfig 스냅샷 저장 설정
type SnapshotConfig struct {
	OutputDir        string `json:"output_dir"`
	FilenameTemplate string `json:"filename_template"`
	CompressData     bool   `json:"compress_data"`
	IncludeMetadata  bool   `json:"include_metadata"`
}

// NotificationConfig 알림 설정
type NotificationConfig struct {
	SlackWebhook   string `json:"slack_webhook"`
	TelegramToken  string `json:"telegram_token"`
	TelegramChatID string `json:"telegram_chat_id"`
	EnableAlerts   bool   `json:"enable_alerts"`
	AlertThreshold int    `json:"alert_threshold"`
}

// LoggingConfig 로깅 설정
type LoggingConfig struct {
	Level                       string  `json:"level"`
	OutputFile                  string  `json:"output_file"`
	MaxSize                     int     `json:"max_size"`
	MaxBackups                  int     `json:"max_backups"`
	LatencyWarnSeconds          float64 `json:"latency_warn_seconds"`
	LatencyCriticalSeconds      float64 `json:"latency_critical_seconds"`
	LatencyStatsIntervalSeconds int     `json:"latency_stats_interval_seconds"`
	LogRotationIntervalMinutes  int     `json:"log_rotation_interval_minutes"`
}

// LoadConfig 설정 파일 로드
func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = "config.json"
	}

	// 기본 설정
	config := &Config{
		Server: ServerConfig{
			Port: 8080,
			Host: "localhost",
		},
		WebSocket: WebSocketConfig{
			Symbols:               []string{"BTCUSDT", "ETHUSDT", "BNBUSDT", "ADAUSDT", "SOLUSDT"},
			ReconnectInterval:     5 * time.Second,
			HeartbeatInterval:     30 * time.Second,
			WorkerCount:           32,    // 16 → 32 (2배 증가)
			BufferSize:            50000, // 1000 → 50000 (50배 증가)
			MaxReconnectAttempts:  10,
			ReconnectBackoff:      5 * time.Second,
			MaxBackoff:            5 * time.Minute,
			KeepAliveInterval:     30 * time.Second,
			ReadTimeout:           60 * time.Second,
			WriteTimeout:          10 * time.Second,
			MaxSymbolsPerGroup:    100,
			ReportIntervalSeconds: 60,
			MessageTimeoutSeconds: 30,
		},
		Memory: MemoryConfig{
			OrderbookRetentionMinutes:  60.0,
			TradeRetentionMinutes:      60,
			MaxOrderbooksPerSymbol:     1000,
			MaxTradesPerSymbol:         1000,
			CleanupIntervalMinutes:     5,
			CompressionIntervalSeconds: 300,
			HeapWarningMB:              100.0,
			GCThresholdOrderbooks:      10000,
			GCThresholdTrades:          10000,
			MaxGoroutines:              1000,
			MonitoringIntervalSeconds:  60,
		},
		Storage: StorageConfig{
			BaseDir:       "./data",
			RetentionDays: 30,
			CompressData:  false,
		},
		Signals: SignalsConfig{
			PumpDetection: PumpDetectionConfig{
				Enabled:              true,
				MinScore:             70.0,
				VolumeThreshold:      1000000.0,
				PriceChangeThreshold: 3.0, // 5.0 → 3.0으로 변경
				TimeWindowSeconds:    300,
			},
			Listing: ListingConfig{
				Enabled:     true,
				AutoTrigger: false,
			},
		},
		Triggers: TriggersConfig{
			PumpDetection: PumpDetectionConfig{
				Enabled:              true,
				MinScore:             70.0,
				VolumeThreshold:      1000000.0,
				PriceChangeThreshold: 3.0, // 5.0 → 3.0으로 변경
				TimeWindowSeconds:    300,
			},
			Snapshot: SnapshotTriggerConfig{
				PreTriggerSeconds:  60,
				PostTriggerSeconds: 60,
				MaxSnapshotsPerDay: 100,
			},
		},
		Snapshot: SnapshotConfig{
			OutputDir:        "./snapshots",
			FilenameTemplate: "snapshot_{timestamp}_{symbol}_{trigger_type}.json",
			CompressData:     true,
			IncludeMetadata:  true,
		},
		Notification: NotificationConfig{
			EnableAlerts:   true,
			AlertThreshold: 5,
		},
		Logging: LoggingConfig{
			Level:      "info",
			OutputFile: "logs/app.log",
			MaxSize:    100,
			MaxBackups: 3,
		},
	}

	// 설정 파일이 있으면 로드
	if _, err := os.Stat(configPath); err == nil {
		file, err := os.Open(configPath)
		if err != nil {
			return nil, fmt.Errorf("설정 파일 열기 실패: %v", err)
		}
		defer file.Close()

		if err := json.NewDecoder(file).Decode(config); err != nil {
			return nil, fmt.Errorf("설정 파일 파싱 실패: %v", err)
		}
	}

	return config, nil
}

// GetSymbols 심볼 리스트 반환
func (c *Config) GetSymbols() []string {
	return c.WebSocket.Symbols
}

// Validate 설정 유효성 검사
func (c *Config) Validate() error {
	// 심볼 동기화가 비활성화된 경우에만 심볼 리스트 체크
	if !c.WebSocket.SyncEnabled && len(c.WebSocket.Symbols) == 0 {
		return fmt.Errorf("심볼 리스트가 비어있습니다 (심볼 동기화가 비활성화된 경우)")
	}
	if c.WebSocket.WorkerCount <= 0 {
		return fmt.Errorf("워커 수는 0보다 커야 합니다")
	}
	if c.Memory.OrderbookRetentionMinutes <= 0.0 {
		return fmt.Errorf("오더북 보관 시간은 0보다 커야 합니다")
	}
	if c.Storage.RetentionDays <= 0 {
		return fmt.Errorf("스토리지 보관 기간은 0보다 커야 합니다")
	}
	return nil
}
