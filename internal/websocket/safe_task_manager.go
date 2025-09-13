package websocket

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"PumpWatch/internal/config"
	"PumpWatch/internal/logging"
	"PumpWatch/internal/models"
	"PumpWatch/internal/symbols"
)

// SafeTaskManager는 SafeWorkerPool들을 관리하는 메모리 누수 없는 관리자
// "무식하게 때려박기" 철학: 단순하고 확실한 구조로 안전성 최우선
type SafeTaskManager struct {
	// Context 기반 생명주기 관리
	ctx    context.Context
	cancel context.CancelFunc

	// WorkerPool 관리
	pools map[string]*SafeWorkerPool // exchange_marketType -> pool
	mu    sync.RWMutex

	// 설정
	config *config.Config
	logger *logging.Logger

	// 통계 (atomic 연산으로 안전성 보장)
	stats SafeManagerStats

	// 콜백
	onTradeEvent func(models.TradeEvent)
	onError      func(error)

	// 상태 관리
	running int32 // atomic
}

// SafeManagerStats는 매니저 전체 통계
type SafeManagerStats struct {
	TotalPools       int32 // atomic
	TotalWorkers     int32 // atomic
	ActiveWorkers    int32 // atomic
	TotalSymbols     int32 // atomic
	TotalMessages    int64 // atomic
	TotalTrades      int64 // atomic
	TotalErrors      int64 // atomic
	LastActivity     int64 // atomic (unix nano)
	SystemUptime     int64 // atomic (unix nano)
}

// NewSafeTaskManager는 새로운 안전한 태스크 매니저 생성
func NewSafeTaskManager(cfg *config.Config) *SafeTaskManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &SafeTaskManager{
		ctx:    ctx,
		cancel: cancel,
		pools:  make(map[string]*SafeWorkerPool),
		config: cfg,
	}
}

