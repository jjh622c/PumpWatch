package performance

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"PumpWatch/internal/buffer"
	"PumpWatch/internal/database"
	"PumpWatch/internal/models"
)

// HybridBenchmark performs comprehensive performance comparison tests
type HybridBenchmark struct {
	// 테스트 대상들
	circularBuffer *buffer.CircularTradeBuffer
	questDB        *database.QuestDBManager

	// 테스트 설정
	config         BenchmarkConfig
	ctx            context.Context
	cancel         context.CancelFunc

	// 성능 메트릭
	metrics        *BenchmarkMetrics
	metricsLock    sync.RWMutex

	// 부하 생성
	loadGenerator  *LoadGenerator
}

// BenchmarkConfig holds benchmark configuration
type BenchmarkConfig struct {
	// 테스트 시나리오
	TestDuration     time.Duration `yaml:"test_duration"`      // 5m
	WarmupDuration   time.Duration `yaml:"warmup_duration"`    // 30s
	CooldownDuration time.Duration `yaml:"cooldown_duration"`  // 10s

	// 부하 설정
	BaseTradesPerSecond   int     `yaml:"base_trades_per_sec"`   // 5000
	PumpTradesPerSecond   int     `yaml:"pump_trades_per_sec"`   // 50000
	PumpDuration         time.Duration `yaml:"pump_duration"`    // 30s
	PumpInterval         time.Duration `yaml:"pump_interval"`    // 120s

	// 동시성 설정
	ProducerWorkers      int     `yaml:"producer_workers"`      // 10
	ConsumerWorkers      int     `yaml:"consumer_workers"`      // 5

	// 검증 설정
	DataConsistencyCheck bool    `yaml:"data_consistency_check"` // true
	MemoryProfileEnabled bool    `yaml:"memory_profile_enabled"` // true
}

// BenchmarkMetrics holds all performance metrics
type BenchmarkMetrics struct {
	// 테스트 정보
	StartTime      time.Time     `json:"start_time"`
	EndTime        time.Time     `json:"end_time"`
	Duration       time.Duration `json:"duration"`
	TestPhase      string        `json:"test_phase"` // warmup, normal, pump, cooldown

	// CircularBuffer 메트릭
	CircularBuffer CircularBufferMetrics `json:"circular_buffer"`

	// QuestDB 메트릭
	QuestDB        QuestDBMetrics        `json:"questdb"`

	// 비교 메트릭
	Comparison     ComparisonMetrics     `json:"comparison"`

	// 시스템 메트릭
	System         SystemMetrics         `json:"system"`
}

// CircularBufferMetrics holds CircularBuffer performance metrics
type CircularBufferMetrics struct {
	TotalWrites       int64         `json:"total_writes"`
	FailedWrites      int64         `json:"failed_writes"`
	AverageWriteTime  time.Duration `json:"average_write_time"`
	MaxWriteTime      time.Duration `json:"max_write_time"`
	WritesPerSecond   float64       `json:"writes_per_second"`

	TotalReads        int64         `json:"total_reads"`
	AverageReadTime   time.Duration `json:"average_read_time"`
	MaxReadTime       time.Duration `json:"max_read_time"`
	ReadsPerSecond    float64       `json:"reads_per_second"`
}

// QuestDBMetrics holds QuestDB performance metrics
type QuestDBMetrics struct {
	TotalWrites       int64         `json:"total_writes"`
	FailedWrites      int64         `json:"failed_writes"`
	DroppedWrites     int64         `json:"dropped_writes"`
	AverageWriteTime  time.Duration `json:"average_write_time"`
	MaxWriteTime      time.Duration `json:"max_write_time"`
	WritesPerSecond   float64       `json:"writes_per_second"`

	BatchesProcessed  int64         `json:"batches_processed"`
	AverageBatchSize  float64       `json:"average_batch_size"`
	BatchFlushTime    time.Duration `json:"batch_flush_time"`
}

