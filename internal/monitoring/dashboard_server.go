package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"PumpWatch/internal/database"
	"PumpWatch/internal/maintenance"
)

// DashboardServer provides real-time monitoring REST API
type DashboardServer struct {
	questDB    *database.QuestDBManager
	ttlManager *maintenance.TTLManager
	httpServer *http.Server
	metrics    *MetricsCollector
	startTime  time.Time
}

// DashboardConfig holds dashboard server configuration
type DashboardConfig struct {
	Port        int    `yaml:"port"`         // 8080
	Host        string `yaml:"host"`         // localhost
	EnableCORS  bool   `yaml:"enable_cors"`  // true
	MetricsPath string `yaml:"metrics_path"` // /metrics
}

// MetricsCollector gathers system metrics
type MetricsCollector struct {
	questDB   *database.QuestDBManager
	startTime time.Time

	// 캐시된 메트릭
	lastUpdate       time.Time
	cachedMetrics    *DashboardMetrics
	cacheValidityDur time.Duration
}

// DashboardMetrics represents all dashboard metrics
type DashboardMetrics struct {
	// 시스템 상태
	SystemHealth    SystemHealth    `json:"system_health"`
	QuestDBHealth   QuestDBHealth   `json:"questdb_health"`

	// 성능 지표
	TradesPerSecond float64         `json:"trades_per_second"`
	CacheHitRate    float64         `json:"cache_hit_rate"`

	// 용량 지표
	DiskUsage       *maintenance.DiskUsage `json:"disk_usage"`

	// 최근 활동
	LastListingEvent *ListingEvent  `json:"last_listing_event"`
	RecentTrades     []TradeInfo    `json:"recent_trades"`

	// 시간 정보
	Uptime          string         `json:"uptime"`
	LastUpdate      time.Time      `json:"last_update"`
}

// SystemHealth represents overall system health
type SystemHealth struct {
	Status      string    `json:"status"`      // healthy, warning, error
	Uptime      string    `json:"uptime"`
	MemoryUsage float64   `json:"memory_usage"`
	CPUUsage    float64   `json:"cpu_usage"`
	LastCheck   time.Time `json:"last_check"`
}

// QuestDBHealth represents QuestDB specific health metrics
type QuestDBHealth struct {
	Status            string        `json:"status"`
	BatchesProcessed  int64         `json:"batches_processed"`
	FailedBatches     int64         `json:"failed_batches"`
	DroppedTrades     int64         `json:"dropped_trades"`
	AverageLatency    time.Duration `json:"average_latency"`
	LastFlushTime     time.Time     `json:"last_flush_time"`
	WorkerStatus      []string      `json:"worker_status"`
}

// ListingEvent represents a listing event for API response
type ListingEvent struct {
	Symbol      string    `json:"symbol"`
	Title       string    `json:"title"`
	DetectedAt  time.Time `json:"detected_at"`
	TriggerTime time.Time `json:"trigger_time"`
	IsKRWListing bool     `json:"is_krw_listing"`
}

// TradeInfo represents trade information for API response
type TradeInfo struct {
	Timestamp   int64   `json:"timestamp"`
	Exchange    string  `json:"exchange"`
	MarketType  string  `json:"market_type"`
	Symbol      string  `json:"symbol"`
	Price       float64 `json:"price"`
	Quantity    float64 `json:"quantity"`
	Side        string  `json:"side"`
}

// DefaultDashboardConfig returns default dashboard configuration
func DefaultDashboardConfig() DashboardConfig {
	return DashboardConfig{
		Port:        8080,
		Host:        "localhost",
		EnableCORS:  true,
		MetricsPath: "/metrics",
	}
}

// NewDashboardServer creates a new dashboard server
func NewDashboardServer(
	questDB *database.QuestDBManager,
	ttlManager *maintenance.TTLManager,
	config DashboardConfig) *DashboardServer {

	metrics := &MetricsCollector{
		questDB:          questDB,
		startTime:        time.Now(),
		cacheValidityDur: 5 * time.Second, // 5초 캐시
	}

	server := &DashboardServer{
		questDB:    questDB,
		ttlManager: ttlManager,
		metrics:    metrics,
		startTime:  time.Now(),
	}

	// HTTP 서버 설정
	mux := http.NewServeMux()
	server.setupRoutes(mux, config)

	server.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.Host, config.Port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return server
}

