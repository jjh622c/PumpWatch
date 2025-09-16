package analyzer

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"PumpWatch/internal/models"
	"PumpWatch/internal/storage"
)

// PumpAnalyzer detects and analyzes pump events from trade data
// Following METDC v2.0 pump detection algorithm: >3% price increases in 1-second windows
type PumpAnalyzer struct {
	// Configuration
	minPumpPercentage float64       // Minimum pump percentage (default: 3.0%)
	timeWindowSize    time.Duration // Analysis window size (default: 1 second)
	minTradesRequired int           // Minimum trades for valid pump (default: 5)

	// Statistics
	totalAnalyses      int64
	totalPumpsDetected int64
	lastAnalysis       time.Time
}

// NewPumpAnalyzer creates a new pump analyzer with default settings
func NewPumpAnalyzer() *PumpAnalyzer {
	return &PumpAnalyzer{
		minPumpPercentage: 3.0,             // 3% minimum pump
		timeWindowSize:    1 * time.Second, // 1-second windows
		minTradesRequired: 5,               // At least 5 trades
	}
}

// AnalyzePumps analyzes a CollectionEvent for pump events
func (pa *PumpAnalyzer) AnalyzePumps(collectionEvent *models.CollectionEvent) (*storage.PumpAnalysis, error) {
	if collectionEvent == nil {
		return nil, fmt.Errorf("collection event is nil")
	}

	log.Printf("🔍 Starting pump analysis for %s", collectionEvent.Symbol)

	var allPumpEvents []storage.PumpEvent
	exchangeResults := make(map[string]*ExchangeAnalysisResult)

	// Analyze each exchange-market combination
	exchangeMarkets := []struct {
		name   string
		trades []models.TradeEvent
	}{
		{"binance_spot", collectionEvent.BinanceSpot},
		{"binance_futures", collectionEvent.BinanceFutures},
		{"bybit_spot", collectionEvent.BybitSpot},
		{"bybit_futures", collectionEvent.BybitFutures},
		{"okx_spot", collectionEvent.OKXSpot},
		{"okx_futures", collectionEvent.OKXFutures},
		{"kucoin_spot", collectionEvent.KuCoinSpot},
		{"kucoin_futures", collectionEvent.KuCoinFutures},
		{"phemex_spot", collectionEvent.PhemexSpot},
		{"phemex_futures", collectionEvent.PhemexFutures},
		{"gate_spot", collectionEvent.GateSpot},
		{"gate_futures", collectionEvent.GateFutures},
	}

	// Process each exchange-market
	for _, em := range exchangeMarkets {
		if len(em.trades) == 0 {
			continue
		}

		exchangeName, marketType := pa.parseExchangeMarket(em.name)
		log.Printf("📊 Analyzing %s: %d trades", em.name, len(em.trades))

		// Analyze this exchange-market for pumps
		pumpEvents, result := pa.analyzeExchangeMarket(exchangeName, marketType, collectionEvent.Symbol,
			em.trades, collectionEvent.TriggerTime)

		if len(pumpEvents) > 0 {
			allPumpEvents = append(allPumpEvents, pumpEvents...)
			exchangeResults[em.name] = result

			log.Printf("🚨 Found %d pump events in %s", len(pumpEvents), em.name)
			for _, pump := range pumpEvents {
				log.Printf("   💥 Pump: %.2f%% price increase (%s -> %s)",
					pump.PriceChange, pump.StartPrice, pump.PeakPrice)
			}
		}
	}

	// Generate analysis summary
	summary := pa.generateAnalysisSummary(allPumpEvents, exchangeResults)

	// Convert analyzer results to storage format
	storageExchangeResults := make(map[string]*storage.ExchangeAnalysisResult)
	for key, result := range exchangeResults {
		storageExchangeResults[key] = &storage.ExchangeAnalysisResult{
			Exchange:        result.Exchange,
			MarketType:      result.MarketType,
			TotalTrades:     result.TotalTrades,
			TotalVolume:     result.TotalVolume,
			PumpEventsCount: result.PumpEventsCount,
			FirstTradeTime:  result.FirstTradeTime,
			LastTradeTime:   result.LastTradeTime,
		}
	}

	// Create final analysis
	analysis := &storage.PumpAnalysis{
		PumpEvents:      allPumpEvents,
		Summary:         summary,
		ExchangeResults: storageExchangeResults,
	}

	// Update statistics
	pa.totalAnalyses++
	pa.totalPumpsDetected += int64(len(allPumpEvents))
	pa.lastAnalysis = time.Now()

	log.Printf("✅ Pump analysis completed: %d pump events across %d exchanges",
		len(allPumpEvents), len(exchangeResults))

	return analysis, nil
}

