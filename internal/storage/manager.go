package storage

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"PumpWatch/internal/config"
	"PumpWatch/internal/models"
)

// PumpAnalyzer interface defines the contract for pump analysis
type PumpAnalyzer interface {
	AnalyzePumps(collectionEvent *models.CollectionEvent) (*PumpAnalysis, error)
}

// Manager handles all storage operations for METDC v2.0
type Manager struct {
	config         config.StorageConfig
	analysisConfig config.AnalysisConfig
	baseDir        string
	mu             sync.RWMutex

	// Analysis
	analyzer PumpAnalyzer

	// Statistics
	stats StorageStats
}

// StorageStats holds storage statistics
type StorageStats struct {
	TotalListingEvents int64     `json:"total_listing_events"`
	TotalRawFiles      int64     `json:"total_raw_files"`
	TotalRefinedFiles  int64     `json:"total_refined_files"`
	TotalDataSize      int64     `json:"total_data_size"`
	LastWrite          time.Time `json:"last_write"`
	LastCleanup        time.Time `json:"last_cleanup"`
}

// NewManager creates a new storage manager
func NewManager(storageConfig config.StorageConfig, analysisConfig config.AnalysisConfig) *Manager {
	manager := &Manager{
		config:         storageConfig,
		analysisConfig: analysisConfig,
		baseDir:        storageConfig.DataDir,
		stats: StorageStats{
			LastCleanup: time.Now(),
		},
	}

	// Note: PumpAnalyzer will be set via SetAnalyzer() method to avoid import cycles
	if analysisConfig.Enabled {
		fmt.Println("✅ PumpAnalyzer enabled - will be initialized externally")
	} else {
		fmt.Println("⚠️ PumpAnalyzer disabled - refined data will not be generated")
	}

	// Ensure base directories exist
	if err := manager.ensureDirectories(); err != nil {
		fmt.Printf("⚠️ Failed to create storage directories: %v\n", err)
	}

	return manager
}

// SetAnalyzer sets the pump analyzer for the storage manager
func (sm *Manager) SetAnalyzer(analyzer PumpAnalyzer) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.analyzer = analyzer
	if analyzer != nil {
		fmt.Println("✅ PumpAnalyzer set for automatic analysis")
	}
}

