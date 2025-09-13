package websocket

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"PumpWatch/internal/analyzer"
	"PumpWatch/internal/config"
	"PumpWatch/internal/logging"
	"PumpWatch/internal/models"
	"PumpWatch/internal/recovery"
	"PumpWatch/internal/storage"
	"PumpWatch/internal/symbols"
	"PumpWatch/internal/websocket/connectors"
)

// TaskManager manages WebSocket connections for all exchanges and markets
// Enhanced with intelligent error recovery system
type TaskManager struct {
	ctx             context.Context
	cancel          context.CancelFunc
	
	// Configuration
	exchangesConfig config.ExchangesConfig
	symbolsConfig   *symbols.SymbolsConfig
	storageManager  *storage.Manager
	pumpAnalyzer    *analyzer.PumpAnalyzer
	
	// Connection management - 12 independent connections
	connections     map[string]*ConnectionManager
	connectionsMu   sync.RWMutex
	
	// Intelligent error recovery system
	recoveryScheduler *recovery.ReconnectionScheduler
	logger           *logging.Logger
	
	// Data collection
	currentCollection *models.CollectionEvent
	collectionMu      sync.RWMutex
	collectionTimer   *time.Timer
	
	// Status tracking
	stats           TaskManagerStats
	statsMu         sync.RWMutex
	
	// Health monitoring
	healthTicker    *time.Ticker
	running         bool
	runningMu       sync.RWMutex
}

// TaskManagerStats holds task manager statistics
type TaskManagerStats struct {
	ActiveConnections       int       `json:"active_connections"`
	ReconnectingConnections int       `json:"reconnecting_connections"`
	FailedConnections       int       `json:"failed_connections"`
	TotalMessagesReceived   int64     `json:"total_messages_received"`
	LastHealthCheck         time.Time `json:"last_health_check"`
	LastDataCollection      time.Time `json:"last_data_collection"`
	CollectionActive        bool      `json:"collection_active"`
}

// ConnectionManager manages individual exchange-market connections
type ConnectionManager struct {
	Exchange        string
	MarketType      string
	Connector       connectors.WebSocketConnector
	Symbols         []string
	Status          ConnectionStatus
	LastError       error
	RetryCount      int
	MaxRetries      int
	LastReconnect   time.Time
	LastMessage     time.Time
	MessageCount    int64
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.RWMutex
}

// ConnectionStatus represents connection state
type ConnectionStatus int

const (
	StatusDisconnected ConnectionStatus = iota
	StatusConnecting
	StatusConnected
	StatusReconnecting
	StatusFailed
)

