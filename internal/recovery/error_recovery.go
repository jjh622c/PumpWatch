package recovery

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"PumpWatch/internal/logging"
)

// ErrorSeverity represents the severity level of an error
type ErrorSeverity int

const (
	ErrorRecoverable ErrorSeverity = iota // Individual exchange recovery possible
	ErrorFatal                           // Full system restart required
)

// ExchangeState represents the current state of an exchange connection
type ExchangeState int

const (
	StateHealthy ExchangeState = iota
	StateRetrying  // Currently reconnecting
	StateFailed    // Failed 3 times, waiting for hard reset
	StateCooldown  // In 3-minute cooldown period
)

var stateNames = map[ExchangeState]string{
	StateHealthy:  "HEALTHY",
	StateRetrying: "RETRYING",
	StateFailed:   "FAILED",
	StateCooldown: "COOLDOWN",
}

// ExchangeError represents an error that occurred on a specific exchange
type ExchangeError struct {
	Exchange  string
	Error     error
	Severity  ErrorSeverity
	Timestamp time.Time
}

// ExchangeManager manages the state and recovery of a single exchange
type ExchangeManager struct {
	Exchange      string
	MarketType    string
	State         ExchangeState
	RetryCount    int
	LastFailure   time.Time
	CooldownUntil time.Time
	TotalFailures int64
	LastSuccess   time.Time
	
	// Recovery callbacks
	OnReconnect func(exchange, marketType string) error
	OnHardReset func(reason string) error
	
	mu sync.RWMutex
}

// ReconnectionScheduler manages intelligent error recovery for all exchanges
type ReconnectionScheduler struct {
	exchanges       map[string]*ExchangeManager
	ticker          *time.Ticker
	ctx             context.Context
	cancel          context.CancelFunc
	logger          *logging.Logger
	
	// Configuration
	maxRetries      int
	cooldownPeriod  time.Duration
	checkInterval   time.Duration
	
	// Statistics
	totalRecoveries int64
	totalHardResets int64
	
	mu sync.RWMutex
}

// NewReconnectionScheduler creates a new intelligent error recovery system
func NewReconnectionScheduler(ctx context.Context) *ReconnectionScheduler {
	schedulerCtx, cancel := context.WithCancel(ctx)
	
	return &ReconnectionScheduler{
		exchanges:      make(map[string]*ExchangeManager),
		ctx:           schedulerCtx,
		cancel:        cancel,
		logger:        logging.GetGlobalLogger(),
		maxRetries:    3,                // 3 attempts before hard reset
		cooldownPeriod: 3 * time.Minute, // 3 minute cooldown
		checkInterval:  30 * time.Second, // Check every 30 seconds
	}
}

// RegisterExchange registers an exchange for monitoring and recovery
func (rs *ReconnectionScheduler) RegisterExchange(exchange, marketType string, reconnectCallback func(string, string) error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	
	key := fmt.Sprintf("%s_%s", exchange, marketType)
	manager := &ExchangeManager{
		Exchange:    exchange,
		MarketType:  marketType,
		State:       StateHealthy,
		LastSuccess: time.Now(),
		OnReconnect: reconnectCallback,
	}
	
	rs.exchanges[key] = manager
	rs.logger.Info("Registered exchange for recovery monitoring: %s", key)
}

// Start begins the intelligent error recovery monitoring
func (rs *ReconnectionScheduler) Start() error {
	rs.ticker = time.NewTicker(rs.checkInterval)
	
	go rs.monitorLoop()
	
	rs.logger.Info("🛡️ Intelligent Error Recovery System started (3min cooldown, 3 retries)")
	return nil
}

// Stop stops the error recovery system
func (rs *ReconnectionScheduler) Stop() error {
	if rs.ticker != nil {
		rs.ticker.Stop()
	}
	rs.cancel()
	
	rs.logger.Info("🛡️ Intelligent Error Recovery System stopped")
	return nil
}

// HandleError processes an error and determines the appropriate recovery action
func (rs *ReconnectionScheduler) HandleError(exchange, marketType string, err error) {
	if err == nil {
		return
	}
	
	key := fmt.Sprintf("%s_%s", exchange, marketType)
	severity := rs.classifyError(err)
	
	exchangeError := ExchangeError{
		Exchange:  key,
		Error:     err,
		Severity:  severity,
		Timestamp: time.Now(),
	}
	
	switch severity {
	case ErrorRecoverable:
		rs.handleRecoverableError(key, exchangeError)
	case ErrorFatal:
		rs.handleFatalError(exchangeError)
	}
}