// Start는 태스크 매니저 시작
func (tm *SafeTaskManager) Start() error {
	if !atomic.CompareAndSwapInt32(&tm.running, 0, 1) {
		return fmt.Errorf("SafeTaskManager가 이미 실행 중입니다")
	}

	tm.initLogger()
	tm.logger.Info("🚀 SafeTaskManager 시작: 메모리 누수 없는 아키텍처")

	atomic.StoreInt64(&tm.stats.SystemUptime, time.Now().UnixNano())

	// 심볼 데이터 로드
	symbolData, err := symbols.LoadConfig("config/symbols/symbols.yaml")
	if err != nil {
		tm.logger.Warn("⚠️ 심볼 데이터 로드 실패: %v, 기본 설정 사용", err)
		// 기본 심볼로 진행 (symbols.yaml 파일이 없을 수 있음)
	}

	// 거래소별 WorkerPool 생성
	exchanges := map[string]config.ExchangeConfig{
		"binance": tm.config.Exchanges.Binance,
		"bybit":   tm.config.Exchanges.Bybit,
		"okx":     tm.config.Exchanges.OKX,
		"kucoin":  tm.config.Exchanges.KuCoin,
		"phemex":  tm.config.Exchanges.Phemex,
		"gate":    tm.config.Exchanges.Gate,
	}

	for exchangeKey, exchangeConfig := range exchanges {
		if !exchangeConfig.Enabled {
			continue
		}

		for _, marketType := range []string{"spot", "futures"} {
			// 심볼 가져오기
			poolKey := fmt.Sprintf("%s_%s", exchangeKey, marketType)
			var exchangeSymbols []string

			if symbolData != nil {
				exchangeSymbols = symbolData.GetSubscriptionList(exchangeKey, marketType)
			}

			// 심볼이 없으면 기본 심볼 사용
			if len(exchangeSymbols) == 0 {
				switch exchangeKey {
				case "binance", "bybit":
					exchangeSymbols = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
				case "okx":
					if marketType == "spot" {
						exchangeSymbols = []string{"BTC-USDT", "ETH-USDT", "SOL-USDT"}
					} else {
						exchangeSymbols = []string{"BTC-USDT-SWAP", "ETH-USDT-SWAP", "SOL-USDT-SWAP"}
					}
				case "kucoin":
					if marketType == "spot" {
						exchangeSymbols = []string{"BTC-USDT", "ETH-USDT", "SOL-USDT"}
					} else {
						exchangeSymbols = []string{"XBTUSDTM", "ETHUSDTM", "SOLUSDTM"}
					}
				case "gate":
					exchangeSymbols = []string{"BTC_USDT", "ETH_USDT", "SOL_USDT"}
				case "phemex":
					exchangeSymbols = []string{} // Phemex는 현재 비활성화
				}
			}

			if len(exchangeSymbols) == 0 {
				tm.logger.Warn("⚠️ No symbols found for %s", poolKey)
				continue
			}

			// ExchangeConfig 생성
			exchangeConf := ExchangeConfig{
				MaxSymbolsPerConnection: exchangeConfig.MaxSymbolsPerConnection,
				RetryInterval:           exchangeConfig.RetryCooldown,
				ConnectionTimeout:       exchangeConfig.ConnectionTimeout,
			}

			// SafeWorkerPool 생성
			pool := NewSafeWorkerPool(exchangeKey, marketType, exchangeSymbols, exchangeConf)

			// 콜백 설정
			pool.SetOnTradeEvent(tm.handleTradeEvent)
			pool.SetOnError(tm.handleError)

			tm.mu.Lock()
			tm.pools[poolKey] = pool
			tm.mu.Unlock()

			atomic.AddInt32(&tm.stats.TotalPools, 1)
			atomic.AddInt32(&tm.stats.TotalWorkers, int32(pool.GetWorkerCount()))
			atomic.AddInt32(&tm.stats.TotalSymbols, int32(len(exchangeSymbols)))

			tm.logger.Info("🏭 SafeWorkerPool 생성: %s (%d workers, %d symbols)",
				poolKey, pool.GetWorkerCount(), len(exchangeSymbols))
		}
	}

	// 순차적으로 WorkerPool 시작 (과부하 방지)
	tm.mu.RLock()
	poolKeys := make([]string, 0, len(tm.pools))
	for key := range tm.pools {
		poolKeys = append(poolKeys, key)
	}
	tm.mu.RUnlock()

	for i, poolKey := range poolKeys {
		tm.mu.RLock()
		pool := tm.pools[poolKey]
		tm.mu.RUnlock()

		tm.logger.Info("🚀 Starting SafeWorkerPool: %s", poolKey)

		if err := pool.Start(); err != nil {
			tm.logger.Error("❌ SafeWorkerPool %s 시작 실패: %v", poolKey, err)
			continue
		}

		// 시작 간격 (동시 연결 부하 방지)
		if i < len(poolKeys)-1 {
			time.Sleep(2 * time.Second)
		}
	}

	tm.logger.Info("✅ SafeTaskManager 초기화 완료: %d pools, %d workers, %d symbols",
		atomic.LoadInt32(&tm.stats.TotalPools),
		atomic.LoadInt32(&tm.stats.TotalWorkers),
		atomic.LoadInt32(&tm.stats.TotalSymbols))

	// 통계 모니터링 시작
	go tm.monitorStats()

	return nil
}

// StartDataCollection은 특정 심볼에 대한 데이터 수집 시작 (monitor.DataCollectionManager 인터페이스 구현)
func (tm *SafeTaskManager) StartDataCollection(symbol string, triggerTime time.Time) error {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if !tm.IsRunning() {
		return fmt.Errorf("SafeTaskManager가 실행 중이 아닙니다")
	}

	// 모든 활성화된 풀에 데이터 수집 트리거 전달
	tm.logger.Info("🎯 데이터 수집 시작: %s (트리거 시간: %s)", symbol, triggerTime.Format("15:04:05.000"))

	collectionCount := 0
	for poolKey, pool := range tm.pools {
		if pool.IsRunning() {
			// 각 풀이 해당 심볼에 대한 데이터 수집을 시작하도록 알림
			// 현재 구조에서는 이미 모든 스트림을 수신하고 있으므로 로깅만 수행
			tm.logger.Info("📊 %s 풀에서 %s 심볼 데이터 수집 활성화", poolKey, symbol)
			collectionCount++
		}
	}

	if collectionCount > 0 {
		tm.logger.Info("✅ %d개 풀에서 %s 심볼 데이터 수집 시작됨", collectionCount, symbol)
		return nil
	} else {
		return fmt.Errorf("활성화된 풀이 없어 데이터 수집을 시작할 수 없습니다")
	}
}

