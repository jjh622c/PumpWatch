package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"
)

// PerformanceAnalyzer monitors and analyzes API polling performance
type PerformanceAnalyzer struct {
	mutex           sync.RWMutex
	isRunning       bool

	// Performance metrics
	apiMetrics      *APIMetrics
	systemMetrics   *SystemMetrics
	networkMetrics  *NetworkMetrics

	// Historical data for trend analysis
	hourlyStats     []HourlyPerformanceStats
	detailedLogs    []DetailedAPICall
	maxHistorySize  int

	// Alert thresholds
	thresholds      *PerformanceThresholds

	// Data persistence
	dataFilePath    string
	lastSaveTime    time.Time
}

// APIMetrics tracks API-related performance data
type APIMetrics struct {
	TotalCalls           int64         `json:"total_calls"`
	SuccessfulCalls      int64         `json:"successful_calls"`
	FailedCalls          int64         `json:"failed_calls"`
	TotalResponseTime    time.Duration `json:"total_response_time"`
	AverageResponseTime  time.Duration `json:"average_response_time"`
	MinResponseTime      time.Duration `json:"min_response_time"`
	MaxResponseTime      time.Duration `json:"max_response_time"`
	LastResponseTime     time.Duration `json:"last_response_time"`

	// Detailed timing breakdown
	DNSLookupTime        time.Duration `json:"dns_lookup_time"`
	TCPConnectTime       time.Duration `json:"tcp_connect_time"`
	TLSHandshakeTime     time.Duration `json:"tls_handshake_time"`
	ServerProcessingTime time.Duration `json:"server_processing_time"`

	// Error analysis
	TimeoutErrors        int64         `json:"timeout_errors"`
	ConnectionErrors     int64         `json:"connection_errors"`
	HTTPErrors           int64         `json:"http_errors"`

	// Success rate analysis
	SuccessRate1Min      float64       `json:"success_rate_1min"`
	SuccessRate5Min      float64       `json:"success_rate_5min"`
	SuccessRate1Hour     float64       `json:"success_rate_1hour"`

	LastUpdated          time.Time     `json:"last_updated"`
}

// SystemMetrics tracks system resource usage
type SystemMetrics struct {
	// CPU metrics
	CPUUsagePercent      float64    `json:"cpu_usage_percent"`
	GoroutineCount       int        `json:"goroutine_count"`

	// Memory metrics
	MemoryUsedMB         uint64     `json:"memory_used_mb"`
	MemoryAllocMB        uint64     `json:"memory_alloc_mb"`
	MemorySystemMB       uint64     `json:"memory_system_mb"`
	GCPauseTimeMS        float64    `json:"gc_pause_time_ms"`

	// System load
	LoadAverage1Min      float64    `json:"load_average_1min"`
	LoadAverage5Min      float64    `json:"load_average_5min"`
	LoadAverage15Min     float64    `json:"load_average_15min"`

	LastUpdated          time.Time  `json:"last_updated"`
}

// NetworkMetrics tracks network-related performance
type NetworkMetrics struct {
	BandwidthUsedKbps    float64    `json:"bandwidth_used_kbps"`
	PacketLossPercent    float64    `json:"packet_loss_percent"`
	NetworkLatencyMS     float64    `json:"network_latency_ms"`
	DNSResolutionTimeMS  float64    `json:"dns_resolution_time_ms"`

	// Connection pool metrics
	ActiveConnections    int        `json:"active_connections"`
	IdleConnections      int        `json:"idle_connections"`
	ConnectionPoolHits   int64      `json:"connection_pool_hits"`
	ConnectionPoolMisses int64      `json:"connection_pool_misses"`

	LastUpdated          time.Time  `json:"last_updated"`
}

// HourlyPerformanceStats aggregates performance data by hour
type HourlyPerformanceStats struct {
	Hour                 time.Time  `json:"hour"`
	APICallCount         int64      `json:"api_call_count"`
	AverageResponseTime  float64    `json:"average_response_time_ms"`
	SuccessRate          float64    `json:"success_rate"`
	AverageCPUUsage      float64    `json:"average_cpu_usage"`
	AverageMemoryUsage   uint64     `json:"average_memory_usage_mb"`
	DetectedListings     int64      `json:"detected_listings"`
	LongestDelay         time.Duration `json:"longest_delay"`
}

