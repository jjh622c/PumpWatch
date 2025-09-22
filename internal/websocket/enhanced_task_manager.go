package websocket

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"PumpWatch/internal/analyzer"
	"PumpWatch/internal/buffer"
	"PumpWatch/internal/config"
	"PumpWatch/internal/logging"
	"PumpWatch/internal/models"
	"PumpWatch/internal/recovery"
	"PumpWatch/internal/storage"
	"PumpWatch/internal/symbols"
)

// EnhancedTaskManager manages multiple WorkerPools for all exchanges with intelligent scaling
type EnhancedTaskManager struct {
	ctx    context.Context
	cancel context.CancelFunc

	// Configuration
	exchangesConfig config.ExchangesConfig
	symbolsConfig   *symbols.SymbolsConfig
	storageManager  *storage.Manager
	pumpAnalyzer    *analyzer.PumpAnalyzer
	circularBuffer  *buffer.CircularTradeBuffer

	// Multi-worker connection management
	workerPools map[string]*WorkerPool // key: "exchange_markettype"
	poolsMu     sync.RWMutex

	// Intelligent error recovery system
	recoveryScheduler *recovery.ReconnectionScheduler
	logger            *logging.Logger

	// Data collection
	currentCollection *models.CollectionEvent
	collectionMu      sync.RWMutex
	collectionTimer   *time.Timer

	// Enhanced statistics tracking
	stats   EnhancedTaskManagerStats
	statsMu sync.RWMutex

	// Health monitoring
	healthTicker *time.Ticker
	running      bool
	runningMu    sync.RWMutex
}

// EnhancedTaskManagerStats holds comprehensive task manager statistics
type EnhancedTaskManagerStats struct {
	TotalPools            int       `json:"total_pools"`
	ActivePools           int       `json:"active_pools"`
	TotalWorkers          int       `json:"total_workers"`
	ActiveWorkers         int       `json:"active_workers"`
	TotalSymbols          int       `json:"total_symbols"`
	TotalMessagesReceived int64     `json:"total_messages_received"`
	MessagesPerSecond     float64   `json:"messages_per_second"`
	LastHealthCheck       time.Time `json:"last_health_check"`
	LastDataCollection    time.Time `json:"last_data_collection"`
	CollectionActive      bool      `json:"collection_active"`

	// Per-exchange statistics
	ExchangeStats map[string]ExchangeStats `json:"exchange_stats"`
}