// setupRoutes configures HTTP routes
func (ds *DashboardServer) setupRoutes(mux *http.ServeMux, config DashboardConfig) {
	// CORS 미들웨어 래퍼
	corsHandler := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if config.EnableCORS {
				w.Header().Set("Access-Control-Allow-Origin", "*")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

				if r.Method == "OPTIONS" {
					w.WriteHeader(http.StatusOK)
					return
				}
			}
			next(w, r)
		}
	}

	// API 엔드포인트 설정
	mux.HandleFunc("/api/health", corsHandler(ds.handleHealth))
	mux.HandleFunc("/api/metrics", corsHandler(ds.handleMetrics))
	mux.HandleFunc("/api/trades/recent", corsHandler(ds.handleRecentTrades))
	mux.HandleFunc("/api/listings", corsHandler(ds.handleListings))
	mux.HandleFunc("/api/system/status", corsHandler(ds.handleSystemStatus))

	// 정적 파일 (향후 웹 UI용)
	mux.HandleFunc("/", corsHandler(ds.handleRoot))

	log.Printf("🌐 Dashboard routes configured:")
	log.Printf("  - GET  /api/health")
	log.Printf("  - GET  /api/metrics")
	log.Printf("  - GET  /api/trades/recent?limit=N")
	log.Printf("  - GET  /api/listings?limit=N")
	log.Printf("  - GET  /api/system/status")
}

// Start starts the dashboard server
func (ds *DashboardServer) Start(ctx context.Context) error {
	log.Printf("🚀 Starting Dashboard Server on %s", ds.httpServer.Addr)

	// 서버 시작
	go func() {
		if err := ds.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("❌ Dashboard server error: %v", err)
		}
	}()

	// Context 기반 종료 처리
	go func() {
		<-ctx.Done()
		log.Printf("🛑 Shutting down Dashboard Server...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := ds.httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("⚠️ Dashboard server shutdown error: %v", err)
		}
	}()

	return nil
}

// handleHealth handles /api/health endpoint
func (ds *DashboardServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now(),
		"uptime":    time.Since(ds.startTime).String(),
		"services": map[string]string{
			"questdb":    ds.getQuestDBStatus(),
			"ttl_manager": "running",
			"dashboard":  "running",
		},
	}

	ds.writeJSONResponse(w, http.StatusOK, health)
}

// handleMetrics handles /api/metrics endpoint
func (ds *DashboardServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	metrics, err := ds.metrics.GetMetrics()
	if err != nil {
		ds.writeErrorResponse(w, http.StatusInternalServerError, "Failed to get metrics", err)
		return
	}

	ds.writeJSONResponse(w, http.StatusOK, metrics)
}

// handleRecentTrades handles /api/trades/recent endpoint
func (ds *DashboardServer) handleRecentTrades(w http.ResponseWriter, r *http.Request) {
	limit := 100 // 기본값
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	trades, err := ds.getRecentTradesFromQuestDB(limit)
	if err != nil {
		ds.writeErrorResponse(w, http.StatusInternalServerError, "Failed to get recent trades", err)
		return
	}

	response := map[string]interface{}{
		"trades":    trades,
		"count":     len(trades),
		"limit":     limit,
		"timestamp": time.Now(),
	}

	ds.writeJSONResponse(w, http.StatusOK, response)
}

// handleListings handles /api/listings endpoint
func (ds *DashboardServer) handleListings(w http.ResponseWriter, r *http.Request) {
	limit := 50 // 기본값
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	listings, err := ds.getRecentListingsFromQuestDB(limit)
	if err != nil {
		ds.writeErrorResponse(w, http.StatusInternalServerError, "Failed to get listings", err)
		return
	}

	response := map[string]interface{}{
		"listings":  listings,
		"count":     len(listings),
		"limit":     limit,
		"timestamp": time.Now(),
	}

	ds.writeJSONResponse(w, http.StatusOK, response)
}

// handleSystemStatus handles /api/system/status endpoint
func (ds *DashboardServer) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	// TTL Manager 상태
	var ttlStats map[string]interface{}
	if ds.ttlManager != nil {
		ttlStats = ds.ttlManager.GetStats()
	}

	// QuestDB 상태
	questDBStats := ds.questDB.GetStats()

	status := map[string]interface{}{
		"system": map[string]interface{}{
			"uptime":     time.Since(ds.startTime).String(),
			"timestamp":  time.Now(),
		},
		"questdb": map[string]interface{}{
			"batches_processed": questDBStats.BatchesProcessed,
			"failed_batches":    questDBStats.FailedBatches,
			"dropped_trades":    questDBStats.DroppedTrades,
			"last_flush":        questDBStats.LastFlushTime,
		},
		"ttl_manager": ttlStats,
	}

	ds.writeJSONResponse(w, http.StatusOK, status)
}