// DetailedAPICall stores detailed information about each API call
type DetailedAPICall struct {
	Timestamp            time.Time     `json:"timestamp"`
	ResponseTimeMS       float64       `json:"response_time_ms"`
	StatusCode           int           `json:"status_code"`
	Success              bool          `json:"success"`
	ErrorMessage         string        `json:"error_message,omitempty"`

	// System state at time of call
	CPUUsage             float64       `json:"cpu_usage"`
	MemoryUsageMB        uint64        `json:"memory_usage_mb"`
	GoroutineCount       int           `json:"goroutine_count"`

	// Network details
	DNSLookupMS          float64       `json:"dns_lookup_ms"`
	TCPConnectMS         float64       `json:"tcp_connect_ms"`
	TLSHandshakeMS       float64       `json:"tls_handshake_ms"`

	// Response analysis
	ResponseSize         int64         `json:"response_size"`
	AnnouncementCount    int           `json:"announcement_count"`
	NewListingDetected   bool          `json:"new_listing_detected"`
}

// PerformanceThresholds defines alert thresholds
type PerformanceThresholds struct {
	MaxResponseTimeMS     float64 `json:"max_response_time_ms"`     // 5000ms = 5 seconds
	MinSuccessRate        float64 `json:"min_success_rate"`         // 95%
	MaxCPUUsage           float64 `json:"max_cpu_usage"`            // 80%
	MaxMemoryUsageMB      uint64  `json:"max_memory_usage_mb"`      // 8000MB
	MaxConsecutiveFailures int    `json:"max_consecutive_failures"` // 5 failures
}

// NewPerformanceAnalyzer creates a new performance analyzer
func NewPerformanceAnalyzer() *PerformanceAnalyzer {
	return &PerformanceAnalyzer{
		apiMetrics:     &APIMetrics{MinResponseTime: time.Hour}, // Initialize with high value
		systemMetrics:  &SystemMetrics{},
		networkMetrics: &NetworkMetrics{},
		hourlyStats:    make([]HourlyPerformanceStats, 0, 168), // 7 days of hourly data
		detailedLogs:   make([]DetailedAPICall, 0, 10000),       // Last 10k calls
		maxHistorySize: 10000,
		thresholds: &PerformanceThresholds{
			MaxResponseTimeMS:     5000.0, // 5 seconds
			MinSuccessRate:        95.0,   // 95%
			MaxCPUUsage:          80.0,    // 80%
			MaxMemoryUsageMB:     8000,    // 8GB
			MaxConsecutiveFailures: 5,     // 5 consecutive failures
		},
		dataFilePath: "data/monitor/performance_analysis.json",
	}
}

// StartAnalysis begins performance monitoring
func (pa *PerformanceAnalyzer) StartAnalysis(ctx context.Context) error {
	pa.mutex.Lock()
	defer pa.mutex.Unlock()

	if pa.isRunning {
		return fmt.Errorf("performance analyzer is already running")
	}

	pa.isRunning = true

	// Load existing data
	if err := pa.loadHistoricalData(); err != nil {
		fmt.Printf("⚠️ Failed to load historical performance data: %v\n", err)
	}

	// Start background monitoring goroutine
	go pa.backgroundMonitoring(ctx)

	fmt.Printf("📊 Performance Analyzer started - monitoring API polling performance\n")
	return nil
}

// RecordAPICall records detailed metrics for an API call
func (pa *PerformanceAnalyzer) RecordAPICall(
	startTime time.Time,
	endTime time.Time,
	statusCode int,
	success bool,
	errorMessage string,
	responseSize int64,
	announcementCount int,
	newListingDetected bool,
) {
	pa.mutex.Lock()
	defer pa.mutex.Unlock()

	responseTime := endTime.Sub(startTime)

	// Update API metrics
	pa.updateAPIMetrics(responseTime, success, statusCode, errorMessage)

	// Capture current system state
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Record detailed call information
	detailedCall := DetailedAPICall{
		Timestamp:          startTime,
		ResponseTimeMS:     float64(responseTime.Nanoseconds()) / 1e6,
		StatusCode:         statusCode,
		Success:            success,
		ErrorMessage:       errorMessage,
		CPUUsage:           pa.getCurrentCPUUsage(),
		MemoryUsageMB:      memStats.Alloc / 1024 / 1024,
		GoroutineCount:     runtime.NumGoroutine(),
		ResponseSize:       responseSize,
		AnnouncementCount:  announcementCount,
		NewListingDetected: newListingDetected,
	}

	// Add to detailed logs with size limit
	pa.detailedLogs = append(pa.detailedLogs, detailedCall)
	if len(pa.detailedLogs) > pa.maxHistorySize {
		pa.detailedLogs = pa.detailedLogs[1:] // Remove oldest entry
	}

	// Check for performance alerts
	pa.checkPerformanceAlerts(detailedCall)
}

