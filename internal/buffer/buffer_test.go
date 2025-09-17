package buffer

import (
	"fmt"
	"math/rand"
	"runtime"
	"strconv"
	"testing"
	"time"

	"PumpWatch/internal/models"
)

// TestExtendedBufferMemoryUsage tests memory usage under realistic conditions
func TestExtendedBufferMemoryUsage(t *testing.T) {
	// Test parameters based on design document
	bufferDuration := 10 * time.Minute

	// Create buffer
	buffer := NewExtendedBuffer(bufferDuration)
	defer buffer.Close()

	// Record initial memory
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	initialMem := m1.Alloc

	// Simulate 10 minutes of trading data (5000 trades/second)
	simulateRealisticTrading(buffer, bufferDuration, 5000)

	// Force GC and measure memory
	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	finalMem := m2.Alloc

	memoryUsed := finalMem - initialMem
	memoryUsedMB := float64(memoryUsed) / 1024 / 1024

	// Get buffer stats
	stats := buffer.GetStats()

	t.Logf("🔍 ExtendedBuffer Memory Usage Test Results:")
	t.Logf("   Buffer Duration: %v", bufferDuration)
	t.Logf("   Total Events: %d", stats.TotalEvents)
	t.Logf("   Hot Events: %d", stats.HotEvents)
	t.Logf("   Cold Events: %d", stats.ColdEvents)
	t.Logf("   Memory Used: %.2f MB", memoryUsedMB)
	t.Logf("   Memory Usage (Buffer Stats): %.2f MB", float64(stats.MemoryUsage)/1024/1024)
	t.Logf("   Compression Rate: %.1f%%", stats.CompressionRate*100)

	// Verify memory usage is within expected limits (4GB as per design)
	maxExpectedMB := 4096.0 // 4GB
	if memoryUsedMB > maxExpectedMB {
		t.Errorf("❌ Memory usage %.2f MB exceeds expected limit of %.2f MB", memoryUsedMB, maxExpectedMB)
	} else {
		t.Logf("✅ Memory usage within expected limits (%.2f MB < %.2f MB)", memoryUsedMB, maxExpectedMB)
	}

	// Verify compression effectiveness
	if stats.CompressionRate > 0 && stats.CompressionRate < 0.8 {
		t.Logf("✅ Good compression achieved: %.1f%%", stats.CompressionRate*100)
	} else if stats.ColdEvents == 0 {
		t.Logf("ℹ️  No compression needed - all data in hot buffer")
	} else {
		t.Logf("⚠️  Compression rate may need optimization: %.1f%%", stats.CompressionRate*100)
	}
}

