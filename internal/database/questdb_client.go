package database

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver for QuestDB

	"PumpWatch/internal/models"
)

// QuestDBClient handles all QuestDB database operations
type QuestDBClient struct {
	db *sql.DB

	// 성능 최적화를 위한 prepared statements
	insertTradeStmt    *sql.Stmt
	insertListingStmt  *sql.Stmt
	insertPumpStmt     *sql.Stmt
	insertMetricStmt   *sql.Stmt
}

// ConnectionConfig holds QuestDB connection configuration
type ConnectionConfig struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string

	// 연결 풀 설정
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// DefaultConfig returns optimized default configuration
func DefaultConfig() *ConnectionConfig {
	return &ConnectionConfig{
		Host:     "localhost",
		Port:     8812, // QuestDB PostgreSQL wire protocol port
		Database: "qdb",
		User:     "admin",
		Password: "quest",

		// 고성능 배치 처리를 위한 연결 풀 최적화
		MaxOpenConns:    20,  // 동시 연결 수
		MaxIdleConns:    10,  // 유휴 연결 수
		ConnMaxLifetime: 30 * time.Minute,
	}
}

// NewQuestDBClient creates a new QuestDB client with optimized settings
func NewQuestDBClient(config *ConnectionConfig) (*QuestDBClient, error) {
	// PostgreSQL DSN for QuestDB
	dsn := fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=disable",
		config.Host, config.Port, config.Database, config.User, config.Password)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 연결 풀 최적화 설정
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)

	// 연결 테스트
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	client := &QuestDBClient{
		db: db,
	}

	// Prepared statements 준비 (성능 최적화)
	if err := client.prepareBatchStatements(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to prepare statements: %w", err)
	}

	fmt.Println("✅ QuestDB client initialized successfully")
	return client, nil
}