// MarkHealthy marks an exchange as healthy (successful reconnection)
func (rs *ReconnectionScheduler) MarkHealthy(exchange, marketType string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	
	key := fmt.Sprintf("%s_%s", exchange, marketType)
	if manager, exists := rs.exchanges[key]; exists {
		manager.mu.Lock()
		oldState := manager.State
		manager.State = StateHealthy
		manager.RetryCount = 0
		manager.LastSuccess = time.Now()
		manager.mu.Unlock()
		
		if oldState != StateHealthy {
			rs.totalRecoveries++
			rs.logger.Info("🎉 Exchange %s recovered successfully! (Total recoveries: %d)", 
				key, rs.totalRecoveries)
		}
	}
}

// classifyError determines if an error is recoverable or fatal
func (rs *ReconnectionScheduler) classifyError(err error) ErrorSeverity {
	if err == nil {
		return ErrorRecoverable
	}
	
	errStr := strings.ToLower(err.Error())

	// WebSocket-specific panics are recoverable, not fatal
	webSocketPanicPatterns := []string{
		"readmessage panic",
		"ping panic",
		"repeated read on failed websocket",
		"websocket panic",
		"connection panic",
		"sendmessage panic",
	}

	// Check if it's a WebSocket panic (recoverable)
	for _, pattern := range webSocketPanicPatterns {
		if strings.Contains(errStr, pattern) {
			return ErrorRecoverable
		}
	}

	// Fatal errors that require full system restart
	fatalPatterns := []string{
		"out of memory",
		"no space left",
		"disk space",
		"configuration error",
		"initialization failed",
		"multiple exchanges failed simultaneously",
		"segmentation fault",
		"fatal system error",
	}

	for _, pattern := range fatalPatterns {
		if strings.Contains(errStr, pattern) {
			return ErrorFatal
		}
	}
	
	// Recoverable errors (network, WebSocket, etc.)
	recoverablePatterns := []string{
		"websocket", "connection", "timeout", "network", 
		"503", "502", "429", "500", "rate limit",
		"ping", "heartbeat", "read", "write", "dial",
		"refused", "reset", "broken pipe", "eof",
	}
	
	for _, pattern := range recoverablePatterns {
		if strings.Contains(errStr, pattern) {
			return ErrorRecoverable
		}
	}
	
	// Default to recoverable for unknown errors
	return ErrorRecoverable
}

// handleRecoverableError processes recoverable errors with intelligent retry logic
func (rs *ReconnectionScheduler) handleRecoverableError(key string, exchangeError ExchangeError) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	
	manager, exists := rs.exchanges[key]
	if !exists {
		rs.logger.Warn("Received error for unregistered exchange: %s", key)
		return
	}
	
	manager.mu.Lock()
	defer manager.mu.Unlock()
	
	// Skip processing if exchange is already in cooldown, failed, or retrying state
	// This prevents multiple concurrent errors from triggering multiple recovery attempts
	if manager.State != StateHealthy {
		// Skip debug logging to prevent log spam - state changes are already logged elsewhere
		return
	}
	
	manager.LastFailure = exchangeError.Timestamp
	manager.TotalFailures++
	
	// Check if exchange has already exceeded retry limit
	if manager.RetryCount >= rs.maxRetries {
		rs.logger.Error("💥 Exchange %s exceeded %d retries, triggering HARD RESET", 
			key, rs.maxRetries)
		manager.State = StateFailed
		rs.totalHardResets++
		
		// Trigger hard reset in a separate goroutine to avoid blocking
		go rs.triggerHardReset(fmt.Sprintf("Exchange %s exceeded retry limit (%d attempts)", 
			key, rs.maxRetries))
		return
	}
	
	// Increment retry count and schedule recovery attempt
	manager.RetryCount++
	manager.State = StateCooldown
	manager.CooldownUntil = time.Now().Add(rs.cooldownPeriod)
	
	rs.logger.Warn("⏰ Exchange %s failed (attempt %d/%d), cooling down for %v", 
		key, manager.RetryCount, rs.maxRetries, rs.cooldownPeriod)
	rs.logger.Debug("Error details: %v", exchangeError.Error)
}

