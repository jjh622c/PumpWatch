package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the main system configuration
type Config struct {
	Upbit      UpbitConfig      `yaml:"upbit"`
	Exchanges  ExchangesConfig  `yaml:"exchanges"`
	Storage    StorageConfig    `yaml:"storage"`
	Analysis   AnalysisConfig   `yaml:"analysis"`
	Collection CollectionConfig `yaml:"collection"`
	System     SystemConfig     `yaml:"system"`
	Connection ConnectionConfig `yaml:"connection"`
	QuestDB    QuestDBConfig    `yaml:"questdb"`
}

// UpbitConfig represents Upbit monitoring configuration
type UpbitConfig struct {
	Enabled      bool          `yaml:"enabled"`
	APIURL       string        `yaml:"api_url"`
	PollInterval time.Duration `yaml:"poll_interval"`
	Timeout      time.Duration `yaml:"timeout"`
	UserAgent    string        `yaml:"user_agent"`
}

// ExchangesConfig represents exchanges configuration
type ExchangesConfig struct {
	Binance ExchangeConfig `yaml:"binance"`
	Bybit   ExchangeConfig `yaml:"bybit"`
	OKX     ExchangeConfig `yaml:"okx"`
	KuCoin  ExchangeConfig `yaml:"kucoin"`
	Phemex  ExchangeConfig `yaml:"phemex"`
	Gate    ExchangeConfig `yaml:"gate"`
}

// ExchangeConfig represents individual exchange configuration
type ExchangeConfig struct {
	Enabled                 bool          `yaml:"enabled"`
	SpotEndpoint            string        `yaml:"spot_endpoint"`
	FuturesEndpoint         string        `yaml:"futures_endpoint"`
	MaxSymbolsPerConnection int           `yaml:"max_symbols_per_connection"`
	RetryCooldown           time.Duration `yaml:"retry_cooldown"`
	MaxRetries              int           `yaml:"max_retries"`
	ConnectionTimeout       time.Duration `yaml:"connection_timeout"`
	ReadTimeout             time.Duration `yaml:"read_timeout"`
	WriteTimeout            time.Duration `yaml:"write_timeout"`
}

// StorageConfig represents storage configuration
type StorageConfig struct {
	Enabled        bool          `yaml:"enabled"`
	DataDir        string        `yaml:"data_dir"`
	RawDataEnabled bool          `yaml:"raw_data_enabled"`
	RefinedEnabled bool          `yaml:"refined_enabled"`
	Compression    bool          `yaml:"compression"`
	RetentionDays  int           `yaml:"retention_days"`
	MaxFileSize    string        `yaml:"max_file_size"`
	BackupEnabled  bool          `yaml:"backup_enabled"`
	BackupInterval time.Duration `yaml:"backup_interval"`
}

// AnalysisConfig represents pump analysis configuration
type AnalysisConfig struct {
	Enabled          bool          `yaml:"enabled"`
	ThresholdPercent float64       `yaml:"threshold_percent"`
	TimeWindow       time.Duration `yaml:"time_window"`
	MinVolumeRatio   float64       `yaml:"min_volume_ratio"`
	MaxAnalysisDelay time.Duration `yaml:"max_analysis_delay"`
}

// CollectionConfig represents data collection timing configuration
type CollectionConfig struct {
	PreTriggerDuration time.Duration `yaml:"pre_trigger_duration"`
	CollectionDuration time.Duration `yaml:"collection_duration"`
	PostAnalysisDelay  time.Duration `yaml:"post_analysis_delay"`
}

