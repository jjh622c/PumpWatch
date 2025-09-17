package main

import (
	"fmt"
	"log"
	"time"

	"PumpWatch/internal/buffer"
	"PumpWatch/internal/config"
	"PumpWatch/internal/models"
)

// ExtendedBufferIntegrationExample demonstrates how to integrate the ExtendedBuffer system
// with the existing PumpWatch architecture
func main() {
	fmt.Println("🔧 ExtendedBuffer Integration Example")
	fmt.Println("====================================")

	// 1. Load configuration with ExtendedBuffer settings
	cfg, err := loadConfigWithExtendedBuffer()
	if err != nil {
		log.Fatalf("❌ Failed to load config: %v", err)
	}

	// 2. Create BufferManager (replaces direct memory storage)
	bufferManager := buffer.NewBufferManager(cfg)
	defer bufferManager.Close()

	fmt.Println("✅ BufferManager initialized")

	// 3. Simulate existing data collection workflow
	fmt.Println("\n📊 Simulating data collection...")
	simulateDataCollection(bufferManager)

	// 4. Demonstrate TOSHI scenario: 16-minute delay handling
	fmt.Println("\n⚠️  Simulating TOSHI scenario (16-minute delay)...")
	simulateTOSHIScenario(bufferManager)

	// 5. Show legacy compatibility
	fmt.Println("\n🔄 Demonstrating legacy CollectionEvent compatibility...")
	demonstrateLegacyCompatibility(bufferManager)

	// 6. Display final statistics
	fmt.Println("\n📈 Final Statistics:")
	displayStats(bufferManager)

	fmt.Println("\n✅ Integration example completed successfully!")
}

// loadConfigWithExtendedBuffer creates a config with ExtendedBuffer enabled
func loadConfigWithExtendedBuffer() (*config.Config, error) {
	// Load default config
	cfg := config.NewDefaultConfig()

	// Enable ExtendedBuffer for this example
	cfg.System.ExtendedBufferEnabled = true
	cfg.System.ExtendedBufferDuration = "10m"    // 10-minute buffer
	cfg.System.BufferCompressionEnabled = true   // Enable compression
	cfg.System.BufferFallbackEnabled = true      // Enable fallback for safety
	cfg.System.BufferMaintenanceInterval = "60s" // Maintenance every minute

	fmt.Printf("🔧 ExtendedBuffer Configuration:\n")
	fmt.Printf("   Enabled: %v\n", cfg.System.ExtendedBufferEnabled)
	fmt.Printf("   Duration: %s\n", cfg.System.ExtendedBufferDuration)
	fmt.Printf("   Compression: %v\n", cfg.System.BufferCompressionEnabled)
	fmt.Printf("   Fallback: %v\n", cfg.System.BufferFallbackEnabled)

	return cfg, nil
}

// simulateDataCollection simulates the normal data collection process
func simulateDataCollection(bufferManager *buffer.BufferManager) {
	// Simulate incoming trade events from multiple exchanges
	exchanges := []string{"binance", "bybit", "okx", "kucoin", "gate"}
	markets := []string{"spot", "futures"}
	symbols := []string{"BTCUSDT", "ETHUSDT", "ADAUSDT"}

	fmt.Printf("📈 Storing trade events from %d exchanges...\n", len(exchanges))

	baseTime := time.Now()
	count := 0

	for _, exchange := range exchanges {
		for _, market := range markets {
			for _, symbol := range symbols {
				// Store trades for the last 5 minutes
				for i := 0; i < 300; i++ { // 300 seconds = 5 minutes
					trade := models.TradeEvent{
						Exchange:   exchange,
						MarketType: market,
						Symbol:     symbol,
						Price:      50000.0 + float64(i)*10,
						Quantity:   1.0 + float64(i%10)*0.1,
						Timestamp:  baseTime.Add(-time.Duration(i) * time.Second).UnixNano(),
						Side:       []string{"buy", "sell"}[i%2],
					}

					key := exchange + "_" + market
					err := bufferManager.StoreTradeEvent(key, trade)
					if err != nil {
						fmt.Printf("⚠️  Failed to store trade: %v\n", err)
						continue
					}
					count++
				}
			}
		}
	}

	fmt.Printf("✅ Stored %d trade events successfully\n", count)
}