// AnalyzeRefinedPumps analyzes a CollectionEvent with complete user analysis
func (pa *PumpAnalyzer) AnalyzeRefinedPumps(collectionEvent *models.CollectionEvent) (*storage.RefinedAnalysis, error) {
	if collectionEvent == nil {
		return nil, fmt.Errorf("collection event is nil")
	}

	log.Printf("🔍 Starting refined pump analysis for %s", collectionEvent.Symbol)

	// First get basic pump events
	pumpAnalysis, err := pa.AnalyzePumps(collectionEvent)
	if err != nil {
		return nil, fmt.Errorf("failed to get basic pump analysis: %w", err)
	}

	if len(pumpAnalysis.PumpEvents) == 0 {
		log.Printf("⚠️ No pump events detected, skipping user analysis")
		return &storage.RefinedAnalysis{
			PumpEvents:   pumpAnalysis.PumpEvents,
			UserAnalysis: []storage.UserAnalysis{},
			Summary:      pumpAnalysis.Summary,
			TopUsers:     []storage.UserAnalysis{},
		}, nil
	}

	// Find the earliest pump start time for analysis window
	pumpStartTime := pa.findEarliestPumpTime(pumpAnalysis.PumpEvents)
	if pumpStartTime.IsZero() {
		return nil, fmt.Errorf("failed to determine pump start time")
	}

	// 10-second analysis window from pump start
	analysisStart := pumpStartTime
	analysisEnd := pumpStartTime.Add(10 * time.Second)

	log.Printf("📊 User analysis window: %s to %s (10s)",
		analysisStart.Format("15:04:05.000"), analysisEnd.Format("15:04:05.000"))

	// Collect all trades from all exchanges within analysis window
	allTrades := pa.collectAllTrades(collectionEvent, analysisStart, analysisEnd)
	log.Printf("📈 Collected %d trades within analysis window", len(allTrades))

	if len(allTrades) == 0 {
		log.Printf("⚠️ No trades found in analysis window")
		return &storage.RefinedAnalysis{
			PumpEvents:   pumpAnalysis.PumpEvents,
			UserAnalysis: []storage.UserAnalysis{},
			Summary:      pumpAnalysis.Summary,
			TopUsers:     []storage.UserAnalysis{},
		}, nil
	}

	// Group trades by user (same timestamp = same user)
	userGroups := pa.groupTradesByUser(allTrades)
	log.Printf("👥 Identified %d unique users", len(userGroups))

	// Analyze each user's trading behavior
	userAnalyses := pa.analyzeUsers(userGroups, pumpStartTime)
	log.Printf("📊 Completed analysis for %d users", len(userAnalyses))

	// Sort users by first trade time to identify early movers
	sort.Slice(userAnalyses, func(i, j int) bool {
		return userAnalyses[i].FirstTradeTime.Before(userAnalyses[j].FirstTradeTime)
	})

	// Assign user ranks
	for i := range userAnalyses {
		userAnalyses[i].UserRank = i + 1
	}

	// Get top 10 users for quick access
	topUsers := userAnalyses
	if len(topUsers) > 10 {
		topUsers = userAnalyses[:10]
	}

	log.Printf("🏆 Top 3 users: %s, %s, %s",
		getUserID(topUsers, 0),
		getUserID(topUsers, 1),
		getUserID(topUsers, 2))

	// Analyze exchange insights and comparison
	exchangeInsights := pa.analyzeExchangeInsights(pumpAnalysis.PumpEvents, userAnalyses, pumpStartTime)
	exchangeComparison := pa.analyzeExchangeComparison(exchangeInsights, pumpAnalysis.PumpEvents)

	log.Printf("📊 Exchange analysis: %d exchanges, fastest: %s",
		len(exchangeInsights), exchangeComparison.FastestExchange)

	// Create refined analysis
	refinedAnalysis := &storage.RefinedAnalysis{
		PumpEvents:         pumpAnalysis.PumpEvents,
		UserAnalysis:       userAnalyses,
		Summary:            pumpAnalysis.Summary,
		TopUsers:           topUsers,
		ExchangeInsights:   exchangeInsights,
		ExchangeComparison: exchangeComparison,
	}

	// Set analysis window metadata
	refinedAnalysis.AnalysisWindow.StartTime = analysisStart
	refinedAnalysis.AnalysisWindow.EndTime = analysisEnd
	refinedAnalysis.AnalysisWindow.Duration = analysisEnd.Sub(analysisStart).Milliseconds()

	// Update statistics
	pa.totalAnalyses++
	pa.totalPumpsDetected += int64(len(pumpAnalysis.PumpEvents))
	pa.lastAnalysis = time.Now()

	log.Printf("✅ Refined analysis completed: %d pump events, %d users, top %d tracked",
		len(pumpAnalysis.PumpEvents), len(userAnalyses), len(topUsers))

	return refinedAnalysis, nil
}