// TestBufferPerformance tests hot and cold buffer access performance
func TestBufferPerformance(t *testing.T) {
	buffer := NewExtendedBuffer(10 * time.Minute)
	defer buffer.Close()

	// Store test data
	exchange := "binance_spot"
	baseTime := time.Now()

	// Store data spanning 5 minutes (some in hot, some in cold)
	for i := 0; i < 10000; i++ {
		trade := models.TradeEvent{
			Exchange:   "binance",
			MarketType: "spot",
			Symbol:     "BTCUSDT",
			Price:      fmt.Sprintf("%.2f", 50000.0 + rand.Float64()*1000),
			Quantity:   fmt.Sprintf("%.6f", rand.Float64() * 10),
			// 🔧 BUG FIX: 나노초를 밀리초로 변환 (실제 시스템과 일치)
			Timestamp:  baseTime.Add(-time.Duration(i) * time.Second).UnixNano() / 1e6,
			Side:       []string{"buy", "sell"}[rand.Intn(2)],
		}
		buffer.StoreTradeEvent(exchange, trade)
	}

	// Test hot buffer access (recent 2 minutes)
	hotStartTime := time.Now().Add(-2 * time.Minute)
	hotEndTime := time.Now()

	start := time.Now()
	hotTrades, err := buffer.GetTradeEvents(exchange, hotStartTime, hotEndTime)
	hotLatency := time.Since(start)

	if err != nil {
		t.Fatalf("❌ Hot buffer access failed: %v", err)
	}

	t.Logf("🔥 Hot Buffer Performance:")
	t.Logf("   Trades Retrieved: %d", len(hotTrades))
	t.Logf("   Access Latency: %v", hotLatency)

	// Verify hot buffer latency (should be < 1ms as per design)
	if hotLatency > time.Millisecond {
		t.Errorf("❌ Hot buffer latency %v exceeds 1ms threshold", hotLatency)
	} else {
		t.Logf("✅ Hot buffer latency within expected limits")
	}

	// Test cold buffer access (older data)
	coldStartTime := baseTime.Add(-5 * time.Minute)
	coldEndTime := baseTime.Add(-3 * time.Minute)

	start = time.Now()
	coldTrades, err := buffer.GetTradeEvents(exchange, coldStartTime, coldEndTime)
	coldLatency := time.Since(start)

	if err != nil {
		t.Fatalf("❌ Cold buffer access failed: %v", err)
	}

	t.Logf("❄️  Cold Buffer Performance:")
	t.Logf("   Trades Retrieved: %d", len(coldTrades))
	t.Logf("   Access Latency: %v", coldLatency)

	// Verify cold buffer latency (should be < 10ms as per design)
	if coldLatency > 10*time.Millisecond {
		t.Errorf("❌ Cold buffer latency %v exceeds 10ms threshold", coldLatency)
	} else {
		t.Logf("✅ Cold buffer latency within expected limits")
	}
}

// TestCompressionRatio tests the compression effectiveness
func TestCompressionRatio(t *testing.T) {
	compressor := NewDeltaCompressor()

	// Generate realistic trading data
	trades := generateRealisticTrades(1000)

	// Compress data
	compressed, err := compressor.Compress(trades)
	if err != nil {
		t.Fatalf("❌ Compression failed: %v", err)
	}

	// Calculate compression ratio
	originalSize := len(trades) * 200 // Approximate 200 bytes per trade
	compressedSize := len(compressed)
	compressionRatio := float64(compressedSize) / float64(originalSize)

	t.Logf("🗜️  Compression Test Results:")
	t.Logf("   Original Trades: %d", len(trades))
	t.Logf("   Original Size: %d bytes (%.2f KB)", originalSize, float64(originalSize)/1024)
	t.Logf("   Compressed Size: %d bytes (%.2f KB)", compressedSize, float64(compressedSize)/1024)
	t.Logf("   Compression Ratio: %.1f%%", compressionRatio*100)
	t.Logf("   Space Savings: %.1f%%", (1-compressionRatio)*100)

	// Verify compression effectiveness (should achieve ~70% as per design)
	if compressionRatio > 0.8 {
		t.Errorf("❌ Compression ratio %.1f%% is too high (should be ~70%%)", compressionRatio*100)
	} else {
		t.Logf("✅ Good compression achieved")
	}

	// Test decompression
	decompressed, err := compressor.Decompress(compressed)
	if err != nil {
		t.Fatalf("❌ Decompression failed: %v", err)
	}

	// Verify data integrity
	if len(decompressed) != len(trades) {
		t.Errorf("❌ Decompressed data count mismatch: got %d, want %d", len(decompressed), len(trades))
	}

	// Spot check a few trades
	for i := 0; i < min(10, len(trades)); i++ {
		original := trades[i]
		recovered := decompressed[i]

		originalPrice, _ := strconv.ParseFloat(original.Price, 64)
		recoveredPrice, _ := strconv.ParseFloat(recovered.Price, 64)
		if abs(originalPrice-recoveredPrice) > 0.01 {
			t.Errorf("❌ Price mismatch at index %d: got %.4f, want %.4f", i, recoveredPrice, originalPrice)
		}
		originalQty, _ := strconv.ParseFloat(original.Quantity, 64)
		recoveredQty, _ := strconv.ParseFloat(recovered.Quantity, 64)
		if abs(originalQty-recoveredQty) > 0.000001 {
			t.Errorf("❌ Quantity mismatch at index %d: got %.6f, want %.6f", i, recoveredQty, originalQty)
		}
		if original.Symbol != recovered.Symbol {
			t.Errorf("❌ Symbol mismatch at index %d: got %s, want %s", i, recovered.Symbol, original.Symbol)
		}
	}

	t.Logf("✅ Data integrity verified")
}

