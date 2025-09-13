package websocket

import (
	"context"
	"fmt"
	"sync"
	"time"

	"PumpWatch/internal/analyzer"
	"PumpWatch/internal/config"
	"PumpWatch/internal/logging"
	"PumpWatch/internal/models"
	"PumpWatch/internal/recovery"
	"PumpWatch/internal/storage"
	"PumpWatch/internal/symbols"
)

// EnhancedTaskManager manages multiple WorkerPools for all exchanges with intelligent scaling
type EnhancedTaskManager struct {
	ctx             context.Context
	cancel          context.CancelFunc

	// Configuration
	exchangesConfig config.ExchangesConfig
	symbolsConfig   *symbols.SymbolsConfig
	storageManager  *storage.Manager
	pumpAnalyzer    *analyzer.PumpAnalyzer

	// Multi-worker connection management
	workerPools     map[string]*WorkerPool // key: "exchange_markettype"
	poolsMu         sync.RWMutex

	// Intelligent error recovery system
	recoveryScheduler *recovery.ReconnectionScheduler
	logger           *logging.Logger

	// Data collection
	currentCollection *models.CollectionEvent
	collectionMu      sync.RWMutex
	collectionTimer   *time.Timer

	// Enhanced statistics tracking
	stats           EnhancedTaskManagerStats
	statsMu         sync.RWMutex

	// Health monitoring
	healthTicker    *time.Ticker
	running         bool
	runningMu       sync.RWMutex
}

// EnhancedTaskManagerStats holds comprehensive task manager statistics
type EnhancedTaskManagerStats struct {
	TotalPools             int       `json:"total_pools"`
	ActivePools            int       `json:"active_pools"`
	TotalWorkers           int       `json:"total_workers"`
	ActiveWorkers          int       `json:"active_workers"`
	TotalSymbols           int       `json:"total_symbols"`
	TotalMessagesReceived  int64     `json:"total_messages_received"`
	MessagesPerSecond      float64   `json:"messages_per_second"`
	LastHealthCheck        time.Time `json:"last_health_check"`
	LastDataCollection     time.Time `json:"last_data_collection"`
	CollectionActive       bool      `json:"collection_active"`

	// Per-exchange statistics
	ExchangeStats          map[string]ExchangeStats `json:"exchange_stats"`
}

// ExchangeStats holds statistics for a specific exchange
type ExchangeStats struct {
	Exchange         string `json:"exchange"`
	TotalWorkers     int    `json:"total_workers"`
	ActiveWorkers    int    `json:"active_workers"`
	TotalSymbols     int    `json:"total_symbols"`
	TotalMessages    int64  `json:"total_messages"`
	AvgLatency       float64 `json:"avg_latency_ms"`
	ErrorRate        float64 `json:"error_rate"`
	UptimePercent    float64 `json:"uptime_percent"`
}

// NewEnhancedTaskManager creates a new enhanced WebSocket task manager with multi-worker support
func NewEnhancedTaskManager(ctx context.Context, exchangesConfig config.ExchangesConfig, symbolsConfig *symbols.SymbolsConfig, storageManager *storage.Manager) (*EnhancedTaskManager, error) {
	taskCtx, cancel := context.WithCancel(ctx)

	tm := &EnhancedTaskManager{
		ctx:             taskCtx,
		cancel:          cancel,
		exchangesConfig: exchangesConfig,
		symbolsConfig:   symbolsConfig,
		storageManager:  storageManager,
		pumpAnalyzer:    analyzer.NewPumpAnalyzer(),
		workerPools:     make(map[string]*WorkerPool),
		logger:          logging.GetGlobalLogger(),
		stats: EnhancedTaskManagerStats{
			LastHealthCheck: time.Now(),
			ExchangeStats:   make(map[string]ExchangeStats),
		},
	}

	// Initialize intelligent error recovery system
	tm.recoveryScheduler = recovery.NewReconnectionScheduler(taskCtx)

	// Initialize all worker pools for each exchange-market combination
	if err := tm.initializeWorkerPools(); err != nil {
		return nil, fmt.Errorf("failed to initialize worker pools: %w", err)
	}

	return tm, nil
}

