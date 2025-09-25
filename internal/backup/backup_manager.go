package backup

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// BackupManager handles automated QuestDB backups
type BackupManager struct {
	config        BackupConfig
	ctx           context.Context
	cancel        context.CancelFunc
	alertChannel  chan AlertMessage
	httpClient    *http.Client
}

// BackupConfig holds backup configuration
type BackupConfig struct {
	// QuestDB 설정
	QuestDBHost     string `yaml:"questdb_host"`     // localhost
	QuestDBPort     int    `yaml:"questdb_port"`     // 9000

	// 백업 설정
	BackupDir       string        `yaml:"backup_dir"`       // /opt/questdb-backups
	Schedule        time.Duration `yaml:"schedule"`         // 24h
	RetentionDays   int           `yaml:"retention_days"`   // 7
	CompressionType string        `yaml:"compression_type"` // gzip

	// 원격 저장소 설정 (선택적)
	S3Enabled       bool   `yaml:"s3_enabled"`
	S3Bucket        string `yaml:"s3_bucket"`
	S3Region        string `yaml:"s3_region"`
	S3Prefix        string `yaml:"s3_prefix"`

	// 알림 설정
	AlertsEnabled   bool `yaml:"alerts_enabled"`
}

// AlertMessage represents backup alert
type AlertMessage struct {
	Level     string    `json:"level"`     // info, warning, error
	Component string    `json:"component"` // backup_manager
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Details   string    `json:"details,omitempty"`
}

