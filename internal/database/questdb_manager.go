package database

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"PumpWatch/internal/models"
)

// QuestDBManager handles high-performance asynchronous batch processing
type QuestDBManager struct {
	db            *sql.DB
	config        QuestDBManagerConfig

	// 비동기 채널 기반 시스템
	tradeChannel  chan models.TradeEvent
	batchChannel  chan []models.TradeEvent
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup

	// 성능 메트릭
	stats         *QuestDBStats

	// 강화된 에러 핸들링
	circuitBreaker *CircuitBreaker

	// Prepared statements
	insertTradeStmt *sql.Stmt
}

// QuestDBManagerConfig holds configuration for batch processing
type QuestDBManagerConfig struct {
	Host            string        // "localhost"
	Port            int          // 8812
	Database        string        // "qdb"
	User            string        // "admin"
	Password        string        // "quest"

	// 배치 처리 설정 (문서 사양)
	BatchSize       int          // 1000 (문서 기준)
	FlushInterval   time.Duration // 1초 (문서 기준)
	BufferSize      int          // 50,000 (문서 기준)
	WorkerCount     int          // 4개 워커 (문서 기준)

	// 연결 풀 설정
	MaxOpenConns    int          // 20
	MaxIdleConns    int          // 10
	ConnMaxLifetime time.Duration // 30분

	// 재시도 설정
	MaxRetries      int          // 3
	BaseDelay       time.Duration // 100ms
	MaxDelay        time.Duration // 5초
	BackoffFactor   float64      // 2.0
}

// QuestDBStats holds performance metrics
type QuestDBStats struct {
	TotalTrades       int64
	BatchesProcessed  int64
	FailedBatches     int64
	DroppedTrades     int64
	AverageLatency    int64      // nanoseconds - atomic 연산용
	LastFlushTime     int64      // unix nanoseconds - atomic 연산용
	WorkerStatus      [4]string  // 4개 워커 상태
	mu                sync.RWMutex // WorkerStatus 보호용

	// 강화된 에러 메트릭
	RetryableErrors   int64     // 재시도 가능 에러 수
	FatalErrors       int64     // 치명적 에러 수
	CircuitBreakerTrips int64   // 회로 차단 횟수
}

// CircuitBreaker holds circuit breaker state
type CircuitBreaker struct {
	failureCount    int64
	lastFailureTime int64
	state          int32 // 0: closed, 1: open, 2: half-open
	threshold      int64
	timeout        time.Duration
	mu             sync.RWMutex
}

// ErrorType classifies database errors
type ErrorType int

const (
	ErrorTypeRetryable ErrorType = iota
	ErrorTypeFatal
	ErrorTypeTemporary
)

// DefaultManagerConfig returns optimized configuration based on document specs
func DefaultManagerConfig() QuestDBManagerConfig {
	return QuestDBManagerConfig{
		Host:     "localhost",
		Port:     8812,
		Database: "qdb",
		User:     "admin",
		Password: "quest",

		// 문서 기준 고성능 배치 설정
		BatchSize:     1000,                // Phase 2 문서 사양
		FlushInterval: 1 * time.Second,     // Phase 2 문서 사양
		BufferSize:    50000,               // Phase 2 문서 사양
		WorkerCount:   4,                   // Phase 2 문서 사양 (병렬 처리 4배)

		MaxOpenConns:    20,
		MaxIdleConns:    10,
		ConnMaxLifetime: 30 * time.Minute,

		MaxRetries:    3,
		BaseDelay:     100 * time.Millisecond,
		MaxDelay:      5 * time.Second,
		BackoffFactor: 2.0,
	}
}

// NewQuestDBManager creates a new high-performance batch processing manager
func NewQuestDBManager(config QuestDBManagerConfig) (*QuestDBManager, error) {
	// PostgreSQL DSN for QuestDB
	dsn := fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=disable",
		config.Host, config.Port, config.Database, config.User, config.Password)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open QuestDB: %w", err)
	}

	// 연결 풀 최적화 설정
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)

	// 연결 테스트
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping QuestDB: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	manager := &QuestDBManager{
		db:           db,
		config:       config,
		tradeChannel: make(chan models.TradeEvent, config.BufferSize),
		batchChannel: make(chan []models.TradeEvent, config.WorkerCount*2), // 워커별 2개씩 버퍼
		ctx:          ctx,
		cancel:       cancel,
		stats: &QuestDBStats{
			WorkerStatus: [4]string{"idle", "idle", "idle", "idle"},
		},
		// Circuit breaker: 5회 연속 실패 시 30초간 차단
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
	}

	// Prepared statement 생성
	if err := manager.prepareBatchStatements(); err != nil {
		db.Close()
		cancel()
		return nil, fmt.Errorf("failed to prepare statements: %w", err)
	}

	// 배치 처리 시스템 시작
	manager.startBatchProcessing()

	fmt.Printf("✅ QuestDBManager initialized: %d workers, batch=%d, buffer=%d\n",
		config.WorkerCount, config.BatchSize, config.BufferSize)

	return manager, nil
}