// initializeWorkerPools creates worker pools for all exchange-market combinations
func (tm *EnhancedTaskManager) initializeWorkerPools() error {
	exchanges := []string{"binance", "bybit", "okx", "kucoin", "phemex", "gate"}
	markets := []string{"spot", "futures"}

	totalSymbols := 0
	totalWorkers := 0

	for _, exchange := range exchanges {
		exchangeConfig := tm.getExchangeConfig(exchange)
		if exchangeConfig == nil {
			tm.logger.Warn("⚠️ Exchange configuration not found: %s", exchange)
			continue
		}

		for _, market := range markets {
			poolID := fmt.Sprintf("%s_%s", exchange, market)

			// Get symbols for this exchange-market combination
			symbols := tm.getSymbolsForExchangeMarket(exchange, market)
			if len(symbols) == 0 {
				tm.logger.Warn("⚠️ No symbols found for %s", poolID)
				continue
			}

			// Create worker pool
			pool, err := NewWorkerPool(tm.ctx, exchange, market, symbols)
			if err != nil {
				tm.logger.Error("❌ Failed to create worker pool for %s: %v", poolID, err)
				continue
			}

			// Set up pool callbacks
			tm.setupPoolCallbacks(pool, exchange, market)

			tm.workerPools[poolID] = pool

			totalSymbols += len(symbols)
			totalWorkers += pool.TotalWorkers

			tm.logger.Info("🏭 Initialized WorkerPool: %s (%d workers, %d symbols)",
				poolID, pool.TotalWorkers, len(symbols))
		}
	}

	tm.stats.TotalPools = len(tm.workerPools)
	tm.stats.TotalWorkers = totalWorkers
	tm.stats.TotalSymbols = totalSymbols

	tm.logger.Info("✅ Initialized %d WorkerPools with %d total workers monitoring %d symbols",
		len(tm.workerPools), totalWorkers, totalSymbols)

	return nil
}

// setupPoolCallbacks configures callbacks for a worker pool
func (tm *EnhancedTaskManager) setupPoolCallbacks(pool *WorkerPool, exchange, marketType string) {
	// Trade event callback
	pool.OnTradeEvent = func(tradeEvent models.TradeEvent) {
		tm.handleTradeEvent(exchange, marketType, tradeEvent)
	}

	// Error callback for recovery system
	pool.OnError = func(err error) {
		tm.recoveryScheduler.HandleError(exchange, marketType, err)
	}

	// Worker connection callbacks
	pool.OnWorkerConnected = func(workerID int) {
		tm.statsMu.Lock()
		tm.stats.ActiveWorkers++
		tm.statsMu.Unlock()

		tm.logger.Debug("🔌 Worker connected: %s_%s #%d", exchange, marketType, workerID)
	}

	pool.OnWorkerDisconnected = func(workerID int) {
		tm.statsMu.Lock()
		if tm.stats.ActiveWorkers > 0 {
			tm.stats.ActiveWorkers--
		}
		tm.statsMu.Unlock()

		tm.logger.Debug("🔌 Worker disconnected: %s_%s #%d", exchange, marketType, workerID)
	}
}

// handleTradeEvent processes incoming trade events
func (tm *EnhancedTaskManager) handleTradeEvent(exchange, marketType string, tradeEvent models.TradeEvent) {
	// Update statistics
	tm.statsMu.Lock()
	tm.stats.TotalMessagesReceived++

	// Update exchange-specific stats
	exchangeStats := tm.stats.ExchangeStats[exchange]
	exchangeStats.TotalMessages++
	tm.stats.ExchangeStats[exchange] = exchangeStats
	tm.statsMu.Unlock()

	// Store trade event if collection is active
	tm.collectionMu.RLock()
	if tm.currentCollection != nil {
		tm.storeTradeEvent(exchange, marketType, &tradeEvent)
	}
	tm.collectionMu.RUnlock()
}