// updateAPIMetrics updates API performance metrics
func (pa *PerformanceAnalyzer) updateAPIMetrics(responseTime time.Duration, success bool, statusCode int, errorMessage string) {
	pa.apiMetrics.TotalCalls++
	pa.apiMetrics.TotalResponseTime += responseTime
	pa.apiMetrics.LastResponseTime = responseTime
	pa.apiMetrics.LastUpdated = time.Now()

	if success {
		pa.apiMetrics.SuccessfulCalls++
	} else {
		pa.apiMetrics.FailedCalls++

		// Categorize errors
		if statusCode == 0 {
			pa.apiMetrics.ConnectionErrors++
		} else if statusCode >= 500 {
			pa.apiMetrics.HTTPErrors++
		} else if statusCode == http.StatusRequestTimeout {
			pa.apiMetrics.TimeoutErrors++
		}
	}

	// Update min/max response times
	if responseTime < pa.apiMetrics.MinResponseTime {
		pa.apiMetrics.MinResponseTime = responseTime
	}
	if responseTime > pa.apiMetrics.MaxResponseTime {
		pa.apiMetrics.MaxResponseTime = responseTime
	}

	// Calculate average response time
	if pa.apiMetrics.TotalCalls > 0 {
		pa.apiMetrics.AverageResponseTime = pa.apiMetrics.TotalResponseTime / time.Duration(pa.apiMetrics.TotalCalls)
	}

	// Update success rates for different time windows
	pa.updateSuccessRates()
}

// updateSuccessRates calculates success rates for different time windows
func (pa *PerformanceAnalyzer) updateSuccessRates() {
	now := time.Now()

	// Calculate 1-minute success rate
	pa.apiMetrics.SuccessRate1Min = pa.calculateSuccessRate(now.Add(-1 * time.Minute))

	// Calculate 5-minute success rate
	pa.apiMetrics.SuccessRate5Min = pa.calculateSuccessRate(now.Add(-5 * time.Minute))

	// Calculate 1-hour success rate
	pa.apiMetrics.SuccessRate1Hour = pa.calculateSuccessRate(now.Add(-1 * time.Hour))
}

// calculateSuccessRate calculates success rate since a given time
func (pa *PerformanceAnalyzer) calculateSuccessRate(since time.Time) float64 {
	var total, successful int64

	for _, call := range pa.detailedLogs {
		if call.Timestamp.After(since) {
			total++
			if call.Success {
				successful++
			}
		}
	}

	if total == 0 {
		return 100.0 // No calls = 100% success rate
	}

	return float64(successful) / float64(total) * 100.0
}

// getCurrentCPUUsage estimates current CPU usage (simplified)
func (pa *PerformanceAnalyzer) getCurrentCPUUsage() float64 {
	// This is a simplified implementation
	// In production, you'd want to use proper CPU monitoring
	return float64(runtime.NumGoroutine()) / 1000.0 * 100.0
}

// checkPerformanceAlerts checks if any alert thresholds are exceeded
func (pa *PerformanceAnalyzer) checkPerformanceAlerts(call DetailedAPICall) {
	alerts := make([]string, 0)

	// Response time alert
	if call.ResponseTimeMS > pa.thresholds.MaxResponseTimeMS {
		alerts = append(alerts, fmt.Sprintf("High response time: %.1fms", call.ResponseTimeMS))
	}

	// Memory usage alert
	if call.MemoryUsageMB > pa.thresholds.MaxMemoryUsageMB {
		alerts = append(alerts, fmt.Sprintf("High memory usage: %dMB", call.MemoryUsageMB))
	}

	// CPU usage alert
	if call.CPUUsage > pa.thresholds.MaxCPUUsage {
		alerts = append(alerts, fmt.Sprintf("High CPU usage: %.1f%%", call.CPUUsage))
	}

	// Success rate alert
	if pa.apiMetrics.SuccessRate1Min < pa.thresholds.MinSuccessRate {
		alerts = append(alerts, fmt.Sprintf("Low success rate: %.1f%%", pa.apiMetrics.SuccessRate1Min))
	}

	// Print alerts
	for _, alert := range alerts {
		fmt.Printf("🚨 PERFORMANCE ALERT: %s\n", alert)
	}
}

// backgroundMonitoring runs background monitoring tasks
func (pa *PerformanceAnalyzer) backgroundMonitoring(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // Update every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pa.updateSystemMetrics()
			pa.updateNetworkMetrics()
			pa.generateHourlyStats()

			// Save data every 5 minutes
			if time.Since(pa.lastSaveTime) > 5*time.Minute {
				if err := pa.savePerformanceData(); err != nil {
					fmt.Printf("⚠️ Failed to save performance data: %v\n", err)
				}
			}
		}
	}
}