// analyzeExchangeMarket analyzes pump events for a specific exchange-market combination
func (pa *PumpAnalyzer) analyzeExchangeMarket(exchange, marketType, symbol string, trades []models.TradeEvent, triggerTime time.Time) ([]storage.PumpEvent, *ExchangeAnalysisResult) {
	if len(trades) < pa.minTradesRequired {
		return nil, nil
	}

	// Sort trades by timestamp (int64 Unix milliseconds)
	sort.Slice(trades, func(i, j int) bool {
		return trades[i].Timestamp < trades[j].Timestamp
	})

	// Create time windows (1-second intervals)
	timeWindows := pa.createTimeWindows(trades, pa.timeWindowSize)

	var pumpEvents []storage.PumpEvent
	var totalVolume float64

	// Analyze each time window for pumps
	for _, window := range timeWindows {
		if len(window.trades) < pa.minTradesRequired {
			continue
		}

		pumpEvent := pa.analyzeTimeWindow(exchange, marketType, symbol, window, triggerTime)
		if pumpEvent != nil {
			pumpEvents = append(pumpEvents, *pumpEvent)
		}

		// Calculate total volume
		for _, trade := range window.trades {
			if qty, err := strconv.ParseFloat(trade.Quantity, 64); err == nil {
				totalVolume += qty
			}
		}
	}

	result := &ExchangeAnalysisResult{
		Exchange:        exchange,
		MarketType:      marketType,
		TotalTrades:     len(trades),
		TotalVolume:     totalVolume,
		PumpEventsCount: len(pumpEvents),
		FirstTradeTime:  trades[0].Timestamp,
		LastTradeTime:   trades[len(trades)-1].Timestamp,
	}

	return pumpEvents, result
}

// analyzeTimeWindow analyzes a single time window for pump events
func (pa *PumpAnalyzer) analyzeTimeWindow(exchange, marketType, symbol string, window *TimeWindow, triggerTime time.Time) *storage.PumpEvent {
	if len(window.trades) < pa.minTradesRequired {
		return nil
	}

	// Find price range in this window
	var prices []float64
	var volumes []float64

	for _, trade := range window.trades {
		if price, err := strconv.ParseFloat(trade.Price, 64); err == nil {
			prices = append(prices, price)
		}
		if qty, err := strconv.ParseFloat(trade.Quantity, 64); err == nil {
			volumes = append(volumes, qty)
		}
	}

	if len(prices) == 0 {
		return nil
	}

	// Calculate price statistics
	minPrice := prices[0]
	maxPrice := prices[0]
	var totalVolume float64

	for _, price := range prices {
		if price < minPrice {
			minPrice = price
		}
		if price > maxPrice {
			maxPrice = price
		}
	}

	for _, vol := range volumes {
		totalVolume += vol
	}

	// Calculate price change percentage
	priceChange := ((maxPrice - minPrice) / minPrice) * 100

	// Check if this qualifies as a pump
	if priceChange < pa.minPumpPercentage {
		return nil
	}

	// Create pump event
	pumpEvent := &storage.PumpEvent{
		Exchange:       exchange,
		MarketType:     marketType,
		Symbol:         symbol,
		StartTime:      time.UnixMilli(window.startTime),
		EndTime:        time.UnixMilli(window.endTime),
		Duration:       window.endTime - window.startTime,
		StartPrice:     fmt.Sprintf("%.8f", minPrice),
		PeakPrice:      fmt.Sprintf("%.8f", maxPrice),
		PriceChange:    priceChange,
		Volume:         fmt.Sprintf("%.8f", totalVolume),
		TradeCount:     len(window.trades),
		TriggerTime:    triggerTime,
		RelativeTimeMs: window.startTime - triggerTime.UnixMilli(),
	}

	return pumpEvent
}