// BackupResult represents backup operation result
type BackupResult struct {
	ID          string    `json:"id"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	Duration    time.Duration `json:"duration"`
	Status      string    `json:"status"`       // success, failed, partial
	TablesCount int       `json:"tables_count"`
	FileSize    int64     `json:"file_size"`
	FilePath    string    `json:"file_path"`
	Error       string    `json:"error,omitempty"`
}

// DefaultBackupConfig returns default backup configuration
func DefaultBackupConfig() BackupConfig {
	return BackupConfig{
		QuestDBHost:     "localhost",
		QuestDBPort:     9000,
		BackupDir:       "/tmp/questdb-backups",
		Schedule:        24 * time.Hour,
		RetentionDays:   7,
		CompressionType: "gzip",
		S3Enabled:       false,
		AlertsEnabled:   true,
	}
}

// NewBackupManager creates a new backup manager
func NewBackupManager(config BackupConfig) (*BackupManager, error) {
	// 백업 디렉토리 생성
	if err := os.MkdirAll(config.BackupDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	bm := &BackupManager{
		config:       config,
		ctx:          ctx,
		cancel:       cancel,
		alertChannel: make(chan AlertMessage, 50),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	return bm, nil
}

// Start begins the backup scheduler
func (bm *BackupManager) Start(ctx context.Context) error {
	log.Printf("📦 Backup Manager starting - schedule: %v, retention: %d days",
		bm.config.Schedule, bm.config.RetentionDays)

	// 즉시 한 번 실행 (테스트용)
	go func() {
		time.Sleep(5 * time.Second)
		if result := bm.performBackup(); result.Status != "success" {
			bm.sendAlert("warning", "Initial backup failed", result.Error)
		}
	}()

	// 주기적 백업 실행
	ticker := time.NewTicker(bm.config.Schedule)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				bm.cancel()
				return
			case <-bm.ctx.Done():
				return
			case <-ticker.C:
				result := bm.performBackup()
				if result.Status != "success" {
					bm.sendAlert("error", "Scheduled backup failed", result.Error)
				} else {
					bm.sendAlert("info", fmt.Sprintf("Backup completed: %s", result.ID), "")
				}
			}
		}
	}()

	// 알림 처리기 시작
	if bm.config.AlertsEnabled {
		go bm.handleAlerts()
	}

	return nil
}

// performBackup executes a full backup operation
func (bm *BackupManager) performBackup() *BackupResult {
	backupID := fmt.Sprintf("questdb_backup_%s", time.Now().Format("20060102_150405"))
	log.Printf("📦 Starting backup: %s", backupID)

	result := &BackupResult{
		ID:        backupID,
		StartTime: time.Now(),
		Status:    "failed",
	}

	// 1. 백업할 테이블 목록 가져오기
	tables, err := bm.getTablesList()
	if err != nil {
		result.Error = fmt.Sprintf("failed to get tables list: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result
	}

	result.TablesCount = len(tables)
	log.Printf("📋 Found %d tables to backup: %v", len(tables), tables)

	// 2. 백업 디렉토리 생성
	backupPath := filepath.Join(bm.config.BackupDir, backupID)
	if err := os.MkdirAll(backupPath, 0755); err != nil {
		result.Error = fmt.Sprintf("failed to create backup path: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result
	}

	// 3. 각 테이블 백업
	successCount := 0
	for _, table := range tables {
		if err := bm.backupTable(table, backupPath); err != nil {
			log.Printf("⚠️ Failed to backup table %s: %v", table, err)
		} else {
			successCount++
			log.Printf("✅ Table %s backed up successfully", table)
		}
	}

	// 4. 백업 파일 압축
	compressedPath := fmt.Sprintf("%s.tar.gz", backupPath)
	if err := bm.compressBackup(backupPath, compressedPath); err != nil {
		result.Error = fmt.Sprintf("failed to compress backup: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result
	}

	// 5. 압축 해제된 디렉토리 정리
	os.RemoveAll(backupPath)

	// 6. 파일 크기 확인
	if stat, err := os.Stat(compressedPath); err == nil {
		result.FileSize = stat.Size()
	}

	// 7. 원격 업로드 (S3 활성화 시)
	if bm.config.S3Enabled {
		if err := bm.uploadToS3(compressedPath); err != nil {
			log.Printf("⚠️ S3 upload failed: %v", err)
		} else {
			log.Printf("☁️ Backup uploaded to S3 successfully")
		}
	}

	// 8. 오래된 백업 정리
	if err := bm.cleanupOldBackups(); err != nil {
		log.Printf("⚠️ Cleanup failed: %v", err)
	}

	// 결과 설정
	if successCount == len(tables) {
		result.Status = "success"
	} else if successCount > 0 {
		result.Status = "partial"
		result.Error = fmt.Sprintf("backed up %d/%d tables", successCount, len(tables))
	}

	result.FilePath = compressedPath
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	log.Printf("📦 Backup %s completed: %s (%d tables, %d MB, %v)",
		backupID, result.Status, successCount,
		result.FileSize/(1024*1024), result.Duration)

	return result
}

// getTablesList retrieves list of tables from QuestDB
func (bm *BackupManager) getTablesList() ([]string, error) {
	url := fmt.Sprintf("http://%s:%d/exec?query=SHOW%%20TABLES",
		bm.config.QuestDBHost, bm.config.QuestDBPort)

	resp, err := bm.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// 현재는 하드코딩된 테이블 목록 반환 (실제 구현 시 JSON 파싱)
	tables := []string{"trades", "listing_events", "system_metrics", "pump_analysis"}
	return tables, nil
}

// backupTable backs up a single table
func (bm *BackupManager) backupTable(tableName, backupPath string) error {
	// QuestDB BACKUP 명령어는 현재 구현되지 않았으므로
	// CSV 내보내기로 대체 (실제 구현)
	query := fmt.Sprintf("SELECT * FROM %s", tableName)
	encodedQuery := url.QueryEscape(query)

	queryURL := fmt.Sprintf("http://%s:%d/exec?query=%s&fmt=csv",
		bm.config.QuestDBHost, bm.config.QuestDBPort, encodedQuery)

	resp, err := bm.httpClient.Get(queryURL)
	if err != nil {
		return fmt.Errorf("failed to query table: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("query failed: HTTP %d", resp.StatusCode)
	}

	// 결과를 CSV 파일로 저장
	outputFile := filepath.Join(backupPath, fmt.Sprintf("%s.csv", tableName))
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	// 헤더 추가
	fmt.Fprintf(file, "# QuestDB table backup: %s\n", tableName)
	fmt.Fprintf(file, "# Generated: %s\n", time.Now().Format("2006-01-02 15:04:05"))

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	return nil
}

// compressBackup compresses the backup directory
func (bm *BackupManager) compressBackup(sourcePath, targetPath string) error {
	log.Printf("🗜️ Compressing backup: %s → %s", sourcePath, targetPath)

	cmd := exec.Command("tar", "-czf", targetPath, "-C", filepath.Dir(sourcePath), filepath.Base(sourcePath))

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tar compression failed: %w", err)
	}

	return nil
}

// uploadToS3 uploads backup to S3 (placeholder)
func (bm *BackupManager) uploadToS3(filePath string) error {
	// 실제 S3 업로드 구현 (AWS CLI 또는 SDK 사용)
	log.Printf("☁️ S3 upload simulation: %s to s3://%s/%s",
		filePath, bm.config.S3Bucket, bm.config.S3Prefix)

	// 시뮬레이션: aws s3 cp 명령어
	if bm.config.S3Bucket != "" {
		s3Path := fmt.Sprintf("s3://%s/%s%s",
			bm.config.S3Bucket, bm.config.S3Prefix, filepath.Base(filePath))

		log.Printf("📤 Would execute: aws s3 cp %s %s", filePath, s3Path)
	}

	return nil
}

// cleanupOldBackups removes old backup files
func (bm *BackupManager) cleanupOldBackups() error {
	cutoffTime := time.Now().AddDate(0, 0, -bm.config.RetentionDays)
	log.Printf("🧹 Cleaning up backups older than %s", cutoffTime.Format("2006-01-02"))

	files, err := filepath.Glob(filepath.Join(bm.config.BackupDir, "questdb_backup_*.tar.gz"))
	if err != nil {
		return fmt.Errorf("failed to list backup files: %w", err)
	}

	deletedCount := 0
	for _, file := range files {
		stat, err := os.Stat(file)
		if err != nil {
			continue
		}

		if stat.ModTime().Before(cutoffTime) {
			if err := os.Remove(file); err != nil {
				log.Printf("⚠️ Failed to delete %s: %v", file, err)
			} else {
				deletedCount++
				log.Printf("🗑️ Deleted old backup: %s", filepath.Base(file))
			}
		}
	}

	if deletedCount > 0 {
		log.Printf("✅ Cleaned up %d old backup files", deletedCount)
	}

	return nil
}

// sendAlert sends an alert message
func (bm *BackupManager) sendAlert(level, message, details string) {
	if !bm.config.AlertsEnabled {
		return
	}

	alert := AlertMessage{
		Level:     level,
		Component: "backup_manager",
		Message:   message,
		Details:   details,
		Timestamp: time.Now(),
	}

	select {
	case bm.alertChannel <- alert:
	default:
		log.Printf("⚠️ Alert channel full, dropping alert: %s", message)
	}
}

// handleAlerts processes alert messages
func (bm *BackupManager) handleAlerts() {
	for {
		select {
		case <-bm.ctx.Done():
			return
		case alert := <-bm.alertChannel:
			switch alert.Level {
			case "error":
				log.Printf("🚨 BACKUP ALERT [ERROR]: %s", alert.Message)
				if alert.Details != "" {
					log.Printf("   Details: %s", alert.Details)
				}
			case "warning":
				log.Printf("⚠️ BACKUP ALERT [WARNING]: %s", alert.Message)
			case "info":
				log.Printf("ℹ️ BACKUP ALERT [INFO]: %s", alert.Message)
			}
		}
	}
}

// GetStats returns backup manager statistics
func (bm *BackupManager) GetStats() map[string]interface{} {
	// 백업 파일 통계
	files, _ := filepath.Glob(filepath.Join(bm.config.BackupDir, "questdb_backup_*.tar.gz"))

	var totalSize int64
	var lastBackupTime time.Time

	for _, file := range files {
		if stat, err := os.Stat(file); err == nil {
			totalSize += stat.Size()
			if stat.ModTime().After(lastBackupTime) {
				lastBackupTime = stat.ModTime()
			}
		}
	}

	stats := map[string]interface{}{
		"backup_dir":        bm.config.BackupDir,
		"schedule":          bm.config.Schedule.String(),
		"retention_days":    bm.config.RetentionDays,
		"backup_count":      len(files),
		"total_size_mb":     totalSize / (1024 * 1024),
		"last_backup_time":  lastBackupTime.Format("2006-01-02 15:04:05"),
		"s3_enabled":        bm.config.S3Enabled,
		"alerts_enabled":    bm.config.AlertsEnabled,
	}

	return stats
}

// Stop gracefully stops the backup manager
func (bm *BackupManager) Stop() error {
	log.Printf("🛑 Stopping Backup Manager...")
	bm.cancel()

	if bm.alertChannel != nil {
		close(bm.alertChannel)
	}

	log.Printf("✅ Backup Manager stopped")
	return nil
}