// Stop은 태스크 매니저 중지
func (tm *SafeTaskManager) Stop() error {
	if !atomic.CompareAndSwapInt32(&tm.running, 1, 0) {
		return nil
	}

	tm.logger.Info("🛑 SafeTaskManager 중지 시작")

	// Context 취소로 모든 고루틴 중지 신호
	tm.cancel()

	// 모든 WorkerPool 중지
	tm.mu.RLock()
	pools := make([]*SafeWorkerPool, 0, len(tm.pools))
	for _, pool := range tm.pools {
		pools = append(pools, pool)
	}
	tm.mu.RUnlock()

	for _, pool := range pools {
		if err := pool.Stop(); err != nil {
			tm.logger.Error("❌ SafeWorkerPool 중지 실패: %v", err)
		}
	}

	// 완전 종료 대기 (최대 15초)
	stopTimeout := 15 * time.Second
	stopTimer := time.NewTimer(stopTimeout)
	defer stopTimer.Stop()

	for {
		activeWorkers := atomic.LoadInt32(&tm.stats.ActiveWorkers)
		if activeWorkers == 0 {
			break
		}

		select {
		case <-stopTimer.C:
			tm.logger.Warn("⏰ SafeTaskManager 중지 타임아웃: %d개 워커가 여전히 활성화", activeWorkers)
			break
		default:
			time.Sleep(200 * time.Millisecond)
		}
	}

	tm.logger.Info("✅ SafeTaskManager 완전 중지 완료")
	return nil
}

// handleTradeEvent는 거래 이벤트 처리
func (tm *SafeTaskManager) handleTradeEvent(tradeEvent models.TradeEvent) {
	atomic.AddInt64(&tm.stats.TotalTrades, 1)
	atomic.StoreInt64(&tm.stats.LastActivity, time.Now().UnixNano())

	if tm.onTradeEvent != nil {
		tm.onTradeEvent(tradeEvent)
	}
}

// handleError는 에러 처리
func (tm *SafeTaskManager) handleError(err error) {
	atomic.AddInt64(&tm.stats.TotalErrors, 1)
	tm.logger.Warn("⚠️ SafeTaskManager 에러: %v", err)

	if tm.onError != nil {
		tm.onError(err)
	}
}

// monitorStats는 통계 모니터링
func (tm *SafeTaskManager) monitorStats() {
	ticker := time.NewTicker(30 * time.Second) // 30초마다 통계 출력
	defer ticker.Stop()

	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-ticker.C:
			stats := tm.GetStats()

			// 개별 Pool 통계 수집
			tm.updatePoolStats()

			uptime := time.Since(time.Unix(0, stats.SystemUptime))
			tm.logger.Info("📊 SafeTaskManager 통계 - 가동시간: %v, 풀: %d, 활성워커: %d/%d, 메시지: %d, 거래: %d, 에러: %d",
				uptime.Round(time.Second),
				stats.TotalPools,
				stats.ActiveWorkers,
				stats.TotalWorkers,
				stats.TotalMessages,
				stats.TotalTrades,
				stats.TotalErrors)
		}
	}
}

// updatePoolStats는 개별 Pool 통계를 집계
func (tm *SafeTaskManager) updatePoolStats() {
	var totalMessages, totalTrades, totalErrors int64
	var activeWorkers int32

	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, pool := range tm.pools {
		if pool.IsRunning() {
			poolStats := pool.GetStats()
			totalMessages += poolStats.TotalMessages
			totalTrades += poolStats.TotalTrades
			totalErrors += poolStats.TotalErrors
			activeWorkers += poolStats.ActiveWorkers
		}
	}

	atomic.StoreInt64(&tm.stats.TotalMessages, totalMessages)
	atomic.StoreInt64(&tm.stats.TotalTrades, totalTrades)
	atomic.StoreInt64(&tm.stats.TotalErrors, totalErrors)
	atomic.StoreInt32(&tm.stats.ActiveWorkers, activeWorkers)
}

// GetStats는 현재 통계 반환
func (tm *SafeTaskManager) GetStats() SafeManagerStats {
	// 실시간 통계 업데이트
	tm.updatePoolStats()

	return SafeManagerStats{
		TotalPools:    atomic.LoadInt32(&tm.stats.TotalPools),
		TotalWorkers:  atomic.LoadInt32(&tm.stats.TotalWorkers),
		ActiveWorkers: atomic.LoadInt32(&tm.stats.ActiveWorkers),
		TotalSymbols:  atomic.LoadInt32(&tm.stats.TotalSymbols),
		TotalMessages: atomic.LoadInt64(&tm.stats.TotalMessages),
		TotalTrades:   atomic.LoadInt64(&tm.stats.TotalTrades),
		TotalErrors:   atomic.LoadInt64(&tm.stats.TotalErrors),
		LastActivity:  atomic.LoadInt64(&tm.stats.LastActivity),
		SystemUptime:  atomic.LoadInt64(&tm.stats.SystemUptime),
	}
}