// SystemConfig represents system-level configuration
type SystemConfig struct {
	LogLevel                  string        `yaml:"log_level"`
	MaxMemoryUsage            string        `yaml:"max_memory_usage"`
	GCInterval                time.Duration `yaml:"gc_interval"`
	StatusInterval            time.Duration `yaml:"status_interval"`
	ShutdownTimeout           time.Duration `yaml:"shutdown_timeout"`
	StatusReportInterval      time.Duration `yaml:"status_report_interval"`
	AutoSaveInterval          time.Duration `yaml:"auto_save_interval"`
	PoolStartupDelay          time.Duration `yaml:"pool_startup_delay"`
	ProfilingEnabled          bool          `yaml:"profiling_enabled"`
	ProfilingPort             int           `yaml:"profiling_port"`

	// Extended Buffer Configuration
	ExtendedBufferEnabled     bool   `yaml:"extended_buffer_enabled"`
	ExtendedBufferDuration    string `yaml:"extended_buffer_duration"`    // e.g., "10m"
	BufferCompressionEnabled  bool   `yaml:"buffer_compression_enabled"`
	BufferFallbackEnabled     bool   `yaml:"buffer_fallback_enabled"`
	BufferMaintenanceInterval string `yaml:"buffer_maintenance_interval"` // e.g., "60s"
}

// ConnectionConfig represents connection timing configuration
type ConnectionConfig struct {
	DefaultPingInterval      time.Duration `yaml:"default_ping_interval"`
	DefaultConnectionTimeout time.Duration `yaml:"default_connection_timeout"`
	DefaultRetryDelay        time.Duration `yaml:"default_retry_delay"`
	BatchProcessingDelay     time.Duration `yaml:"batch_processing_delay"`
	APIRateLimitDelay        time.Duration `yaml:"api_rate_limit_delay"`
}

// QuestDBConfig represents QuestDB database configuration
type QuestDBConfig struct {
	Enabled         bool          `yaml:"enabled"`
	Host            string        `yaml:"host"`
	Port            int           `yaml:"port"`
	Database        string        `yaml:"database"`
	User            string        `yaml:"user"`
	Password        string        `yaml:"password"`

	// 배치 처리 설정 (Phase 2 사양)
	BatchSize       int           `yaml:"batch_size")`
	FlushInterval   time.Duration `yaml:"flush_interval"`
	BufferSize      int           `yaml:"buffer_size"`
	WorkerCount     int           `yaml:"worker_count"`

	// 연결 풀 설정
	MaxOpenConns    int           `yaml:"max_open_conns"`
	MaxIdleConns    int           `yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`

	// 재시도 설정
	MaxRetries      int           `yaml:"max_retries"`
	BaseDelay       time.Duration `yaml:"base_delay"`
	MaxDelay        time.Duration `yaml:"max_delay"`
	BackoffFactor   float64       `yaml:"backoff_factor"`

	// 하이브리드 모드 설정
	HybridMode      bool          `yaml:"hybrid_mode"`       // 기존 시스템과 병렬 운영
	CompareResults  bool          `yaml:"compare_results"`   // 결과 비교 모드
	MigrationPhase  int           `yaml:"migration_phase"`   // 1-4: 점진적 전환 단계
}

// Load loads configuration from YAML file
func Load(configPath string) (*Config, error) {
	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		// If file doesn't exist, create default config
		if os.IsNotExist(err) {
			defaultConfig := NewDefaultConfig()
			if err := Save(defaultConfig, configPath); err != nil {
				return nil, fmt.Errorf("failed to create default config: %w", err)
			}
			return defaultConfig, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

// Save saves configuration to YAML file
func Save(config *Config, configPath string) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Atomic write using temp file
	tempPath := configPath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp config file: %w", err)
	}

	if err := os.Rename(tempPath, configPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to move config file: %w", err)
	}

	return nil
}