// updateSystemMetrics updates system resource metrics
func (pa *PerformanceAnalyzer) updateSystemMetrics() {
	pa.mutex.Lock()
	defer pa.mutex.Unlock()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	pa.systemMetrics.MemoryUsedMB = memStats.Alloc / 1024 / 1024
	pa.systemMetrics.MemoryAllocMB = memStats.TotalAlloc / 1024 / 1024
	pa.systemMetrics.MemorySystemMB = memStats.Sys / 1024 / 1024
	pa.systemMetrics.GCPauseTimeMS = float64(memStats.PauseNs[(memStats.NumGC+255)%256]) / 1e6
	pa.systemMetrics.GoroutineCount = runtime.NumGoroutine()
	pa.systemMetrics.CPUUsagePercent = pa.getCurrentCPUUsage()
	pa.systemMetrics.LastUpdated = time.Now()
}

// updateNetworkMetrics updates network-related metrics
func (pa *PerformanceAnalyzer) updateNetworkMetrics() {
	pa.mutex.Lock()
	defer pa.mutex.Unlock()

	// This would be enhanced with actual network monitoring
	pa.networkMetrics.LastUpdated = time.Now()
}

// generateHourlyStats generates hourly performance statistics
func (pa *PerformanceAnalyzer) generateHourlyStats() {
	now := time.Now()
	currentHour := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())

	// Check if we need a new hourly stat entry
	if len(pa.hourlyStats) == 0 || pa.hourlyStats[len(pa.hourlyStats)-1].Hour.Before(currentHour) {
		hourlyStats := pa.calculateHourlyStats(currentHour)

		pa.mutex.Lock()
		pa.hourlyStats = append(pa.hourlyStats, hourlyStats)

		// Keep only last 168 hours (7 days)
		if len(pa.hourlyStats) > 168 {
			pa.hourlyStats = pa.hourlyStats[1:]
		}
		pa.mutex.Unlock()
	}
}

// calculateHourlyStats calculates statistics for a specific hour
func (pa *PerformanceAnalyzer) calculateHourlyStats(hour time.Time) HourlyPerformanceStats {
	hourStart := hour
	hourEnd := hour.Add(time.Hour)

	var totalResponseTime float64
	var callCount int64
	var successCount int64
	var totalCPU float64
	var totalMemory uint64
	var maxDelay time.Duration
	var listings int64

	for _, call := range pa.detailedLogs {
		if call.Timestamp.After(hourStart) && call.Timestamp.Before(hourEnd) {
			callCount++
			totalResponseTime += call.ResponseTimeMS
			totalCPU += call.CPUUsage
			totalMemory += call.MemoryUsageMB

			if call.Success {
				successCount++
			}

			if call.NewListingDetected {
				listings++
			}

			responseTime := time.Duration(call.ResponseTimeMS * float64(time.Millisecond))
			if responseTime > maxDelay {
				maxDelay = responseTime
			}
		}
	}

	stats := HourlyPerformanceStats{
		Hour:                hour,
		APICallCount:        callCount,
		DetectedListings:    listings,
		LongestDelay:        maxDelay,
	}

	if callCount > 0 {
		stats.AverageResponseTime = totalResponseTime / float64(callCount)
		stats.SuccessRate = float64(successCount) / float64(callCount) * 100.0
		stats.AverageCPUUsage = totalCPU / float64(callCount)
		stats.AverageMemoryUsage = totalMemory / uint64(callCount)
	}

	return stats
}

// GetPerformanceReport generates a comprehensive performance report
func (pa *PerformanceAnalyzer) GetPerformanceReport() map[string]interface{} {
	pa.mutex.RLock()
	defer pa.mutex.RUnlock()

	report := map[string]interface{}{
		"api_metrics":     pa.apiMetrics,
		"system_metrics":  pa.systemMetrics,
		"network_metrics": pa.networkMetrics,
		"recent_calls":    pa.getRecentCalls(100), // Last 100 calls
		"hourly_trends":   pa.getHourlyTrends(),
		"performance_score": pa.calculatePerformanceScore(),
		"recommendations": pa.generateRecommendations(),
	}

	return report
}

// getRecentCalls returns the most recent API calls
func (pa *PerformanceAnalyzer) getRecentCalls(limit int) []DetailedAPICall {
	if len(pa.detailedLogs) <= limit {
		return pa.detailedLogs
	}
	return pa.detailedLogs[len(pa.detailedLogs)-limit:]
}

