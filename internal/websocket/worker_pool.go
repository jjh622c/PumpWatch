package websocket

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"PumpWatch/internal/logging"
	"PumpWatch/internal/models"
	"PumpWatch/internal/websocket/connectors"
)

// WorkerPool manages multiple WebSocket workers per exchange based on connection limits
type WorkerPool struct {
	Exchange          string
	MarketType        string
	Symbols           []string
	Workers           []*Worker
	MaxSymbolsPerConn int
	MaxConnections    int
	PingInterval      time.Duration
	ConnectionTimeout time.Duration
	RateLimit         int // requests per second/minute

	// Connection management
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	logger *logging.Logger

	// Statistics
	TotalWorkers  int
	ActiveWorkers int
	TotalMessages int64
	LastActivity  time.Time

	// Callbacks
	OnTradeEvent         func(models.TradeEvent)
	OnError              func(error)
	OnWorkerConnected    func(workerID int)
	OnWorkerDisconnected func(workerID int)
}

// Worker represents a single WebSocket connection worker
type Worker struct {
	ID              int
	Exchange        string
	MarketType      string
	Symbols         []string
	Connector       connectors.WebSocketConnector
	Status          WorkerStatus
	LastMessageTime time.Time
	MessageCount    int64
	ReconnectCount  int

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
	logger *logging.Logger

	// Rate limiting
	rateLimiter *RateLimiter

	// Callbacks
	OnTradeEvent   func(models.TradeEvent)
	OnError        func(error)
	OnConnected    func()
	OnDisconnected func()
}

// WorkerStatus represents the status of a worker
type WorkerStatus int

const (
	WorkerIdle WorkerStatus = iota
	WorkerConnecting
	WorkerConnected
	WorkerReconnecting
	WorkerFailed
	WorkerStopped
)