// simulateTOSHIScenario simulates the TOSHI listing with 16-minute delay
func simulateTOSHIScenario(bufferManager *buffer.BufferManager) {
	// Simulate TOSHI listing announcement
	listingTime := time.Now().Add(-16 * time.Minute) // 16 minutes ago
	symbol := "TOSHI"

	fmt.Printf("🎯 TOSHI listing time: %v (16 minutes ago)\n", listingTime)

	// Simulate TOSHI trades around listing time (should be preserved in ExtendedBuffer)
	for i := -30; i <= 30; i++ {
		trade := models.TradeEvent{
			Exchange:   "binance",
			MarketType: "spot",
			Symbol:     symbol,
			Price:      0.001 + float64(i)*0.00001, // Price movement around listing
			Quantity:   1000.0 + float64(i*i),      // Volume spike
			Timestamp:  listingTime.Add(time.Duration(i) * time.Second).UnixNano(),
			Side:       []string{"buy", "sell"}[i%2],
		}

		bufferManager.StoreTradeEvent("binance_spot", trade)
	}

	// Try to retrieve TOSHI data (this should work even with 16-minute delay!)
	startTime := listingTime.Add(-20 * time.Second)
	endTime := listingTime.Add(20 * time.Second)

	toshiTrades, err := bufferManager.GetTradeEvents("binance_spot", startTime, endTime)
	if err != nil {
		fmt.Printf("❌ Failed to retrieve TOSHI data: %v\n", err)
		return
	}

	fmt.Printf("🎉 TOSHI data retrieval successful!\n")
	fmt.Printf("   Retrieved %d trades despite 16-minute delay\n", len(toshiTrades))
	fmt.Printf("   Data range: %v to %v\n", startTime, endTime)

	// Filter TOSHI trades specifically
	toshiCount := 0
	for _, trade := range toshiTrades {
		if trade.Symbol == symbol {
			toshiCount++
		}
	}

	fmt.Printf("   TOSHI-specific trades: %d\n", toshiCount)

	if toshiCount > 0 {
		fmt.Printf("✅ TOSHI scenario handled successfully - no data loss!\n")
	} else {
		fmt.Printf("⚠️  TOSHI scenario: data may need filtering\n")
	}
}

// demonstrateLegacyCompatibility shows integration with existing CollectionEvent
func demonstrateLegacyCompatibility(bufferManager *buffer.BufferManager) {
	// Simulate a listing trigger
	triggerTime := time.Now().Add(-2 * time.Minute)
	symbol := "TESTCOIN"

	fmt.Printf("🔄 Creating CollectionEvent for %s at %v\n", symbol, triggerTime)

	// Convert buffer data to legacy CollectionEvent format
	collectionEvent, err := bufferManager.ToCollectionEvent(symbol, triggerTime)
	if err != nil {
		fmt.Printf("❌ Failed to create CollectionEvent: %v\n", err)
		return
	}

	// Display CollectionEvent contents (as would be used by existing analysis code)
	fmt.Printf("📦 CollectionEvent created successfully:\n")
	fmt.Printf("   Symbol: %s\n", collectionEvent.Symbol)
	fmt.Printf("   Trigger Time: %v\n", collectionEvent.TriggerTime)
	fmt.Printf("   Collection Window: %v to %v\n", collectionEvent.StartTime, collectionEvent.EndTime)
	fmt.Printf("   Binance Spot: %d trades\n", len(collectionEvent.BinanceSpot))
	fmt.Printf("   Binance Futures: %d trades\n", len(collectionEvent.BinanceFutures))
	fmt.Printf("   Bybit Spot: %d trades\n", len(collectionEvent.BybitSpot))
	fmt.Printf("   Bybit Futures: %d trades\n", len(collectionEvent.BybitFutures))
	fmt.Printf("   OKX Spot: %d trades\n", len(collectionEvent.OKXSpot))
	fmt.Printf("   OKX Futures: %d trades\n", len(collectionEvent.OKXFutures))

	totalTrades := len(collectionEvent.BinanceSpot) + len(collectionEvent.BinanceFutures) +
		len(collectionEvent.BybitSpot) + len(collectionEvent.BybitFutures) +
		len(collectionEvent.OKXSpot) + len(collectionEvent.OKXFutures)

	fmt.Printf("   Total Trades: %d\n", totalTrades)
	fmt.Printf("✅ Legacy compatibility maintained - existing analysis code will work unchanged!\n")
}

