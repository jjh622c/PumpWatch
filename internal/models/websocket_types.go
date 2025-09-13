package models

import (
	"context"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// ConnectionStatus는 WebSocket 연결 상태를 나타냄
type ConnectionStatus int

const (
	StatusDisconnected ConnectionStatus = iota
	StatusConnecting
	StatusConnected
	StatusReconnecting
	StatusError
	StatusCooldown
)

func (s ConnectionStatus) String() string {
	switch s {
	case StatusDisconnected:
		return "Disconnected"
	case StatusConnecting:
		return "Connecting"
	case StatusConnected:
		return "Connected"
	case StatusReconnecting:
		return "Reconnecting"
	case StatusError:
		return "Error"
	case StatusCooldown:
		return "Cooldown"
	default:
		return "Unknown"
	}
}

// ErrorType은 에러 분류를 위한 타입
type ErrorType int

const (
	ErrorTypeNetwork ErrorType = iota // 네트워크 에러 -> 재연결 시도
	ErrorTypeAuth                     // 인증 에러 -> 설정 확인 후 재연결  
	ErrorTypeRateLimit                // Rate limit -> 긴 쿨다운 후 재연결
	ErrorTypeMarketData               // 마켓 데이터 에러 -> 심볼 목록 재확인
	ErrorTypeCritical                 // 심각한 에러 -> Hard Reset
)

// ExchangeWebSocketManager는 개별 거래소의 WebSocket 연결을 관리
type ExchangeWebSocketManager struct {
	ID                string             // 고유 ID (예: "binance-spot")
	Exchange          string             // 거래소명 ("binance", "bybit" 등)
	MarketType        string             // "spot" 또는 "futures"
	Connection        *websocket.Conn    // WebSocket 연결
	Status            ConnectionStatus   // 현재 연결 상태
	SubscribedSymbols []string           // 구독 중인 심볼 목록
	
	// 재연결 관련
	MaxSymbolsPerConnection int           // 거래소별 최대 심볼 수
	RetryCount              int           // 현재 재시도 횟수
	MaxRetries              int           // 최대 재시도 횟수
	LastError               error         // 마지막 에러
	CooldownUntil           time.Time     // 쿨다운 종료 시간
	
	// 채널 관리
	MessageChan chan []byte   // 수신 메시지 채널
	ErrorChan   chan error    // 에러 채널
	CloseChan   chan struct{} // 종료 신호 채널
	
	// Context 관리
	ctx    context.Context
	cancel context.CancelFunc
	
	// 통계
	LastMessageTime time.Time // 마지막 메시지 수신 시간
	TotalMessages   int64     // 총 수신 메시지 수
	LastPing        time.Time // 마지막 Ping 시간
	LastPong        time.Time // 마지막 Pong 시간
}

// NewExchangeWebSocketManager는 새로운 WebSocket Manager 생성
func NewExchangeWebSocketManager(exchange, marketType string, config ExchangeConfig) *ExchangeWebSocketManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &ExchangeWebSocketManager{
		ID:                      exchange + "-" + marketType,
		Exchange:                exchange,
		MarketType:              marketType,
		Status:                  StatusDisconnected,
		SubscribedSymbols:       make([]string, 0),
		MaxSymbolsPerConnection: config.MaxSymbolsPerConnection,
		MaxRetries:              config.MaxRetries,
		MessageChan:             make(chan []byte, 1000),   // 버퍼링된 채널
		ErrorChan:               make(chan error, 100),
		CloseChan:               make(chan struct{}),
		ctx:                     ctx,
		cancel:                  cancel,
		LastMessageTime:         time.Now(),
	}
}

// GetStatus는 현재 연결 상태 반환
func (m *ExchangeWebSocketManager) GetStatus() ConnectionStatus {
	return m.Status
}

// SetStatus는 연결 상태 업데이트
func (m *ExchangeWebSocketManager) SetStatus(status ConnectionStatus) {
	m.Status = status
}

// IsHealthy는 연결이 건강한지 확인 (30초 이내 메시지 수신)
func (m *ExchangeWebSocketManager) IsHealthy() bool {
	return time.Since(m.LastMessageTime) < 30*time.Second
}

// ShouldRetry는 재연결 시도 여부 결정
func (m *ExchangeWebSocketManager) ShouldRetry() bool {
	if m.RetryCount >= m.MaxRetries {
		return false
	}
	
	if m.Status == StatusCooldown && time.Now().Before(m.CooldownUntil) {
		return false
	}
	
	return true
}

