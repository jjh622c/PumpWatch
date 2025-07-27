package storage

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"noticepumpcatch/internal/memory"
	"noticepumpcatch/internal/triggers"
)

// StorageManager 파일 기반 스토리지 관리자
type StorageManager struct {
	baseDir       string
	retentionDays int
	mu            sync.RWMutex
	hashCache     map[string]bool // 중복 저장 방지용 해시 캐시

	// 🔥 고루틴 관리용 context 추가
	ctx    context.Context
	cancel context.CancelFunc
}

// StorageConfig 스토리지 설정
type StorageConfig struct {
	BaseDir       string `json:"base_dir"`
	RetentionDays int    `json:"retention_days"`
	CompressData  bool   `json:"compress_data"`
}

// NewStorageManager 스토리지 관리자 생성
func NewStorageManager(config *StorageConfig) *StorageManager {
	ctx, cancel := context.WithCancel(context.Background()) // 🔥 컨텍스트 생성

	sm := &StorageManager{
		baseDir:       config.BaseDir,
		retentionDays: config.RetentionDays,
		hashCache:     make(map[string]bool),
		ctx:           ctx,    // 🔥 컨텍스트 설정
		cancel:        cancel, // 🔥 취소 함수 설정
	}

	// 디렉토리 생성
	sm.createDirectories()

	// 정리 고루틴 시작
	go sm.cleanupRoutine(sm.ctx) // 🔥 context 전달

	return sm
}

// Stop 스토리지 관리자 중지
func (sm *StorageManager) Stop() {
	log.Printf("🛑 스토리지 관리자 중지 시작")
	if sm.cancel != nil {
		sm.cancel()
	}
	log.Printf("✅ 스토리지 관리자 중지 완료")
}

// createDirectories 필요한 디렉토리 생성
func (sm *StorageManager) createDirectories() {
	dirs := []string{
		"signals",
		"orderbooks",
		"trades",
		"snapshots",
	}

	for _, dir := range dirs {
		path := filepath.Join(sm.baseDir, dir)
		if err := os.MkdirAll(path, 0755); err != nil {
			log.Printf("❌ 디렉토리 생성 실패: %s - %v", path, err)
		}
	}
}

// SaveSnapshot 트리거 발생 시 스냅샷 저장
func (sm *StorageManager) SaveSnapshot(trigger *triggers.Trigger, memManager *memory.Manager) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 중복 저장 방지
	hash := sm.generateSnapshotHash(trigger)
	if sm.hashCache[hash] {
		log.Printf("⚠️  중복 스냅샷 무시: %s", trigger.Symbol)
		return nil
	}

	// 스냅샷 데이터 수집
	snapshotData := sm.collectSnapshotData(trigger, memManager)

	// 파일 저장
	if err := sm.saveSnapshotFile(snapshotData); err != nil {
		return fmt.Errorf("스냅샷 저장 실패: %v", err)
	}

	// 해시 캐시에 추가
	sm.hashCache[hash] = true

	log.Printf("💾 스냅샷 저장 완료: %s (%d개 오더북, %d개 체결)",
		trigger.Symbol, len(snapshotData.Orderbooks), len(snapshotData.Trades))

	return nil
}

// generateSnapshotHash 스냅샷 해시 생성 (중복 방지용)
func (sm *StorageManager) generateSnapshotHash(trigger *triggers.Trigger) string {
	// 심볼 + 트리거 타입 + 시간(분 단위) + 신뢰도로 해시 생성
	timeStr := trigger.Timestamp.Format("20060102_1504")
	confidence := "0"
	if trigger.Metadata != nil {
		if conf, ok := trigger.Metadata["confidence"]; ok {
			if confFloat, ok := conf.(float64); ok {
				confidence = fmt.Sprintf("%.1f", confFloat)
			}
		}
	}
	data := fmt.Sprintf("%s_%s_%s_%s", trigger.Symbol, trigger.Type, timeStr, confidence)

	hash := md5.Sum([]byte(data))
	return fmt.Sprintf("%x", hash)
}

// collectSnapshotData 스냅샷 데이터 수집
func (sm *StorageManager) collectSnapshotData(trigger *triggers.Trigger, memManager *memory.Manager) *SnapshotData {
	// 트리거 발생 시점 ±60초 데이터 조회
	startTime := trigger.Timestamp.Add(-60 * time.Second)
	endTime := trigger.Timestamp.Add(60 * time.Second)

	orderbooks := memManager.GetTimeRangeOrderbooks(trigger.Symbol, startTime, endTime)
	trades := memManager.GetTimeRangeTrades(trigger.Symbol, startTime, endTime)

	return &SnapshotData{
		Trigger:    trigger,
		Orderbooks: orderbooks,
		Trades:     trades,
		Metadata: SnapshotMetadata{
			SnapshotID:      sm.generateSnapshotID(trigger),
			CreatedAt:       time.Now(),
			Symbol:          trigger.Symbol,
			TriggerType:     string(trigger.Type),
			TriggerTime:     trigger.Timestamp,
			PreTriggerSecs:  60,
			PostTriggerSecs: 60,
			DataPoints:      len(orderbooks) + len(trades),
			Hash:            sm.generateSnapshotHash(trigger),
		},
	}
}