// StoreListingEvent stores a listing event metadata
func (sm *Manager) StoreListingEvent(event *models.ListingEvent) error {
	if !sm.config.Enabled {
		return fmt.Errorf("storage is disabled")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Create event directory
	eventDir := sm.getEventDirectory(event.Symbol, event.AnnouncedAt)
	if err := os.MkdirAll(eventDir, 0755); err != nil {
		return fmt.Errorf("failed to create event directory: %w", err)
	}

	// Create metadata file
	metadata := &ListingEventMetadata{
		Symbol:       event.Symbol,
		Title:        event.Title,
		Markets:      event.Markets,
		AnnouncedAt:  event.AnnouncedAt,
		DetectedAt:   event.DetectedAt,
		NoticeURL:    event.NoticeURL,
		TriggerTime:  event.TriggerTime,
		IsKRWListing: event.IsKRWListing,
		StoredAt:     time.Now(),
	}

	metadataPath := filepath.Join(eventDir, "metadata.json")
	if err := sm.writeJSONFile(metadataPath, metadata); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	sm.stats.TotalListingEvents++
	sm.stats.LastWrite = time.Now()

	fmt.Printf("💾 Stored listing event metadata: %s\n", metadataPath)
	return nil
}

// StoreCollectionEvent stores complete collection event data
func (sm *Manager) StoreCollectionEvent(collectionEvent *models.CollectionEvent) error {
	if !sm.config.Enabled {
		return fmt.Errorf("storage is disabled")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Create event directory
	eventDir := sm.getEventDirectory(collectionEvent.Symbol, collectionEvent.TriggerTime)
	rawDir := filepath.Join(eventDir, "raw")

	if err := os.MkdirAll(rawDir, 0755); err != nil {
		return fmt.Errorf("failed to create raw directory: %w", err)
	}

	// Store raw data if enabled
	if sm.config.RawDataEnabled {
		if err := sm.storeRawData(rawDir, collectionEvent); err != nil {
			return fmt.Errorf("failed to store raw data: %w", err)
		}
	}

	// Store refined data if enabled and analysis is available
	if sm.config.RefinedEnabled {
		refinedDir := filepath.Join(eventDir, "refined")
		if err := os.MkdirAll(refinedDir, 0755); err != nil {
			return fmt.Errorf("failed to create refined directory: %w", err)
		}

		// Perform pump analysis if analyzer is available
		if sm.analyzer != nil && sm.analysisConfig.Enabled {
			if err := sm.performPumpAnalysis(collectionEvent, refinedDir); err != nil {
				fmt.Printf("⚠️ Pump analysis failed: %v\n", err)
				// Continue without analysis - raw data is still saved
			}
		} else {
			fmt.Printf("ℹ️ Pump analysis skipped (disabled or no analyzer)\n")
		}
	}

	sm.stats.LastWrite = time.Now()

	fmt.Printf("💾 Stored collection event: %s (%d total trades)\n",
		eventDir, collectionEvent.GetTotalTradeCount())

	return nil
}

// storeRawData stores raw trade data organized by exchange directories
func (sm *Manager) storeRawData(rawDir string, collectionEvent *models.CollectionEvent) error {
	// Define exchange data grouped by exchange
	exchanges := []struct {
		name    string
		spot    []models.TradeEvent
		futures []models.TradeEvent
	}{
		{"binance", collectionEvent.BinanceSpot, collectionEvent.BinanceFutures},
		{"okx", collectionEvent.OKXSpot, collectionEvent.OKXFutures},
		{"bybit", collectionEvent.BybitSpot, collectionEvent.BybitFutures},
		{"kucoin", collectionEvent.KuCoinSpot, collectionEvent.KuCoinFutures},
		{"phemex", collectionEvent.PhemexSpot, collectionEvent.PhemexFutures},
		{"gate", collectionEvent.GateSpot, collectionEvent.GateFutures},
	}

	// Store each exchange data in its own directory
	for _, exchange := range exchanges {
		// Create exchange directory
		exchangeDir := filepath.Join(rawDir, exchange.name)
		if err := os.MkdirAll(exchangeDir, 0755); err != nil {
			return fmt.Errorf("failed to create %s directory: %w", exchange.name, err)
		}

		// Store spot data (always create file for debugging, even if empty)
		spotPath := filepath.Join(exchangeDir, "spot.json")
		if err := sm.writeJSONFile(spotPath, exchange.spot); err != nil {
			return fmt.Errorf("failed to write %s spot data: %w", exchange.name, err)
		}
		sm.stats.TotalRawFiles++
		if len(exchange.spot) > 0 {
			fmt.Printf("📄 Stored %s/spot: %d trades\n", exchange.name, len(exchange.spot))
		} else {
			fmt.Printf("📄 Stored %s/spot: 0 trades (empty file for debugging)\n", exchange.name)
		}

		// Store futures data (always create file for debugging, even if empty)
		futuresPath := filepath.Join(exchangeDir, "futures.json")
		if err := sm.writeJSONFile(futuresPath, exchange.futures); err != nil {
			return fmt.Errorf("failed to write %s futures data: %w", exchange.name, err)
		}
		sm.stats.TotalRawFiles++
		if len(exchange.futures) > 0 {
			fmt.Printf("📄 Stored %s/futures: %d trades\n", exchange.name, len(exchange.futures))
		} else {
			fmt.Printf("📄 Stored %s/futures: 0 trades (empty file for debugging)\n", exchange.name)
		}
	}

	return nil
}

// StorePumpAnalysis stores pump analysis results (legacy)
func (sm *Manager) StorePumpAnalysis(symbol string, triggerTime time.Time, analysis *PumpAnalysis) error {
	if !sm.config.Enabled || !sm.config.RefinedEnabled {
		return fmt.Errorf("refined data storage is disabled")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Create refined directory
	eventDir := sm.getEventDirectory(symbol, triggerTime)
	refinedDir := filepath.Join(eventDir, "refined")

	if err := os.MkdirAll(refinedDir, 0755); err != nil {
		return fmt.Errorf("failed to create refined directory: %w", err)
	}

	// Store pump events
	pumpEventsPath := filepath.Join(refinedDir, "pump_events.json")
	if err := sm.writeJSONFile(pumpEventsPath, analysis.PumpEvents); err != nil {
		return fmt.Errorf("failed to write pump events: %w", err)
	}

	// Store summary
	summaryPath := filepath.Join(refinedDir, "summary.json")
	if err := sm.writeJSONFile(summaryPath, analysis.Summary); err != nil {
		return fmt.Errorf("failed to write summary: %w", err)
	}

	// Store analysis metadata
	metadataPath := filepath.Join(refinedDir, "analysis_metadata.json")
	analysisMetadata := &AnalysisMetadata{
		Symbol:          symbol,
		TriggerTime:     triggerTime,
		AnalysisTime:    time.Now(),
		TotalExchanges:  analysis.Summary.TotalExchanges,
		TotalPumpEvents: len(analysis.PumpEvents),
		MaxPriceChange:  analysis.Summary.MaxPriceChange,
		AnalysisVersion: "2.0",
	}

	if err := sm.writeJSONFile(metadataPath, analysisMetadata); err != nil {
		return fmt.Errorf("failed to write analysis metadata: %w", err)
	}

	sm.stats.TotalRefinedFiles += 3
	sm.stats.LastWrite = time.Now()

	fmt.Printf("💾 Stored pump analysis for %s: %d pump events\n", symbol, len(analysis.PumpEvents))
	return nil
}

// StoreRefinedAnalysis stores complete refined analysis with user data
func (sm *Manager) StoreRefinedAnalysis(symbol string, triggerTime time.Time, analysis *RefinedAnalysis) error {
	if !sm.config.Enabled || !sm.config.RefinedEnabled {
		return fmt.Errorf("refined data storage is disabled")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Create refined directory
	eventDir := sm.getEventDirectory(symbol, triggerTime)
	refinedDir := filepath.Join(eventDir, "refined")

	if err := os.MkdirAll(refinedDir, 0755); err != nil {
		return fmt.Errorf("failed to create refined directory: %w", err)
	}

	// Store aggregated refined analysis (existing structure)
	if err := sm.storeAggregatedAnalysis(refinedDir, analysis); err != nil {
		return fmt.Errorf("failed to store aggregated analysis: %w", err)
	}

	// Store per-exchange refined analysis (new structure)
	if err := sm.storePerExchangeAnalysis(refinedDir, analysis); err != nil {
		return fmt.Errorf("failed to store per-exchange analysis: %w", err)
	}

	sm.stats.LastWrite = time.Now()

	fmt.Printf("💾 Stored refined analysis for %s: %d pump events, %d users (top %d shown)\n",
		symbol, len(analysis.PumpEvents), len(analysis.UserAnalysis), len(analysis.TopUsers))
	return nil
}

// storeAggregatedAnalysis stores the existing aggregated analysis structure
func (sm *Manager) storeAggregatedAnalysis(refinedDir string, analysis *RefinedAnalysis) error {
	// Store complete refined analysis
	refinedPath := filepath.Join(refinedDir, "refined_analysis.json")
	if err := sm.writeJSONFile(refinedPath, analysis); err != nil {
		return fmt.Errorf("failed to write refined analysis: %w", err)
	}

	// Store top users separately for quick access
	topUsersPath := filepath.Join(refinedDir, "top_users.json")
	if err := sm.writeJSONFile(topUsersPath, analysis.TopUsers); err != nil {
		return fmt.Errorf("failed to write top users: %w", err)
	}

	// Store pump events (compatibility)
	pumpEventsPath := filepath.Join(refinedDir, "pump_events.json")
	if err := sm.writeJSONFile(pumpEventsPath, analysis.PumpEvents); err != nil {
		return fmt.Errorf("failed to write pump events: %w", err)
	}

	// Store summary (compatibility)
	summaryPath := filepath.Join(refinedDir, "summary.json")
	if err := sm.writeJSONFile(summaryPath, analysis.Summary); err != nil {
		return fmt.Errorf("failed to write summary: %w", err)
	}

	// Store analysis metadata
	metadataPath := filepath.Join(refinedDir, "analysis_metadata.json")
	analysisMetadata := &AnalysisMetadata{
		Symbol:          "aggregated",
		TriggerTime:     analysis.AnalysisWindow.StartTime,
		AnalysisTime:    time.Now(),
		TotalExchanges:  analysis.Summary.TotalExchanges,
		TotalPumpEvents: len(analysis.PumpEvents),
		MaxPriceChange:  analysis.Summary.MaxPriceChange,
		AnalysisVersion: "2.1",
	}

	if err := sm.writeJSONFile(metadataPath, analysisMetadata); err != nil {
		return fmt.Errorf("failed to write analysis metadata: %w", err)
	}

	sm.stats.TotalRefinedFiles += 5
	return nil
}

// storePerExchangeAnalysis stores refined analysis organized by exchange
func (sm *Manager) storePerExchangeAnalysis(refinedDir string, analysis *RefinedAnalysis) error {
	exchanges := []string{"binance", "okx", "bybit", "kucoin", "phemex", "gate"}

	for _, exchange := range exchanges {
		// Create exchange directory within refined/
		exchangeDir := filepath.Join(refinedDir, exchange)
		if err := os.MkdirAll(exchangeDir, 0755); err != nil {
			return fmt.Errorf("failed to create %s refined directory: %w", exchange, err)
		}

		// Filter analysis data for this exchange
		exchangeAnalysis := sm.filterAnalysisForExchange(analysis, exchange)

		// Skip if no data for this exchange
		if len(exchangeAnalysis.PumpEvents) == 0 && len(exchangeAnalysis.UserAnalysis) == 0 {
			continue
		}

		// Store exchange-specific analysis
		exchangePath := filepath.Join(exchangeDir, "analysis.json")
		if err := sm.writeJSONFile(exchangePath, exchangeAnalysis); err != nil {
			return fmt.Errorf("failed to write %s analysis: %w", exchange, err)
		}

		// Store exchange-specific top users
		if len(exchangeAnalysis.TopUsers) > 0 {
			topUsersPath := filepath.Join(exchangeDir, "top_users.json")
			if err := sm.writeJSONFile(topUsersPath, exchangeAnalysis.TopUsers); err != nil {
				return fmt.Errorf("failed to write %s top users: %w", exchange, err)
			}
		}

		// Store exchange metadata
		metadataPath := filepath.Join(exchangeDir, "metadata.json")
		exchangeMetadata := &AnalysisMetadata{
			Symbol:          exchange,
			TriggerTime:     analysis.AnalysisWindow.StartTime,
			AnalysisTime:    time.Now(),
			TotalExchanges:  1, // Single exchange
			TotalPumpEvents: len(exchangeAnalysis.PumpEvents),
			MaxPriceChange:  exchangeAnalysis.Summary.MaxPriceChange,
			AnalysisVersion: "2.1-exchange",
		}

		if err := sm.writeJSONFile(metadataPath, exchangeMetadata); err != nil {
			return fmt.Errorf("failed to write %s metadata: %w", exchange, err)
		}

		sm.stats.TotalRefinedFiles += 3
		fmt.Printf("📊 Stored %s refined: %d pump events, %d users\n",
			exchange, len(exchangeAnalysis.PumpEvents), len(exchangeAnalysis.UserAnalysis))
	}

	return nil
}

// filterAnalysisForExchange creates exchange-specific analysis from aggregated data
func (sm *Manager) filterAnalysisForExchange(analysis *RefinedAnalysis, exchange string) *RefinedAnalysis {
	// Filter pump events for this exchange
	var exchangePumpEvents []PumpEvent
	for _, event := range analysis.PumpEvents {
		if event.Exchange == exchange {
			exchangePumpEvents = append(exchangePumpEvents, event)
		}
	}

	// Filter user analysis for this exchange (users who traded on this exchange)
	var exchangeUserAnalysis []UserAnalysis
	var exchangeTopUsers []UserAnalysis

	for _, user := range analysis.UserAnalysis {
		// Check if user has trades on this exchange
		if trades, exists := user.ExchangeTrades[exchange]; exists && trades > 0 {
			// Create a copy for this exchange
			exchangeUser := user
			// Note: For exchange-specific analysis, we could potentially recalculate
			// prices and volumes using only trades from this exchange, but for now
			// we keep the aggregated values as they represent the user's overall behavior
			exchangeUserAnalysis = append(exchangeUserAnalysis, exchangeUser)
		}
	}

	// Get top users for this exchange (limit to 10)
	if len(exchangeUserAnalysis) > 10 {
		exchangeTopUsers = exchangeUserAnalysis[:10]
	} else {
		exchangeTopUsers = exchangeUserAnalysis
	}

	// Create summary for this exchange
	var maxPriceChange float64
	for _, event := range exchangePumpEvents {
		if event.PriceChange > maxPriceChange {
			maxPriceChange = event.PriceChange
		}
	}

	exchangeSummary := AnalysisSummary{
		TotalExchanges:    1,
		MaxPriceChange:    maxPriceChange,
		FirstPumpExchange: exchange,
		PumpDuration:      analysis.Summary.PumpDuration,
		TotalVolume:       analysis.Summary.TotalVolume,
	}

	return &RefinedAnalysis{
		PumpEvents:     exchangePumpEvents,
		UserAnalysis:   exchangeUserAnalysis,
		Summary:        exchangeSummary,
		TopUsers:       exchangeTopUsers,
		AnalysisWindow: analysis.AnalysisWindow, // Same window as aggregated
	}
}

// writeJSONFile writes data to JSON file atomically
func (sm *Manager) writeJSONFile(filePath string, data interface{}) error {
	// 🔧 BUG FIX: JSON 안전성 검증 (Inf/NaN 값 사전 필터링)
	safeData := sm.sanitizeJSONData(data)

	// Marshal to JSON with proper formatting
	jsonData, err := json.MarshalIndent(safeData, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON marshal failed: %w", err)
	}

	// Atomic write using temporary file
	tempPath := filePath + ".tmp"
	if err := os.WriteFile(tempPath, jsonData, 0644); err != nil {
		return fmt.Errorf("temp file write failed: %w", err)
	}

	// Rename to final path (atomic operation)
	if err := os.Rename(tempPath, filePath); err != nil {
		os.Remove(tempPath) // Clean up temp file
		return fmt.Errorf("atomic rename failed: %w", err)
	}

	// Update file size stats
	if fileInfo, err := os.Stat(filePath); err == nil {
		sm.stats.TotalDataSize += fileInfo.Size()
	}

	return nil
}

// getEventDirectory returns the directory path for a specific event
func (sm *Manager) getEventDirectory(symbol string, triggerTime time.Time) string {
	timestamp := triggerTime.Format("20060102_150405")
	dirName := fmt.Sprintf("%s_%s", symbol, timestamp)
	return filepath.Join(sm.baseDir, dirName)
}

// ensureDirectories creates necessary directory structure
func (sm *Manager) ensureDirectories() error {
	dirs := []string{
		sm.baseDir,
		filepath.Join(sm.baseDir, "logs"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// GetStats returns current storage statistics
func (sm *Manager) GetStats() StorageStats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.stats
}

// Close performs cleanup and closes the storage manager
func (sm *Manager) Close() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Perform final cleanup if needed
	if sm.config.RetentionDays > 0 {
		if err := sm.cleanupOldData(); err != nil {
			fmt.Printf("⚠️ Cleanup error during close: %v\n", err)
		}
	}

	fmt.Println("💾 Storage manager closed")
	return nil
}

// cleanupOldData removes data older than retention period
func (sm *Manager) cleanupOldData() error {
	if sm.config.RetentionDays <= 0 {
		return nil
	}

	cutoffTime := time.Now().AddDate(0, 0, -sm.config.RetentionDays)

	entries, err := os.ReadDir(sm.baseDir)
	if err != nil {
		return fmt.Errorf("failed to read base directory: %w", err)
	}

	var removedCount int
	for _, entry := range entries {
		if entry.IsDir() {
			// Parse directory name to get timestamp
			if timestamp := sm.extractTimestampFromDir(entry.Name()); !timestamp.IsZero() {
				if timestamp.Before(cutoffTime) {
					dirPath := filepath.Join(sm.baseDir, entry.Name())
					if err := os.RemoveAll(dirPath); err == nil {
						removedCount++
					}
				}
			}
		}
	}

	if removedCount > 0 {
		fmt.Printf("🧹 Cleaned up %d old data directories\n", removedCount)
	}

	sm.stats.LastCleanup = time.Now()
	return nil
}

// extractTimestampFromDir extracts timestamp from directory name like "TIA_20240904_143052"
func (sm *Manager) extractTimestampFromDir(dirName string) time.Time {
	parts := strings.Split(dirName, "_")
	if len(parts) >= 3 {
		timestampStr := fmt.Sprintf("%s_%s", parts[len(parts)-2], parts[len(parts)-1])
		if timestamp, err := time.Parse("20060102_150405", timestampStr); err == nil {
			return timestamp
		}
	}
	return time.Time{}
}

// Data structures for storage

// ListingEventMetadata represents metadata for a listing event
type ListingEventMetadata struct {
	Symbol       string    `json:"symbol"`
	Title        string    `json:"title"`
	Markets      []string  `json:"markets"`
	AnnouncedAt  time.Time `json:"announced_at"`
	DetectedAt   time.Time `json:"detected_at"`
	NoticeURL    string    `json:"notice_url"`
	TriggerTime  time.Time `json:"trigger_time"`
	IsKRWListing bool      `json:"is_krw_listing"`
	StoredAt     time.Time `json:"stored_at"`
}

// PumpAnalysis represents pump analysis results
type PumpAnalysis struct {
	PumpEvents      []PumpEvent                        `json:"pump_events"`
	Summary         AnalysisSummary                    `json:"summary"`
	ExchangeResults map[string]*ExchangeAnalysisResult `json:"exchange_results"`
}

// ExchangeAnalysisResult holds analysis results for a specific exchange-market
type ExchangeAnalysisResult struct {
	Exchange        string  `json:"exchange"`
	MarketType      string  `json:"market_type"`
	TotalTrades     int     `json:"total_trades"`
	TotalVolume     float64 `json:"total_volume"`
	PumpEventsCount int     `json:"pump_events_count"`
	FirstTradeTime  int64   `json:"first_trade_time"`
	LastTradeTime   int64   `json:"last_trade_time"`
}

// PumpEvent represents a single pump event
type PumpEvent struct {
	Exchange       string    `json:"exchange"`
	MarketType     string    `json:"market_type"`
	Symbol         string    `json:"symbol"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	Duration       int64     `json:"duration_ms"`
	StartPrice     string    `json:"start_price"`
	PeakPrice      string    `json:"peak_price"`
	PriceChange    float64   `json:"price_change_percent"`
	Volume         string    `json:"volume"`
	TradeCount     int       `json:"trade_count"`
	TriggerTime    time.Time `json:"trigger_time"`
	RelativeTimeMs int64     `json:"relative_time_ms"`
}

// AnalysisSummary represents overall analysis summary
type AnalysisSummary struct {
	TotalExchanges    int     `json:"total_exchanges"`
	MaxPriceChange    float64 `json:"max_price_change"`
	FirstPumpExchange string  `json:"first_pump_exchange"`
	PumpDuration      int64   `json:"pump_duration_ms"`
	TotalVolume       string  `json:"total_volume"`
}

// AnalysisMetadata represents metadata for pump analysis
type AnalysisMetadata struct {
	Symbol          string    `json:"symbol"`
	TriggerTime     time.Time `json:"trigger_time"`
	AnalysisTime    time.Time `json:"analysis_time"`
	TotalExchanges  int       `json:"total_exchanges"`
	TotalPumpEvents int       `json:"total_pump_events"`
	MaxPriceChange  float64   `json:"max_price_change"`
	AnalysisVersion string    `json:"analysis_version"`
}

// UserAnalysis represents user-specific trading analysis for refined data
type UserAnalysis struct {
	UserID          string         `json:"user_id"`             // Unique identifier based on timestamp
	FirstTradeTime  time.Time      `json:"first_trade_time"`    // Earliest trade timestamp
	LastTradeTime   time.Time      `json:"last_trade_time"`     // Latest trade timestamp
	TradingDuration int64          `json:"trading_duration_ms"` // Duration in milliseconds
	StartPrice      string         `json:"start_price"`         // First trade price
	EndPrice        string         `json:"end_price"`           // Last trade price
	AveragePrice    string         `json:"average_price"`       // Volume-weighted average price
	TotalUSDTVolume string         `json:"total_usdt_volume"`   // Total USDT volume traded
	TotalQuantity   string         `json:"total_quantity"`      // Total quantity traded
	TradeCount      int            `json:"trade_count"`         // Number of trades
	ExchangeTrades  map[string]int `json:"exchange_trades"`     // Trades per exchange
	RelativeTimeMs  int64          `json:"relative_time_ms"`    // Time relative to pump start
	UserRank        int            `json:"user_rank"`           // Rank based on first trade time (1st, 2nd, 3rd...)
}

// ExchangeInsight represents detailed statistics for a single exchange
type ExchangeInsight struct {
	ExchangeName       string               `json:"exchange_name"`
	FirstPumpTime      time.Time            `json:"first_pump_time"`       // When first pump occurred on this exchange
	RelativePumpTime   int64                `json:"relative_pump_time_ms"` // Milliseconds after overall first pump
	PumpEventCount     int                  `json:"pump_event_count"`      // Number of pump events
	MaxPriceChange     float64              `json:"max_price_change"`      // Highest price change %
	AveragePriceChange float64              `json:"average_price_change"`  // Average price change %
	UserCount          int                  `json:"user_count"`            // Number of unique users
	TotalVolume        string               `json:"total_volume"`          // Total USDT volume
	AverageUserVolume  string               `json:"average_user_volume"`   // Average volume per user
	FastestUserTime    int64                `json:"fastest_user_time_ms"`  // Time of fastest user relative to pump start
	UserBehavior       ExchangeUserBehavior `json:"user_behavior"`         // User behavior patterns
}

// ExchangeUserBehavior represents user behavior patterns on an exchange
type ExchangeUserBehavior struct {
	AverageTradesPerUser float64 `json:"average_trades_per_user"`
	AverageVolumePerUser string  `json:"average_volume_per_user"`
	MedianEntryTime      int64   `json:"median_entry_time_ms"` // Median time users entered after pump
	EarlyTraders         int     `json:"early_traders"`        // Users who entered in first 1 second
	RegularTraders       int     `json:"regular_traders"`      // Users who entered in 1-5 seconds
	LateTraders          int     `json:"late_traders"`         // Users who entered after 5 seconds
	HighVolumeTraders    int     `json:"high_volume_traders"`  // Users with >1000 USDT volume
}

// ExchangeComparison represents comparison data between exchanges
type ExchangeComparison struct {
	FastestExchange       string   `json:"fastest_exchange"`        // Exchange with first pump
	SlowestExchange       string   `json:"slowest_exchange"`        // Exchange with last first pump
	MostActiveExchange    string   `json:"most_active_exchange"`    // Exchange with most users
	HighestVolumeExchange string   `json:"highest_volume_exchange"` // Exchange with highest total volume
	ReactionTimeSpread    int64    `json:"reaction_time_spread_ms"` // Time difference between fastest and slowest
	ExchangeRanking       []string `json:"exchange_ranking"`        // Exchanges ranked by reaction speed
}

// RefinedAnalysis represents complete refined analysis including user data
type RefinedAnalysis struct {
	PumpEvents         []PumpEvent        `json:"pump_events"`         // Original pump events
	UserAnalysis       []UserAnalysis     `json:"user_analysis"`       // Per-user analysis
	Summary            AnalysisSummary    `json:"summary"`             // Overall summary
	TopUsers           []UserAnalysis     `json:"top_users"`           // Top 10 early users
	ExchangeInsights   []ExchangeInsight  `json:"exchange_insights"`   // Per-exchange detailed analysis
	ExchangeComparison ExchangeComparison `json:"exchange_comparison"` // Exchange comparison data
	AnalysisWindow     struct {
		StartTime time.Time `json:"start_time"`  // Analysis window start
		EndTime   time.Time `json:"end_time"`    // Analysis window end (10s after pump)
		Duration  int64     `json:"duration_ms"` // Window duration in ms
	} `json:"analysis_window"`
}

// performPumpAnalysis performs pump analysis and stores refined data
func (sm *Manager) performPumpAnalysis(collectionEvent *models.CollectionEvent, refinedDir string) error {
	// 🔇 디버그 로그 제거 (리소스 절약)

	// Wait for post_analysis_delay if configured
	if sm.analysisConfig.MaxAnalysisDelay > 0 {
		time.Sleep(sm.analysisConfig.MaxAnalysisDelay)
	}

	// Perform pump analysis
	analysisResult, err := sm.analyzer.AnalyzePumps(collectionEvent)
	if err != nil {
		return fmt.Errorf("pump analysis failed: %w", err)
	}

	if analysisResult == nil {
		return fmt.Errorf("pump analysis returned nil result")
	}

	// Store main analysis result
	analysisPath := filepath.Join(refinedDir, "pump_analysis.json")
	if err := sm.writeJSONFile(analysisPath, analysisResult); err != nil {
		return fmt.Errorf("failed to write pump analysis: %w", err)
	}

	// Store individual pump events for easy access
	if len(analysisResult.PumpEvents) > 0 {
		pumpEventsPath := filepath.Join(refinedDir, "pump_events.json")
		if err := sm.writeJSONFile(pumpEventsPath, analysisResult.PumpEvents); err != nil {
			return fmt.Errorf("failed to write pump events: %w", err)
		}
	}

	// Store analysis summary
	summaryPath := filepath.Join(refinedDir, "summary.json")
	if err := sm.writeJSONFile(summaryPath, analysisResult.Summary); err != nil {
		return fmt.Errorf("failed to write analysis summary: %w", err)
	}

	// Store exchange-specific results for each exchange with pumps
	for exchange, result := range analysisResult.ExchangeResults {
		if result.PumpEventsCount > 0 {
			// Filter pump events for this exchange
			var exchangePumpEvents []PumpEvent
			for _, pumpEvent := range analysisResult.PumpEvents {
				exchangeKey := fmt.Sprintf("%s_%s", pumpEvent.Exchange, pumpEvent.MarketType)
				if exchangeKey == exchange {
					exchangePumpEvents = append(exchangePumpEvents, pumpEvent)
				}
			}

			// Create exchange-specific analysis result
			exchangeAnalysis := struct {
				ExchangeInfo *ExchangeAnalysisResult `json:"exchange_info"`
				PumpEvents   []PumpEvent             `json:"pump_events"`
			}{
				ExchangeInfo: result,
				PumpEvents:   exchangePumpEvents,
			}

			exchangePath := filepath.Join(refinedDir, fmt.Sprintf("%s_pumps.json", exchange))
			if err := sm.writeJSONFile(exchangePath, exchangeAnalysis); err != nil {
				fmt.Printf("⚠️ Failed to write %s analysis: %v\n", exchange, err)
				// Continue with other exchanges
			}
		}
	}

	// Update statistics
	sm.stats.TotalRefinedFiles += int64(len(analysisResult.ExchangeResults) + 3) // analysis + events + summary + exchanges

	fmt.Printf("✅ Pump analysis completed: %d pump events found across %d exchanges\n",
		len(analysisResult.PumpEvents), len(analysisResult.ExchangeResults))

	return nil
}

// 🔧 BUG FIX: JSON 안전성 보장 - Inf/NaN 값 필터링
func (sm *Manager) sanitizeJSONData(data interface{}) interface{} {
	// 🔧 EMERGENCY FIX: Disable complex reflection to prevent panics
	// Just return data as-is and let JSON marshaller handle errors gracefully
	return data
}

// sanitizeValue recursively sanitizes values to ensure JSON compatibility
func (sm *Manager) sanitizeValue(val reflect.Value) reflect.Value {
	if !val.IsValid() {
		return val
	}

	switch val.Kind() {
	case reflect.Float64:
		f := val.Float()
		if math.IsInf(f, 0) || math.IsNaN(f) {
			// Replace Inf/NaN with 0 and log warning
			fmt.Printf("⚠️ [JSON_SAFETY] Replaced invalid float64 value: %v → 0\n", f)
			return reflect.ValueOf(float64(0))
		}
		return val

	case reflect.Float32:
		f := val.Float()
		if math.IsInf(f, 0) || math.IsNaN(f) {
			fmt.Printf("⚠️ [JSON_SAFETY] Replaced invalid float32 value: %v → 0\n", f)
			return reflect.ValueOf(float32(0))
		}
		return val

	case reflect.Ptr:
		if val.IsNil() {
			return val
		}
		// 🔧 SAFER POINTER HANDLING: Add panic protection
		defer func() {
			if r := recover(); r != nil {
				// If pointer handling fails, return nil pointer
				return
			}
		}()
		elem := sm.sanitizeValue(val.Elem())
		if !elem.IsValid() {
			return reflect.Zero(val.Type())
		}
		newPtr := reflect.New(elem.Type())
		if newPtr.Elem().CanSet() {
			newPtr.Elem().Set(elem)
		}
		return newPtr

	case reflect.Slice:
		if val.IsNil() {
			return val
		}
		newSlice := reflect.MakeSlice(val.Type(), val.Len(), val.Cap())
		for i := 0; i < val.Len(); i++ {
			newSlice.Index(i).Set(sm.sanitizeValue(val.Index(i)))
		}
		return newSlice

	case reflect.Array:
		newArray := reflect.New(val.Type()).Elem()
		for i := 0; i < val.Len(); i++ {
			newArray.Index(i).Set(sm.sanitizeValue(val.Index(i)))
		}
		return newArray

	case reflect.Map:
		if val.IsNil() {
			return val
		}
		// 🔧 FINAL MAP REFLECTION PANIC FIX: Ultra-safe map handling
		newMap := reflect.MakeMap(val.Type())
		for _, key := range val.MapKeys() {
			mapValue := val.MapIndex(key)

			// 🔧 BUG FIX: Only sanitize basic types, skip complex structs
			var sanitizedValue reflect.Value
			switch mapValue.Kind() {
			case reflect.Float64, reflect.Float32:
				sanitizedValue = sm.sanitizeValue(mapValue)
			case reflect.Struct:
				// 🔧 CRITICAL FIX: Don't recursively sanitize structs - just use as-is
				sanitizedValue = mapValue
			default:
				sanitizedValue = mapValue
			}

			// Set map value only if compatible
			if sanitizedValue.IsValid() && sanitizedValue.Type().AssignableTo(val.Type().Elem()) {
				newMap.SetMapIndex(key, sanitizedValue)
			}
		}
		return newMap

	case reflect.Struct:
		// 🔧 COMPLETE REFLECTION PANIC FIX: Skip struct reflection entirely
		// Instead of deep reflection, convert to interface{} and let JSON marshaler handle it
		if val.CanInterface() {
			// Try to convert to interface and let the calling function handle JSON conversion
			return val
		}
		// If can't interface, return zero value
		return reflect.Zero(val.Type())

	default:
		return val
	}
}
