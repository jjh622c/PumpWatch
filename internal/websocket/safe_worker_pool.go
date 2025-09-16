package websocket

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"PumpWatch/internal/logging"
	"PumpWatch/internal/models"
	"PumpWatch/internal/websocket/connectors"
)

// SafeWorkerPool은 SafeWorker들을 관리하는 메모리 누수 없는 풀
// "무식하게 때려박기" 철학: 단순하고 확실한 구조
type SafeWorkerPool struct {
	// 기본 정보
	Exchange   string
	MarketType string
	Symbols    []string

	// Context 기반 생명주기 관리
	ctx    context.Context
	cancel context.CancelFunc

	// Worker 관리
	workers     []*SafeWorker
	workerCount int

	// 설정
	maxSymbolsPerWorker int
	exchangeConfig      ExchangeConfig

	// 통계 (atomic 연산으로 안전성 보장)
	stats SafePoolStats

	// 콜백
	onTradeEvent func(models.TradeEvent)
	onError      func(error)

	// 로깅
	logger *logging.Logger

	// 상태 관리
	mu      sync.RWMutex
	running bool
}

// SafePoolStats는 풀 전체 통계
type SafePoolStats struct {
	TotalWorkers  int32 // atomic
	ActiveWorkers int32 // atomic
	TotalSymbols  int32 // atomic
	TotalMessages int64 // atomic
	TotalTrades   int64 // atomic
	TotalErrors   int64 // atomic
	LastActivity  int64 // atomic (unix nano)
}

// ExchangeConfig는 거래소별 설정
type ExchangeConfig struct {
	MaxSymbolsPerConnection int
	RetryInterval           time.Duration
	ConnectionTimeout       time.Duration
}

// NewSafeWorkerPool은 새로운 안전한 워커 풀 생성
func NewSafeWorkerPool(exchange, marketType string, symbols []string, config ExchangeConfig) *SafeWorkerPool {
	ctx, cancel := context.WithCancel(context.Background())

	// Worker 개수 계산 ("무식하게 때려박기" 방식)
	maxSymbols := config.MaxSymbolsPerConnection
	if maxSymbols <= 0 {
		maxSymbols = 100 // 기본값
	}

	workerCount := len(symbols) / maxSymbols
	if len(symbols)%maxSymbols != 0 {
		workerCount++
	}

	if workerCount == 0 {
		workerCount = 1 // 최소 1개 워커
	}

	pool := &SafeWorkerPool{
		Exchange:            exchange,
		MarketType:          marketType,
		Symbols:             symbols,
		ctx:                 ctx,
		cancel:              cancel,
		workerCount:         workerCount,
		maxSymbolsPerWorker: maxSymbols,
		exchangeConfig:      config,
		workers:             make([]*SafeWorker, workerCount),
	}

	// 통계 초기화
	atomic.StoreInt32(&pool.stats.TotalWorkers, int32(workerCount))
	atomic.StoreInt32(&pool.stats.TotalSymbols, int32(len(symbols)))

	return pool
}

// Start는 워커 풀 시작
func (p *SafeWorkerPool) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("워커 풀이 이미 실행 중입니다")
	}

	p.initLogger()
	p.logger.Info("🏗️ SafeWorkerPool 시작: %s %s, %d 워커로 %d 심볼 처리",
		p.Exchange, p.MarketType, p.workerCount, len(p.Symbols))

	// 심볼을 워커별로 분배
	symbolChunks := p.distributeSymbols()

	// 각 워커 생성 및 시작
	for i := 0; i < p.workerCount; i++ {
		symbols := symbolChunks[i]
		if len(symbols) == 0 {
			continue
		}

		// 거래소별 커넥터 생성
		connector, err := p.createConnector()
		if err != nil {
			p.logger.Error("❌ SafeWorker %d 커넥터 생성 실패: %v", i, err)
			continue
		}

		// SafeWorker 생성
		worker := NewSafeWorker(i, p.Exchange, p.MarketType, symbols, connector)

		// 콜백 설정
		worker.SetOnTradeEvent(p.handleTradeEvent)
		worker.SetOnError(p.handleError)
		worker.SetOnConnected(p.handleWorkerConnected)

		p.workers[i] = worker

		// 워커 시작 (각각 별도 고루틴에서)
		go func(w *SafeWorker) {
			defer func() {
				if r := recover(); r != nil {
					p.logger.Error("🚨 SafeWorker %d 패닉 복구: %v", w.ID, r)
				}
				atomic.AddInt32(&p.stats.ActiveWorkers, -1)
			}()

			atomic.AddInt32(&p.stats.ActiveWorkers, 1)
			w.Start() // 이 메소드는 블로킹됨
		}(worker)

		// 워커 시작 간격 (동시 연결 부하 방지)
		time.Sleep(100 * time.Millisecond)
	}

	p.running = true
	p.logger.Info("✅ SafeWorkerPool 시작 완료: %d개 워커 활성화", p.workerCount)

	// 통계 모니터링 시작
	go p.monitorStats()

	return nil
}

// Stop은 워커 풀 중지 (완전한 정리)
func (p *SafeWorkerPool) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return nil
	}

	p.logger.Info("🛑 SafeWorkerPool 중지 시작: %s %s", p.Exchange, p.MarketType)

	// Context 취소로 모든 워커 중지
	p.cancel()

	// 모든 워커가 종료될 때까지 대기 (최대 10초)
	stopTimeout := 10 * time.Second
	stopTimer := time.NewTimer(stopTimeout)
	defer stopTimer.Stop()

	for {
		activeWorkers := atomic.LoadInt32(&p.stats.ActiveWorkers)
		if activeWorkers == 0 {
			break
		}

		select {
		case <-stopTimer.C:
			p.logger.Warn("⏰ SafeWorkerPool 중지 타임아웃: %d개 워커가 여전히 활성화", activeWorkers)
			break
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}

	p.running = false
	p.logger.Info("✅ SafeWorkerPool 완전 중지 완료")

	return nil
}