// saveSnapshotFile 스냅샷 파일 저장
func (sm *StorageManager) saveSnapshotFile(snapshotData *SnapshotData) error {
	// 파일명 생성
	filename := sm.generateSnapshotFilename(snapshotData)
	filepath := filepath.Join(sm.baseDir, "snapshots", filename)

	// JSON 마샬링
	data, err := json.MarshalIndent(snapshotData, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON 마샬링 실패: %v", err)
	}

	// 파일 저장
	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("파일 저장 실패: %v", err)
	}

	// 파일 크기 업데이트
	fileInfo, err := os.Stat(filepath)
	if err == nil {
		snapshotData.Metadata.FileSize = fileInfo.Size()
	}

	return nil
}

// generateSnapshotFilename 스냅샷 파일명 생성
func (sm *StorageManager) generateSnapshotFilename(snapshotData *SnapshotData) string {
	timestamp := snapshotData.Trigger.Timestamp.Format("20060102_150405")
	symbol := strings.ToLower(snapshotData.Trigger.Symbol)
	triggerType := strings.ToLower(string(snapshotData.Trigger.Type))

	return fmt.Sprintf("snapshot_%s_%s_%s.json", timestamp, symbol, triggerType)
}

// generateSnapshotID 스냅샷 ID 생성
func (sm *StorageManager) generateSnapshotID(trigger *triggers.Trigger) string {
	return fmt.Sprintf("snapshot_%s_%d", trigger.Symbol, trigger.Timestamp.UnixNano())
}

// SaveOrderbookData 오더북 데이터 저장
func (sm *StorageManager) SaveOrderbookData(symbol string, orderbooks []*memory.OrderbookSnapshot) error {
	if len(orderbooks) == 0 {
		return nil
	}

	// 날짜별 디렉토리 생성
	date := orderbooks[0].Timestamp.Format("20060102")
	dir := filepath.Join(sm.baseDir, "orderbooks", date)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("디렉토리 생성 실패: %v", err)
	}

	// 파일명 생성
	filename := fmt.Sprintf("orderbook_%s_%s.json", strings.ToLower(symbol), date)
	filepath := filepath.Join(dir, filename)

	// 데이터 저장
	data := OrderbookData{
		Symbol:     symbol,
		Date:       date,
		Orderbooks: orderbooks,
		Count:      len(orderbooks),
		CreatedAt:  time.Now(),
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON 마샬링 실패: %v", err)
	}

	return os.WriteFile(filepath, jsonData, 0644)
}

// SaveTradeData 체결 데이터 저장
func (sm *StorageManager) SaveTradeData(symbol string, trades []*memory.TradeData) error {
	if len(trades) == 0 {
		return nil
	}

	// 날짜별 디렉토리 생성
	date := trades[0].Timestamp.Format("20060102")
	dir := filepath.Join(sm.baseDir, "trades", date)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("디렉토리 생성 실패: %v", err)
	}

	// 파일명 생성
	filename := fmt.Sprintf("trade_%s_%s.json", strings.ToLower(symbol), date)
	filepath := filepath.Join(dir, filename)

	// 데이터 저장
	data := TradeData{
		Symbol:    symbol,
		Date:      date,
		Trades:    trades,
		Count:     len(trades),
		CreatedAt: time.Now(),
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON 마샬링 실패: %v", err)
	}

	return os.WriteFile(filepath, jsonData, 0644)
}

// SaveSignal 시그널 데이터 저장
func (sm *StorageManager) SaveSignal(signal *memory.AdvancedPumpSignal) error {
	// 날짜별 디렉토리 생성
	date := signal.Timestamp.Format("20060102")
	dir := filepath.Join(sm.baseDir, "signals", date)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("디렉토리 생성 실패: %v", err)
	}

	// 파일명 생성
	timestamp := signal.Timestamp.Format("150405")
	filename := fmt.Sprintf("signal_%s_%s_%.0f.json",
		strings.ToLower(signal.Symbol), timestamp, signal.CompositeScore)
	filepath := filepath.Join(dir, filename)

	// 데이터 저장
	jsonData, err := json.MarshalIndent(signal, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON 마샬링 실패: %v", err)
	}

	return os.WriteFile(filepath, jsonData, 0644)
}

// cleanupRoutine 정리 고루틴 (오래된 데이터 제거)
func (sm *StorageManager) cleanupRoutine(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour) // 24시간마다 실행
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("🔴 스토리지 정리 고루틴 컨텍스트 종료")
			return
		case <-ticker.C:
			sm.cleanup()
			sm.cleanupHashCache() // 해시 캐시 정리
		}
	}
}