// ExchangeStats holds statistics for a specific exchange
type ExchangeStats struct {
	Exchange      string  `json:"exchange"`
	TotalWorkers  int     `json:"total_workers"`
	ActiveWorkers int     `json:"active_workers"`
	TotalSymbols  int     `json:"total_symbols"`
	TotalMessages int64   `json:"total_messages"`
	AvgLatency    float64 `json:"avg_latency_ms"`
	ErrorRate     float64 `json:"error_rate"`
	UptimePercent float64 `json:"uptime_percent"`
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
		circularBuffer:  buffer.NewCircularTradeBuffer(taskCtx),
		workerPools:     make(map[string]*WorkerPool),
		logger:          logging.GetGlobalLogger(),
		stats: EnhancedTaskManagerStats{
			LastHealthCheck: time.Now(),
			ExchangeStats:   make(map[string]ExchangeStats),
		},
	}

	// 🔧 CRITICAL FIX: Restore CircularBuffer data from backup (하드리셋 대비)
	if err := tm.circularBuffer.LoadFromBackup(); err != nil {
		tm.logger.Warn("⚠️ CircularBuffer 백업 복원 실패 (정상 시작): %v", err)
	} else {
		tm.logger.Info("✅ CircularBuffer 백업 복원 완료 (하드리셋 복구)")
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

			// Create worker pool with config from config.yaml
			exchangeConfig := tm.getExchangeConfigForWorkerPool(exchange)
			pool, err := NewWorkerPool(tm.ctx, exchange, market, symbols, exchangeConfig)
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

	// 🔧 FIX: 이미 생성된 Worker들의 콜백도 다시 설정
	for _, worker := range pool.Workers {
		worker.OnTradeEvent = pool.OnTradeEvent
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
	// 🔇 SOMI 특화 로그 제거 (시스템 정상 작동 확인됨)

	// Update statistics
	tm.statsMu.Lock()
	tm.stats.TotalMessagesReceived++

	// Update exchange-specific stats (ensure entry exists)
	exchangeStats, exists := tm.stats.ExchangeStats[exchange]
	if !exists {
		exchangeStats = ExchangeStats{
			Exchange: exchange,
		}
	}
	exchangeStats.TotalMessages++
	tm.stats.ExchangeStats[exchange] = exchangeStats
	tm.statsMu.Unlock()

	// Always store trade event in CircularTradeBuffer for 20-minute rolling history
	exchangeKey := fmt.Sprintf("%s_%s", exchange, marketType)

	// 🔍 SOMI 호출 디버깅 (모든 호출 확인)
	if strings.Contains(strings.ToUpper(tradeEvent.Symbol), "SOMI") && (tradeEvent.Timestamp%1000 == 0) {
		tm.logger.Info("🔍 [EnhancedTM] Calling CircularBuffer.StoreTradeEvent: %s, Symbol: %s, Timestamp: %d",
			exchangeKey, tradeEvent.Symbol, tradeEvent.Timestamp)
	}

	if err := tm.circularBuffer.StoreTradeEvent(exchangeKey, tradeEvent); err != nil {
		tm.logger.Info("⚠️ Failed to store trade in circular buffer: %v", err)
	} else {
		// 🔍 SOMI 저장 디버깅 (샘플링 - 100개 중 1개만)
		if strings.Contains(strings.ToUpper(tradeEvent.Symbol), "SOMI") && (tradeEvent.Timestamp%100 == 0) {
			tm.logger.Info("💾 SOMI 데이터 CircularBuffer 저장: %s_%s, Symbol: %s, Timestamp: %d",
				exchange, marketType, tradeEvent.Symbol, tradeEvent.Timestamp)
		}
	}

	// Store trade event if collection is active
	tm.collectionMu.RLock()
	if tm.currentCollection != nil {
		tm.storeTradeEvent(exchange, marketType, &tradeEvent)
	}
	tm.collectionMu.RUnlock()
}

// storeTradeEvent stores trade event using CollectionEvent.AddTrade() with proper time filtering
func (tm *EnhancedTaskManager) storeTradeEvent(exchange, marketType string, tradeEvent *models.TradeEvent) {
	if tm.currentCollection == nil {
		return
	}

	// Set exchange and market type in trade event for proper filtering
	tradeEvent.Exchange = exchange
	tradeEvent.MarketType = marketType

	// Use CollectionEvent.AddTrade() which includes time filtering logic
	tm.currentCollection.AddTrade(*tradeEvent)
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

	// Stop circular buffer
	if tm.circularBuffer != nil {
		tm.logger.Info("🛑 Closing CircularTradeBuffer")
		if err := tm.circularBuffer.Close(); err != nil {
			tm.logger.Error("❌ Failed to close CircularTradeBuffer: %v", err)
		}
	}

	// Cancel main context
	tm.cancel()

	tm.logger.Info("✅ Enhanced Task Manager stopped")
	return nil
}

// GetCircularBuffer returns the CircularTradeBuffer instance for external access
func (tm *EnhancedTaskManager) GetCircularBuffer() *buffer.CircularTradeBuffer {
	return tm.circularBuffer
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
// 🔧 CRITICAL FIX: Use immediate CircularBuffer extraction instead of fresh collection
func (tm *EnhancedTaskManager) StartDataCollection(symbol string, triggerTime time.Time) error {
	tm.collectionMu.Lock()
	defer tm.collectionMu.Unlock()

	if tm.currentCollection != nil {
		return fmt.Errorf("data collection already active for symbol: %s", tm.currentCollection.Symbol)
	}

	detectedTime := time.Now()
	tm.logger.Info("📡 Starting IMMEDIATE data extraction for %s", symbol)
	tm.logger.Info("🕐 Trigger: %s, Detected: %s (delay: %v)",
		triggerTime.Format("15:04:05"), detectedTime.Format("15:04:05"),
		detectedTime.Sub(triggerTime).Round(time.Second))

	// 🔧 FIXED: Extract data IMMEDIATELY from CircularBuffer (no waiting)
	startTime := triggerTime.Add(-20 * time.Second)
	endTime := triggerTime.Add(20 * time.Second)

	tm.logger.Info("⏰ Extracting data range: %s ~ %s (trigger ± 20s)",
		startTime.Format("15:04:05"), endTime.Format("15:04:05"))

	// Create collection event for data storage
	collectionEvent := models.NewCollectionEvent(symbol, triggerTime)

	// Extract data from CircularBuffer for all exchanges IMMEDIATELY
	exchangeMarkets := []string{
		"binance_spot", "binance_futures",
		"bybit_spot", "bybit_futures",
		"okx_spot", "okx_futures",
		"kucoin_spot", "kucoin_futures",
		"gate_spot", "gate_futures",
		"phemex_spot", "phemex_futures",
	}

	totalExtracted := 0
	for _, exchangeKey := range exchangeMarkets {
		tm.logger.Info("🔍 Extracting from %s...", exchangeKey)

		if trades, err := tm.circularBuffer.GetTradeEvents(exchangeKey, startTime, endTime); err == nil {
			tm.logger.Info("🔍 %s returned %d total trades", exchangeKey, len(trades))

			extractedCount := 0
			for _, trade := range trades {
				if collectionEvent.IsTargetSymbol(trade.Symbol) {
					collectionEvent.AddTradeFromBuffer(exchangeKey, trade)
					extractedCount++
				}
			}

			tm.logger.Info("📊 %s: %d target trades extracted", exchangeKey, extractedCount)
			totalExtracted += extractedCount
		} else {
			tm.logger.Warn("⚠️ %s extraction failed: %v", exchangeKey, err)
		}
	}

	tm.logger.Info("📊 Total extracted: %d trades from CircularBuffer", totalExtracted)

	// Save data immediately (no waiting for collection completion)
	go func() {
		if err := tm.storageManager.StoreCollectionEvent(collectionEvent); err != nil {
			tm.logger.Error("💾 Storage failed: %v", err)
		} else {
			tm.logger.Info("✅ Data saved successfully for %s", symbol)
		}
	}()

	tm.statsMu.Lock()
	tm.stats.LastDataCollection = time.Now()
	tm.stats.CollectionActive = false // Immediate completion
	tm.statsMu.Unlock()

	return nil
}

// scheduleCollectionCompletion is no longer needed with immediate extraction
// Kept for interface compatibility but does nothing
func (tm *EnhancedTaskManager) scheduleCollectionCompletion(endTime time.Time) {
	// 🔧 DEPRECATED: No longer needed with immediate CircularBuffer extraction
	tm.logger.Info("⚠️ scheduleCollectionCompletion called but not needed (immediate extraction used)")
}

// completeDataCollection is no longer needed with immediate extraction
// Kept for interface compatibility but does minimal cleanup
func (tm *EnhancedTaskManager) completeDataCollection() {
	tm.collectionMu.Lock()
	defer tm.collectionMu.Unlock()

	// 🔧 DEPRECATED: Data collection now happens immediately in StartDataCollection
	tm.logger.Info("⚠️ completeDataCollection called but not needed (immediate extraction used)")

	// Clean up any stale state
	tm.currentCollection = nil

	tm.statsMu.Lock()
	tm.stats.CollectionActive = false
	tm.statsMu.Unlock()

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

	// 🔧 DEADLOCK FIX: Collect pool data first, then update stats atomically
	type poolData struct {
		poolID       string
		exchange     string
		activeWorkers int
		totalWorkers int
		totalMessages int64
	}

	var poolsData []poolData

	tm.poolsMu.RLock()
	for poolID, pool := range tm.workerPools {
		poolStats := pool.GetStats()
		poolsData = append(poolsData, poolData{
			poolID:       poolID,
			exchange:     pool.Exchange,
			activeWorkers: poolStats["active_workers"].(int),
			totalWorkers: pool.TotalWorkers,
			totalMessages: poolStats["total_messages"].(int64),
		})
	}
	tm.poolsMu.RUnlock()

	// Now update stats atomically without holding pools lock
	tm.statsMu.Lock()
	for _, data := range poolsData {
		if data.activeWorkers > 0 {
			tm.stats.ActivePools++
		}
		tm.stats.ActiveWorkers += data.activeWorkers

		// Update exchange stats
		exchangeStats := tm.stats.ExchangeStats[data.exchange]
		exchangeStats.Exchange = data.exchange
		exchangeStats.ActiveWorkers += data.activeWorkers
		exchangeStats.TotalMessages = data.totalMessages
		tm.stats.ExchangeStats[data.exchange] = exchangeStats

		tm.logger.Debug("🏭 Pool %s: %d/%d workers active, %d messages",
			data.poolID, data.activeWorkers, data.totalWorkers, data.totalMessages)
	}
	tm.statsMu.Unlock()

	// 거래소별 상태 요약 생성
	var exchangeSummary []string
	exchangeAbbrev := map[string]string{
		"binance": "BN", "bybit": "BY", "okx": "OKX",
		"kucoin": "KC", "phemex": "PH", "gate": "GT",
	}

	tm.poolsMu.RLock()
	exchangeWorkers := make(map[string][2]int) // [spot_workers, futures_workers]

	for _, pool := range tm.workerPools {
		if _, exists := exchangeWorkers[pool.Exchange]; !exists {
			exchangeWorkers[pool.Exchange] = [2]int{0, 0}
		}
		workers := exchangeWorkers[pool.Exchange]
		if pool.MarketType == "spot" {
			workers[0] = pool.GetStats()["active_workers"].(int)
		} else {
			workers[1] = pool.GetStats()["active_workers"].(int)
		}
		exchangeWorkers[pool.Exchange] = workers
	}
	tm.poolsMu.RUnlock()

	// 거래소별 요약 생성 (spot+futures 형태)
	for _, exchange := range []string{"binance", "bybit", "okx", "kucoin", "phemex", "gate"} {
		abbrev := exchangeAbbrev[exchange]
		workers, exists := exchangeWorkers[exchange]
		if !exists || (workers[0] == 0 && workers[1] == 0) {
			exchangeSummary = append(exchangeSummary, fmt.Sprintf("%s(0+0❌)", abbrev))
		} else {
			exchangeSummary = append(exchangeSummary, fmt.Sprintf("%s(%d+%d✅)", abbrev, workers[0], workers[1]))
		}
	}

	// 메시지 수 포맷팅 (K, M 단위)
	msgCount := tm.stats.TotalMessagesReceived
	var msgStr string
	if msgCount >= 1000000 {
		msgStr = fmt.Sprintf("%.1fM", float64(msgCount)/1000000)
	} else if msgCount >= 1000 {
		msgStr = fmt.Sprintf("%.1fK", float64(msgCount)/1000)
	} else {
		msgStr = fmt.Sprintf("%d", msgCount)
	}

	tm.logger.Info("💗 Health: %s | %d/%d workers, %s msgs",
		strings.Join(exchangeSummary, ", "), tm.stats.ActiveWorkers, tm.stats.TotalWorkers, msgStr)
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

// getExchangeConfigForWorkerPool converts config.ExchangeConfig to ExchangeWorkerConfig
func (tm *EnhancedTaskManager) getExchangeConfigForWorkerPool(exchange string) ExchangeWorkerConfig {
	var exchangeConfig config.ExchangeConfig

	switch exchange {
	case "binance":
		exchangeConfig = tm.exchangesConfig.Binance
	case "bybit":
		exchangeConfig = tm.exchangesConfig.Bybit
	case "okx":
		exchangeConfig = tm.exchangesConfig.OKX
	case "kucoin":
		exchangeConfig = tm.exchangesConfig.KuCoin
	case "gate":
		exchangeConfig = tm.exchangesConfig.Gate
	case "phemex":
		exchangeConfig = tm.exchangesConfig.Phemex
	default:
		// Fallback to hardcoded values for unknown exchanges
		tm.logger.Warn("⚠️ Unknown exchange %s, using default config", exchange)
		return getExchangeWorkerConfig(exchange)
	}

	// Convert config.ExchangeConfig to ExchangeWorkerConfig
	workerConfig := ExchangeWorkerConfig{
		MaxSymbolsPerConnection: exchangeConfig.MaxSymbolsPerConnection,
		MaxConnections:          10, // Conservative default
		PingInterval:            20 * time.Second,
		ConnectionTimeout:       exchangeConfig.ConnectionTimeout,
		RateLimit:               5, // Conservative default
		RateLimitInterval:       1 * time.Second,
	}

	// 하드코딩 오버라이드 제거 - config.yaml 설정을 우선 사용
	// 추가 설정이 필요한 경우 config.yaml에서 설정하세요
	// 기본값은 이미 설정되어 있으므로 별도의 하드코딩 조정 불필요
	tm.logger.Debug("%s 거래소: config.yaml 설정 사용 (MaxConnections=%d, ConnectionTimeout=%v, RateLimit=%d)",
		exchange, workerConfig.MaxConnections, workerConfig.ConnectionTimeout, workerConfig.RateLimit)

	tm.logger.Info("📋 Using config for %s: MaxSymbols=%d, MaxConnections=%d",
		exchange, workerConfig.MaxSymbolsPerConnection, workerConfig.MaxConnections)

	return workerConfig
}