// NewDefaultConfig creates default configuration
func NewDefaultConfig() *Config {
	return &Config{
		Upbit: UpbitConfig{
			Enabled:      true,
			APIURL:       "https://api-manager.upbit.com/api/v1/announcements?os=web&page=1&per_page=20&category=trade",
			PollInterval: 5 * time.Second,
			Timeout:      10 * time.Second,
			UserAgent:    "PumpWatch/2.0",
		},
		Exchanges: ExchangesConfig{
			Binance: ExchangeConfig{
				Enabled:                 true,
				SpotEndpoint:            "wss://stream.binance.com:9443/ws",
				FuturesEndpoint:         "wss://fstream.binance.com/ws",
				MaxSymbolsPerConnection: 100,
				RetryCooldown:           30 * time.Second,
				MaxRetries:              5,
				ConnectionTimeout:       30 * time.Second,
				ReadTimeout:             60 * time.Second,
				WriteTimeout:            10 * time.Second,
			},
			Bybit: ExchangeConfig{
				Enabled:                 true,
				SpotEndpoint:            "wss://stream.bybit.com/v5/public/spot",
				FuturesEndpoint:         "wss://stream.bybit.com/v5/public/linear",
				MaxSymbolsPerConnection: 50,
				RetryCooldown:           60 * time.Second,
				MaxRetries:              3,
				ConnectionTimeout:       30 * time.Second,
				ReadTimeout:             60 * time.Second,
				WriteTimeout:            10 * time.Second,
			},
			OKX: ExchangeConfig{
				Enabled:                 true,
				SpotEndpoint:            "wss://ws.okx.com:8443/ws/v5/public",
				FuturesEndpoint:         "wss://ws.okx.com:8443/ws/v5/public",
				MaxSymbolsPerConnection: 100,
				RetryCooldown:           30 * time.Second,
				MaxRetries:              5,
				ConnectionTimeout:       30 * time.Second,
				ReadTimeout:             60 * time.Second,
				WriteTimeout:            10 * time.Second,
			},
			KuCoin: ExchangeConfig{
				Enabled:                 true,
				SpotEndpoint:            "wss://ws-api.kucoin.com/endpoint",
				FuturesEndpoint:         "wss://ws-api-futures.kucoin.com/endpoint",
				MaxSymbolsPerConnection: 100,
				RetryCooldown:           45 * time.Second,
				MaxRetries:              4,
				ConnectionTimeout:       30 * time.Second,
				ReadTimeout:             60 * time.Second,
				WriteTimeout:            10 * time.Second,
			},
			Phemex: ExchangeConfig{
				Enabled:                 true,
				SpotEndpoint:            "wss://ws.phemex.com",
				FuturesEndpoint:         "wss://ws.phemex.com",
				MaxSymbolsPerConnection: 20,
				RetryCooldown:           90 * time.Second,
				MaxRetries:              3,
				ConnectionTimeout:       30 * time.Second,
				ReadTimeout:             60 * time.Second,
				WriteTimeout:            10 * time.Second,
			},
			Gate: ExchangeConfig{
				Enabled:                 true,
				SpotEndpoint:            "wss://api.gateio.ws/ws/v4/",
				FuturesEndpoint:         "wss://fx-ws.gateio.ws/v4/ws/usdt",
				MaxSymbolsPerConnection: 100,
				RetryCooldown:           30 * time.Second,
				MaxRetries:              5,
				ConnectionTimeout:       30 * time.Second,
				ReadTimeout:             60 * time.Second,
				WriteTimeout:            10 * time.Second,
			},
		},
		Storage: StorageConfig{
			Enabled:        true,
			DataDir:        "data",
			RawDataEnabled: true,
			RefinedEnabled: true,
			Compression:    false,
			RetentionDays:  30,
			MaxFileSize:    "100MB",
			BackupEnabled:  true,
			BackupInterval: 24 * time.Hour,
		},
		Analysis: AnalysisConfig{
			Enabled:          true,
			ThresholdPercent: 3.0,
			TimeWindow:       1 * time.Second,
			MinVolumeRatio:   2.0,
			MaxAnalysisDelay: 10 * time.Second,
		},
		Collection: CollectionConfig{
			PreTriggerDuration: 20 * time.Second,
			CollectionDuration: 20 * time.Second,
			PostAnalysisDelay:  2 * time.Second,
		},
		System: SystemConfig{
			LogLevel:                  "info",
			MaxMemoryUsage:            "16GB",
			GCInterval:                5 * time.Minute,
			StatusInterval:            60 * time.Second,
			ShutdownTimeout:           30 * time.Second,
			StatusReportInterval:      60 * time.Second,
			AutoSaveInterval:          5 * time.Minute,
			PoolStartupDelay:          500 * time.Millisecond,
			ProfilingEnabled:          false,
			ProfilingPort:             6060,

			// Extended Buffer Configuration (기본값: 비활성화, 안전을 위해)
			ExtendedBufferEnabled:     false,
			ExtendedBufferDuration:    "10m",
			BufferCompressionEnabled:  true,
			BufferFallbackEnabled:     true,
			BufferMaintenanceInterval: "60s",
		},
		Connection: ConnectionConfig{
			DefaultPingInterval:      25 * time.Second,
			DefaultConnectionTimeout: 30 * time.Second,
			DefaultRetryDelay:        1 * time.Second,
			BatchProcessingDelay:     500 * time.Millisecond,
			APIRateLimitDelay:        100 * time.Millisecond,
		},
		QuestDB: QuestDBConfig{
			Enabled:         false, // 기본값: 비활성화 (안전한 점진적 도입)
			Host:            "localhost",
			Port:            8812,
			Database:        "qdb",
			User:            "admin",
			Password:        "quest",

			// 문서 Phase 2 사양대로 고성능 배치 처리 설정
			BatchSize:       1000,                // Phase 2 문서 사양
			FlushInterval:   1 * time.Second,     // Phase 2 문서 사양
			BufferSize:      50000,               // Phase 2 문서 사양
			WorkerCount:     4,                   // Phase 2 문서 사양

			MaxOpenConns:    20,
			MaxIdleConns:    10,
			ConnMaxLifetime: 30 * time.Minute,

			MaxRetries:      3,
			BaseDelay:       100 * time.Millisecond,
			MaxDelay:        5 * time.Second,
			BackoffFactor:   2.0,

			// 하이브리드 모드 (Phase 5 점진적 전환)
			HybridMode:      true,  // 기본값: 하이브리드 모드 활성화
			CompareResults:  false, // 결과 비교는 기본 비활성화
			MigrationPhase:  1,     // Phase 1: 10% QuestDB 사용
		},
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate Upbit config
	if c.Upbit.Enabled {
		if c.Upbit.APIURL == "" {
			return fmt.Errorf("upbit.api_url cannot be empty")
		}
		if c.Upbit.PollInterval < 1*time.Second {
			return fmt.Errorf("upbit.poll_interval must be at least 1 second")
		}
	}

	// Validate exchange configs
	exchanges := map[string]ExchangeConfig{
		"binance": c.Exchanges.Binance,
		"bybit":   c.Exchanges.Bybit,
		"okx":     c.Exchanges.OKX,
		"kucoin":  c.Exchanges.KuCoin,
		"phemex":  c.Exchanges.Phemex,
		"gate":    c.Exchanges.Gate,
	}

	for name, exchange := range exchanges {
		if exchange.Enabled {
			if exchange.SpotEndpoint == "" && exchange.FuturesEndpoint == "" {
				return fmt.Errorf("exchange %s must have at least one endpoint", name)
			}
			if exchange.MaxSymbolsPerConnection < 1 {
				return fmt.Errorf("exchange %s max_symbols_per_connection must be positive", name)
			}
			if exchange.MaxRetries < 0 {
				return fmt.Errorf("exchange %s max_retries cannot be negative", name)
			}
		}
	}

	// Validate analysis config
	if c.Analysis.Enabled {
		if c.Analysis.ThresholdPercent <= 0 {
			return fmt.Errorf("analysis.threshold_percent must be positive")
		}
		if c.Analysis.TimeWindow < 100*time.Millisecond {
			return fmt.Errorf("analysis.time_window must be at least 100ms")
		}
	}

	return nil
}

// GetEnabledExchanges returns list of enabled exchanges
func (c *Config) GetEnabledExchanges() []string {
	var enabled []string

	exchanges := map[string]ExchangeConfig{
		"binance": c.Exchanges.Binance,
		"bybit":   c.Exchanges.Bybit,
		"okx":     c.Exchanges.OKX,
		"kucoin":  c.Exchanges.KuCoin,
		"phemex":  c.Exchanges.Phemex,
		"gate":    c.Exchanges.Gate,
	}

	for name, config := range exchanges {
		if config.Enabled {
			enabled = append(enabled, name)
		}
	}

	return enabled
}