// prepareBatchStatements prepares optimized batch insert statement
func (qm *QuestDBManager) prepareBatchStatements() error {
	var err error

	// 고성능 배치 INSERT prepared statement
	qm.insertTradeStmt, err = qm.db.Prepare(`
		INSERT INTO trades (timestamp, exchange, market_type, symbol, trade_id, price, quantity, side, collected_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare trade insert statement: %w", err)
	}

	return nil
}

// startBatchProcessing starts the asynchronous batch processing pipeline
func (qm *QuestDBManager) startBatchProcessing() {
	// 배치 수집기 시작 (단일 고루틴)
	qm.wg.Add(1)
	go qm.batchCollector()

	// 워커 풀 시작 (4개 워커)
	for i := 0; i < qm.config.WorkerCount; i++ {
		qm.wg.Add(1)
		go qm.batchWorker(i)
	}

	fmt.Printf("🚀 Started %d batch workers with %dms flush interval\n",
		qm.config.WorkerCount, qm.config.FlushInterval/time.Millisecond)
}

// batchCollector collects trades into batches based on size or time
func (qm *QuestDBManager) batchCollector() {
	defer qm.wg.Done()

	ticker := time.NewTicker(qm.config.FlushInterval)
	defer ticker.Stop()

	batch := make([]models.TradeEvent, 0, qm.config.BatchSize)

	for {
		select {
		case <-qm.ctx.Done():
			// 종료 시 남은 배치 처리
			if len(batch) > 0 {
				select {
				case qm.batchChannel <- append([]models.TradeEvent(nil), batch...):
				default:
					atomic.AddInt64(&qm.stats.DroppedTrades, int64(len(batch)))
				}
			}
			return

		case trade := <-qm.tradeChannel:
			batch = append(batch, trade)

			// 배치가 찬 경우 즉시 전송
			if len(batch) >= qm.config.BatchSize {
				select {
				case qm.batchChannel <- append([]models.TradeEvent(nil), batch...):
					batch = batch[:0] // 슬라이스 재사용
				default:
					// 채널이 가득 찬 경우 드롭
					atomic.AddInt64(&qm.stats.DroppedTrades, int64(len(batch)))
					batch = batch[:0]
				}
			}

		case <-ticker.C:
			// 주기적 플러시 (시간 기반)
			if len(batch) > 0 {
				select {
				case qm.batchChannel <- append([]models.TradeEvent(nil), batch...):
					batch = batch[:0]
				default:
					atomic.AddInt64(&qm.stats.DroppedTrades, int64(len(batch)))
					batch = batch[:0]
				}
			}
		}
	}
}

// batchWorker processes batches in parallel (4 workers)
func (qm *QuestDBManager) batchWorker(workerID int) {
	defer qm.wg.Done()

	for {
		select {
		case <-qm.ctx.Done():
			// WorkerStatus 업데이트 - mutex로 보호
			qm.stats.mu.Lock()
			qm.stats.WorkerStatus[workerID] = "stopped"
			qm.stats.mu.Unlock()
			return

		case batch := <-qm.batchChannel:
			// WorkerStatus 업데이트 - mutex로 보호
			qm.stats.mu.Lock()
			qm.stats.WorkerStatus[workerID] = "processing"
			qm.stats.mu.Unlock()
			startTime := time.Now()

			// 재시도 로직으로 배치 처리
			err := qm.processBatchWithRetry(batch)
			processingTime := time.Since(startTime)

			if err != nil {
				fmt.Printf("❌ Worker %d batch failed: %v\n", workerID, err)
				atomic.AddInt64(&qm.stats.FailedBatches, 1)
				atomic.AddInt64(&qm.stats.DroppedTrades, int64(len(batch)))
			} else {
				atomic.AddInt64(&qm.stats.BatchesProcessed, 1)
				atomic.AddInt64(&qm.stats.TotalTrades, int64(len(batch)))

				// 평균 지연시간 업데이트 (단순 이동 평균) - atomic 연산으로 race condition 방지
				oldAvg := atomic.LoadInt64(&qm.stats.AverageLatency)
				newAvg := (oldAvg + processingTime.Nanoseconds()) / 2
				atomic.StoreInt64(&qm.stats.AverageLatency, newAvg)

				// LastFlushTime 업데이트 - atomic 연산으로 race condition 방지
				atomic.StoreInt64(&qm.stats.LastFlushTime, time.Now().UnixNano())
			}

			// WorkerStatus 업데이트 - mutex로 보호
			qm.stats.mu.Lock()
			qm.stats.WorkerStatus[workerID] = "idle"
			qm.stats.mu.Unlock()
		}
	}
}

// processBatchWithRetry executes batch with enhanced error handling and circuit breaker
func (qm *QuestDBManager) processBatchWithRetry(batch []models.TradeEvent) error {
	// Circuit breaker 상태 확인
	if !qm.circuitBreaker.canExecute() {
		atomic.AddInt64(&qm.stats.CircuitBreakerTrips, 1)
		return fmt.Errorf("circuit breaker is open")
	}

	var lastErr error

	for attempt := 0; attempt <= qm.config.MaxRetries; attempt++ {
		err := qm.insertBatch(batch)

		if err == nil {
			// 성공 시 circuit breaker 상태 리셋
			qm.circuitBreaker.recordSuccess()
			return nil
		}

		lastErr = err

		// 에러 분류 및 처리 결정
		errorType := qm.classifyError(err)

		switch errorType {
		case ErrorTypeFatal:
			// 치명적 에러는 재시도하지 않음
			qm.circuitBreaker.recordFailure()
			return fmt.Errorf("fatal error, no retry: %w", err)

		case ErrorTypeTemporary, ErrorTypeRetryable:
			// 마지막 재시도가 아닌 경우만 대기
			if attempt < qm.config.MaxRetries {
				// 지수 백오프 계산 (개선된 공식)
				backoffMultiplier := 1 << uint(attempt) // 2^attempt
				jitter := time.Duration(rand.Int63n(int64(qm.config.BaseDelay))) // 지터 추가
				delay := time.Duration(float64(qm.config.BaseDelay) * float64(backoffMultiplier)) + jitter

				if delay > qm.config.MaxDelay {
					delay = qm.config.MaxDelay
				}

				// Context 취소 확인하며 대기
				select {
				case <-qm.ctx.Done():
					return qm.ctx.Err()
				case <-time.After(delay):
					// 계속 진행
				}
			}
		}
	}

	// 모든 재시도 실패 후 circuit breaker에 기록
	qm.circuitBreaker.recordFailure()
	return fmt.Errorf("batch processing failed after %d retries, last error: %w",
		qm.config.MaxRetries, lastErr)
}

// insertBatch performs the actual batch INSERT operation
func (qm *QuestDBManager) insertBatch(batch []models.TradeEvent) error {
	if len(batch) == 0 {
		return nil
	}

	// 트랜잭션 시작
	tx, err := qm.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 트랜잭션 내에서 prepared statement 사용
	stmt := tx.Stmt(qm.insertTradeStmt)

	for _, trade := range batch {
		// 타임스탬프 변환 및 타입 변환
		timestamp := time.Unix(0, trade.Timestamp*1e6)
		collectedAt := time.Now()

		price, err := strconv.ParseFloat(trade.Price, 64)
		if err != nil {
			continue // 잘못된 데이터는 스킵
		}
		quantity, err := strconv.ParseFloat(trade.Quantity, 64)
		if err != nil {
			continue
		}

		_, err = stmt.Exec(
			timestamp,
			trade.Exchange,
			trade.MarketType,
			trade.Symbol,
			trade.TradeID,
			price,
			quantity,
			trade.Side,
			collectedAt,
		)

		if err != nil {
			return fmt.Errorf("failed to insert trade: %w", err)
		}
	}

	// 트랜잭션 커밋
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}

	return nil
}

// AddTrade adds a trade to the processing pipeline (non-blocking)
func (qm *QuestDBManager) AddTrade(trade models.TradeEvent) bool {
	select {
	case qm.tradeChannel <- trade:
		return true
	default:
		// 채널이 가득 찬 경우
		atomic.AddInt64(&qm.stats.DroppedTrades, 1)
		return false
	}
}

// InsertListingEvent stores a listing event in QuestDB
func (qm *QuestDBManager) InsertListingEvent(event *models.ListingEvent) error {
	// Insert the listing event using raw SQL for direct execution
	query := `
		INSERT INTO listing_events (id, symbol, title, markets, announced_at, detected_at, trigger_time, notice_url, is_krw_listing, stored_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	// Generate unique ID based on timestamp
	id := time.Now().UnixNano()

	// Convert markets slice to JSON string
	marketsJSON := fmt.Sprintf(`["%s"]`, strings.Join(event.Markets, `","`))

	_, err := qm.db.Exec(query,
		id,
		event.Symbol,
		event.Title,
		marketsJSON,
		event.AnnouncedAt,
		event.DetectedAt,
		event.TriggerTime,
		event.NoticeURL,
		event.IsKRWListing,
		time.Now(),
	)

	if err != nil {
		return fmt.Errorf("failed to insert listing event: %w", err)
	}

	return nil
}

// GetStats returns a thread-safe copy of current statistics
func (qm *QuestDBManager) GetStats() QuestDBStats {
	stats := QuestDBStats{
		TotalTrades:      atomic.LoadInt64(&qm.stats.TotalTrades),
		BatchesProcessed: atomic.LoadInt64(&qm.stats.BatchesProcessed),
		FailedBatches:    atomic.LoadInt64(&qm.stats.FailedBatches),
		DroppedTrades:    atomic.LoadInt64(&qm.stats.DroppedTrades),
		AverageLatency:   atomic.LoadInt64(&qm.stats.AverageLatency),
		LastFlushTime:    atomic.LoadInt64(&qm.stats.LastFlushTime),
	}

	// WorkerStatus는 mutex로 보호하여 복사
	qm.stats.mu.RLock()
	stats.WorkerStatus = qm.stats.WorkerStatus
	qm.stats.mu.RUnlock()

	return stats
}

// Close gracefully shuts down the batch processing system
func (qm *QuestDBManager) Close() error {
	fmt.Println("🔌 Shutting down QuestDBManager...")

	// Context 취소로 모든 고루틴 종료 신호
	qm.cancel()

	// 모든 워커가 종료될 때까지 대기
	qm.wg.Wait()

	// Prepared statement 정리
	if qm.insertTradeStmt != nil {
		qm.insertTradeStmt.Close()
	}

	// 데이터베이스 연결 종료
	if qm.db != nil {
		qm.db.Close()
	}

	// 채널 정리
	close(qm.tradeChannel)
	close(qm.batchChannel)

	fmt.Printf("✅ QuestDBManager shutdown completed. Stats: %+v\n", qm.stats)
	return nil
}

// GetAverageLatencyDuration returns AverageLatency as time.Duration
func (qm *QuestDBManager) GetAverageLatencyDuration() time.Duration {
	return time.Duration(atomic.LoadInt64(&qm.stats.AverageLatency))
}

// GetLastFlushTime returns LastFlushTime as time.Time
func (qm *QuestDBManager) GetLastFlushTime() time.Time {
	return time.Unix(0, atomic.LoadInt64(&qm.stats.LastFlushTime))
}

// ============================================================================
// 강화된 에러 핸들링 및 재시도 로직
// ============================================================================

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(threshold int64, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold: threshold,
		timeout:   timeout,
		state:     0, // closed
	}
}

// classifyError categorizes database errors for appropriate handling
func (qm *QuestDBManager) classifyError(err error) ErrorType {
	if err == nil {
		return ErrorTypeRetryable
	}

	errStr := strings.ToLower(err.Error())

	// 치명적 에러 (재시도 불가)
	fatalPatterns := []string{
		"syntax error",
		"column does not exist",
		"table does not exist",
		"permission denied",
		"authentication failed",
		"invalid credentials",
	}

	for _, pattern := range fatalPatterns {
		if strings.Contains(errStr, pattern) {
			atomic.AddInt64(&qm.stats.FatalErrors, 1)
			return ErrorTypeFatal
		}
	}

	// 일시적 에러 (재시도 가능)
	temporaryPatterns := []string{
		"connection refused",
		"connection reset",
		"timeout",
		"deadline exceeded",
		"network",
		"i/o timeout",
		"too many connections",
		"lock timeout",
	}

	for _, pattern := range temporaryPatterns {
		if strings.Contains(errStr, pattern) {
			atomic.AddInt64(&qm.stats.RetryableErrors, 1)
			return ErrorTypeTemporary
		}
	}

	// 기본적으로 재시도 가능으로 분류
	atomic.AddInt64(&qm.stats.RetryableErrors, 1)
	return ErrorTypeRetryable
}

// canExecute checks if circuit breaker allows execution
func (cb *CircuitBreaker) canExecute() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	state := atomic.LoadInt32(&cb.state)

	switch state {
	case 0: // closed
		return true
	case 1: // open
		lastFailure := atomic.LoadInt64(&cb.lastFailureTime)
		if time.Since(time.Unix(0, lastFailure)) > cb.timeout {
			// Try to transition to half-open
			if atomic.CompareAndSwapInt32(&cb.state, 1, 2) {
				return true
			}
		}
		return false
	case 2: // half-open
		return true
	default:
		return false
	}
}

// recordSuccess records successful operation
func (cb *CircuitBreaker) recordSuccess() {
	atomic.StoreInt64(&cb.failureCount, 0)
	atomic.StoreInt32(&cb.state, 0) // closed
}

// recordFailure records failed operation
func (cb *CircuitBreaker) recordFailure() {
	atomic.StoreInt64(&cb.lastFailureTime, time.Now().UnixNano())

	count := atomic.AddInt64(&cb.failureCount, 1)
	if count >= cb.threshold {
		atomic.StoreInt32(&cb.state, 1) // open
	}
}