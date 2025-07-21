package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"
)

// Config 설정 구조체
type Config struct {
	// WebSocket 설정
	WebSocket struct {
		MaxStreams        int           `json:"max_streams"`        // 최대 스트림 수
		WorkerCount       int           `json:"worker_count"`       // 워커 수
		BufferSize        int           `json:"buffer_size"`        // 버퍼 크기
		ReconnectDelay    time.Duration `json:"reconnect_delay"`    // 재연결 지연
		HeartbeatInterval time.Duration `json:"heartbeat_interval"` // 하트비트 간격
	} `json:"websocket"`

	// 분석 설정
	Analysis struct {
		MinScore        float64       `json:"min_score"`        // 최소 점수
		SignalThreshold float64       `json:"signal_threshold"` // 시그널 임계값
		RetentionTime   time.Duration `json:"retention_time"`   // 보관 시간
		UpdateInterval  time.Duration `json:"update_interval"`  // 업데이트 간격
	} `json:"analysis"`

	// 알림 설정
	Notification struct {
		Enabled        bool          `json:"enabled"`          // 활성화 여부
		SlackWebhook   string        `json:"slack_webhook"`    // 슬랙 웹훅
		TelegramToken  string        `json:"telegram_token"`   // 텔레그램 토큰
		TelegramChatID string        `json:"telegram_chat_id"` // 텔레그램 채팅 ID
		RateLimit      time.Duration `json:"rate_limit"`       // 레이트 리밋
		MinLevel       string        `json:"min_level"`        // 최소 알림 레벨
	} `json:"notification"`

	// 모니터링 설정
	Monitoring struct {
		Enabled             bool          `json:"enabled"`               // 활성화 여부
		MetricsPort         int           `json:"metrics_port"`          // 메트릭 포트
		HealthCheckInterval time.Duration `json:"health_check_interval"` // 헬스체크 간격
		AutoRestart         bool          `json:"auto_restart"`          // 자동 재시작
		MaxErrors           int           `json:"max_errors"`            // 최대 에러 수
	} `json:"monitoring"`

	// HTTP 서버 설정
	Server struct {
		Port           int           `json:"port"`            // 서버 포트
		ReadTimeout    time.Duration `json:"read_timeout"`    // 읽기 타임아웃
		WriteTimeout   time.Duration `json:"write_timeout"`   // 쓰기 타임아웃
		MaxConnections int           `json:"max_connections"` // 최대 연결 수
		EnableCORS     bool          `json:"enable_cors"`     // CORS 활성화
	} `json:"server"`

	// 거래 설정
	Trading struct {
		Enabled         bool    `json:"enabled"`           // 활성화 여부
		MaxPositionSize float64 `json:"max_position_size"` // 최대 포지션 크기
		RiskPercentage  float64 `json:"risk_percentage"`   // 위험 비율
		StopLoss        float64 `json:"stop_loss"`         // 손절 비율
		TakeProfit      float64 `json:"take_profit"`       // 익절 비율
		SimulationMode  bool    `json:"simulation_mode"`   // 시뮬레이션 모드
	} `json:"trading"`

	// 백테스팅 설정
	Backtest struct {
		Enabled        bool    `json:"enabled"`         // 활성화 여부
		StartDate      string  `json:"start_date"`      // 시작 날짜
		EndDate        string  `json:"end_date"`        // 종료 날짜
		InitialBalance float64 `json:"initial_balance"` // 초기 잔고
		Commission     float64 `json:"commission"`      // 수수료
		DataDirectory  string  `json:"data_directory"`  // 데이터 디렉토리
	} `json:"backtest"`

	// 심볼 설정
	Symbols []string `json:"symbols"` // 모니터링할 심볼 리스트
}

// LoadConfig 설정 파일 로드
func LoadConfig(configPath string) (*Config, error) {
	// 기본 설정값
	config := &Config{}

	// 기본값 설정
	config.setDefaults()

	// 환경변수에서 설정 로드
	config.loadFromEnv()

	// 설정 파일이 있으면 로드
	if configPath != "" {
		if err := config.loadFromFile(configPath); err != nil {
			log.Printf("⚠️  설정 파일 로드 실패: %v", err)
		}
	}

	// 설정 유효성 검사
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("설정 유효성 검사 실패: %v", err)
	}

	log.Printf("✅ 설정 로드 완료")
	return config, nil
}