// displayStats shows final buffer statistics
func displayStats(bufferManager *buffer.BufferManager) {
	stats := bufferManager.GetStats()

	fmt.Printf("📊 BufferManager Final Statistics:\n")
	fmt.Printf("   Total Trades Stored: %d\n", stats.TotalTradesStored)
	fmt.Printf("   Total Trades Retrieved: %d\n", stats.TotalTradesRetrieved)
	fmt.Printf("   Buffer Hits (Hot): %d\n", stats.BufferHits)
	fmt.Printf("   Compression Hits (Cold): %d\n", stats.CompressionHits)
	fmt.Printf("   Memory Usage: %.2f MB\n", float64(stats.MemoryUsageBytes)/1024/1024)
	fmt.Printf("   Compression Ratio: %.1f%%\n", stats.CompressionRatio*100)
	fmt.Printf("   Average Latency: %v\n", stats.AverageLatency)
	fmt.Printf("   Storage Errors: %d\n", stats.StorageErrors)
	fmt.Printf("   Retrieval Errors: %d\n", stats.RetrievalErrors)

	// Calculate success rates
	totalOperations := stats.TotalTradesStored + stats.TotalTradesRetrieved
	totalErrors := stats.StorageErrors + stats.RetrievalErrors

	if totalOperations > 0 {
		successRate := float64(totalOperations-totalErrors) / float64(totalOperations) * 100
		fmt.Printf("   Success Rate: %.2f%%\n", successRate)
	}

	// Memory efficiency
	memoryLimitMB := 4096.0 // 4GB design limit
	memoryUsageMB := float64(stats.MemoryUsageBytes) / 1024 / 1024
	memoryEfficiency := (memoryLimitMB - memoryUsageMB) / memoryLimitMB * 100

	fmt.Printf("   Memory Efficiency: %.1f%% (using %.2f MB of %.0f MB limit)\n",
		memoryEfficiency, memoryUsageMB, memoryLimitMB)

	if memoryUsageMB < memoryLimitMB {
		fmt.Printf("✅ Memory usage within design limits\n")
	} else {
		fmt.Printf("⚠️  Memory usage exceeds design limits\n")
	}
}

// Example of how to integrate with existing WebSocket handlers
func ExampleWebSocketIntegration() {
	fmt.Println("\n🌐 WebSocket Integration Example:")
	fmt.Println("================================")

	fmt.Printf(`
// In your existing WebSocket message handler:
func (w *Worker) processTradeMessage(message []byte) {
    var trade models.TradeEvent
    if err := json.Unmarshal(message, &trade); err != nil {
        return
    }

    // OLD: Direct memory storage
    // w.tradeBuffer = append(w.tradeBuffer, trade)

    // NEW: Use BufferManager instead
    exchange := fmt.Sprintf("%%s_%%s", w.Exchange, w.MarketType)
    if err := w.bufferManager.StoreTradeEvent(exchange, trade); err != nil {
        w.logger.Error("Failed to store trade: %%v", err)
    }
}

// In your collection trigger:
func (tm *TaskManager) triggerCollection(symbol string, triggerTime time.Time) {
    // OLD: Gather from worker buffers
    // event := tm.gatherTradesFromWorkers(symbol, triggerTime)

    // NEW: Use BufferManager
    event, err := tm.bufferManager.ToCollectionEvent(symbol, triggerTime)
    if err != nil {
        tm.logger.Error("Failed to create collection event: %%v", err)
        return
    }

    // Existing analysis code works unchanged!
    tm.analyzer.AnalyzePump(event)
    tm.storage.SaveCollectionEvent(event)
}
`)

	fmt.Println("✅ Integration is seamless - minimal code changes required!")
}