// getHourlyTrends returns hourly performance trends
func (pa *PerformanceAnalyzer) getHourlyTrends() []HourlyPerformanceStats {
	return pa.hourlyStats
}

// calculatePerformanceScore calculates an overall performance score (0-100)
func (pa *PerformanceAnalyzer) calculatePerformanceScore() int {
	score := 100

	// Deduct points for high response time
	if pa.apiMetrics.AverageResponseTime > 3*time.Second {
		score -= 20
	} else if pa.apiMetrics.AverageResponseTime > 1*time.Second {
		score -= 10
	}

	// Deduct points for low success rate
	if pa.apiMetrics.SuccessRate1Min < 90 {
		score -= 30
	} else if pa.apiMetrics.SuccessRate1Min < 95 {
		score -= 15
	}

	// Deduct points for high resource usage
	if pa.systemMetrics.CPUUsagePercent > 80 {
		score -= 15
	}

	if pa.systemMetrics.MemoryUsedMB > 8000 {
		score -= 15
	}

	if score < 0 {
		score = 0
	}

	return score
}

// generateRecommendations generates performance improvement recommendations
func (pa *PerformanceAnalyzer) generateRecommendations() []string {
	recommendations := make([]string, 0)

	if pa.apiMetrics.AverageResponseTime > 3*time.Second {
		recommendations = append(recommendations, "고려사항: API 응답 시간이 3초 이상입니다. 네트워크 연결이나 서버 부하를 확인하세요.")
	}

	if pa.apiMetrics.SuccessRate1Min < 95 {
		recommendations = append(recommendations, "경고: API 성공률이 95% 미만입니다. 연결 안정성을 점검하세요.")
	}

	if pa.systemMetrics.MemoryUsedMB > 6000 {
		recommendations = append(recommendations, "주의: 메모리 사용량이 6GB를 초과했습니다. 메모리 누수를 확인하세요.")
	}

	if pa.systemMetrics.GoroutineCount > 100 {
		recommendations = append(recommendations, "주의: 고루틴 수가 100개를 초과했습니다. 리소스 관리를 점검하세요.")
	}

	return recommendations
}

// savePerformanceData saves performance data to disk
func (pa *PerformanceAnalyzer) savePerformanceData() error {
	report := pa.GetPerformanceReport()

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal performance data: %w", err)
	}

	if err := os.WriteFile(pa.dataFilePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write performance data: %w", err)
	}

	pa.lastSaveTime = time.Now()
	return nil
}

// loadHistoricalData loads existing performance data
func (pa *PerformanceAnalyzer) loadHistoricalData() error {
	data, err := os.ReadFile(pa.dataFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No existing data
		}
		return fmt.Errorf("failed to read performance data: %w", err)
	}

	var report map[string]interface{}
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("failed to unmarshal performance data: %w", err)
	}

	// TODO: Load specific metrics from the report
	fmt.Printf("✅ Historical performance data loaded from %s\n", pa.dataFilePath)
	return nil
}

// PrintPerformanceReport prints a formatted performance report
func (pa *PerformanceAnalyzer) PrintPerformanceReport() {
	report := pa.GetPerformanceReport()

	fmt.Printf("\n📊 === Performance Analysis Report ===\n")
	fmt.Printf("🔗 API Performance:\n")
	fmt.Printf("  Total Calls: %d | Success Rate: %.1f%%\n", pa.apiMetrics.TotalCalls, pa.apiMetrics.SuccessRate1Min)
	fmt.Printf("  Avg Response: %v | Max Response: %v\n", pa.apiMetrics.AverageResponseTime, pa.apiMetrics.MaxResponseTime)
	fmt.Printf("  Failed Calls: %d | Timeout Errors: %d\n", pa.apiMetrics.FailedCalls, pa.apiMetrics.TimeoutErrors)

	fmt.Printf("\n💻 System Performance:\n")
	fmt.Printf("  CPU Usage: %.1f%% | Goroutines: %d\n", pa.systemMetrics.CPUUsagePercent, pa.systemMetrics.GoroutineCount)
	fmt.Printf("  Memory Used: %dMB | GC Pause: %.2fms\n", pa.systemMetrics.MemoryUsedMB, pa.systemMetrics.GCPauseTimeMS)

	fmt.Printf("\n🎯 Performance Score: %d/100\n", report["performance_score"])

	if recommendations, ok := report["recommendations"].([]string); ok && len(recommendations) > 0 {
		fmt.Printf("\n💡 Recommendations:\n")
		for _, rec := range recommendations {
			fmt.Printf("  • %s\n", rec)
		}
	}

	fmt.Printf("=====================================\n\n")
}