// storeTradeEvent stores trade event in appropriate CollectionEvent slice
func (tm *EnhancedTaskManager) storeTradeEvent(exchange, marketType string, tradeEvent *models.TradeEvent) {
	if tm.currentCollection == nil {
		return
	}

	connID := fmt.Sprintf("%s_%s", exchange, marketType)

	// Store in appropriate slice based on connection ID
	switch connID {
	case "binance_spot":
		tm.currentCollection.BinanceSpot = append(tm.currentCollection.BinanceSpot, *tradeEvent)
	case "binance_futures":
		tm.currentCollection.BinanceFutures = append(tm.currentCollection.BinanceFutures, *tradeEvent)
	case "bybit_spot":
		tm.currentCollection.BybitSpot = append(tm.currentCollection.BybitSpot, *tradeEvent)
	case "bybit_futures":
		tm.currentCollection.BybitFutures = append(tm.currentCollection.BybitFutures, *tradeEvent)
	case "okx_spot":
		tm.currentCollection.OKXSpot = append(tm.currentCollection.OKXSpot, *tradeEvent)
	case "okx_futures":
		tm.currentCollection.OKXFutures = append(tm.currentCollection.OKXFutures, *tradeEvent)
	case "kucoin_spot":
		tm.currentCollection.KuCoinSpot = append(tm.currentCollection.KuCoinSpot, *tradeEvent)
	case "kucoin_futures":
		tm.currentCollection.KuCoinFutures = append(tm.currentCollection.KuCoinFutures, *tradeEvent)
	case "phemex_spot":
		tm.currentCollection.PhemexSpot = append(tm.currentCollection.PhemexSpot, *tradeEvent)
	case "phemex_futures":
		tm.currentCollection.PhemexFutures = append(tm.currentCollection.PhemexFutures, *tradeEvent)
	case "gate_spot":
		tm.currentCollection.GateSpot = append(tm.currentCollection.GateSpot, *tradeEvent)
	case "gate_futures":
		tm.currentCollection.GateFutures = append(tm.currentCollection.GateFutures, *tradeEvent)
	}
}

// Start begins the Enhanced WebSocket Task Manager
func (tm *EnhancedTaskManager) Start() error {
	tm.runningMu.Lock()
	defer tm.runningMu.Unlock()

	if tm.running {
		return fmt.Errorf("enhanced task manager is already running")
	}

	tm.running = true

	// Start intelligent error recovery system
	if err := tm.recoveryScheduler.Start(); err != nil {
		return fmt.Errorf("failed to start recovery scheduler: %w", err)
	}

	// Start health monitoring
	tm.healthTicker = time.NewTicker(45 * time.Second)
	go tm.healthCheckWorker()

	// Start all worker pools with staggered timing
	tm.poolsMu.RLock()
	for poolID, pool := range tm.workerPools {
		tm.logger.Info("🚀 Starting WorkerPool: %s", poolID)

		if err := pool.Start(); err != nil {
			tm.logger.Error("❌ Failed to start WorkerPool %s: %v", poolID, err)
		}

		// Register with recovery system
		tm.recoveryScheduler.RegisterExchange(pool.Exchange, pool.MarketType, tm.createPoolReconnectCallback(poolID))

		// Staggered start to avoid overwhelming APIs
		time.Sleep(500 * time.Millisecond)
	}
	tm.poolsMu.RUnlock()

	tm.logger.Info("🚀 Enhanced Task Manager started with %d WorkerPools + intelligent recovery", len(tm.workerPools))
	return nil
}

// Stop gracefully stops the Enhanced WebSocket Task Manager
func (tm *EnhancedTaskManager) Stop() error {
	tm.runningMu.Lock()
	defer tm.runningMu.Unlock()

	if !tm.running {
		return nil
	}

	tm.running = false

	// Stop intelligent error recovery system
	if tm.recoveryScheduler != nil {
		tm.recoveryScheduler.Stop()
	}

	// Stop health monitoring
	if tm.healthTicker != nil {
		tm.healthTicker.Stop()
	}

	// Stop collection timer if active
	if tm.collectionTimer != nil {
		tm.collectionTimer.Stop()
	}

	// Stop all worker pools
	tm.poolsMu.Lock()
	for poolID, pool := range tm.workerPools {
		tm.logger.Info("🛑 Stopping WorkerPool: %s", poolID)
		if err := pool.Stop(); err != nil {
			tm.logger.Error("❌ Failed to stop WorkerPool %s: %v", poolID, err)
		}
	}
	tm.poolsMu.Unlock()

	// Cancel main context
	tm.cancel()

	tm.logger.Info("✅ Enhanced Task Manager stopped")
	return nil
}