func (ws WorkerStatus) String() string {
	switch ws {
	case WorkerIdle:
		return "idle"
	case WorkerConnecting:
		return "connecting"
	case WorkerConnected:
		return "connected"
	case WorkerReconnecting:
		return "reconnecting"
	case WorkerFailed:
		return "failed"
	case WorkerStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// RateLimiter implements exchange-specific rate limiting
type RateLimiter struct {
	maxRequests int
	interval    time.Duration
	requests    []time.Time
	mu          sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(maxRequests int, interval time.Duration) *RateLimiter {
	return &RateLimiter{
		maxRequests: maxRequests,
		interval:    interval,
		requests:    make([]time.Time, 0),
	}
}

// Allow checks if a request is allowed within rate limits
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Remove expired requests
	cutoff := now.Add(-rl.interval)
	var validRequests []time.Time
	for _, t := range rl.requests {
		if t.After(cutoff) {
			validRequests = append(validRequests, t)
		}
	}
	rl.requests = validRequests

	// Check if we can make a new request
	if len(rl.requests) >= rl.maxRequests {
		return false
	}

	// Add new request
	rl.requests = append(rl.requests, now)
	return true
}

// NewWorkerPool creates a new worker pool based on exchange limitations
func NewWorkerPool(ctx context.Context, exchange, marketType string, symbols []string, exchangeConfig ...interface{}) (*WorkerPool, error) {
	poolCtx, cancel := context.WithCancel(ctx)

	// Get exchange-specific configuration
	var config ExchangeWorkerConfig
	if len(exchangeConfig) > 0 {
		// Use provided config (from config.yaml)
		if cfg, ok := exchangeConfig[0].(ExchangeWorkerConfig); ok {
			config = cfg
		} else {
			config = getExchangeWorkerConfig(exchange) // fallback
		}
	} else {
		config = getExchangeWorkerConfig(exchange) // fallback
	}

	pool := &WorkerPool{
		Exchange:          exchange,
		MarketType:        marketType,
		Symbols:           symbols,
		MaxSymbolsPerConn: config.MaxSymbolsPerConnection,
		MaxConnections:    config.MaxConnections,
		PingInterval:      config.PingInterval,
		ConnectionTimeout: config.ConnectionTimeout,
		RateLimit:         config.RateLimit,
		ctx:               poolCtx,
		cancel:            cancel,
		logger:            logging.GetGlobalLogger(),
		LastActivity:      time.Now(),
	}

	// Calculate number of workers needed
	numWorkers := int(math.Ceil(float64(len(symbols)) / float64(config.MaxSymbolsPerConnection)))
	if numWorkers > config.MaxConnections {
		numWorkers = config.MaxConnections
		pool.logger.Warn("🚨 %s %s: 심볼 수(%d)가 최대 연결 제한을 초과합니다. %d 연결로 제한됨",
			exchange, marketType, len(symbols), config.MaxConnections)
	}

	pool.TotalWorkers = numWorkers

	// Create workers
	for i := 0; i < numWorkers; i++ {
		worker, err := pool.createWorker(i)
		if err != nil {
			pool.logger.Error("Worker %d 생성 실패: %v", i, err)
			continue
		}
		pool.Workers = append(pool.Workers, worker)
	}

	pool.logger.Info("🏗️ %s %s WorkerPool 생성: %d 워커, %d 심볼",
		exchange, marketType, len(pool.Workers), len(symbols))

	return pool, nil
}

// ExchangeWorkerConfig holds exchange-specific worker configuration
type ExchangeWorkerConfig struct {
	MaxSymbolsPerConnection int
	MaxConnections          int
	PingInterval            time.Duration
	ConnectionTimeout       time.Duration
	RateLimit               int // requests per interval
	RateLimitInterval       time.Duration
}

// getExchangeWorkerConfig returns exchange-specific worker configuration
func getExchangeWorkerConfig(exchange string) ExchangeWorkerConfig {
	switch exchange {
	case "binance":
		return ExchangeWorkerConfig{
			MaxSymbolsPerConnection: 1000, // Conservative limit from 1,024 max
			MaxConnections:          10,   // Conservative from API limits
			PingInterval:            20 * time.Second,
			ConnectionTimeout:       45 * time.Second,
			RateLimit:               5, // 5 messages per second
			RateLimitInterval:       1 * time.Second,
		}
	case "bybit":
		return ExchangeWorkerConfig{
			MaxSymbolsPerConnection: 200, // Conservative estimate
			MaxConnections:          50,  // Based on 500 connections per 5min
			PingInterval:            20 * time.Second,
			ConnectionTimeout:       60 * time.Second,
			RateLimit:               10, // Conservative estimate
			RateLimitInterval:       1 * time.Second,
		}
	case "okx":
		return ExchangeWorkerConfig{
			MaxSymbolsPerConnection: 100, // Conservative from 480 requests/hour
			MaxConnections:          20,  // Conservative estimate
			PingInterval:            30 * time.Second,
			ConnectionTimeout:       30 * time.Second,
			RateLimit:               8, // 480 per hour = ~8 per minute
			RateLimitInterval:       1 * time.Minute,
		}
	case "kucoin":
		return ExchangeWorkerConfig{
			MaxSymbolsPerConnection: 400, // Conservative from 500 max topics
			MaxConnections:          40,  // Conservative from 50 max connections
			PingInterval:            30 * time.Second,
			ConnectionTimeout:       60 * time.Second,
			RateLimit:               10, // 100 per 10 seconds = 10 per second
			RateLimitInterval:       1 * time.Second,
		}
	case "phemex":
		return ExchangeWorkerConfig{
			MaxSymbolsPerConnection: 100, // Conservative estimate
			MaxConnections:          20,  // Conservative estimate
			PingInterval:            30 * time.Second,
			ConnectionTimeout:       60 * time.Second,
			RateLimit:               5, // Conservative estimate
			RateLimitInterval:       1 * time.Second,
		}
	case "gate":
		return ExchangeWorkerConfig{
			MaxSymbolsPerConnection: 50,               // Conservative from 50 requests/sec limit
			MaxConnections:          10,               // Conservative estimate
			PingInterval:            10 * time.Second, // 5-10 seconds recommended
			ConnectionTimeout:       60 * time.Second,
			RateLimit:               50, // 50 requests per second per channel
			RateLimitInterval:       1 * time.Second,
		}
	default:
		// Default conservative configuration
		return ExchangeWorkerConfig{
			MaxSymbolsPerConnection: 100,
			MaxConnections:          10,
			PingInterval:            30 * time.Second,
			ConnectionTimeout:       60 * time.Second,
			RateLimit:               5,
			RateLimitInterval:       1 * time.Second,
		}
	}
}

// createWorker creates a new worker for the pool
func (wp *WorkerPool) createWorker(workerID int) (*Worker, error) {
	// Calculate symbols for this worker
	startIdx := workerID * wp.MaxSymbolsPerConn
	endIdx := startIdx + wp.MaxSymbolsPerConn
	if endIdx > len(wp.Symbols) {
		endIdx = len(wp.Symbols)
	}

	if startIdx >= len(wp.Symbols) {
		return nil, fmt.Errorf("worker %d: 할당할 심볼이 없음", workerID)
	}

	workerSymbols := wp.Symbols[startIdx:endIdx]

	// Create connector
	factory, err := connectors.GetConnectorFactory(wp.Exchange)
	if err != nil {
		return nil, fmt.Errorf("connector factory 생성 실패: %w", err)
	}

	connector := factory(wp.MarketType, len(workerSymbols))

	// Create worker context
	workerCtx, workerCancel := context.WithCancel(wp.ctx)

	// Create rate limiter
	config := getExchangeWorkerConfig(wp.Exchange)
	rateLimiter := NewRateLimiter(config.RateLimit, config.RateLimitInterval)

	worker := &Worker{
		ID:              workerID,
		Exchange:        wp.Exchange,
		MarketType:      wp.MarketType,
		Symbols:         workerSymbols,
		Connector:       connector,
		Status:          WorkerIdle,
		LastMessageTime: time.Now(),
		ctx:             workerCtx,
		cancel:          workerCancel,
		logger:          wp.logger,
		rateLimiter:     rateLimiter,
	}

	// Set up callbacks
	worker.OnTradeEvent = wp.OnTradeEvent
	worker.OnError = wp.OnError
	worker.OnConnected = func() {
		wp.mu.Lock()
		wp.ActiveWorkers++
		wp.mu.Unlock()
		if wp.OnWorkerConnected != nil {
			wp.OnWorkerConnected(workerID)
		}
	}
	worker.OnDisconnected = func() {
		wp.mu.Lock()
		if wp.ActiveWorkers > 0 {
			wp.ActiveWorkers--
		}
		wp.mu.Unlock()
		if wp.OnWorkerDisconnected != nil {
			wp.OnWorkerDisconnected(workerID)
		}
	}

	wp.logger.Info("👷 Worker %d 생성: %s %s, %d 심볼 [%s...%s]",
		workerID, wp.Exchange, wp.MarketType, len(workerSymbols),
		workerSymbols[0], workerSymbols[len(workerSymbols)-1])

	return worker, nil
}

// Start starts all workers in the pool
func (wp *WorkerPool) Start() error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	wp.logger.Info("🚀 %s %s WorkerPool 시작: %d 워커", wp.Exchange, wp.MarketType, len(wp.Workers))

	for _, worker := range wp.Workers {
		go worker.Start()

		// 연결 제한을 피하기 위해 약간의 지연
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

// Stop stops all workers in the pool
func (wp *WorkerPool) Stop() error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	wp.logger.Info("🛑 %s %s WorkerPool 중지", wp.Exchange, wp.MarketType)

	wp.cancel()

	for _, worker := range wp.Workers {
		worker.Stop()
	}

	wp.ActiveWorkers = 0
	return nil
}

// GetStats returns pool statistics
func (wp *WorkerPool) GetStats() map[string]interface{} {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	var totalMessages int64
	workerStats := make(map[string]interface{})

	for _, worker := range wp.Workers {
		worker.mu.RLock()
		totalMessages += worker.MessageCount
		workerStats[fmt.Sprintf("worker_%d", worker.ID)] = map[string]interface{}{
			"status":       worker.Status.String(),
			"symbols":      len(worker.Symbols),
			"messages":     worker.MessageCount,
			"reconnects":   worker.ReconnectCount,
			"last_message": worker.LastMessageTime,
		}
		worker.mu.RUnlock()
	}

	return map[string]interface{}{
		"exchange":       wp.Exchange,
		"market_type":    wp.MarketType,
		"total_workers":  wp.TotalWorkers,
		"active_workers": wp.ActiveWorkers,
		"total_symbols":  len(wp.Symbols),
		"total_messages": totalMessages,
		"last_activity":  wp.LastActivity,
		"worker_details": workerStats,
	}
}

// Start starts the worker
func (w *Worker) Start() {
	w.mu.Lock()
	w.Status = WorkerConnecting
	w.mu.Unlock()

	w.logger.Info("👷 Worker %d 시작: %s %s, %d 심볼",
		w.ID, w.Exchange, w.MarketType, len(w.Symbols))

	// Connection loop with automatic reconnection
	for {
		select {
		case <-w.ctx.Done():
			w.mu.Lock()
			w.Status = WorkerStopped
			w.mu.Unlock()
			w.logger.Info("👷 Worker %d 중지됨", w.ID)
			return
		default:
		}

		// Rate limiting check
		if !w.rateLimiter.Allow() {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Attempt connection
		if err := w.connect(); err != nil {
			w.logger.Warn("👷 Worker %d 연결 실패: %v", w.ID, err)
			w.handleConnectionError(err)
			continue
		}

		// Connection successful
		w.mu.Lock()
		w.Status = WorkerConnected
		w.ReconnectCount++
		w.mu.Unlock()

		if w.OnConnected != nil {
			w.OnConnected()
		}

		w.logger.Info("👷 Worker %d 연결 성공", w.ID)

		// Start message processing
		w.processMessages()

		// Connection lost, prepare for reconnection
		w.mu.Lock()
		w.Status = WorkerReconnecting
		w.mu.Unlock()

		if w.OnDisconnected != nil {
			w.OnDisconnected()
		}

		w.logger.Info("👷 Worker %d 재연결 필요", w.ID)

		// Exponential backoff with jitter
		backoff := time.Duration(w.ReconnectCount) * time.Second
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}

		select {
		case <-w.ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// connect establishes WebSocket connection for the worker
func (w *Worker) connect() error {
	return w.Connector.Connect(w.ctx, w.Symbols)
}

// processMessages processes incoming messages from the connector
func (w *Worker) processMessages() {
	messageChan := make(chan models.TradeEvent, 500000) // 상장 펌핑 대응을 위한 대용량 버퍼

	// Start message loop
	go func() {
		if err := w.Connector.StartMessageLoop(w.ctx, messageChan); err != nil {
			w.logger.Error("👷 Worker %d 메시지 루프 오류: %v", w.ID, err)
			if w.OnError != nil {
				w.OnError(err)
			}
		}
	}()

	// Process messages
	for {
		select {
		case <-w.ctx.Done():
			return
		case tradeEvent := <-messageChan:
			w.mu.Lock()
			w.MessageCount++
			w.LastMessageTime = time.Now()
			w.mu.Unlock()

			if w.OnTradeEvent != nil {
				w.OnTradeEvent(tradeEvent)
			}
		}
	}
}

// handleConnectionError handles connection errors
func (w *Worker) handleConnectionError(err error) {
	w.mu.Lock()
	w.Status = WorkerReconnecting
	w.mu.Unlock()

	if w.OnError != nil {
		w.OnError(fmt.Errorf("worker %d connection error: %w", w.ID, err))
	}
}

// Stop stops the worker
func (w *Worker) Stop() {
	w.cancel()
	if w.Connector != nil {
		w.Connector.Disconnect()
	}
	w.mu.Lock()
	w.Status = WorkerStopped
	w.mu.Unlock()

	w.logger.Info("👷 Worker %d 중지 완료", w.ID)
}