func (cs ConnectionStatus) String() string {
	switch cs {
	case StatusDisconnected:
		return "disconnected"
	case StatusConnecting:
		return "connecting"
	case StatusConnected:
		return "connected"
	case StatusReconnecting:
		return "reconnecting"
	case StatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// NewTaskManager creates a new WebSocket task manager with intelligent error recovery
func NewTaskManager(ctx context.Context, exchangesConfig config.ExchangesConfig, symbolsConfig *symbols.SymbolsConfig, storageManager *storage.Manager) (*TaskManager, error) {
	taskCtx, cancel := context.WithCancel(ctx)
	
	tm := &TaskManager{
		ctx:             taskCtx,
		cancel:          cancel,
		exchangesConfig: exchangesConfig,
		symbolsConfig:   symbolsConfig,
		storageManager:  storageManager,
		pumpAnalyzer:    analyzer.NewPumpAnalyzer(),
		connections:     make(map[string]*ConnectionManager),
		logger:          logging.GetGlobalLogger(),
		stats: TaskManagerStats{
			LastHealthCheck: time.Now(),
		},
	}
	
	// Initialize intelligent error recovery system
	tm.recoveryScheduler = recovery.NewReconnectionScheduler(taskCtx)
	
	// Initialize all 12 connection managers
	if err := tm.initializeConnections(); err != nil {
		return nil, fmt.Errorf("failed to initialize connections: %w", err)
	}
	
	return tm, nil
}

// initializeConnections creates all 12 connection managers (6 exchanges × 2 markets)
func (tm *TaskManager) initializeConnections() error {
	exchanges := []string{"binance", "bybit", "okx", "kucoin", "phemex", "gate"}
	markets := []string{"spot", "futures"}
	
	for _, exchange := range exchanges {
		exchangeConfig := tm.getExchangeConfig(exchange)
		if exchangeConfig == nil {
			log.Printf("⚠️ Exchange configuration not found: %s", exchange)
			continue
		}
		
		for _, market := range markets {
			connID := fmt.Sprintf("%s_%s", exchange, market)
			
			// Create connector based on exchange
			connector, err := tm.createConnector(exchange, market, *exchangeConfig)
			if err != nil {
				log.Printf("❌ Failed to create connector for %s: %v", connID, err)
				continue
			}
			
			// Set up error callback for intelligent recovery
			tm.setConnectorCallbacks(connector, exchange, market)
			
			// Get symbols for this exchange-market combination
			symbols := tm.getSymbolsForExchangeMarket(exchange, market)
			
			// Create connection manager
			connCtx, connCancel := context.WithCancel(tm.ctx)
			cm := &ConnectionManager{
				Exchange:     exchange,
				MarketType:   market,
				Connector:    connector,
				Symbols:      symbols,
				Status:       StatusDisconnected,
				MaxRetries:   exchangeConfig.MaxRetries,
				LastMessage:  time.Now(),
				ctx:          connCtx,
				cancel:       connCancel,
			}
			
			tm.connections[connID] = cm
			
			// Register with intelligent recovery system
			tm.recoveryScheduler.RegisterExchange(exchange, market, tm.createReconnectCallback(connID))
			
			log.Printf("🔗 Initialized connection: %s (%d symbols)", connID, len(symbols))
		}
	}
	
	log.Printf("✅ Initialized %d connection managers", len(tm.connections))
	return nil
}

// createConnector creates appropriate connector for exchange and market type
func (tm *TaskManager) createConnector(exchange, market string, exchangeConfig config.ExchangeConfig) (connectors.WebSocketConnector, error) {
	maxSymbols := exchangeConfig.MaxSymbolsPerConnection
	if maxSymbols == 0 {
		maxSymbols = 100 // Default value
	}
	
	switch exchange {
	case "binance":
		return connectors.NewBinanceConnector(market, maxSymbols), nil
	case "bybit":
		return connectors.NewBybitConnector(market, maxSymbols), nil
	case "okx":
		return connectors.NewOKXConnector(market, maxSymbols), nil
	case "kucoin":
		return connectors.NewKuCoinConnector(market, maxSymbols), nil
	case "phemex":
		return connectors.NewPhemexConnector(market, maxSymbols), nil
	case "gate":
		return connectors.NewGateConnector(market, maxSymbols), nil
	default:
		return nil, fmt.Errorf("unsupported exchange: %s", exchange)
	}
}

// getSymbolsForExchangeMarket returns filtered symbols for specific exchange-market
func (tm *TaskManager) getSymbolsForExchangeMarket(exchange, market string) []string {
	subscriptionKey := fmt.Sprintf("%s_%s", exchange, market)
	
	if symbols, exists := tm.symbolsConfig.SubscriptionLists[subscriptionKey]; exists {
		log.Printf("📊 %s symbols: %d", subscriptionKey, len(symbols))
		return symbols
	}
	
	// Fallback to default symbols if subscription list not found
	defaultSymbols := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
	log.Printf("⚠️ Using default symbols for %s: %v", subscriptionKey, defaultSymbols)
	return defaultSymbols
}

// Start begins the WebSocket Task Manager
func (tm *TaskManager) Start() error {
	tm.runningMu.Lock()
	defer tm.runningMu.Unlock()
	
	if tm.running {
		return fmt.Errorf("task manager is already running")
	}
	
	tm.running = true
	
	// Start intelligent error recovery system
	if err := tm.recoveryScheduler.Start(); err != nil {
		return fmt.Errorf("failed to start recovery scheduler: %w", err)
	}
	
	// Start health monitoring (45초 간격으로 ping과 겹치지 않게)
	tm.healthTicker = time.NewTicker(45 * time.Second)
	go tm.healthCheckWorker()
	
	// Start all connections
	tm.connectionsMu.RLock()
	for connID, cm := range tm.connections {
		go tm.startConnection(connID, cm)
	}
	tm.connectionsMu.RUnlock()
	
	tm.logger.Info("🚀 WebSocket Task Manager started with %d connections + intelligent recovery", len(tm.connections))
	return nil
}

// Stop gracefully stops the WebSocket Task Manager
func (tm *TaskManager) Stop() error {
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
	
	// Stop all connections
	tm.connectionsMu.Lock()
	for connID, cm := range tm.connections {
		log.Printf("🛑 Stopping connection: %s", connID)
		cm.cancel()
		if cm.Connector != nil {
			cm.Connector.Disconnect()
		}
	}
	tm.connectionsMu.Unlock()
	
	// Cancel main context
	tm.cancel()
	
	log.Printf("✅ WebSocket Task Manager stopped")
	return nil
}

// startConnection starts individual connection with automatic reconnection
func (tm *TaskManager) startConnection(connID string, cm *ConnectionManager) {
	for {
		select {
		case <-cm.ctx.Done():
			log.Printf("🔌 Connection context cancelled: %s", connID)
			return
		default:
		}
		
		// Update status
		cm.mu.Lock()
		cm.Status = StatusConnecting
		cm.mu.Unlock()
		
		log.Printf("🔌 Connecting to %s...", connID)
		
		// Attempt connection
		err := cm.Connector.Connect(cm.ctx, cm.Symbols)
		if err != nil {
			log.Printf("❌ Connection failed for %s: %v", connID, err)
			tm.handleConnectionError(connID, cm, err)
			continue
		}
		
		// Connection successful
		cm.mu.Lock()
		cm.Status = StatusConnected
		cm.RetryCount = 0
		cm.LastMessage = time.Now()
		cm.mu.Unlock()
		
		// Notify recovery system of successful connection
		tm.recoveryScheduler.MarkHealthy(cm.Exchange, cm.MarketType)
		
		logging.LogWebSocketEvent(cm.Exchange, cm.MarketType, "Connected", "Connection established successfully")
		
		// Start message processing
		tm.processMessages(connID, cm)
		
		// If we reach here, the message processing has ended (connection lost)
		
		// Connection lost, prepare for reconnection
		cm.mu.Lock()
		cm.Status = StatusReconnecting
		cm.mu.Unlock()
		
		log.Printf("🔄 Connection lost, will reconnect: %s", connID)
		
		// Wait before reconnection
		select {
		case <-cm.ctx.Done():
			return
		case <-time.After(time.Duration(cm.RetryCount+1) * time.Second):
		}
	}
}

// processMessages processes incoming WebSocket messages using the existing connector interface
func (tm *TaskManager) processMessages(connID string, cm *ConnectionManager) {
	// Create channel for receiving trade events from connector
	messageChan := make(chan models.TradeEvent, 1000)
	
	// Start the connector's message loop
	go func() {
		err := cm.Connector.StartMessageLoop(cm.ctx, messageChan)
		if err != nil {
			tm.logger.Error("Message loop error for %s: %v", connID, err)
			// Report error to recovery system
			tm.recoveryScheduler.HandleError(cm.Exchange, cm.MarketType, err)
		}
	}()
	
	// Process messages from the channel
	for {
		select {
		case <-cm.ctx.Done():
			return
		case tradeEvent := <-messageChan:
			// Update stats
			cm.mu.Lock()
			cm.MessageCount++
			cm.LastMessage = time.Now()
			cm.mu.Unlock()
			
			tm.statsMu.Lock()
			tm.stats.TotalMessagesReceived++
			tm.statsMu.Unlock()
			
			// Store message if collection is active
			tm.collectionMu.RLock()
			if tm.currentCollection != nil {
				tm.storeTradeEvent(connID, &tradeEvent)
			}
			tm.collectionMu.RUnlock()
		}
	}
}

// storeTradeEvent stores trade event in appropriate CollectionEvent slice
func (tm *TaskManager) storeTradeEvent(connID string, tradeEvent *models.TradeEvent) {
	if tm.currentCollection == nil {
		return
	}
	
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

// handleConnectionError handles connection errors using intelligent recovery system
func (tm *TaskManager) handleConnectionError(connID string, cm *ConnectionManager, err error) {
	cm.mu.Lock()
	cm.LastError = err
	cm.Status = StatusReconnecting
	cm.mu.Unlock()
	
	// Use intelligent error recovery system instead of simple retry logic
	tm.recoveryScheduler.HandleError(cm.Exchange, cm.MarketType, err)
	
	tm.logger.Warn("🔄 Connection error handled by recovery system: %s - %v", connID, err)
	
	// Update stats
	tm.statsMu.Lock()
	tm.stats.ReconnectingConnections++
	tm.statsMu.Unlock()
}

// StartDataCollection starts data collection for a listing event
func (tm *TaskManager) StartDataCollection(symbol string, triggerTime time.Time) error {
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
	
	log.Printf("📡 Starting data collection for %s", symbol)
	log.Printf("⏰ Collection window: %s to %s (40 seconds)", 
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
func (tm *TaskManager) scheduleCollectionCompletion(endTime time.Time) {
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
func (tm *TaskManager) completeDataCollection() {
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
	
	log.Printf("✅ Data collection completed for %s", collectionEvent.Symbol)
	log.Printf("📊 Total trades collected: %d", collectionEvent.GetTotalTradeCount())
	
	// Store collection event (raw data)
	if err := tm.storageManager.StoreCollectionEvent(collectionEvent); err != nil {
		log.Printf("❌ Failed to store collection event: %v", err)
	}
	
	// Analyze for pump events
	if collectionEvent.GetTotalTradeCount() > 0 {
		log.Printf("🔍 Starting pump analysis for %s...", collectionEvent.Symbol)
		
		pumpAnalysis, err := tm.pumpAnalyzer.AnalyzePumps(collectionEvent)
		if err != nil {
			log.Printf("❌ Pump analysis failed: %v", err)
		} else if len(pumpAnalysis.PumpEvents) > 0 {
			// Store pump analysis results
			if err := tm.storageManager.StorePumpAnalysis(collectionEvent.Symbol, collectionEvent.TriggerTime, pumpAnalysis); err != nil {
				log.Printf("❌ Failed to store pump analysis: %v", err)
			} else {
				log.Printf("💾 Stored pump analysis: %d pump events, max change: %.2f%%", 
					len(pumpAnalysis.PumpEvents), pumpAnalysis.Summary.MaxPriceChange)
			}
		} else {
			log.Printf("📈 No significant pump events detected for %s", collectionEvent.Symbol)
		}
	}
	
	// Clear collection timer
	if tm.collectionTimer != nil {
		tm.collectionTimer.Stop()
		tm.collectionTimer = nil
	}
}

// healthCheckWorker performs periodic health checks
func (tm *TaskManager) healthCheckWorker() {
	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-tm.healthTicker.C:
			tm.performHealthCheck()
		}
	}
}

// performHealthCheck checks the health of all connections
func (tm *TaskManager) performHealthCheck() {
	tm.statsMu.Lock()
	tm.stats.LastHealthCheck = time.Now()
	tm.stats.ActiveConnections = 0
	tm.stats.ReconnectingConnections = 0
	tm.stats.FailedConnections = 0
	tm.statsMu.Unlock()
	
	tm.connectionsMu.RLock()
	for connID, cm := range tm.connections {
		cm.mu.RLock()
		status := cm.Status
		lastMessage := cm.LastMessage
		cm.mu.RUnlock()
		
		// Check if connection is stale (no messages for 90 seconds)
		if status == StatusConnected && time.Since(lastMessage) > 90*time.Second {
			log.Printf("⚠️ Stale connection detected: %s (last message: %v ago)", 
				connID, time.Since(lastMessage))
			
			// Force reconnection
			cm.cancel()
		}
		
		// Update stats
		tm.statsMu.Lock()
		switch status {
		case StatusConnected:
			tm.stats.ActiveConnections++
		case StatusReconnecting:
			tm.stats.ReconnectingConnections++
		case StatusFailed:
			tm.stats.FailedConnections++
		}
		tm.statsMu.Unlock()
	}
	tm.connectionsMu.RUnlock()
	
	log.Printf("💗 Health check - Active: %d, Reconnecting: %d, Failed: %d", 
		tm.stats.ActiveConnections, tm.stats.ReconnectingConnections, tm.stats.FailedConnections)
}

// GetStats returns current task manager statistics
func (tm *TaskManager) GetStats() TaskManagerStats {
	tm.statsMu.RLock()
	defer tm.statsMu.RUnlock()
	return tm.stats
}

// GetConnectionStatuses returns status of all connections
func (tm *TaskManager) GetConnectionStatuses() map[string]ConnectionStatus {
	tm.connectionsMu.RLock()
	defer tm.connectionsMu.RUnlock()
	
	statuses := make(map[string]ConnectionStatus)
	for connID, cm := range tm.connections {
		cm.mu.RLock()
		statuses[connID] = cm.Status
		cm.mu.RUnlock()
	}
	
	return statuses
}

// getExchangeConfig returns the configuration for a specific exchange
func (tm *TaskManager) getExchangeConfig(exchange string) *config.ExchangeConfig {
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

// createReconnectCallback creates a reconnection callback for the recovery system
func (tm *TaskManager) createReconnectCallback(connID string) func(string, string) error {
	return func(exchange, marketType string) error {
		tm.connectionsMu.RLock()
		cm, exists := tm.connections[connID]
		tm.connectionsMu.RUnlock()
		
		if !exists {
			return fmt.Errorf("connection manager not found for %s", connID)
		}
		
		tm.logger.Info("🔄 Attempting intelligent reconnection for %s", connID)
		
		// Disconnect existing connection if any
		if cm.Connector != nil {
			cm.Connector.Disconnect()
		}
		
		// Create new context for this connection attempt
		connCtx, connCancel := context.WithCancel(tm.ctx)
		cm.mu.Lock()
		// Cancel old context
		if cm.cancel != nil {
			cm.cancel()
		}
		cm.ctx = connCtx
		cm.cancel = connCancel
		cm.Status = StatusConnecting
		cm.mu.Unlock()
		
		// Attempt reconnection
		err := cm.Connector.Connect(cm.ctx, cm.Symbols)
		if err != nil {
			cm.mu.Lock()
			cm.Status = StatusReconnecting
			cm.LastError = err
			cm.mu.Unlock()
			return fmt.Errorf("reconnection failed: %w", err)
		}
		
		// Success
		cm.mu.Lock()
		cm.Status = StatusConnected
		cm.RetryCount = 0
		cm.LastError = nil
		cm.LastMessage = time.Now()
		cm.mu.Unlock()
		
		// Initialize logger for connector
		if baseConnector, ok := cm.Connector.(interface{ InitLogger() }); ok {
			baseConnector.InitLogger()
		}
		
		// Restart message processing
		go tm.processMessages(connID, cm)
		
		tm.logger.Info("✅ Intelligent reconnection successful for %s", connID)
		return nil
	}
}

// setConnectorCallbacks sets up OnError callback for intelligent recovery
func (tm *TaskManager) setConnectorCallbacks(connector connectors.WebSocketConnector, exchange, marketType string) {
	// Convert to base connector to access callbacks
	if baseConnector, ok := connector.(interface{
		SetOnError(func(error))
	}); ok {
		baseConnector.SetOnError(func(err error) {
			// Report error to intelligent recovery system
			tm.recoveryScheduler.HandleError(exchange, marketType, err)
		})
	}
}

// GetRecoveryStats returns intelligent recovery system statistics
func (tm *TaskManager) GetRecoveryStats() map[string]interface{} {
	if tm.recoveryScheduler == nil {
		return map[string]interface{}{"error": "recovery scheduler not initialized"}
	}
	return tm.recoveryScheduler.GetStats()
}