// setDefaults 기본값 설정
func (c *Config) setDefaults() {
	// WebSocket 기본값
	c.WebSocket.MaxStreams = 1024
	c.WebSocket.WorkerCount = 16
	c.WebSocket.BufferSize = 1000
	c.WebSocket.ReconnectDelay = 5 * time.Second
	c.WebSocket.HeartbeatInterval = 30 * time.Second

	// 분석 기본값
	c.Analysis.MinScore = 60.0
	c.Analysis.SignalThreshold = 80.0
	c.Analysis.RetentionTime = 10 * time.Minute
	c.Analysis.UpdateInterval = 100 * time.Millisecond

	// 알림 기본값
	c.Notification.Enabled = true
	c.Notification.RateLimit = 30 * time.Second
	c.Notification.MinLevel = "WARNING"

	// 모니터링 기본값
	c.Monitoring.Enabled = true
	c.Monitoring.MetricsPort = 8081
	c.Monitoring.HealthCheckInterval = 30 * time.Second
	c.Monitoring.AutoRestart = true
	c.Monitoring.MaxErrors = 10

	// 서버 기본값
	c.Server.Port = 8080
	c.Server.ReadTimeout = 30 * time.Second
	c.Server.WriteTimeout = 30 * time.Second
	c.Server.MaxConnections = 1000
	c.Server.EnableCORS = true

	// 거래 기본값
	c.Trading.Enabled = false
	c.Trading.MaxPositionSize = 1000.0
	c.Trading.RiskPercentage = 2.0
	c.Trading.StopLoss = 5.0
	c.Trading.TakeProfit = 10.0
	c.Trading.SimulationMode = true

	// 백테스팅 기본값
	c.Backtest.Enabled = false
	c.Backtest.InitialBalance = 10000.0
	c.Backtest.Commission = 0.1
	c.Backtest.DataDirectory = "./data"

	// 기본 심볼 리스트
	c.Symbols = []string{
		"BTC", "ETH", "BNB", "ADA", "SOL", "DOT", "AVAX", "MATIC",
		"LINK", "UNI", "ATOM", "LTC", "BCH", "XLM", "VET", "FIL",
		"TRX", "ETC", "XMR", "EOS", "AAVE", "ALGO", "MKR", "COMP",
	}
}

// loadFromEnv 환경변수에서 설정 로드
func (c *Config) loadFromEnv() {
	// WebSocket 설정
	if val := os.Getenv("WS_MAX_STREAMS"); val != "" {
		if maxStreams, err := strconv.Atoi(val); err == nil {
			c.WebSocket.MaxStreams = maxStreams
		}
	}

	if val := os.Getenv("WS_WORKER_COUNT"); val != "" {
		if workerCount, err := strconv.Atoi(val); err == nil {
			c.WebSocket.WorkerCount = workerCount
		}
	}

	// 알림 설정
	if val := os.Getenv("SLACK_WEBHOOK"); val != "" {
		c.Notification.SlackWebhook = val
	}

	if val := os.Getenv("TELEGRAM_TOKEN"); val != "" {
		c.Notification.TelegramToken = val
	}

	if val := os.Getenv("TELEGRAM_CHAT_ID"); val != "" {
		c.Notification.TelegramChatID = val
	}

	// 서버 설정
	if val := os.Getenv("SERVER_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			c.Server.Port = port
		}
	}

	// 모니터링 설정
	if val := os.Getenv("METRICS_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			c.Monitoring.MetricsPort = port
		}
	}
}

// loadFromFile 설정 파일에서 로드
func (c *Config) loadFromFile(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, c)
}

// validate 설정 유효성 검사
func (c *Config) validate() error {
	// WebSocket 설정 검사
	if c.WebSocket.MaxStreams <= 0 {
		return fmt.Errorf("max_streams는 0보다 커야 합니다")
	}

	if c.WebSocket.WorkerCount <= 0 {
		return fmt.Errorf("worker_count는 0보다 커야 합니다")
	}

	// 분석 설정 검사
	if c.Analysis.MinScore < 0 || c.Analysis.MinScore > 100 {
		return fmt.Errorf("min_score는 0-100 사이여야 합니다")
	}

	// 서버 설정 검사
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("port는 1-65535 사이여야 합니다")
	}

	// 모니터링 설정 검사
	if c.Monitoring.MetricsPort <= 0 || c.Monitoring.MetricsPort > 65535 {
		return fmt.Errorf("metrics_port는 1-65535 사이여야 합니다")
	}

	// 심볼 리스트 검사
	if len(c.Symbols) == 0 {
		return fmt.Errorf("최소 하나의 심볼이 필요합니다")
	}

	return nil
}

// SaveConfig 설정을 파일에 저장
func (c *Config) SaveConfig(configPath string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

// GetSymbols 심볼 리스트 조회
func (c *Config) GetSymbols() []string {
	return c.Symbols
}

// IsNotificationEnabled 알림 활성화 여부 조회
func (c *Config) IsNotificationEnabled() bool {
	return c.Notification.Enabled
}

// IsTradingEnabled 거래 활성화 여부 조회
func (c *Config) IsTradingEnabled() bool {
	return c.Trading.Enabled
}

// IsBacktestEnabled 백테스팅 활성화 여부 조회
func (c *Config) IsBacktestEnabled() bool {
	return c.Backtest.Enabled
}