// TestCollectionEventCompatibility tests integration with legacy CollectionEvent
func TestCollectionEventCompatibility(t *testing.T) {
	buffer := NewExtendedBuffer(5 * time.Minute)
	defer buffer.Close()

	// Store test data for multiple exchanges
	baseTime := time.Now()
	triggerTime := baseTime.Add(-1 * time.Minute)

	exchanges := []string{"binance", "bybit", "okx"}
	markets := []string{"spot", "futures"}

	for _, exchange := range exchanges {
		for _, market := range markets {
			key := exchange + "_" + market

			// Generate trades around trigger time (-20s to +20s)
			for i := -25; i <= 25; i++ {
				trade := models.TradeEvent{
					Exchange:   exchange,
					MarketType: market,
					Symbol:     "BTCUSDT",
					Price:      fmt.Sprintf("%.2f", 50000.0 + float64(i)*10),
					Quantity:   fmt.Sprintf("%.6f", 1.0 + rand.Float64()),
					// 🔧 BUG FIX: 나노초를 밀리초로 변환 (실제 시스템과 일치)
					Timestamp:  triggerTime.Add(time.Duration(i) * time.Second).UnixNano() / 1e6,
					Side:       []string{"buy", "sell"}[rand.Intn(2)],
				}
				buffer.StoreTradeEvent(key, trade)
			}
		}
	}

	// Convert to CollectionEvent
	collectionEvent, err := buffer.ToCollectionEvent("BTCUSDT", triggerTime)
	if err != nil {
		t.Fatalf("❌ CollectionEvent conversion failed: %v", err)
	}

	t.Logf("🔄 CollectionEvent Compatibility Test:")
	t.Logf("   Symbol: %s", collectionEvent.Symbol)
	t.Logf("   Trigger Time: %v", collectionEvent.TriggerTime)
	t.Logf("   Binance Spot Trades: %d", len(collectionEvent.BinanceSpot))
	t.Logf("   Binance Futures Trades: %d", len(collectionEvent.BinanceFutures))
	t.Logf("   Bybit Spot Trades: %d", len(collectionEvent.BybitSpot))
	t.Logf("   Bybit Futures Trades: %d", len(collectionEvent.BybitFutures))
	t.Logf("   OKX Spot Trades: %d", len(collectionEvent.OKXSpot))
	t.Logf("   OKX Futures Trades: %d", len(collectionEvent.OKXFutures))

	// Verify data was collected properly
	totalTrades := len(collectionEvent.BinanceSpot) + len(collectionEvent.BinanceFutures) +
		len(collectionEvent.BybitSpot) + len(collectionEvent.BybitFutures) +
		len(collectionEvent.OKXSpot) + len(collectionEvent.OKXFutures)

	expectedTrades := float64(40 * 6) // 40 seconds * 6 exchange-market combinations
	if float64(totalTrades) < expectedTrades*0.8 { // Allow 20% tolerance
		t.Errorf("❌ Too few trades collected: got %d, expected ~%.0f", totalTrades, expectedTrades)
	} else {
		t.Logf("✅ CollectionEvent compatibility verified (%d trades collected)", totalTrades)
	}
}