// ComparisonMetrics holds comparison results
type ComparisonMetrics struct {
	PerformanceRatio    float64 `json:"performance_ratio"`    // QuestDB/CircularBuffer
	ErrorRateRatio      float64 `json:"error_rate_ratio"`
	DataConsistency     float64 `json:"data_consistency"`     // 0.0-1.0
	MemoryEfficiency    float64 `json:"memory_efficiency"`

	CircularBufferErrors int64  `json:"circular_buffer_errors"`
	QuestDBErrors        int64  `json:"questdb_errors"`
	ConsistencyMismatches int64 `json:"consistency_mismatches"`
}

// SystemMetrics holds system resource metrics
type SystemMetrics struct {
	CPUUsagePercent    float64 `json:"cpu_usage_percent"`
	MemoryUsageMB      int64   `json:"memory_usage_mb"`
	GoroutineCount     int     `json:"goroutine_count"`
	GCPauseTime        time.Duration `json:"gc_pause_time"`
}

// LoadGenerator generates realistic trading load
type LoadGenerator struct {
	config    BenchmarkConfig
	exchanges []string
	symbols   []string
	rand      *rand.Rand
}

// DefaultBenchmarkConfig returns default benchmark configuration
func DefaultBenchmarkConfig() BenchmarkConfig {
	return BenchmarkConfig{
		TestDuration:         5 * time.Minute,
		WarmupDuration:       30 * time.Second,
		CooldownDuration:     10 * time.Second,
		BaseTradesPerSecond:  5000,
		PumpTradesPerSecond:  50000,
		PumpDuration:         30 * time.Second,
		PumpInterval:         120 * time.Second,
		ProducerWorkers:      10,
		ConsumerWorkers:      5,
		DataConsistencyCheck: true,
		MemoryProfileEnabled: true,
	}
}