// createTimeWindows divides trades into time-based windows
func (pa *PumpAnalyzer) createTimeWindows(trades []models.TradeEvent, windowSize time.Duration) []*TimeWindow {
	if len(trades) == 0 {
		return nil
	}

	var windows []*TimeWindow

	// Convert window size to milliseconds
	windowSizeMs := windowSize.Milliseconds()

	// Start from the first trade time (Unix milliseconds)
	startTimeMs := trades[0].Timestamp
	endTimeMs := startTimeMs + windowSizeMs

	var currentWindow *TimeWindow

	for _, trade := range trades {
		// Check if we need a new window
		if currentWindow == nil || trade.Timestamp >= endTimeMs {
			// Save previous window if it has trades
			if currentWindow != nil && len(currentWindow.trades) > 0 {
				windows = append(windows, currentWindow)
			}

			// Create new window (aligned to window boundaries)
			windowStart := (trade.Timestamp / windowSizeMs) * windowSizeMs
			currentWindow = &TimeWindow{
				startTime: windowStart,
				endTime:   windowStart + windowSizeMs,
				trades:    []models.TradeEvent{},
			}
			endTimeMs = currentWindow.endTime
		}

		// Add trade to current window
		currentWindow.trades = append(currentWindow.trades, trade)
	}

	// Add final window
	if currentWindow != nil && len(currentWindow.trades) > 0 {
		windows = append(windows, currentWindow)
	}

	return windows
}

// generateAnalysisSummary creates an overall summary of the pump analysis
func (pa *PumpAnalyzer) generateAnalysisSummary(pumpEvents []storage.PumpEvent, exchangeResults map[string]*ExchangeAnalysisResult) storage.AnalysisSummary {
	summary := storage.AnalysisSummary{
		TotalExchanges: len(exchangeResults),
	}

	if len(pumpEvents) == 0 {
		return summary
	}

	// Find maximum price change
	var maxPriceChange float64
	var firstPumpExchange string
	var earliestPumpTime time.Time
	var latestPumpTime time.Time
	var totalVolume float64

	for i, pump := range pumpEvents {
		if pump.PriceChange > maxPriceChange {
			maxPriceChange = pump.PriceChange
		}

		// Track first pump (earliest relative time)
		if i == 0 || pump.RelativeTimeMs < pumpEvents[0].RelativeTimeMs {
			firstPumpExchange = fmt.Sprintf("%s_%s", pump.Exchange, pump.MarketType)
		}

		// Track time range
		if i == 0 || pump.StartTime.Before(earliestPumpTime) {
			earliestPumpTime = pump.StartTime
		}
		if i == 0 || pump.EndTime.After(latestPumpTime) {
			latestPumpTime = pump.EndTime
		}

		// Add volume
		if vol, err := strconv.ParseFloat(pump.Volume, 64); err == nil {
			totalVolume += vol
		}
	}

	summary.MaxPriceChange = maxPriceChange
	summary.FirstPumpExchange = firstPumpExchange
	summary.TotalVolume = fmt.Sprintf("%.8f", totalVolume)

	if !latestPumpTime.IsZero() && !earliestPumpTime.IsZero() {
		summary.PumpDuration = latestPumpTime.Sub(earliestPumpTime).Milliseconds()
	}

	return summary
}

