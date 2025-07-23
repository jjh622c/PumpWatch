// Package interfaces 외부 시스템과의 연동을 위한 인터페이스 정의
package interfaces

import (
	"time"
)

// PumpDetector 펌핑 감지 시스템의 메인 인터페이스
type PumpDetector interface {
	// Start 펌핑 감지 시작
	Start() error

	// Stop 펌핑 감지 중지
	Stop() error

	// SetPumpCallback 펌핑 감지 콜백 설정
	SetPumpCallback(callback PumpCallback)

	// SetListingCallback 상장공시 감지 콜백 설정
	SetListingCallback(callback ListingCallback)

	// UpdateConfig 실시간 설정 업데이트
	UpdateConfig(config DetectorConfig) error

	// GetStatus 현재 상태 조회
	GetStatus() DetectorStatus

	// GetStats 통계 정보 조회
	GetStats() DetectorStats
}

// PumpCallback 펌핑 감지 콜백 함수 타입
type PumpCallback func(event PumpEvent)

// ListingCallback 상장공시 감지 콜백 함수 타입
type ListingCallback func(event ListingEvent)

// PumpEvent 펌핑 이벤트 정보
type PumpEvent struct {
	Symbol        string    `json:"symbol"`         // 심볼 (예: BTCUSDT)
	Exchange      string    `json:"exchange"`       // 거래소 (binance)
	Timestamp     time.Time `json:"timestamp"`      // 감지 시간
	PriceChange   float64   `json:"price_change"`   // 가격 변동률 (%)
	CurrentPrice  float64   `json:"current_price"`  // 현재 가격
	PreviousPrice float64   `json:"previous_price"` // 이전 가격
	TimeWindow    int       `json:"time_window"`    // 감지 시간 윈도우 (초)
	TradeCount    int       `json:"trade_count"`    // 체결 건수
	Volume        float64   `json:"volume"`         // 거래량 변화 (%)
	Confidence    float64   `json:"confidence"`     // 신뢰도 (0-100)
	Threshold     float64   `json:"threshold"`      // 사용된 임계값 (%)
	Action        string    `json:"action"`         // 권장 액션 (BUY/SELL/HOLD)

	// 추가 메타데이터
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ListingEvent 상장공시 이벤트 정보
type ListingEvent struct {
	Symbol     string                 `json:"symbol"`     // 심볼
	Exchange   string                 `json:"exchange"`   // 거래소
	Timestamp  time.Time              `json:"timestamp"`  // 공시 시간
	Source     string                 `json:"source"`     // 소스 (upbit_notice 등)
	Confidence float64                `json:"confidence"` // 신뢰도 (0-100)
	Content    string                 `json:"content"`    // 공시 내용
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// DetectorConfig 감지기 설정
type DetectorConfig struct {
	// 펌핑 감지 설정
	PumpDetection struct {
		Enabled              bool    `json:"enabled"`                // 펌핑 감지 활성화
		PriceChangeThreshold float64 `json:"price_change_threshold"` // 가격 변동 임계값 (%)
		TimeWindowSeconds    int     `json:"time_window_seconds"`    // 시간 윈도우 (초)
		MinTradeCount        int     `json:"min_trade_count"`        // 최소 체결 건수
		VolumeThreshold      float64 `json:"volume_threshold"`       // 거래량 임계값 (%)
	} `json:"pump_detection"`

	// 상장공시 감지 설정
	ListingDetection struct {
		Enabled     bool `json:"enabled"`      // 상장공시 감지 활성화
		AutoTrigger bool `json:"auto_trigger"` // 자동 트리거
	} `json:"listing_detection"`

	// 심볼 필터링
	SymbolFilter struct {
		EnableUpbitFilter bool     `json:"enable_upbit_filter"` // 업비트 상장 코인만 필터링
		CustomSymbols     []string `json:"custom_symbols"`      // 커스텀 심볼 목록
		ExcludeSymbols    []string `json:"exclude_symbols"`     // 제외할 심볼 목록
	} `json:"symbol_filter"`
}

// DetectorStatus 감지기 상태
type DetectorStatus struct {
	IsRunning        bool      `json:"is_running"`         // 실행 중 여부
	StartTime        time.Time `json:"start_time"`         // 시작 시간
	WebSocketStatus  string    `json:"websocket_status"`   // WebSocket 연결 상태
	SymbolCount      int       `json:"symbol_count"`       // 모니터링 중인 심볼 수
	LastPumpDetected time.Time `json:"last_pump_detected"` // 마지막 펌핑 감지 시간
	LastListingEvent time.Time `json:"last_listing_event"` // 마지막 상장공시 시간
	ErrorCount       int       `json:"error_count"`        // 에러 발생 횟수
	LastError        string    `json:"last_error"`         // 마지막 에러 메시지
}

// DetectorStats 감지기 통계
type DetectorStats struct {
	// 처리 통계
	TotalOrderbooks int     `json:"total_orderbooks"` // 총 처리한 오더북 수
	TotalTrades     int     `json:"total_trades"`     // 총 처리한 체결 수
	OrderbookRate   float64 `json:"orderbook_rate"`   // 초당 오더북 처리율
	TradeRate       float64 `json:"trade_rate"`       // 초당 체결 처리율

	// 감지 통계
	TotalPumpEvents    int     `json:"total_pump_events"`    // 총 펌핑 이벤트 수
	TotalListingEvents int     `json:"total_listing_events"` // 총 상장공시 이벤트 수
	AvgPumpChange      float64 `json:"avg_pump_change"`      // 평균 펌핑 변동률
	MaxPumpChange      float64 `json:"max_pump_change"`      // 최대 펌핑 변동률

	// 시스템 통계
	MemoryUsageMB  float64 `json:"memory_usage_mb"` // 메모리 사용량 (MB)
	GoroutineCount int     `json:"goroutine_count"` // 고루틴 수
	UptimeSeconds  int64   `json:"uptime_seconds"`  // 가동 시간 (초)

	// 성능 통계
	AvgLatencyMs float64 `json:"avg_latency_ms"` // 평균 지연시간 (ms)
	MaxLatencyMs float64 `json:"max_latency_ms"` // 최대 지연시간 (ms)
	ErrorRate    float64 `json:"error_rate"`     // 에러율 (%)
}

// PumpDetectorFactory 펌핑 감지기 생성 팩토리
type PumpDetectorFactory interface {
	// CreateDetector 새로운 감지기 생성
	CreateDetector(configPath string) (PumpDetector, error)

	// CreateDetectorWithConfig 설정으로 감지기 생성
	CreateDetectorWithConfig(config DetectorConfig) (PumpDetector, error)

	// GetDefaultConfig 기본 설정 반환
	GetDefaultConfig() DetectorConfig
}