// IsRunning은 실행 상태 확인
func (tm *SafeTaskManager) IsRunning() bool {
	return atomic.LoadInt32(&tm.running) == 1
}

// GetPoolCount는 풀 개수 반환
func (tm *SafeTaskManager) GetPoolCount() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return len(tm.pools)
}

// GetPoolInfo는 특정 풀 정보 반환
func (tm *SafeTaskManager) GetPoolInfo(poolKey string) (*SafePoolStats, error) {
	tm.mu.RLock()
	pool, exists := tm.pools[poolKey]
	tm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("Pool %s not found", poolKey)
	}

	stats := pool.GetStats()
	return &stats, nil
}

// ListPools는 모든 풀 목록 반환
func (tm *SafeTaskManager) ListPools() []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	keys := make([]string, 0, len(tm.pools))
	for key := range tm.pools {
		keys = append(keys, key)
	}
	return keys
}

// SetOnTradeEvent는 거래 이벤트 콜백 설정
func (tm *SafeTaskManager) SetOnTradeEvent(callback func(models.TradeEvent)) {
	tm.onTradeEvent = callback
}

// SetOnError는 에러 콜백 설정
func (tm *SafeTaskManager) SetOnError(callback func(error)) {
	tm.onError = callback
}

// GetHealthStatus는 시스템 건강 상태 반환
func (tm *SafeTaskManager) GetHealthStatus() map[string]interface{} {
	stats := tm.GetStats()
	uptime := time.Since(time.Unix(0, stats.SystemUptime))
	lastActivity := time.Since(time.Unix(0, stats.LastActivity))

	health := map[string]interface{}{
		"running":       tm.IsRunning(),
		"uptime":        uptime.String(),
		"pools":         stats.TotalPools,
		"workers":       fmt.Sprintf("%d/%d", stats.ActiveWorkers, stats.TotalWorkers),
		"symbols":       stats.TotalSymbols,
		"messages":      stats.TotalMessages,
		"trades":        stats.TotalTrades,
		"errors":        stats.TotalErrors,
		"last_activity": lastActivity.String(),
		"health_score":  tm.calculateHealthScore(stats),
	}

	// 개별 Pool 상태 추가
	pools := make(map[string]interface{})
	tm.mu.RLock()
	for key, pool := range tm.pools {
		poolStats := pool.GetStats()
		pools[key] = map[string]interface{}{
			"running":     pool.IsRunning(),
			"workers":     fmt.Sprintf("%d/%d", poolStats.ActiveWorkers, poolStats.TotalWorkers),
			"symbols":     poolStats.TotalSymbols,
			"messages":    poolStats.TotalMessages,
			"trades":      poolStats.TotalTrades,
			"errors":      poolStats.TotalErrors,
		}
	}
	tm.mu.RUnlock()
	health["pools_detail"] = pools

	return health
}

// calculateHealthScore는 건강 점수 계산 (0.0 ~ 1.0)
func (tm *SafeTaskManager) calculateHealthScore(stats SafeManagerStats) float64 {
	if !tm.IsRunning() {
		return 0.0
	}

	score := 1.0

	// 활성 워커 비율
	if stats.TotalWorkers > 0 {
		activeRatio := float64(stats.ActiveWorkers) / float64(stats.TotalWorkers)
		score *= activeRatio
	}

	// 에러율 (최근 활동 기준)
	if stats.TotalMessages > 0 {
		errorRate := float64(stats.TotalErrors) / float64(stats.TotalMessages)
		score *= (1.0 - errorRate*0.5) // 에러율의 50%만큼 점수 감점
	}

	// 최근 활동성 (10분 이내 활동이 있으면 만점)
	if stats.LastActivity > 0 {
		lastActivity := time.Since(time.Unix(0, stats.LastActivity))
		if lastActivity > 10*time.Minute {
			score *= 0.5 // 10분 이상 비활성이면 50% 감점
		}
	}

	if score < 0.0 {
		score = 0.0
	}
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// initLogger는 로거 초기화
func (tm *SafeTaskManager) initLogger() {
	if tm.logger == nil {
		globalLogger := logging.GetGlobalLogger()
		if globalLogger != nil {
			tm.logger = globalLogger.WebSocketLogger("SafeTaskManager", "system")
		}
	}
}