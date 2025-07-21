package triggers

import (
	"fmt"
	"log"
	"sync"
	"time"

	"noticepumpcatch/internal/memory"
)

// TriggerType 트리거 유형
type TriggerType string

const (
	TriggerTypePumpDetection TriggerType = "pump_detection"
	TriggerTypeListing       TriggerType = "listing_announcement"
	TriggerTypeVolumeSpike   TriggerType = "volume_spike"
	TriggerTypePriceSpike    TriggerType = "price_spike"
)

// Trigger 트리거 구조체
type Trigger struct {
	ID          string                 `json:"id"`
	Type        TriggerType            `json:"type"`
	Symbol      string                 `json:"symbol"`
	Timestamp   time.Time              `json:"timestamp"`
	Confidence  float64                `json:"confidence"`  // 0-100
	Score       float64                `json:"score"`       // 트리거 점수
	Description string                 `json:"description"` // 트리거 설명
	Metadata    map[string]interface{} `json:"metadata"`    // 추가 메타데이터
}

// TriggerHandler 트리거 핸들러 인터페이스
type TriggerHandler interface {
	HandleTrigger(trigger *Trigger) error
}

// Manager 트리거 관리자
type Manager struct {
	mu sync.RWMutex

	// 핸들러 등록
	handlers map[TriggerType][]TriggerHandler

	// 트리거 설정
	config *TriggerConfig

	// 메모리 관리자 참조
	memManager *memory.Manager

	// 트리거 통계
	stats *TriggerStats
}

// TriggerConfig 트리거 설정
type TriggerConfig struct {
	PumpDetection PumpDetectionConfig `json:"pump_detection"`
	Snapshot      SnapshotConfig      `json:"snapshot"`
}

// PumpDetectionConfig 펌핑 감지 설정
type PumpDetectionConfig struct {
	Enabled              bool    `json:"enabled"`
	MinScore             float64 `json:"min_score"`
	VolumeThreshold      float64 `json:"volume_threshold"`
	PriceChangeThreshold float64 `json:"price_change_threshold"`
	TimeWindowSeconds    int     `json:"time_window_seconds"`
}

// SnapshotConfig 스냅샷 설정
type SnapshotConfig struct {
	PreTriggerSeconds  int `json:"pre_trigger_seconds"`
	PostTriggerSeconds int `json:"post_trigger_seconds"`
	MaxSnapshotsPerDay int `json:"max_snapshots_per_day"`
}

// TriggerStats 트리거 통계
type TriggerStats struct {
	TotalTriggers     int                 `json:"total_triggers"`
	TriggersByType    map[TriggerType]int `json:"triggers_by_type"`
	TriggersBySymbol  map[string]int      `json:"triggers_by_symbol"`
	LastTriggerTime   time.Time           `json:"last_trigger_time"`
	DailyTriggerCount int                 `json:"daily_trigger_count"`
	DailySnapshots    map[string]int      `json:"daily_snapshots"`
}

// NewManager 트리거 관리자 생성
func NewManager(config *TriggerConfig, memManager *memory.Manager) *Manager {
	tm := &Manager{
		handlers:   make(map[TriggerType][]TriggerHandler),
		config:     config,
		memManager: memManager,
		stats: &TriggerStats{
			TriggersByType:   make(map[TriggerType]int),
			TriggersBySymbol: make(map[string]int),
			DailySnapshots:   make(map[string]int),
		},
	}

	// 기본 핸들러 등록
	tm.registerDefaultHandlers()

	return tm
}

// RegisterHandler 핸들러 등록
func (tm *Manager) RegisterHandler(triggerType TriggerType, handler TriggerHandler) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.handlers[triggerType] = append(tm.handlers[triggerType], handler)
	log.Printf("🔧 트리거 핸들러 등록: %s", triggerType)
}

// TriggerPumpDetection 펌핑 감지 트리거 발생
func (tm *Manager) TriggerPumpDetection(symbol string, score float64, confidence float64, metadata map[string]interface{}) error {
	if !tm.config.PumpDetection.Enabled {
		return nil
	}

	if score < tm.config.PumpDetection.MinScore {
		return nil
	}

	trigger := &Trigger{
		ID:          generateTriggerID(),
		Type:        TriggerTypePumpDetection,
		Symbol:      symbol,
		Timestamp:   time.Now(),
		Confidence:  confidence,
		Score:       score,
		Description: fmt.Sprintf("펌핑 감지: %s (점수: %.2f, 신뢰도: %.2f%%)", symbol, score, confidence),
		Metadata:    metadata,
	}

	return tm.processTrigger(trigger)
}

// TriggerListingAnnouncement 상장공시 트리거 발생
func (tm *Manager) TriggerListingAnnouncement(symbol string, confidence float64, metadata map[string]interface{}) error {
	trigger := &Trigger{
		ID:          generateTriggerID(),
		Type:        TriggerTypeListing,
		Symbol:      symbol,
		Timestamp:   time.Now(),
		Confidence:  confidence,
		Score:       100.0, // 상장공시는 최고 점수
		Description: fmt.Sprintf("상장공시 감지: %s (신뢰도: %.2f%%)", symbol, confidence),
		Metadata:    metadata,
	}

	return tm.processTrigger(trigger)
}