// IncrementRetry는 재시도 횟수 증가
func (m *ExchangeWebSocketManager) IncrementRetry() {
	m.RetryCount++
}

// ResetRetry는 재시도 횟수 초기화
func (m *ExchangeWebSocketManager) ResetRetry() {
	m.RetryCount = 0
}

// SetCooldown은 쿨다운 설정
func (m *ExchangeWebSocketManager) SetCooldown(duration time.Duration) {
	m.Status = StatusCooldown
	m.CooldownUntil = time.Now().Add(duration)
}

// Close는 WebSocket 연결 정리
func (m *ExchangeWebSocketManager) Close() error {
	if m.Connection != nil {
		m.Connection.Close()
	}
	
	m.cancel()
	close(m.CloseChan)
	close(m.MessageChan)
	close(m.ErrorChan)
	
	m.Status = StatusDisconnected
	return nil
}

// RetryTask는 재시도 작업 정보
type RetryTask struct {
	ManagerID  string    // Manager ID
	RetryAfter time.Time // 재시도 시간
	RetryCount int       // 현재 재시도 횟수
	MaxRetries int       // 최대 재시도 횟수
	ErrorType  ErrorType // 에러 타입
}

// CircuitBreaker는 Circuit Breaker 패턴 구현
type CircuitBreaker struct {
	failureCount    int       // 실패 횟수
	maxFailures     int       // 최대 실패 허용 횟수
	timeout         time.Duration // Circuit Open 유지 시간
	lastFailureTime time.Time // 마지막 실패 시간
	state           string    // "closed", "open", "half-open"
}

// NewCircuitBreaker는 새로운 Circuit Breaker 생성
func NewCircuitBreaker(maxFailures int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures: maxFailures,
		timeout:     timeout,
		state:       "closed",
	}
}

// CanExecute는 실행 가능 여부 확인
func (cb *CircuitBreaker) CanExecute() bool {
	switch cb.state {
	case "closed":
		return true
	case "open":
		if time.Since(cb.lastFailureTime) > cb.timeout {
			cb.state = "half-open"
			return true
		}
		return false
	case "half-open":
		return true
	default:
		return false
	}
}

// OnSuccess는 성공 시 호출
func (cb *CircuitBreaker) OnSuccess() {
	cb.failureCount = 0
	cb.state = "closed"
}

// OnFailure는 실패 시 호출
func (cb *CircuitBreaker) OnFailure() {
	cb.failureCount++
	cb.lastFailureTime = time.Now()
	
	if cb.failureCount >= cb.maxFailures {
		cb.state = "open"
	}
}

// GetState는 현재 상태 반환
func (cb *CircuitBreaker) GetState() string {
	return cb.state
}

// HardResetManager는 Hard Reset 메커니즘 관리
type HardResetManager struct {
	resetCount      int           // 리셋 횟수
	lastReset       time.Time     // 마지막 리셋 시간
	resetCooldown   time.Duration // 리셋 쿨다운
	maxResetPerHour int           // 시간당 최대 리셋 횟수
	
	// Critical error patterns that trigger hard reset
	criticalErrors []string
}

// NewHardResetManager는 새로운 Hard Reset Manager 생성
func NewHardResetManager(maxResetPerHour int, resetCooldown time.Duration) *HardResetManager {
	return &HardResetManager{
		resetCooldown:   resetCooldown,
		maxResetPerHour: maxResetPerHour,
		criticalErrors: []string{
			"connection refused",
			"authentication failed",
			"invalid api key",
			"system overload",
		},
	}
}

// ShouldTriggerHardReset는 Hard Reset 트리거 여부 결정
func (h *HardResetManager) ShouldTriggerHardReset(err error) bool {
	// 시간당 리셋 제한 확인
	if h.resetCount >= h.maxResetPerHour && time.Since(h.lastReset) < time.Hour {
		return false
	}
	
	// 쿨다운 시간 확인
	if time.Since(h.lastReset) < h.resetCooldown {
		return false
	}
	
	// Critical error pattern 매칭
	errorMsg := err.Error()
	for _, pattern := range h.criticalErrors {
		if strings.Contains(errorMsg, pattern) {
			return true
		}
	}
	
	return false
}

// ExecuteHardReset는 Hard Reset 실행 (실제 구현은 WebSocketTaskManager에서)
func (h *HardResetManager) ExecuteHardReset() {
	h.resetCount++
	h.lastReset = time.Now()
}