// NewHybridBenchmark creates a new hybrid benchmark
func NewHybridBenchmark(
	circularBuffer *buffer.CircularTradeBuffer,
	questDB *database.QuestDBManager,
	config BenchmarkConfig) *HybridBenchmark {

	ctx, cancel := context.WithCancel(context.Background())

	// Load generator 초기화
	loadGen := &LoadGenerator{
		config: config,
		exchanges: []string{"binance", "bybit", "okx", "kucoin", "gate", "phemex"},
		symbols: []string{"BTCUSDT", "ETHUSDT", "ADAUSDT", "SOLUSDT", "DOTUSDT"},
		rand: rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	return &HybridBenchmark{
		circularBuffer: circularBuffer,
		questDB:        questDB,
		config:         config,
		ctx:            ctx,
		cancel:         cancel,
		metrics: &BenchmarkMetrics{
			StartTime: time.Now(),
			TestPhase: "initializing",
		},
		loadGenerator: loadGen,
	}
}

// RunBenchmark executes the full benchmark suite
func (hb *HybridBenchmark) RunBenchmark() (*BenchmarkMetrics, error) {
	log.Printf("🚀 Starting Hybrid Benchmark Suite")
	log.Printf("📋 Config: %d base TPS, %d pump TPS, %v duration",
		hb.config.BaseTradesPerSecond, hb.config.PumpTradesPerSecond, hb.config.TestDuration)

	hb.metrics.StartTime = time.Now()

	// 1. Warmup Phase
	if err := hb.runPhase("warmup", hb.config.WarmupDuration, hb.config.BaseTradesPerSecond); err != nil {
		return nil, fmt.Errorf("warmup failed: %w", err)
	}

	// 2. Main Test Phase with Pump Simulation
	if err := hb.runMainTest(); err != nil {
		return nil, fmt.Errorf("main test failed: %w", err)
	}

	// 3. Cooldown Phase
	if err := hb.runPhase("cooldown", hb.config.CooldownDuration, hb.config.BaseTradesPerSecond/2); err != nil {
		return nil, fmt.Errorf("cooldown failed: %w", err)
	}

	// 4. Final Analysis
	hb.performFinalAnalysis()

	hb.metrics.EndTime = time.Now()
	hb.metrics.Duration = hb.metrics.EndTime.Sub(hb.metrics.StartTime)
	hb.metrics.TestPhase = "completed"

	return hb.metrics, nil
}

// runMainTest executes main test with pump simulation
func (hb *HybridBenchmark) runMainTest() error {
	log.Printf("📊 Starting main test phase (%v)", hb.config.TestDuration)

	endTime := time.Now().Add(hb.config.TestDuration)
	nextPumpTime := time.Now().Add(hb.config.PumpInterval)

	for time.Now().Before(endTime) {
		currentTPS := hb.config.BaseTradesPerSecond

		// Pump 시뮬레이션
		if time.Now().After(nextPumpTime) {
			log.Printf("💥 PUMP EVENT SIMULATION: %d → %d TPS for %v",
				currentTPS, hb.config.PumpTradesPerSecond, hb.config.PumpDuration)

			// Pump phase
			if err := hb.runPhase("pump", hb.config.PumpDuration, hb.config.PumpTradesPerSecond); err != nil {
				log.Printf("⚠️ Pump simulation error: %v", err)
			}

			nextPumpTime = time.Now().Add(hb.config.PumpInterval)
		} else {
			// Normal phase
			if err := hb.runPhase("normal", 10*time.Second, currentTPS); err != nil {
				log.Printf("⚠️ Normal phase error: %v", err)
			}
		}

		// 중간 통계 출력
		hb.printIntermediateStats()
	}

	return nil
}

// runPhase executes a test phase with specified parameters
func (hb *HybridBenchmark) runPhase(phase string, duration time.Duration, targetTPS int) error {
	log.Printf("🔄 Phase: %s (TPS: %d, Duration: %v)", phase, targetTPS, duration)

	hb.metricsLock.Lock()
	hb.metrics.TestPhase = phase
	hb.metricsLock.Unlock()

	// Producer-Consumer 패턴으로 부하 생성
	tradeChannel := make(chan models.TradeEvent, 10000)
	var wg sync.WaitGroup

	// 부하 생성 워커들
	for i := 0; i < hb.config.ProducerWorkers; i++ {
		wg.Add(1)
		go hb.loadProducerWorker(i, tradeChannel, &wg, targetTPS, duration)
	}

	// 데이터 처리 워커들
	for i := 0; i < hb.config.ConsumerWorkers; i++ {
		wg.Add(1)
		go hb.loadConsumerWorker(i, tradeChannel, &wg, duration)
	}

	// 모든 워커 완료 대기
	wg.Wait()
	close(tradeChannel)

	log.Printf("✅ Phase %s completed", phase)
	return nil
}

// loadProducerWorker generates trades at specified rate
func (hb *HybridBenchmark) loadProducerWorker(workerID int, tradeChannel chan<- models.TradeEvent, wg *sync.WaitGroup, targetTPS int, duration time.Duration) {
	defer wg.Done()

	endTime := time.Now().Add(duration)
	tradesPerWorker := targetTPS / hb.config.ProducerWorkers
	interval := time.Second / time.Duration(tradesPerWorker)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for time.Now().Before(endTime) {
		select {
		case <-hb.ctx.Done():
			return
		case <-ticker.C:
			trade := hb.loadGenerator.GenerateRealisticTrade()

			select {
			case tradeChannel <- trade:
			default:
				// 채널이 가득 찬 경우 스킵
				atomic.AddInt64(&hb.metrics.Comparison.CircularBufferErrors, 1)
			}
		}
	}
}

// loadConsumerWorker processes trades from channel
func (hb *HybridBenchmark) loadConsumerWorker(workerID int, tradeChannel <-chan models.TradeEvent, wg *sync.WaitGroup, duration time.Duration) {
	defer wg.Done()

	endTime := time.Now().Add(duration)

	for time.Now().Before(endTime) {
		select {
		case <-hb.ctx.Done():
			return
		case trade, ok := <-tradeChannel:
			if !ok {
				return
			}

			// 동시에 양쪽 시스템에 저장하고 성능 측정
			hb.processTradeBenchmark(trade)
		}
	}
}

// processTradeBenchmark processes trade on both systems and measures performance
func (hb *HybridBenchmark) processTradeBenchmark(trade models.TradeEvent) {
	// CircularBuffer 성능 측정
	start := time.Now()
	exchangeKey := fmt.Sprintf("%s_%s", trade.Exchange, trade.MarketType)

	if err := hb.circularBuffer.StoreTradeEvent(exchangeKey, trade); err != nil {
		atomic.AddInt64(&hb.metrics.CircularBuffer.FailedWrites, 1)
	} else {
		atomic.AddInt64(&hb.metrics.CircularBuffer.TotalWrites, 1)
	}

	circularTime := time.Since(start)
	hb.updateCircularBufferMetrics(circularTime)

	// QuestDB 성능 측정
	start = time.Now()

	if success := hb.questDB.AddTrade(trade); !success {
		atomic.AddInt64(&hb.metrics.QuestDB.DroppedWrites, 1)
	} else {
		atomic.AddInt64(&hb.metrics.QuestDB.TotalWrites, 1)
	}

	questdbTime := time.Since(start)
	hb.updateQuestDBMetrics(questdbTime)
}

// GenerateRealisticTrade creates a realistic trade event
func (lg *LoadGenerator) GenerateRealisticTrade() models.TradeEvent {
	exchange := lg.exchanges[lg.rand.Intn(len(lg.exchanges))]
	symbol := lg.symbols[lg.rand.Intn(len(lg.symbols))]
	marketType := []string{"spot", "futures"}[lg.rand.Intn(2)]
	side := []string{"buy", "sell"}[lg.rand.Intn(2)]

	// 현실적인 가격 및 수량 생성
	basePrice := 50000.0 // BTC 기준
	price := basePrice * (0.98 + lg.rand.Float64()*0.04) // ±2% 변동
	quantity := 0.01 + lg.rand.Float64()*10.0 // 0.01-10.01

	return models.TradeEvent{
		Exchange:     exchange,
		MarketType:   marketType,
		Symbol:       symbol,
		TradeID:      fmt.Sprintf("%s_%d", exchange, time.Now().UnixNano()),
		Price:        fmt.Sprintf("%.2f", price),
		Quantity:     fmt.Sprintf("%.8f", quantity),
		Side:         side,
		Timestamp:    time.Now().UnixMilli(),
		CollectedAt:  time.Now().UnixMilli(),
	}
}

// updateCircularBufferMetrics updates CircularBuffer performance metrics
func (hb *HybridBenchmark) updateCircularBufferMetrics(latency time.Duration) {
	hb.metricsLock.Lock()
	defer hb.metricsLock.Unlock()

	// 평균 응답시간 업데이트 (단순 이동 평균)
	if hb.metrics.CircularBuffer.AverageWriteTime == 0 {
		hb.metrics.CircularBuffer.AverageWriteTime = latency
	} else {
		hb.metrics.CircularBuffer.AverageWriteTime =
			(hb.metrics.CircularBuffer.AverageWriteTime + latency) / 2
	}

	// 최대 응답시간 업데이트
	if latency > hb.metrics.CircularBuffer.MaxWriteTime {
		hb.metrics.CircularBuffer.MaxWriteTime = latency
	}
}

// updateQuestDBMetrics updates QuestDB performance metrics
func (hb *HybridBenchmark) updateQuestDBMetrics(latency time.Duration) {
	hb.metricsLock.Lock()
	defer hb.metricsLock.Unlock()

	// 평균 응답시간 업데이트
	if hb.metrics.QuestDB.AverageWriteTime == 0 {
		hb.metrics.QuestDB.AverageWriteTime = latency
	} else {
		hb.metrics.QuestDB.AverageWriteTime =
			(hb.metrics.QuestDB.AverageWriteTime + latency) / 2
	}

	// 최대 응답시간 업데이트
	if latency > hb.metrics.QuestDB.MaxWriteTime {
		hb.metrics.QuestDB.MaxWriteTime = latency
	}

	// QuestDB 통계에서 배치 정보 가져오기
	stats := hb.questDB.GetStats()
	hb.metrics.QuestDB.BatchesProcessed = stats.BatchesProcessed
}

// performFinalAnalysis performs comprehensive analysis
func (hb *HybridBenchmark) performFinalAnalysis() {
	log.Printf("📊 Performing final performance analysis...")

	hb.metricsLock.Lock()
	defer hb.metricsLock.Unlock()

	duration := hb.metrics.EndTime.Sub(hb.metrics.StartTime).Seconds()

	// TPS 계산
	hb.metrics.CircularBuffer.WritesPerSecond = float64(hb.metrics.CircularBuffer.TotalWrites) / duration
	hb.metrics.QuestDB.WritesPerSecond = float64(hb.metrics.QuestDB.TotalWrites) / duration

	// 성능 비율 계산
	if hb.metrics.CircularBuffer.WritesPerSecond > 0 {
		hb.metrics.Comparison.PerformanceRatio =
			hb.metrics.QuestDB.WritesPerSecond / hb.metrics.CircularBuffer.WritesPerSecond
	}

	// 에러율 계산
	circularErrorRate := float64(hb.metrics.CircularBuffer.FailedWrites) /
		float64(hb.metrics.CircularBuffer.TotalWrites + hb.metrics.CircularBuffer.FailedWrites)
	questdbErrorRate := float64(hb.metrics.QuestDB.DroppedWrites) /
		float64(hb.metrics.QuestDB.TotalWrites + hb.metrics.QuestDB.DroppedWrites)

	if circularErrorRate > 0 {
		hb.metrics.Comparison.ErrorRateRatio = questdbErrorRate / circularErrorRate
	}

	// 시스템 메트릭 수집
	hb.collectSystemMetrics()

	log.Printf("✅ Final analysis completed")
}

// collectSystemMetrics collects system resource metrics
func (hb *HybridBenchmark) collectSystemMetrics() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	hb.metrics.System.MemoryUsageMB = int64(m.Alloc) / 1024 / 1024
	hb.metrics.System.GoroutineCount = runtime.NumGoroutine()
	hb.metrics.System.GCPauseTime = time.Duration(m.PauseNs[(m.NumGC+255)%256])
}