// handleFatalError immediately triggers a hard reset for fatal errors
func (rs *ReconnectionScheduler) handleFatalError(exchangeError ExchangeError) {
	rs.logger.Error("💀 FATAL ERROR detected in %s: %v", 
		exchangeError.Exchange, exchangeError.Error)
	rs.totalHardResets++
	
	go rs.triggerHardReset(fmt.Sprintf("Fatal error in %s: %s", 
		exchangeError.Exchange, exchangeError.Error.Error()))
}

// monitorLoop runs the periodic recovery check
func (rs *ReconnectionScheduler) monitorLoop() {
	defer func() {
		if r := recover(); r != nil {
			rs.logger.Error("Recovery monitor panic: %v", r)
		}
	}()
	
	for {
		select {
		case <-rs.ctx.Done():
			return
		case <-rs.ticker.C:
			rs.checkRecoveryOpportunities()
		}
	}
}

// checkRecoveryOpportunities checks for exchanges ready to attempt reconnection
func (rs *ReconnectionScheduler) checkRecoveryOpportunities() {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	
	now := time.Now()
	
	for key, manager := range rs.exchanges {
		manager.mu.RLock()
		shouldReconnect := manager.State == StateCooldown && now.After(manager.CooldownUntil)
		onReconnect := manager.OnReconnect
		exchange := manager.Exchange
		marketType := manager.MarketType
		manager.mu.RUnlock()
		
		if shouldReconnect && onReconnect != nil {
			go rs.attemptReconnection(key, exchange, marketType, onReconnect)
		}
	}
}

// attemptReconnection tries to reconnect a specific exchange
func (rs *ReconnectionScheduler) attemptReconnection(key, exchange, marketType string, reconnectFn func(string, string) error) {
	rs.logger.Info("🔄 Attempting reconnection for %s...", key)
	
	// Update state to retrying
	rs.mu.RLock()
	manager := rs.exchanges[key]
	rs.mu.RUnlock()
	
	if manager != nil {
		manager.mu.Lock()
		manager.State = StateRetrying
		manager.mu.Unlock()
	}
	
	// Attempt reconnection
	if err := reconnectFn(exchange, marketType); err != nil {
		rs.logger.Warn("🔴 Reconnection failed for %s: %v", key, err)
		rs.HandleError(exchange, marketType, fmt.Errorf("reconnection failed: %w", err))
	} else {
		rs.logger.Info("🟢 Reconnection successful for %s", key)
		rs.MarkHealthy(exchange, marketType)
	}
}

// triggerHardReset triggers a full system restart
func (rs *ReconnectionScheduler) triggerHardReset(reason string) {
	rs.logger.Error("🚨 TRIGGERING HARD RESET: %s", reason)
	rs.logger.Error("📊 Recovery Stats - Recoveries: %d, Hard Resets: %d", 
		rs.totalRecoveries, rs.totalHardResets)
	
	// TODO: Integrate with existing restart mechanism
	// For now, we'll use os.Exit, but this should be configurable
	// panic(fmt.Sprintf("Hard reset required: %s", reason))
}

// GetStats returns current recovery system statistics
func (rs *ReconnectionScheduler) GetStats() map[string]interface{} {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	
	healthyCount := 0
	retryingCount := 0
	cooldownCount := 0
	failedCount := 0
	
	for _, manager := range rs.exchanges {
		manager.mu.RLock()
		switch manager.State {
		case StateHealthy:
			healthyCount++
		case StateRetrying:
			retryingCount++
		case StateCooldown:
			cooldownCount++
		case StateFailed:
			failedCount++
		}
		manager.mu.RUnlock()
	}
	
	return map[string]interface{}{
		"total_exchanges":    len(rs.exchanges),
		"healthy_exchanges":  healthyCount,
		"retrying_exchanges": retryingCount,
		"cooldown_exchanges": cooldownCount,
		"failed_exchanges":   failedCount,
		"total_recoveries":   rs.totalRecoveries,
		"total_hard_resets":  rs.totalHardResets,
	}
}