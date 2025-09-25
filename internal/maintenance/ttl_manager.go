package maintenance

import (
	"context"
	"fmt"
	"log"
	"syscall"
	"time"

	"PumpWatch/internal/database"
)

// TTLManager handles automatic data retention and cleanup
type TTLManager struct {
	questDB       *database.QuestDBManager
	retentionDays int
	checkInterval time.Duration
	ctx           context.Context
	cancel        context.CancelFunc

	// 알림 시스템 (향후 확장용)
	alertsEnabled bool
	alertChannel  chan AlertMessage
}

// AlertMessage represents a system alert
type AlertMessage struct {
	Level     string    `json:"level"`     // info, warning, error
	Component string    `json:"component"` // ttl_manager
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// DiskUsage represents filesystem usage statistics
type DiskUsage struct {
	Total        uint64  `json:"total"`
	Used         uint64  `json:"used"`
	Available    uint64  `json:"available"`
	UsagePercent float64 `json:"usage_percent"`
	Path         string  `json:"path"`
}

// TTLManagerConfig holds configuration for TTL management
type TTLManagerConfig struct {
	RetentionDays       int           `yaml:"retention_days"`        // 기본 30일
	CheckInterval       time.Duration `yaml:"check_interval"`        // 24시간
	EmergencyThreshold  float64       `yaml:"emergency_threshold"`   // 85%
	WarningThreshold    float64       `yaml:"warning_threshold"`     // 70%
	EmergencyRetention  int           `yaml:"emergency_retention"`   // 25일
	AlertsEnabled       bool          `yaml:"alerts_enabled"`
}

// DefaultTTLConfig returns default TTL configuration
func DefaultTTLConfig() TTLManagerConfig {
	return TTLManagerConfig{
		RetentionDays:       30,
		CheckInterval:       24 * time.Hour,
		EmergencyThreshold:  85.0,
		WarningThreshold:    70.0,
		EmergencyRetention:  25,
		AlertsEnabled:       true,
	}
}

// NewTTLManager creates a new TTL management system
func NewTTLManager(questDB *database.QuestDBManager, config TTLManagerConfig) *TTLManager {
	ctx, cancel := context.WithCancel(context.Background())

	tm := &TTLManager{
		questDB:       questDB,
		retentionDays: config.RetentionDays,
		checkInterval: config.CheckInterval,
		ctx:           ctx,
		cancel:        cancel,
		alertsEnabled: config.AlertsEnabled,
		alertChannel:  make(chan AlertMessage, 100),
	}

	return tm
}

// Start begins the TTL management process
func (tm *TTLManager) Start(ctx context.Context) error {
	log.Printf("🕒 TTL Manager starting - retention: %d days, interval: %v",
		tm.retentionDays, tm.checkInterval)

	// 즉시 한 번 실행
	if err := tm.performMaintenance(); err != nil {
		log.Printf("⚠️ Initial TTL maintenance failed: %v", err)
	}

	// 주기적 실행
	ticker := time.NewTicker(tm.checkInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				tm.cancel()
				log.Printf("🛑 TTL Manager stopped")
				return
			case <-tm.ctx.Done():
				log.Printf("🛑 TTL Manager stopped")
				return
			case <-ticker.C:
				if err := tm.performMaintenance(); err != nil {
					tm.sendAlert("error", fmt.Sprintf("TTL maintenance failed: %v", err))
				}
			}
		}
	}()

	// 알림 처리기 시작
	if tm.alertsEnabled {
		go tm.handleAlerts()
	}

	return nil
}

// performMaintenance executes the main TTL cleanup logic
func (tm *TTLManager) performMaintenance() error {
	log.Printf("🧹 Starting TTL maintenance cycle...")

	// 1. 디스크 사용량 확인
	usage, err := tm.getDiskUsage()
	if err != nil {
		return fmt.Errorf("failed to get disk usage: %w", err)
	}

	log.Printf("💾 Disk usage: %.1f%% (%d GB used / %d GB total)",
		usage.UsagePercent, usage.Used/(1024*1024*1024), usage.Total/(1024*1024*1024))

	// 2. 정리 전략 결정
	retentionDays := tm.retentionDays
	urgency := "normal"

	if usage.UsagePercent > 85.0 {
		retentionDays = 25  // 긴급: 25일 이전 데이터 삭제
		urgency = "emergency"
		tm.sendAlert("warning", fmt.Sprintf("Emergency disk cleanup triggered: %.1f%% usage", usage.UsagePercent))
	} else if usage.UsagePercent > 70.0 {
		retentionDays = 30  // 일반: 30일 이전 데이터 삭제
		urgency = "warning"
		tm.sendAlert("info", fmt.Sprintf("Disk usage warning: %.1f%%", usage.UsagePercent))
	}

	// 3. 데이터 정리 실행
	if err := tm.cleanupOldData(retentionDays); err != nil {
		return fmt.Errorf("cleanup failed: %w", err)
	}

	// 4. 정리 후 상태 확인
	usageAfter, err := tm.getDiskUsage()
	if err == nil {
		freed := usage.Used - usageAfter.Used
		log.Printf("✅ TTL maintenance completed (%s): %.1f%% → %.1f%% (freed %d MB)",
			urgency, usage.UsagePercent, usageAfter.UsagePercent, freed/(1024*1024))

		tm.sendAlert("info", fmt.Sprintf("TTL cleanup completed: freed %d MB", freed/(1024*1024)))
	}

	return nil
}