// printIntermediateStats prints intermediate statistics
func (hb *HybridBenchmark) printIntermediateStats() {
	hb.metricsLock.RLock()
	defer hb.metricsLock.RUnlock()

	elapsed := time.Since(hb.metrics.StartTime).Seconds()
	circularTPS := float64(hb.metrics.CircularBuffer.TotalWrites) / elapsed
	questdbTPS := float64(hb.metrics.QuestDB.TotalWrites) / elapsed

	log.Printf("📊 [%s] Circular: %.0f TPS, QuestDB: %.0f TPS, Ratio: %.2fx",
		hb.metrics.TestPhase, circularTPS, questdbTPS, questdbTPS/circularTPS)
}

// PrintFinalReport prints comprehensive final report
func (hb *HybridBenchmark) PrintFinalReport() {
	m := hb.metrics

	fmt.Printf("\n================================================================================\n")
	fmt.Printf("🎯 HYBRID BENCHMARK FINAL REPORT\n")
	fmt.Printf("================================================================================\n")

	fmt.Printf("📋 Test Configuration:\n")
	fmt.Printf("  Duration: %v (+ %v warmup + %v cooldown)\n",
		hb.config.TestDuration, hb.config.WarmupDuration, hb.config.CooldownDuration)
	fmt.Printf("  Target Load: %d base TPS, %d pump TPS\n",
		hb.config.BaseTradesPerSecond, hb.config.PumpTradesPerSecond)
	fmt.Printf("  Workers: %d producers, %d consumers\n\n",
		hb.config.ProducerWorkers, hb.config.ConsumerWorkers)

	fmt.Printf("📊 CircularBuffer Performance:\n")
	fmt.Printf("  Total Writes: %d\n", m.CircularBuffer.TotalWrites)
	fmt.Printf("  Failed Writes: %d (%.3f%%)\n",
		m.CircularBuffer.FailedWrites,
		float64(m.CircularBuffer.FailedWrites)*100/float64(m.CircularBuffer.TotalWrites+m.CircularBuffer.FailedWrites))
	fmt.Printf("  Writes/Second: %.0f TPS\n", m.CircularBuffer.WritesPerSecond)
	fmt.Printf("  Avg Write Time: %v\n", m.CircularBuffer.AverageWriteTime)
	fmt.Printf("  Max Write Time: %v\n\n", m.CircularBuffer.MaxWriteTime)

	fmt.Printf("📊 QuestDB Performance:\n")
	fmt.Printf("  Total Writes: %d\n", m.QuestDB.TotalWrites)
	fmt.Printf("  Dropped Writes: %d (%.3f%%)\n",
		m.QuestDB.DroppedWrites,
		float64(m.QuestDB.DroppedWrites)*100/float64(m.QuestDB.TotalWrites+m.QuestDB.DroppedWrites))
	fmt.Printf("  Writes/Second: %.0f TPS\n", m.QuestDB.WritesPerSecond)
	fmt.Printf("  Avg Write Time: %v\n", m.QuestDB.AverageWriteTime)
	fmt.Printf("  Max Write Time: %v\n", m.QuestDB.MaxWriteTime)
	fmt.Printf("  Batches Processed: %d\n\n", m.QuestDB.BatchesProcessed)

	fmt.Printf("🔥 Performance Comparison:\n")
	fmt.Printf("  Performance Ratio: %.2fx (QuestDB/Circular)\n", m.Comparison.PerformanceRatio)
	fmt.Printf("  Error Rate Ratio: %.3fx\n", m.Comparison.ErrorRateRatio)

	if m.Comparison.PerformanceRatio >= 2.0 {
		fmt.Printf("  ✅ PERFORMANCE TARGET MET (≥2.0x)\n")
	} else {
		fmt.Printf("  ❌ PERFORMANCE TARGET NOT MET (<2.0x)\n")
	}

	totalErrorRate := float64(m.QuestDB.DroppedWrites + m.CircularBuffer.FailedWrites) /
		float64(m.QuestDB.TotalWrites + m.CircularBuffer.TotalWrites)
	if totalErrorRate <= 0.001 {
		fmt.Printf("  ✅ ERROR RATE TARGET MET (≤0.1%%)\n")
	} else {
		fmt.Printf("  ❌ ERROR RATE TARGET NOT MET (%.3f%%)\n", totalErrorRate*100)
	}

	fmt.Printf("\n💻 System Resources:\n")
	fmt.Printf("  Memory Usage: %d MB\n", m.System.MemoryUsageMB)
	fmt.Printf("  Goroutines: %d\n", m.System.GoroutineCount)
	fmt.Printf("  GC Pause Time: %v\n", m.System.GCPauseTime)

	fmt.Printf("\n================================================================================\n")
}

// Stop gracefully stops the benchmark
func (hb *HybridBenchmark) Stop() error {
	log.Printf("🛑 Stopping Hybrid Benchmark...")
	hb.cancel()
	return nil
}