// parseExchangeMarket parses "binance_spot" into "binance" and "spot"
func (pa *PumpAnalyzer) parseExchangeMarket(exchangeMarket string) (string, string) {
	parts := strings.Split(exchangeMarket, "_")
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return exchangeMarket, "unknown"
}

// GetStats returns analyzer statistics
func (pa *PumpAnalyzer) GetStats() PumpAnalyzerStats {
	return PumpAnalyzerStats{
		TotalAnalyses:      pa.totalAnalyses,
		TotalPumpsDetected: pa.totalPumpsDetected,
		LastAnalysis:       pa.lastAnalysis,
		MinPumpPercentage:  pa.minPumpPercentage,
		TimeWindowSize:     pa.timeWindowSize,
		MinTradesRequired:  pa.minTradesRequired,
	}
}

// Supporting structures

// TimeWindow represents a time-based window of trades
type TimeWindow struct {
	startTime int64 // Unix milliseconds
	endTime   int64 // Unix milliseconds
	trades    []models.TradeEvent
}

// ExchangeAnalysisResult holds analysis results for a specific exchange-market
type ExchangeAnalysisResult struct {
	Exchange        string
	MarketType      string
	TotalTrades     int
	TotalVolume     float64
	PumpEventsCount int
	FirstTradeTime  int64
	LastTradeTime   int64
}

// PumpAnalyzerStats holds analyzer statistics
type PumpAnalyzerStats struct {
	TotalAnalyses      int64         `json:"total_analyses"`
	TotalPumpsDetected int64         `json:"total_pumps_detected"`
	LastAnalysis       time.Time     `json:"last_analysis"`
	MinPumpPercentage  float64       `json:"min_pump_percentage"`
	TimeWindowSize     time.Duration `json:"time_window_size"`
	MinTradesRequired  int           `json:"min_trades_required"`
}

// User analysis helper functions

// findEarliestPumpTime finds the earliest pump start time across all exchanges
func (pa *PumpAnalyzer) findEarliestPumpTime(pumpEvents []storage.PumpEvent) time.Time {
	if len(pumpEvents) == 0 {
		return time.Time{}
	}

	earliestTime := pumpEvents[0].StartTime
	for _, pump := range pumpEvents[1:] {
		if pump.StartTime.Before(earliestTime) {
			earliestTime = pump.StartTime
		}
	}

	return earliestTime
}

// collectAllTrades collects all trades from all exchanges within the analysis window
func (pa *PumpAnalyzer) collectAllTrades(collectionEvent *models.CollectionEvent, start, end time.Time) []models.TradeEvent {
	var allTrades []models.TradeEvent

	// Define all 12 independent slices
	exchangeData := []struct {
		name   string
		trades []models.TradeEvent
	}{
		{"binance_spot", collectionEvent.BinanceSpot},
		{"binance_futures", collectionEvent.BinanceFutures},
		{"bybit_spot", collectionEvent.BybitSpot},
		{"bybit_futures", collectionEvent.BybitFutures},
		{"okx_spot", collectionEvent.OKXSpot},
		{"okx_futures", collectionEvent.OKXFutures},
		{"kucoin_spot", collectionEvent.KuCoinSpot},
		{"kucoin_futures", collectionEvent.KuCoinFutures},
		{"phemex_spot", collectionEvent.PhemexSpot},
		{"phemex_futures", collectionEvent.PhemexFutures},
		{"gate_spot", collectionEvent.GateSpot},
		{"gate_futures", collectionEvent.GateFutures},
	}

	startMs := start.UnixMilli()
	endMs := end.UnixMilli()

	// Collect trades from all exchanges within time window
	for _, exchange := range exchangeData {
		for _, trade := range exchange.trades {
			if trade.Timestamp >= startMs && trade.Timestamp <= endMs {
				allTrades = append(allTrades, trade)
			}
		}
	}

	return allTrades
}