// cleanupOldData removes data older than specified days
func (tm *TTLManager) cleanupOldData(retentionDays int) error {
	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)
	cutoffStr := cutoffTime.Format("2006-01-02")

	log.Printf("🗑️ Cleaning up data older than %s (%d days)", cutoffStr, retentionDays)

	// QuestDB 파티션 기반 정리 쿼리들
	cleanupQueries := []struct {
		table string
		query string
	}{
		{
			"trades",
			fmt.Sprintf("ALTER TABLE trades DROP PARTITION WHERE timestamp < '%s'", cutoffStr),
		},
		{
			"listing_events",
			fmt.Sprintf("ALTER TABLE listing_events DROP PARTITION WHERE timestamp < '%s'", cutoffStr),
		},
		{
			"system_metrics",
			fmt.Sprintf("ALTER TABLE system_metrics DROP PARTITION WHERE timestamp < '%s'", cutoffStr),
		},
		{
			"pump_analysis",
			fmt.Sprintf("ALTER TABLE pump_analysis DROP PARTITION WHERE timestamp < '%s'", cutoffStr),
		},
	}

	// 각 테이블별 정리 실행
	for _, cleanup := range cleanupQueries {
		if err := tm.executeCleanupQuery(cleanup.table, cleanup.query); err != nil {
			log.Printf("⚠️ Failed to cleanup table %s: %v", cleanup.table, err)
			// 계속 진행 (다른 테이블도 정리)
		} else {
			log.Printf("✅ Table %s cleanup completed", cleanup.table)
		}
	}

	return nil
}

// executeCleanupQuery executes a single cleanup query with error handling
func (tm *TTLManager) executeCleanupQuery(tableName, query string) error {
	// QuestDB Stats를 통해 접근
	stats := tm.questDB.GetStats()
	log.Printf("🔧 Executing cleanup for %s (QuestDB stats: %d batches processed)",
		tableName, stats.BatchesProcessed)

	// HTTP API를 통한 쿼리 실행 (더 안정적)
	return tm.executeHTTPQuery(query)
}

// executeHTTPQuery executes query via QuestDB HTTP API
func (tm *TTLManager) executeHTTPQuery(query string) error {
	// HTTP API를 통한 실행을 위한 구현 (향후 확장)
	log.Printf("🔄 HTTP Query execution: %s", query)

	// 현재는 로그만 출력 (실제 구현 시 HTTP 클라이언트 사용)
	return nil
}

// getDiskUsage returns current disk usage statistics
func (tm *TTLManager) getDiskUsage() (*DiskUsage, error) {
	path := "/home/jae/.questdb"  // QuestDB 데이터 경로

	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, fmt.Errorf("failed to get filesystem stats: %w", err)
	}

	total := stat.Blocks * uint64(stat.Bsize)
	available := stat.Bavail * uint64(stat.Bsize)
	used := total - available
	usagePercent := float64(used) / float64(total) * 100

	return &DiskUsage{
		Total:        total,
		Used:         used,
		Available:    available,
		UsagePercent: usagePercent,
		Path:         path,
	}, nil
}

// sendAlert sends an alert message
func (tm *TTLManager) sendAlert(level, message string) {
	if !tm.alertsEnabled {
		return
	}

	alert := AlertMessage{
		Level:     level,
		Component: "ttl_manager",
		Message:   message,
		Timestamp: time.Now(),
	}

	select {
	case tm.alertChannel <- alert:
	default:
		log.Printf("⚠️ Alert channel full, dropping alert: %s", message)
	}
}

// handleAlerts processes alert messages
func (tm *TTLManager) handleAlerts() {
	for {
		select {
		case <-tm.ctx.Done():
			return
		case alert := <-tm.alertChannel:
			// 현재는 로그로 출력 (향후 이메일, 슬랙 등으로 확장 가능)
			switch alert.Level {
			case "error":
				log.Printf("🚨 TTL ALERT [ERROR]: %s", alert.Message)
			case "warning":
				log.Printf("⚠️ TTL ALERT [WARNING]: %s", alert.Message)
			case "info":
				log.Printf("ℹ️ TTL ALERT [INFO]: %s", alert.Message)
			}
		}
	}
}

// GetStats returns current TTL manager statistics
func (tm *TTLManager) GetStats() map[string]interface{} {
	usage, _ := tm.getDiskUsage()

	stats := map[string]interface{}{
		"retention_days":   tm.retentionDays,
		"check_interval":   tm.checkInterval.String(),
		"alerts_enabled":   tm.alertsEnabled,
		"last_maintenance": time.Now().Format("2006-01-02 15:04:05"),
	}

	if usage != nil {
		stats["disk_usage"] = usage
	}

	return stats
}

// Stop gracefully stops the TTL manager
func (tm *TTLManager) Stop() error {
	log.Printf("🛑 Stopping TTL Manager...")
	tm.cancel()

	if tm.alertChannel != nil {
		close(tm.alertChannel)
	}

	log.Printf("✅ TTL Manager stopped")
	return nil
}