// handleRoot handles / endpoint (기본 페이지)
func (ds *DashboardServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	html := `
<!DOCTYPE html>
<html>
<head>
    <title>PumpWatch Dashboard</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; }
        .endpoint { margin: 10px 0; }
        .endpoint a { text-decoration: none; color: #0066cc; }
        .endpoint a:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <h1>🚀 PumpWatch Dashboard API</h1>
    <p>QuestDB 기반 실시간 모니터링 시스템</p>

    <h2>📊 Available Endpoints:</h2>
    <div class="endpoint">🏥 <a href="/api/health">Health Check</a></div>
    <div class="endpoint">📈 <a href="/api/metrics">System Metrics</a></div>
    <div class="endpoint">💹 <a href="/api/trades/recent?limit=10">Recent Trades</a></div>
    <div class="endpoint">📋 <a href="/api/listings?limit=5">Listing Events</a></div>
    <div class="endpoint">⚙️ <a href="/api/system/status">System Status</a></div>

    <h2>🕐 Server Info:</h2>
    <p>Started: ` + ds.startTime.Format("2006-01-02 15:04:05") + `</p>
    <p>Uptime: ` + time.Since(ds.startTime).String() + `</p>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
}

// Helper methods

func (ds *DashboardServer) getQuestDBStatus() string {
	stats := ds.questDB.GetStats()
	if stats.FailedBatches > stats.BatchesProcessed/10 { // 10% 이상 실패 시
		return "warning"
	}
	return "healthy"
}

func (ds *DashboardServer) getRecentTradesFromQuestDB(limit int) ([]TradeInfo, error) {
	// 현재는 빈 배열 반환 (향후 실제 QuestDB 쿼리 구현)
	// 실제 구현 시에는 HTTP API나 SQL 쿼리를 통해 최근 거래 데이터 조회
	return []TradeInfo{}, nil
}

func (ds *DashboardServer) getRecentListingsFromQuestDB(limit int) ([]ListingEvent, error) {
	// 현재는 빈 배열 반환 (향후 실제 QuestDB 쿼리 구현)
	return []ListingEvent{}, nil
}

func (ds *DashboardServer) writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("⚠️ JSON encoding error: %v", err)
	}
}

func (ds *DashboardServer) writeErrorResponse(w http.ResponseWriter, statusCode int, message string, err error) {
	errorResponse := map[string]interface{}{
		"error":     message,
		"timestamp": time.Now(),
	}

	if err != nil {
		errorResponse["details"] = err.Error()
		log.Printf("❌ API Error: %s - %v", message, err)
	}

	ds.writeJSONResponse(w, statusCode, errorResponse)
}

// GetMetrics collects and returns current metrics
func (mc *MetricsCollector) GetMetrics() (*DashboardMetrics, error) {
	// 캐시 유효성 체크
	if time.Since(mc.lastUpdate) < mc.cacheValidityDur && mc.cachedMetrics != nil {
		return mc.cachedMetrics, nil
	}

	// QuestDB 통계 수집
	questDBStats := mc.questDB.GetStats()

	// 메트릭 생성
	metrics := &DashboardMetrics{
		SystemHealth: SystemHealth{
			Status:    "healthy",
			Uptime:    time.Since(mc.startTime).String(),
			LastCheck: time.Now(),
		},
		QuestDBHealth: QuestDBHealth{
			Status:           "healthy",
			BatchesProcessed: questDBStats.BatchesProcessed,
			FailedBatches:    questDBStats.FailedBatches,
			DroppedTrades:    questDBStats.DroppedTrades,
			AverageLatency:   questDBStats.AverageLatency,
			LastFlushTime:    questDBStats.LastFlushTime,
			WorkerStatus:     questDBStats.WorkerStatus[:],
		},
		TradesPerSecond: mc.calculateTradesPerSecond(),
		CacheHitRate:    0.0, // 향후 구현
		RecentTrades:    []TradeInfo{}, // 향후 구현
		Uptime:         time.Since(mc.startTime).String(),
		LastUpdate:     time.Now(),
	}

	// 캐시 업데이트
	mc.cachedMetrics = metrics
	mc.lastUpdate = time.Now()

	return metrics, nil
}

func (mc *MetricsCollector) calculateTradesPerSecond() float64 {
	stats := mc.questDB.GetStats()
	uptime := time.Since(mc.startTime).Seconds()
	if uptime > 0 {
		return float64(stats.TotalTrades) / uptime
	}
	return 0.0
}

// Stop gracefully stops the dashboard server
func (ds *DashboardServer) Stop() error {
	log.Printf("🛑 Stopping Dashboard Server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := ds.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("dashboard server shutdown failed: %w", err)
	}

	log.Printf("✅ Dashboard Server stopped")
	return nil
}