// groupTradesByUser groups trades by user ID (timestamp-based identification)
func (pa *PumpAnalyzer) groupTradesByUser(trades []models.TradeEvent) map[string][]models.TradeEvent {
	userGroups := make(map[string][]models.TradeEvent)

	for _, trade := range trades {
		// User ID is based on timestamp - same timestamp = same user
		userID := fmt.Sprintf("%d", trade.Timestamp)
		userGroups[userID] = append(userGroups[userID], trade)
	}

	return userGroups
}

// analyzeUsers analyzes trading behavior for each user group
func (pa *PumpAnalyzer) analyzeUsers(userGroups map[string][]models.TradeEvent, pumpStartTime time.Time) []storage.UserAnalysis {
	var userAnalyses []storage.UserAnalysis

	for userID, trades := range userGroups {
		if len(trades) == 0 {
			continue
		}

		// Sort trades by timestamp
		sort.Slice(trades, func(i, j int) bool {
			return trades[i].Timestamp < trades[j].Timestamp
		})

		// Calculate user statistics
		userAnalysis := pa.calculateUserStats(userID, trades, pumpStartTime)
		userAnalyses = append(userAnalyses, userAnalysis)
	}

	return userAnalyses
}

// calculateUserStats calculates comprehensive statistics for a single user
func (pa *PumpAnalyzer) calculateUserStats(userID string, trades []models.TradeEvent, pumpStartTime time.Time) storage.UserAnalysis {
	firstTrade := trades[0]
	lastTrade := trades[len(trades)-1]

	firstTradeTime := time.UnixMilli(firstTrade.Timestamp)
	lastTradeTime := time.UnixMilli(lastTrade.Timestamp)

	var totalUSDTVolume float64
	var totalQuantity float64
	var totalValue float64 // For weighted average price calculation
	exchangeTrades := make(map[string]int)

	// Process all trades for this user
	for _, trade := range trades {
		price, _ := strconv.ParseFloat(trade.Price, 64)
		qty, _ := strconv.ParseFloat(trade.Quantity, 64)
		usdtValue := price * qty

		totalUSDTVolume += usdtValue
		totalQuantity += qty
		totalValue += usdtValue

		// Count trades per exchange
		exchangeKey := fmt.Sprintf("%s_%s", trade.Exchange, trade.MarketType)
		exchangeTrades[exchangeKey]++
	}

	// Calculate volume-weighted average price
	var averagePrice float64
	if totalQuantity > 0 {
		averagePrice = totalUSDTVolume / totalQuantity
	}

	firstPrice, _ := strconv.ParseFloat(firstTrade.Price, 64)
	lastPrice, _ := strconv.ParseFloat(lastTrade.Price, 64)

	return storage.UserAnalysis{
		UserID:          userID,
		FirstTradeTime:  firstTradeTime,
		LastTradeTime:   lastTradeTime,
		TradingDuration: lastTradeTime.Sub(firstTradeTime).Milliseconds(),
		StartPrice:      fmt.Sprintf("%.8f", firstPrice),
		EndPrice:        fmt.Sprintf("%.8f", lastPrice),
		AveragePrice:    fmt.Sprintf("%.8f", averagePrice),
		TotalUSDTVolume: fmt.Sprintf("%.8f", totalUSDTVolume),
		TotalQuantity:   fmt.Sprintf("%.8f", totalQuantity),
		TradeCount:      len(trades),
		ExchangeTrades:  exchangeTrades,
		RelativeTimeMs:  firstTradeTime.Sub(pumpStartTime).Milliseconds(),
		UserRank:        0, // Will be set by caller
	}
}

// getUserID safely gets user ID from slice
func getUserID(users []storage.UserAnalysis, index int) string {
	if index < len(users) {
		return users[index].UserID[:8]
	}
	return "N/A"
}

// analyzeExchangeInsights analyzes detailed statistics for each exchange
func (pa *PumpAnalyzer) analyzeExchangeInsights(pumpEvents []storage.PumpEvent, userAnalyses []storage.UserAnalysis, pumpStartTime time.Time) []storage.ExchangeInsight {
	exchanges := []string{"binance", "okx", "bybit", "kucoin", "phemex", "gate"}
	var insights []storage.ExchangeInsight

	for _, exchange := range exchanges {
		insight := pa.calculateExchangeInsight(exchange, pumpEvents, userAnalyses, pumpStartTime)
		if insight.PumpEventCount > 0 || insight.UserCount > 0 {
			insights = append(insights, insight)
		}
	}

	return insights
}