// prepareBatchStatements prepares optimized batch insert statements
func (qc *QuestDBClient) prepareBatchStatements() error {
	var err error

	// 거래 데이터 배치 삽입
	qc.insertTradeStmt, err = qc.db.Prepare(`
		INSERT INTO trades (timestamp, exchange, market_type, symbol, trade_id, price, quantity, side, collected_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare trade insert statement: %w", err)
	}

	// 상장 이벤트 삽입
	qc.insertListingStmt, err = qc.db.Prepare(`
		INSERT INTO listing_events (id, symbol, title, markets, announced_at, detected_at, trigger_time, notice_url, is_krw_listing, stored_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare listing insert statement: %w", err)
	}

	// 펌핑 분석 결과 삽입
	qc.insertPumpStmt, err = qc.db.Prepare(`
		INSERT INTO pump_analysis (id, listing_id, symbol, exchange, market_type, pump_start_time, pump_end_time, duration_ms, start_price, peak_price, end_price, price_change_percent, total_volume, volume_spike_ratio, trade_count, analyzed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare pump analysis insert statement: %w", err)
	}

	// 시스템 메트릭 삽입
	qc.insertMetricStmt, err = qc.db.Prepare(`
		INSERT INTO system_metrics (timestamp, metric_name, metric_value, tags, node_id)
		VALUES ($1, $2, $3, $4, $5)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare metric insert statement: %w", err)
	}

	return nil
}

// BatchInsertTrades inserts multiple trade events efficiently
func (qc *QuestDBClient) BatchInsertTrades(trades []models.TradeEvent) error {
	if len(trades) == 0 {
		return nil
	}

	// 트랜잭션 시작 (배치 성능 최적화)
	tx, err := qc.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 트랜잭션 내에서 prepared statement 사용
	stmt := tx.Stmt(qc.insertTradeStmt)

	insertedCount := 0
	for _, trade := range trades {
		// 타임스탬프 변환 (밀리초 → 나노초)
		timestamp := time.Unix(0, trade.Timestamp*1e6)  // QuestDB는 나노초 정밀도
		collectedAt := time.Now()

		// string을 float64로 변환
		price, err := strconv.ParseFloat(trade.Price, 64)
		if err != nil {
			fmt.Printf("❌ Price parse failed: %v\n", err)
			continue
		}
		quantity, err := strconv.ParseFloat(trade.Quantity, 64)
		if err != nil {
			fmt.Printf("❌ Quantity parse failed: %v\n", err)
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
			fmt.Printf("❌ Trade insert failed: %v\n", err)
			continue
		}
		insertedCount++
	}

	// 트랜잭션 커밋
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	fmt.Printf("💾 Batch inserted %d/%d trades\n", insertedCount, len(trades))
	return nil
}

// InsertListingEvent stores a listing announcement event
func (qc *QuestDBClient) InsertListingEvent(event *models.ListingEvent) error {
	// Generate unique ID (timestamp-based)
	id := time.Now().UnixNano()

	// Markets를 JSON 문자열로 변환
	marketsJSON := fmt.Sprintf(`["%s"]`, strings.Join(event.Markets, `","`))

	_, err := qc.insertListingStmt.Exec(
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

	fmt.Printf("📊 Stored listing event: %s\n", event.Symbol)
	return nil
}

// GetTradesByTimeRange retrieves trades for pump analysis
func (qc *QuestDBClient) GetTradesByTimeRange(symbol string, startTime, endTime time.Time) ([]models.TradeEvent, error) {
	query := `
		SELECT timestamp, exchange, market_type, symbol, trade_id, price, quantity, side, collected_at
		FROM trades
		WHERE symbol = '` + symbol + `'
		  AND timestamp >= '` + startTime.Format("2006-01-02T15:04:05.000000Z") + `'
		  AND timestamp <= '` + endTime.Format("2006-01-02T15:04:05.000000Z") + `'
		ORDER BY timestamp ASC
	`

	rows, err := qc.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query trades: %w", err)
	}
	defer rows.Close()

	var trades []models.TradeEvent
	for rows.Next() {
		var trade models.TradeEvent
		var timestamp time.Time
		var collectedAt time.Time

		err := rows.Scan(
			&timestamp,
			&trade.Exchange,
			&trade.MarketType,
			&trade.Symbol,
			&trade.TradeID,
			&trade.Price,
			&trade.Quantity,
			&trade.Side,
			&collectedAt,
		)

		if err != nil {
			fmt.Printf("❌ Failed to scan trade: %v\n", err)
			continue
		}

		// 타임스탬프 변환 (나노초 → 밀리초)
		trade.Timestamp = timestamp.UnixNano() / 1e6
		trade.CollectedAt = collectedAt.UnixNano() / 1e6

		trades = append(trades, trade)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return trades, nil
}

// InsertSystemMetric stores system performance metrics
func (qc *QuestDBClient) InsertSystemMetric(name string, value float64, tags string) error {
	_, err := qc.insertMetricStmt.Exec(
		time.Now(),
		name,
		value,
		tags,
		"main", // node_id
	)

	if err != nil {
		return fmt.Errorf("failed to insert metric: %w", err)
	}

	return nil
}

// Close closes the database connection and prepared statements
func (qc *QuestDBClient) Close() error {
	// Close prepared statements
	if qc.insertTradeStmt != nil {
		qc.insertTradeStmt.Close()
	}
	if qc.insertListingStmt != nil {
		qc.insertListingStmt.Close()
	}
	if qc.insertPumpStmt != nil {
		qc.insertPumpStmt.Close()
	}
	if qc.insertMetricStmt != nil {
		qc.insertMetricStmt.Close()
	}

	// Close database connection
	if qc.db != nil {
		return qc.db.Close()
	}

	fmt.Println("🔌 QuestDB client closed")
	return nil
}

// Health checks database connection health
func (qc *QuestDBClient) Health() error {
	return qc.db.Ping()
}

// GetStats returns database connection statistics
func (qc *QuestDBClient) GetStats() sql.DBStats {
	return qc.db.Stats()
}