// cleanup 오래된 데이터 정리
func (sm *StorageManager) cleanup() {
	cutoffDate := time.Now().AddDate(0, 0, -sm.retentionDays)
	cutoffStr := cutoffDate.Format("20060102")

	dirs := []string{"signals", "orderbooks", "trades", "snapshots"}

	for _, dir := range dirs {
		dirPath := filepath.Join(sm.baseDir, dir)
		sm.cleanupDirectory(dirPath, cutoffStr)
	}

	log.Printf("🧹 스토리지 정리 완료 (보관 기간: %d일)", sm.retentionDays)
}

// cleanupDirectory 디렉토리 정리
func (sm *StorageManager) cleanupDirectory(dirPath, cutoffStr string) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		log.Printf("❌ 디렉토리 읽기 실패: %s - %v", dirPath, err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// 날짜 형식 디렉토리인지 확인
			if len(entry.Name()) == 8 && entry.Name() < cutoffStr {
				removePath := filepath.Join(dirPath, entry.Name())
				if err := os.RemoveAll(removePath); err != nil {
					log.Printf("❌ 디렉토리 삭제 실패: %s - %v", removePath, err)
				} else {
					log.Printf("🗑️  오래된 데이터 삭제: %s", removePath)
				}
			}
		}
	}
}

// cleanupHashCache 해시 캐시 정리 (오래된 해시 제거)
func (sm *StorageManager) cleanupHashCache() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 7일 이상 된 해시 제거
	cutoff := time.Now().AddDate(0, 0, -7)
	cutoffStr := cutoff.Format("20060102")

	removed := 0
	for hash := range sm.hashCache {
		// 해시에서 날짜 추출 (format: symbol_type_YYYYMMDD_HHMM_confidence)
		parts := strings.Split(hash, "_")
		if len(parts) >= 3 {
			dateStr := parts[2]
			if dateStr < cutoffStr {
				delete(sm.hashCache, hash)
				removed++
			}
		}
	}

	if removed > 0 {
		log.Printf("🧹 해시 캐시 정리: %d개 제거", removed)
	}
}

// GetStorageStats 스토리지 통계 조회
func (sm *StorageManager) GetStorageStats() map[string]interface{} {
	stats := make(map[string]interface{})

	dirs := []string{"signals", "orderbooks", "trades", "snapshots"}

	for _, dir := range dirs {
		dirPath := filepath.Join(sm.baseDir, dir)
		count := sm.countFiles(dirPath)
		size := sm.calculateDirectorySize(dirPath)

		stats[dir+"_count"] = count
		stats[dir+"_size_mb"] = size / (1024 * 1024)
	}

	stats["total_hash_cache"] = len(sm.hashCache)
	stats["retention_days"] = sm.retentionDays

	return stats
}

// countFiles 디렉토리 내 파일 개수 계산
func (sm *StorageManager) countFiles(dirPath string) int {
	count := 0
	filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			count++
		}
		return nil
	})
	return count
}

// calculateDirectorySize 디렉토리 크기 계산
func (sm *StorageManager) calculateDirectorySize(dirPath string) int64 {
	var size int64
	filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

// SnapshotData 스냅샷 데이터 구조체
type SnapshotData struct {
	Trigger    *triggers.Trigger           `json:"trigger"`
	Orderbooks []*memory.OrderbookSnapshot `json:"orderbooks"`
	Trades     []*memory.TradeData         `json:"trades"`
	Metadata   SnapshotMetadata            `json:"metadata"`
}

// SnapshotMetadata 스냅샷 메타데이터
type SnapshotMetadata struct {
	SnapshotID      string    `json:"snapshot_id"`
	CreatedAt       time.Time `json:"created_at"`
	Symbol          string    `json:"symbol"`
	TriggerType     string    `json:"trigger_type"`
	TriggerTime     time.Time `json:"trigger_time"`
	PreTriggerSecs  int       `json:"pre_trigger_seconds"`
	PostTriggerSecs int       `json:"post_trigger_seconds"`
	DataPoints      int       `json:"data_points"`
	FileSize        int64     `json:"file_size"`
	Hash            string    `json:"hash"`
}

// OrderbookData 오더북 데이터 저장 구조체
type OrderbookData struct {
	Symbol     string                      `json:"symbol"`
	Date       string                      `json:"date"`
	Orderbooks []*memory.OrderbookSnapshot `json:"orderbooks"`
	Count      int                         `json:"count"`
	CreatedAt  time.Time                   `json:"created_at"`
}

// TradeData 체결 데이터 저장 구조체
type TradeData struct {
	Symbol    string              `json:"symbol"`
	Date      string              `json:"date"`
	Trades    []*memory.TradeData `json:"trades"`
	Count     int                 `json:"count"`
	CreatedAt time.Time           `json:"created_at"`
}