// calculateExchangeInsight calculates detailed statistics for a single exchange
func (pa *PumpAnalyzer) calculateExchangeInsight(exchangeName string, pumpEvents []storage.PumpEvent, userAnalyses []storage.UserAnalysis, pumpStartTime time.Time) storage.ExchangeInsight {
	// Filter pump events for this exchange
	var exchangePumpEvents []storage.PumpEvent
	var firstPumpTime time.Time
	var maxPriceChange float64
	var totalPriceChange float64

	for _, event := range pumpEvents {
		if event.Exchange == exchangeName {
			exchangePumpEvents = append(exchangePumpEvents, event)

			// Find first pump time
			if firstPumpTime.IsZero() || event.StartTime.Before(firstPumpTime) {
				firstPumpTime = event.StartTime
			}

			// Track max price change
			if event.PriceChange > maxPriceChange {
				maxPriceChange = event.PriceChange
			}

			totalPriceChange += event.PriceChange
		}
	}

	// Calculate average price change
	var averagePriceChange float64
	if len(exchangePumpEvents) > 0 {
		averagePriceChange = totalPriceChange / float64(len(exchangePumpEvents))
	}

	// Filter users who traded on this exchange
	var exchangeUsers []storage.UserAnalysis
	var totalVolume float64
	var fastestUserTime int64 = 999999999 // Large initial value

	for _, user := range userAnalyses {
		// Check if user has trades on this exchange (spot or futures)
		spotKey := exchangeName + "_spot"
		futuresKey := exchangeName + "_futures"

		if user.ExchangeTrades[spotKey] > 0 || user.ExchangeTrades[futuresKey] > 0 {
			exchangeUsers = append(exchangeUsers, user)

			// Add to total volume
			if volume, err := strconv.ParseFloat(user.TotalUSDTVolume, 64); err == nil {
				totalVolume += volume
			}

			// Track fastest user
			if user.RelativeTimeMs < fastestUserTime {
				fastestUserTime = user.RelativeTimeMs
			}
		}
	}

	// Calculate average volume per user
	var averageUserVolume float64
	if len(exchangeUsers) > 0 {
		averageUserVolume = totalVolume / float64(len(exchangeUsers))
	}

	// Calculate user behavior patterns
	userBehavior := pa.calculateUserBehavior(exchangeUsers)

	// Calculate relative pump time
	var relativePumpTime int64
	if !firstPumpTime.IsZero() && !pumpStartTime.IsZero() {
		relativePumpTime = firstPumpTime.Sub(pumpStartTime).Milliseconds()
		if relativePumpTime < 0 {
			relativePumpTime = 0
		}
	}

	return storage.ExchangeInsight{
		ExchangeName:       exchangeName,
		FirstPumpTime:      firstPumpTime,
		RelativePumpTime:   relativePumpTime,
		PumpEventCount:     len(exchangePumpEvents),
		MaxPriceChange:     maxPriceChange,
		AveragePriceChange: averagePriceChange,
		UserCount:          len(exchangeUsers),
		TotalVolume:        fmt.Sprintf("%.8f", totalVolume),
		AverageUserVolume:  fmt.Sprintf("%.8f", averageUserVolume),
		FastestUserTime:    fastestUserTime,
		UserBehavior:       userBehavior,
	}
}