// distributeSymbols는 심볼을 워커별로 분배
func (p *SafeWorkerPool) distributeSymbols() [][]string {
	if len(p.Symbols) == 0 {
		return make([][]string, p.workerCount)
	}

	chunks := make([][]string, p.workerCount)
	symbolsPerWorker := len(p.Symbols) / p.workerCount
	remainder := len(p.Symbols) % p.workerCount

	startIndex := 0
	for i := 0; i < p.workerCount; i++ {
		chunkSize := symbolsPerWorker
		if i < remainder {
			chunkSize++ // 나머지 심볼을 앞의 워커들에 분배
		}

		if startIndex < len(p.Symbols) {
			endIndex := startIndex + chunkSize
			if endIndex > len(p.Symbols) {
				endIndex = len(p.Symbols)
			}
			chunks[i] = p.Symbols[startIndex:endIndex]
			startIndex = endIndex
		}
	}

	return chunks
}

// createConnector는 거래소별 커넥터 생성 (레지스트리 기반)
func (p *SafeWorkerPool) createConnector() (connectors.WebSocketConnector, error) {
	maxSymbols := p.exchangeConfig.MaxSymbolsPerConnection
	if maxSymbols <= 0 {
		maxSymbols = 100 // 기본값
	}

	// 레지스트리에서 커넥터 팩토리 가져오기
	factory, err := connectors.GetConnectorFactory(p.Exchange)
	if err != nil {
		return nil, fmt.Errorf("커넥터 팩토리 가져오기 실패: %w", err)
	}

	// 커넥터 생성
	return factory(p.MarketType, maxSymbols), nil
}

// handleTradeEvent는 거래 이벤트 처리
func (p *SafeWorkerPool) handleTradeEvent(tradeEvent models.TradeEvent) {
	atomic.AddInt64(&p.stats.TotalTrades, 1)
	atomic.StoreInt64(&p.stats.LastActivity, time.Now().UnixNano())

	if p.onTradeEvent != nil {
		p.onTradeEvent(tradeEvent)
	}
}

// handleError는 에러 처리
func (p *SafeWorkerPool) handleError(err error) {
	atomic.AddInt64(&p.stats.TotalErrors, 1)
	p.logger.Warn("⚠️ SafeWorkerPool 에러: %v", err)

	if p.onError != nil {
		p.onError(err)
	}
}

// handleWorkerConnected는 워커 연결 성공 처리
func (p *SafeWorkerPool) handleWorkerConnected() {
	atomic.StoreInt64(&p.stats.LastActivity, time.Now().UnixNano())
}

// monitorStats는 통계 모니터링
func (p *SafeWorkerPool) monitorStats() {
	ticker := time.NewTicker(30 * time.Second) // 30초마다 통계 출력
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			stats := p.GetStats()
			p.logger.Info("📊 SafeWorkerPool 통계 - 활성 워커: %d/%d, 메시지: %d, 거래: %d, 에러: %d",
				stats.ActiveWorkers, stats.TotalWorkers, stats.TotalMessages, stats.TotalTrades, stats.TotalErrors)
		}
	}
}

// GetStats는 현재 통계 반환
func (p *SafeWorkerPool) GetStats() SafePoolStats {
	// Worker들의 개별 통계 수집
	var totalMessages, totalTrades, totalErrors int64

	p.mu.RLock()
	for _, worker := range p.workers {
		if worker != nil {
			workerStats := worker.GetStats()
			totalMessages += workerStats.MessageCount
			totalTrades += workerStats.TradeCount
			totalErrors += workerStats.ErrorCount
		}
	}
	p.mu.RUnlock()

	// 통계 업데이트
	atomic.StoreInt64(&p.stats.TotalMessages, totalMessages)
	atomic.StoreInt64(&p.stats.TotalTrades, totalTrades)
	atomic.StoreInt64(&p.stats.TotalErrors, totalErrors)

	return SafePoolStats{
		TotalWorkers:  atomic.LoadInt32(&p.stats.TotalWorkers),
		ActiveWorkers: atomic.LoadInt32(&p.stats.ActiveWorkers),
		TotalSymbols:  atomic.LoadInt32(&p.stats.TotalSymbols),
		TotalMessages: atomic.LoadInt64(&p.stats.TotalMessages),
		TotalTrades:   atomic.LoadInt64(&p.stats.TotalTrades),
		TotalErrors:   atomic.LoadInt64(&p.stats.TotalErrors),
		LastActivity:  atomic.LoadInt64(&p.stats.LastActivity),
	}
}

// IsRunning은 실행 상태 확인
func (p *SafeWorkerPool) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

// GetWorkerCount는 워커 개수 반환
func (p *SafeWorkerPool) GetWorkerCount() int {
	return p.workerCount
}

// SetOnTradeEvent는 거래 이벤트 콜백 설정
func (p *SafeWorkerPool) SetOnTradeEvent(callback func(models.TradeEvent)) {
	p.onTradeEvent = callback
}

// SetOnError는 에러 콜백 설정
func (p *SafeWorkerPool) SetOnError(callback func(error)) {
	p.onError = callback
}

// initLogger는 로거 초기화
func (p *SafeWorkerPool) initLogger() {
	if p.logger == nil {
		globalLogger := logging.GetGlobalLogger()
		if globalLogger != nil {
			p.logger = globalLogger.WebSocketLogger(p.Exchange, p.MarketType)
		}
	}
}