// TriggerVolumeSpike 거래량 스파이크 트리거 발생
func (tm *Manager) TriggerVolumeSpike(symbol string, volumeChange float64, confidence float64, metadata map[string]interface{}) error {
	trigger := &Trigger{
		ID:          generateTriggerID(),
		Type:        TriggerTypeVolumeSpike,
		Symbol:      symbol,
		Timestamp:   time.Now(),
		Confidence:  confidence,
		Score:       calculateVolumeScore(volumeChange),
		Description: fmt.Sprintf("거래량 스파이크: %s (변화율: %.2f%%, 신뢰도: %.2f%%)", symbol, volumeChange, confidence),
		Metadata:    metadata,
	}

	return tm.processTrigger(trigger)
}

// TriggerPriceSpike 가격 스파이크 트리거 발생
func (tm *Manager) TriggerPriceSpike(symbol string, priceChange float64, confidence float64, metadata map[string]interface{}) error {
	trigger := &Trigger{
		ID:          generateTriggerID(),
		Type:        TriggerTypePriceSpike,
		Symbol:      symbol,
		Timestamp:   time.Now(),
		Confidence:  confidence,
		Score:       calculatePriceScore(priceChange),
		Description: fmt.Sprintf("가격 스파이크: %s (변화율: %.2f%%, 신뢰도: %.2f%%)", symbol, priceChange, confidence),
		Metadata:    metadata,
	}

	return tm.processTrigger(trigger)
}

// processTrigger 트리거 처리
func (tm *Manager) processTrigger(trigger *Trigger) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// 통계 업데이트
	tm.updateStats(trigger)

	// 로그 출력
	log.Printf("🚨 트리거 발생: %s - %s", trigger.Type, trigger.Description)

	// 핸들러 실행
	handlers, exists := tm.handlers[trigger.Type]
	if !exists {
		log.Printf("⚠️  %s 타입의 핸들러가 등록되지 않음", trigger.Type)
		return nil
	}

	// 모든 핸들러 실행
	for _, handler := range handlers {
		if err := handler.HandleTrigger(trigger); err != nil {
			log.Printf("❌ 핸들러 실행 실패: %v", err)
		}
	}

	return nil
}

// updateStats 통계 업데이트
func (tm *Manager) updateStats(trigger *Trigger) {
	tm.stats.TotalTriggers++
	tm.stats.TriggersByType[trigger.Type]++
	tm.stats.TriggersBySymbol[trigger.Symbol]++
	tm.stats.LastTriggerTime = trigger.Timestamp

	// 일일 카운트 리셋 (자정)
	now := time.Now()
	if tm.stats.LastTriggerTime.Day() != now.Day() {
		tm.stats.DailyTriggerCount = 0
		tm.stats.DailySnapshots = make(map[string]int)
	}
	tm.stats.DailyTriggerCount++
}

// GetStats 통계 조회
func (tm *Manager) GetStats() *TriggerStats {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// 복사본 반환
	stats := *tm.stats
	stats.TriggersByType = make(map[TriggerType]int)
	stats.TriggersBySymbol = make(map[string]int)
	stats.DailySnapshots = make(map[string]int)

	for k, v := range tm.stats.TriggersByType {
		stats.TriggersByType[k] = v
	}
	for k, v := range tm.stats.TriggersBySymbol {
		stats.TriggersBySymbol[k] = v
	}
	for k, v := range tm.stats.DailySnapshots {
		stats.DailySnapshots[k] = v
	}

	return &stats
}

// registerDefaultHandlers 기본 핸들러 등록
func (tm *Manager) registerDefaultHandlers() {
	// 스냅샷 핸들러 등록
	snapshotHandler := NewSnapshotHandler(tm.memManager, tm.config.Snapshot)
	tm.RegisterHandler(TriggerTypePumpDetection, snapshotHandler)
	tm.RegisterHandler(TriggerTypeListing, snapshotHandler)
	tm.RegisterHandler(TriggerTypeVolumeSpike, snapshotHandler)
	tm.RegisterHandler(TriggerTypePriceSpike, snapshotHandler)

	// 알림 핸들러 등록 (필요시)
	// notificationHandler := NewNotificationHandler()
	// tm.RegisterHandler(TriggerTypePumpDetection, notificationHandler)
}

// generateTriggerID 트리거 ID 생성
func generateTriggerID() string {
	return fmt.Sprintf("trigger_%d", time.Now().UnixNano())
}

// calculateVolumeScore 거래량 점수 계산
func calculateVolumeScore(volumeChange float64) float64 {
	if volumeChange < 100 {
		return 0
	}
	if volumeChange > 1000 {
		return 100
	}
	return (volumeChange - 100) / 9 // 100-1000% 범위를 0-100 점수로 변환
}

// calculatePriceScore 가격 점수 계산
func calculatePriceScore(priceChange float64) float64 {
	if priceChange < 5 {
		return 0
	}
	if priceChange > 50 {
		return 100
	}
	return (priceChange - 5) / 0.45 // 5-50% 범위를 0-100 점수로 변환
}