// calculateUserBehavior analyzes user behavior patterns for an exchange
func (pa *PumpAnalyzer) calculateUserBehavior(users []storage.UserAnalysis) storage.ExchangeUserBehavior {
	if len(users) == 0 {
		return storage.ExchangeUserBehavior{}
	}

	var totalTrades int
	var totalVolume float64
	var entryTimes []int64
	var earlyTraders, regularTraders, lateTraders, highVolumeTraders int

	for _, user := range users {
		totalTrades += user.TradeCount

		if volume, err := strconv.ParseFloat(user.TotalUSDTVolume, 64); err == nil {
			totalVolume += volume

			// High volume trader (>1000 USDT)
			if volume > 1000 {
				highVolumeTraders++
			}
		}

		entryTimes = append(entryTimes, user.RelativeTimeMs)

		// Categorize traders by entry time
		if user.RelativeTimeMs <= 1000 { // First 1 second
			earlyTraders++
		} else if user.RelativeTimeMs <= 5000 { // 1-5 seconds
			regularTraders++
		} else { // After 5 seconds
			lateTraders++
		}
	}

	// Calculate averages
	averageTradesPerUser := float64(totalTrades) / float64(len(users))
	averageVolumePerUser := totalVolume / float64(len(users))

	// Calculate median entry time
	sort.Slice(entryTimes, func(i, j int) bool { return entryTimes[i] < entryTimes[j] })
	var medianEntryTime int64
	if len(entryTimes) > 0 {
		if len(entryTimes)%2 == 0 {
			medianEntryTime = (entryTimes[len(entryTimes)/2-1] + entryTimes[len(entryTimes)/2]) / 2
		} else {
			medianEntryTime = entryTimes[len(entryTimes)/2]
		}
	}

	return storage.ExchangeUserBehavior{
		AverageTradesPerUser: averageTradesPerUser,
		AverageVolumePerUser: fmt.Sprintf("%.8f", averageVolumePerUser),
		MedianEntryTime:      medianEntryTime,
		EarlyTraders:         earlyTraders,
		RegularTraders:       regularTraders,
		LateTraders:          lateTraders,
		HighVolumeTraders:    highVolumeTraders,
	}
}

// analyzeExchangeComparison creates comparison statistics between exchanges
func (pa *PumpAnalyzer) analyzeExchangeComparison(insights []storage.ExchangeInsight, pumpEvents []storage.PumpEvent) storage.ExchangeComparison {
	if len(insights) == 0 {
		return storage.ExchangeComparison{}
	}

	// Sort insights by first pump time to determine ranking
	sort.Slice(insights, func(i, j int) bool {
		// Handle zero times (no pumps) - put them at the end
		if insights[i].FirstPumpTime.IsZero() {
			return false
		}
		if insights[j].FirstPumpTime.IsZero() {
			return true
		}
		return insights[i].FirstPumpTime.Before(insights[j].FirstPumpTime)
	})

	// Find fastest and slowest exchanges (with pumps)
	var fastestExchange, slowestExchange string
	var fastestTime, slowestTime time.Time

	for _, insight := range insights {
		if !insight.FirstPumpTime.IsZero() {
			if fastestExchange == "" {
				fastestExchange = insight.ExchangeName
				fastestTime = insight.FirstPumpTime
			}
			slowestExchange = insight.ExchangeName
			slowestTime = insight.FirstPumpTime
		}
	}

	// Find most active exchange (most users)
	var mostActiveExchange string
	var maxUsers int
	for _, insight := range insights {
		if insight.UserCount > maxUsers {
			maxUsers = insight.UserCount
			mostActiveExchange = insight.ExchangeName
		}
	}

	// Find highest volume exchange
	var highestVolumeExchange string
	var maxVolume float64
	for _, insight := range insights {
		if volume, err := strconv.ParseFloat(insight.TotalVolume, 64); err == nil {
			if volume > maxVolume {
				maxVolume = volume
				highestVolumeExchange = insight.ExchangeName
			}
		}
	}

	// Calculate reaction time spread
	var reactionTimeSpread int64
	if !fastestTime.IsZero() && !slowestTime.IsZero() {
		reactionTimeSpread = slowestTime.Sub(fastestTime).Milliseconds()
	}

	// Create exchange ranking
	var exchangeRanking []string
	for _, insight := range insights {
		if !insight.FirstPumpTime.IsZero() {
			exchangeRanking = append(exchangeRanking, insight.ExchangeName)
		}
	}

	return storage.ExchangeComparison{
		FastestExchange:       fastestExchange,
		SlowestExchange:       slowestExchange,
		MostActiveExchange:    mostActiveExchange,
		HighestVolumeExchange: highestVolumeExchange,
		ReactionTimeSpread:    reactionTimeSpread,
		ExchangeRanking:       exchangeRanking,
	}
}