// createPoolReconnectCallback creates a reconnection callback for worker pools
func (tm *EnhancedTaskManager) createPoolReconnectCallback(poolID string) func(string, string) error {
	return func(exchange, marketType string) error {
		tm.poolsMu.RLock()
		pool, exists := tm.workerPools[poolID]
		tm.poolsMu.RUnlock()

		if !exists {
			return fmt.Errorf("worker pool not found for %s", poolID)
		}

		tm.logger.Info("🔄 Attempting intelligent reconnection for WorkerPool %s", poolID)

		// Stop the pool
		if err := pool.Stop(); err != nil {
			tm.logger.Warn("Warning stopping pool %s: %v", poolID, err)
		}

		// Wait a moment before restarting
		time.Sleep(2 * time.Second)

		// Restart the pool
		if err := pool.Start(); err != nil {
			return fmt.Errorf("failed to restart WorkerPool %s: %w", poolID, err)
		}

		tm.logger.Info("✅ WorkerPool %s successfully reconnected", poolID)
		return nil
	}
}

// StartDataCollection starts data collection for a listing event
func (tm *EnhancedTaskManager) StartDataCollection(symbol string, triggerTime time.Time) error {
	tm.collectionMu.Lock()
	defer tm.collectionMu.Unlock()

	if tm.currentCollection != nil {
		return fmt.Errorf("data collection already active for symbol: %s", tm.currentCollection.Symbol)
	}

	// Calculate collection window (-20 seconds to +20 seconds)
	collectionStart := triggerTime.Add(-20 * time.Second)
	collectionEnd := triggerTime.Add(20 * time.Second)

	// Create new collection event
	tm.currentCollection = models.NewCollectionEvent(symbol, triggerTime)

	tm.logger.Info("📡 Starting data collection for %s", symbol)
	tm.logger.Info("⏰ Collection window: %s to %s (40 seconds)",
		collectionStart.Format("15:04:05"), collectionEnd.Format("15:04:05"))

	// Schedule collection completion
	tm.scheduleCollectionCompletion(collectionEnd)

	tm.statsMu.Lock()
	tm.stats.LastDataCollection = time.Now()
	tm.stats.CollectionActive = true
	tm.statsMu.Unlock()

	return nil
}

// scheduleCollectionCompletion schedules the end of data collection
func (tm *EnhancedTaskManager) scheduleCollectionCompletion(endTime time.Time) {
	duration := time.Until(endTime)
	if duration <= 0 {
		// Already past end time, complete immediately
		go tm.completeDataCollection()
		return
	}

	tm.collectionTimer = time.AfterFunc(duration, func() {
		tm.completeDataCollection()
	})
}

// completeDataCollection completes the current data collection
func (tm *EnhancedTaskManager) completeDataCollection() {
	tm.collectionMu.Lock()
	defer tm.collectionMu.Unlock()

	if tm.currentCollection == nil {
		return
	}

	collectionEvent := tm.currentCollection
	tm.currentCollection = nil

	tm.statsMu.Lock()
	tm.stats.CollectionActive = false
	tm.statsMu.Unlock()

	tm.logger.Info("✅ Data collection completed for %s", collectionEvent.Symbol)
	tm.logger.Info("📊 Total trades collected: %d", collectionEvent.GetTotalTradeCount())

	// Store collection event (raw data)
	if err := tm.storageManager.StoreCollectionEvent(collectionEvent); err != nil {
		tm.logger.Error("❌ Failed to store collection event: %v", err)
	}

	// Analyze for pump events
	if collectionEvent.GetTotalTradeCount() > 0 {
		tm.logger.Info("🔍 Starting pump analysis for %s...", collectionEvent.Symbol)

		pumpAnalysis, err := tm.pumpAnalyzer.AnalyzePumps(collectionEvent)
		if err != nil {
			tm.logger.Error("❌ Pump analysis failed: %v", err)
		} else if len(pumpAnalysis.PumpEvents) > 0 {
			// Store pump analysis results
			if err := tm.storageManager.StorePumpAnalysis(collectionEvent.Symbol, collectionEvent.TriggerTime, pumpAnalysis); err != nil {
				tm.logger.Error("❌ Failed to store pump analysis: %v", err)
			} else {
				tm.logger.Info("💾 Stored pump analysis: %d pump events, max change: %.2f%%",
					len(pumpAnalysis.PumpEvents), pumpAnalysis.Summary.MaxPriceChange)
			}
		} else {
			tm.logger.Info("📈 No significant pump events detected for %s", collectionEvent.Symbol)
		}
	}

	// Clear collection timer
	if tm.collectionTimer != nil {
		tm.collectionTimer.Stop()
		tm.collectionTimer = nil
	}
}

// healthCheckWorker performs periodic health checks
func (tm *EnhancedTaskManager) healthCheckWorker() {
	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-tm.healthTicker.C:
			tm.performHealthCheck()
		}
	}
}