// GetLastReset는 마지막 리셋 시간 반환
func (h *HardResetManager) GetLastReset() time.Time {
	return h.lastReset
}

// GetResetCount는 현재 리셋 횟수 반환
func (h *HardResetManager) GetResetCount() int {
	return h.resetCount
}

// SystemStatus는 전체 시스템 상태 정보
type SystemStatus struct {
	ActiveConnections    map[string]ConnectionStatus `json:"active_connections"`
	PendingRetries       []RetryTask                  `json:"pending_retries"`
	MemoryUsage          uint64                       `json:"memory_usage"`
	LastHardReset        time.Time                    `json:"last_hard_reset"`
	TotalEventsCollected int64                        `json:"total_events_collected"`
	CircuitBreakerState  string                       `json:"circuit_breaker_state"`
}

// WebSocketTaskManager는 모든 WebSocket 연결을 관리하는 중앙 매니저
type WebSocketTaskManager struct {
	managers       map[string]*ExchangeWebSocketManager // Manager들 (key: exchange-markettype)
	healthChecker  *time.Ticker                         // Health Check 타이머
	retryQueue     chan RetryTask                       // 재시도 큐
	circuitBreaker *CircuitBreaker                      // Circuit Breaker
	hardResetMgr   *HardResetManager                    // Hard Reset Manager
	emergencyStop  chan bool                            // 긴급 중단 신호
	
	ctx    context.Context
	cancel context.CancelFunc
	
	// 통계 및 모니터링
	totalMessages     int64     // 총 수신 메시지 수
	totalReconnects   int64     // 총 재연결 횟수
	totalHardResets   int64     // 총 Hard Reset 횟수
	lastHealthCheck   time.Time // 마지막 Health Check 시간
}

// NewWebSocketTaskManager는 새로운 Task Manager 생성
func NewWebSocketTaskManager() *WebSocketTaskManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &WebSocketTaskManager{
		managers:       make(map[string]*ExchangeWebSocketManager),
		healthChecker:  time.NewTicker(30 * time.Second), // 30초 간격 Health Check
		retryQueue:     make(chan RetryTask, 1000),
		circuitBreaker: NewCircuitBreaker(5, 5*time.Minute), // 5회 실패시 5분 차단
		hardResetMgr:   NewHardResetManager(3, 30*time.Minute), // 시간당 3회, 30분 쿨다운
		emergencyStop:  make(chan bool),
		ctx:            ctx,
		cancel:         cancel,
	}
}

// AddManager는 새로운 Exchange Manager 추가
func (wtm *WebSocketTaskManager) AddManager(manager *ExchangeWebSocketManager) {
	wtm.managers[manager.ID] = manager
}

// GetManager는 특정 Exchange Manager 반환
func (wtm *WebSocketTaskManager) GetManager(id string) (*ExchangeWebSocketManager, bool) {
	manager, exists := wtm.managers[id]
	return manager, exists
}

// GetAllManagers는 모든 Manager 반환
func (wtm *WebSocketTaskManager) GetAllManagers() map[string]*ExchangeWebSocketManager {
	return wtm.managers
}

// GetSystemStatus는 전체 시스템 상태 반환
func (wtm *WebSocketTaskManager) GetSystemStatus() SystemStatus {
	connections := make(map[string]ConnectionStatus)
	for id, manager := range wtm.managers {
		connections[id] = manager.GetStatus()
	}
	
	var pendingRetries []RetryTask
	// retryQueue에서 pending 작업들을 읽어옴 (non-blocking)
	for {
		select {
		case task := <-wtm.retryQueue:
			pendingRetries = append(pendingRetries, task)
		default:
			goto DONE
		}
	}
DONE:
	
	return SystemStatus{
		ActiveConnections:    connections,
		PendingRetries:       pendingRetries,
		TotalEventsCollected: wtm.totalMessages,
		CircuitBreakerState:  wtm.circuitBreaker.GetState(),
		LastHardReset:        wtm.hardResetMgr.lastReset,
	}
}

// Shutdown은 모든 연결 정리
func (wtm *WebSocketTaskManager) Shutdown() error {
	wtm.cancel()
	wtm.healthChecker.Stop()
	
	for _, manager := range wtm.managers {
		manager.Close()
	}
	
	close(wtm.emergencyStop)
	close(wtm.retryQueue)
	
	return nil
}