// Benchmark tests
func BenchmarkHotBufferAccess(b *testing.B) {
	buffer := NewExtendedBuffer(10 * time.Minute)
	defer buffer.Close()

	// Pre-populate with data
	populateBuffer(buffer, "binance_spot", 10000)

	startTime := time.Now().Add(-1 * time.Minute)
	endTime := time.Now()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := buffer.GetTradeEvents("binance_spot", startTime, endTime)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkColdBufferAccess(b *testing.B) {
	buffer := NewExtendedBuffer(10 * time.Minute)
	defer buffer.Close()

	// Pre-populate with data
	populateBuffer(buffer, "binance_spot", 50000)

	// Wait for data to move to cold buffer
	time.Sleep(3 * time.Minute)

	startTime := time.Now().Add(-5 * time.Minute)
	endTime := time.Now().Add(-3 * time.Minute)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := buffer.GetTradeEvents("binance_spot", startTime, endTime)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Helper functions
func simulateRealisticTrading(buffer *ExtendedBuffer, duration time.Duration, tradesPerSecond int) {
	baseTime := time.Now()
	totalTrades := int(duration.Seconds()) * tradesPerSecond

	exchanges := []string{"binance", "bybit", "okx", "kucoin", "gate", "phemex"}
	markets := []string{"spot", "futures"}
	symbols := []string{"BTCUSDT", "ETHUSDT", "ADAUSDT", "DOTUSDT", "LINKUSDT"}

	for i := 0; i < totalTrades; i++ {
		exchange := exchanges[rand.Intn(len(exchanges))]
		market := markets[rand.Intn(len(markets))]
		symbol := symbols[rand.Intn(len(symbols))]

		trade := models.TradeEvent{
			Exchange:   exchange,
			MarketType: market,
			Symbol:     symbol,
			Price:      fmt.Sprintf("%.2f", 1000.0 + rand.Float64()*50000),
			Quantity:   fmt.Sprintf("%.6f", rand.Float64() * 100),
			// 🔧 BUG FIX: 나노초를 밀리초로 변환 (실제 시스템과 일치)
			Timestamp:  baseTime.Add(-time.Duration(i) * time.Second / time.Duration(tradesPerSecond)).UnixNano() / 1e6,
			Side:       []string{"buy", "sell"}[rand.Intn(2)],
		}

		key := exchange + "_" + market
		buffer.StoreTradeEvent(key, trade)
	}
}

func generateRealisticTrades(count int) []models.TradeEvent {
	trades := make([]models.TradeEvent, count)
	baseTime := time.Now()
	basePrice := 50000.0

	for i := 0; i < count; i++ {
		trades[i] = models.TradeEvent{
			Exchange:   "binance",
			MarketType: "spot",
			Symbol:     "BTCUSDT",
			Price:      fmt.Sprintf("%.2f", basePrice + rand.Float64()*1000 - 500), // Price around basePrice ± 500
			Quantity:   fmt.Sprintf("%.6f", rand.Float64() * 10),
			// 🔧 BUG FIX: 나노초를 밀리초로 변환 (실제 시스템과 일치)
			Timestamp:  baseTime.Add(-time.Duration(i) * time.Second).UnixNano() / 1e6,
			Side:       []string{"buy", "sell"}[rand.Intn(2)],
		}
	}

	return trades
}

func populateBuffer(buffer *ExtendedBuffer, exchange string, count int) {
	baseTime := time.Now()

	for i := 0; i < count; i++ {
		trade := models.TradeEvent{
			Exchange:   "binance",
			MarketType: "spot",
			Symbol:     "BTCUSDT",
			Price:      fmt.Sprintf("%.2f", 50000.0 + rand.Float64()*1000),
			Quantity:   fmt.Sprintf("%.6f", rand.Float64() * 10),
			// 🔧 BUG FIX: 나노초를 밀리초로 변환 (실제 시스템과 일치)
			Timestamp:  baseTime.Add(-time.Duration(i) * time.Second).UnixNano() / 1e6,
			Side:       []string{"buy", "sell"}[rand.Intn(2)],
		}
		buffer.StoreTradeEvent(exchange, trade)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}