// performHealthCheck checks the health of all worker pools
func (tm *EnhancedTaskManager) performHealthCheck() {
	tm.statsMu.Lock()
	tm.stats.LastHealthCheck = time.Now()
	tm.stats.ActivePools = 0
	tm.stats.ActiveWorkers = 0

	// Reset exchange stats
	for exchange := range tm.stats.ExchangeStats {
		stats := tm.stats.ExchangeStats[exchange]
		stats.ActiveWorkers = 0
		tm.stats.ExchangeStats[exchange] = stats
	}
	tm.statsMu.Unlock()

	tm.poolsMu.RLock()
	for poolID, pool := range tm.workerPools {
		poolStats := pool.GetStats()

		activeWorkers := poolStats["active_workers"].(int)
		totalMessages := poolStats["total_messages"].(int64)

		tm.statsMu.Lock()
		if activeWorkers > 0 {
			tm.stats.ActivePools++
		}
		tm.stats.ActiveWorkers += activeWorkers

		// Update exchange stats
		exchangeStats := tm.stats.ExchangeStats[pool.Exchange]
		exchangeStats.Exchange = pool.Exchange
		exchangeStats.ActiveWorkers += activeWorkers
		exchangeStats.TotalMessages = totalMessages
		tm.stats.ExchangeStats[pool.Exchange] = exchangeStats
		tm.statsMu.Unlock()

		tm.logger.Debug("🏭 Pool %s: %d/%d workers active, %d messages",
			poolID, activeWorkers, pool.TotalWorkers, totalMessages)
	}
	tm.poolsMu.RUnlock()

	tm.logger.Info("💗 Health check - Active Pools: %d/%d, Active Workers: %d/%d, Total Messages: %d",
		tm.stats.ActivePools, tm.stats.TotalPools, tm.stats.ActiveWorkers, tm.stats.TotalWorkers, tm.stats.TotalMessagesReceived)
}

// GetStats returns current enhanced task manager statistics
func (tm *EnhancedTaskManager) GetStats() EnhancedTaskManagerStats {
	tm.statsMu.RLock()
	defer tm.statsMu.RUnlock()
	return tm.stats
}

// GetStatsInterface returns stats as interface{} for compatibility
func (tm *EnhancedTaskManager) GetStatsInterface() interface{} {
	return tm.GetStats()
}

// GetPoolStats returns detailed statistics for all worker pools
func (tm *EnhancedTaskManager) GetPoolStats() map[string]interface{} {
	tm.poolsMu.RLock()
	defer tm.poolsMu.RUnlock()

	poolStats := make(map[string]interface{})
	for poolID, pool := range tm.workerPools {
		poolStats[poolID] = pool.GetStats()
	}

	return poolStats
}

// GetRecoveryStats returns intelligent recovery system statistics
func (tm *EnhancedTaskManager) GetRecoveryStats() map[string]interface{} {
	if tm.recoveryScheduler == nil {
		return map[string]interface{}{"error": "recovery scheduler not initialized"}
	}
	return tm.recoveryScheduler.GetStats()
}

// getSymbolsForExchangeMarket returns filtered symbols for specific exchange-market
func (tm *EnhancedTaskManager) getSymbolsForExchangeMarket(exchange, market string) []string {
	subscriptionKey := fmt.Sprintf("%s_%s", exchange, market)

	if symbols, exists := tm.symbolsConfig.SubscriptionLists[subscriptionKey]; exists {
		tm.logger.Info("📊 %s symbols: %d", subscriptionKey, len(symbols))
		return symbols
	}

	// Fallback to default symbols if subscription list not found
	defaultSymbols := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
	tm.logger.Warn("⚠️ Using default symbols for %s: %v", subscriptionKey, defaultSymbols)
	return defaultSymbols
}

// getExchangeConfig returns the configuration for a specific exchange
func (tm *EnhancedTaskManager) getExchangeConfig(exchange string) *config.ExchangeConfig {
	switch exchange {
	case "binance":
		return &tm.exchangesConfig.Binance
	case "bybit":
		return &tm.exchangesConfig.Bybit
	case "okx":
		return &tm.exchangesConfig.OKX
	case "kucoin":
		return &tm.exchangesConfig.KuCoin
	case "phemex":
		return &tm.exchangesConfig.Phemex
	case "gate":
		return &tm.exchangesConfig.Gate
	default:
		return nil
	}
}