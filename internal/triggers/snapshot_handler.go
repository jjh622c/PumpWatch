package triggers

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"noticepumpcatch/internal/memory"
)

// SnapshotHandler 스냅샷 핸들러
type SnapshotHandler struct {
	memManager *memory.Manager
	config     SnapshotConfig
}

// SnapshotData 스냅샷 데이터 구조체
type SnapshotData struct {
	Trigger    *Trigger                    `json:"trigger"`
	Metadata   SnapshotMetadata            `json:"metadata"`
	Orderbooks []*memory.OrderbookSnapshot `json:"orderbooks"`
	Trades     []*memory.TradeData         `json:"trades"`
	Statistics SnapshotStatistics          `json:"statistics"`
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
	Compressed      bool      `json:"compressed"`
}

// SnapshotStatistics 스냅샷 통계
type SnapshotStatistics struct {
	OrderbookCount int     `json:"orderbook_count"`
	TradeCount     int     `json:"trade_count"`
	TimeSpan       float64 `json:"time_span_seconds"`
	PriceRange     struct {
		Min float64 `json:"min"`
		Max float64 `json:"max"`
	} `json:"price_range"`
	VolumeRange struct {
		Min float64 `json:"min"`
		Max float64 `json:"max"`
	} `json:"volume_range"`
}

// NewSnapshotHandler 스냅샷 핸들러 생성
func NewSnapshotHandler(memManager *memory.Manager, config SnapshotConfig) *SnapshotHandler {
	return &SnapshotHandler{
		memManager: memManager,
		config:     config,
	}
}

// HandleTrigger 트리거 처리 (TriggerHandler 인터페이스 구현)
func (sh *SnapshotHandler) HandleTrigger(trigger *Trigger) error {
	// 일일 스냅샷 제한 확인
	if !sh.checkDailyLimit(trigger.Symbol) {
		log.Printf("⚠️  일일 스냅샷 제한 도달: %s", trigger.Symbol)
		return nil
	}

	// 스냅샷 데이터 수집
	snapshotData := sh.collectSnapshotData(trigger)

	// 파일 저장
	if err := sh.saveSnapshot(snapshotData); err != nil {
		return fmt.Errorf("스냅샷 저장 실패: %v", err)
	}

	// 통계 업데이트
	sh.updateDailyStats(trigger.Symbol)

	log.Printf("💾 스냅샷 저장 완료: %s (%d개 오더북, %d개 체결)",
		trigger.Symbol, len(snapshotData.Orderbooks), len(snapshotData.Trades))

	return nil
}

// collectSnapshotData 스냅샷 데이터 수집
func (sh *SnapshotHandler) collectSnapshotData(trigger *Trigger) *SnapshotData {
	// 트리거 발생 시점 주변 데이터 조회
	data := sh.memManager.GetSnapshotData(
		trigger.Symbol,
		trigger.Timestamp,
		sh.config.PreTriggerSeconds,
		sh.config.PostTriggerSeconds,
	)

	// 데이터 변환
	orderbooks := data["orderbooks"].([]*memory.OrderbookSnapshot)
	trades := data["trades"].([]*memory.TradeData)

	// 통계 계산
	statistics := sh.calculateStatistics(orderbooks, trades)

	// 메타데이터 생성
	metadata := SnapshotMetadata{
		SnapshotID:      generateSnapshotID(trigger),
		CreatedAt:       time.Now(),
		Symbol:          trigger.Symbol,
		TriggerType:     string(trigger.Type),
		TriggerTime:     trigger.Timestamp,
		PreTriggerSecs:  sh.config.PreTriggerSeconds,
		PostTriggerSecs: sh.config.PostTriggerSeconds,
		DataPoints:      len(orderbooks) + len(trades),
		Compressed:      false, // 압축 기능은 향후 구현
	}

	return &SnapshotData{
		Trigger:    trigger,
		Metadata:   metadata,
		Orderbooks: orderbooks,
		Trades:     trades,
		Statistics: statistics,
	}
}

// saveSnapshot 스냅샷 파일 저장
func (sh *SnapshotHandler) saveSnapshot(snapshotData *SnapshotData) error {
	// 출력 디렉토리 생성
	if err := os.MkdirAll("./snapshots", 0755); err != nil {
		return fmt.Errorf("디렉토리 생성 실패: %v", err)
	}

	// 파일명 생성
	filename := sh.generateFilename(snapshotData)
	filepath := filepath.Join("./snapshots", filename)

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

// generateFilename 파일명 생성
func (sh *SnapshotHandler) generateFilename(snapshotData *SnapshotData) string {
	timestamp := snapshotData.Trigger.Timestamp.Format("20060102_150405")
	symbol := strings.ToLower(snapshotData.Trigger.Symbol)
	triggerType := strings.ToLower(string(snapshotData.Trigger.Type))

	return fmt.Sprintf("snapshot_%s_%s_%s.json", timestamp, symbol, triggerType)
}

// calculateStatistics 통계 계산
func (sh *SnapshotHandler) calculateStatistics(orderbooks []*memory.OrderbookSnapshot, trades []*memory.TradeData) SnapshotStatistics {
	stats := SnapshotStatistics{
		OrderbookCount: len(orderbooks),
		TradeCount:     len(trades),
	}

	if len(orderbooks) > 0 {
		// 시간 범위 계산
		startTime := orderbooks[0].Timestamp
		endTime := orderbooks[len(orderbooks)-1].Timestamp
		stats.TimeSpan = endTime.Sub(startTime).Seconds()

		// 가격 범위 계산 (첫 번째 오더북 기준)
		if len(orderbooks[0].Bids) > 0 && len(orderbooks[0].Asks) > 0 {
			// 최저 매수 가격
			if price, err := parseFloat(orderbooks[0].Bids[0][0]); err == nil {
				stats.PriceRange.Min = price
			}
			// 최고 매도 가격
			if price, err := parseFloat(orderbooks[0].Asks[0][0]); err == nil {
				stats.PriceRange.Max = price
			}
		}
	}

	if len(trades) > 0 {
		// 거래량 범위 계산
		var minVolume, maxVolume float64
		first := true

		for _, trade := range trades {
			if volume, err := parseFloat(trade.Quantity); err == nil {
				if first {
					minVolume = volume
					maxVolume = volume
					first = false
				} else {
					if volume < minVolume {
						minVolume = volume
					}
					if volume > maxVolume {
						maxVolume = volume
					}
				}
			}
		}

		stats.VolumeRange.Min = minVolume
		stats.VolumeRange.Max = maxVolume
	}

	return stats
}

// checkDailyLimit 일일 스냅샷 제한 확인
func (sh *SnapshotHandler) checkDailyLimit(symbol string) bool {
	// 간단한 구현: 향후 더 정교한 제한 로직 구현 가능
	return true
}

// updateDailyStats 일일 통계 업데이트
func (sh *SnapshotHandler) updateDailyStats(symbol string) {
	// 향후 구현: 일일 스냅샷 카운트 추적
}

// generateSnapshotID 스냅샷 ID 생성
func generateSnapshotID(trigger *Trigger) string {
	return fmt.Sprintf("snapshot_%s_%d", trigger.Symbol, trigger.Timestamp.UnixNano())
}

// parseFloat 문자열을 float64로 변환 (간단한 구현)
func parseFloat(s string) (float64, error) {
	var result float64
	_, err := fmt.Sscanf(s, "%f", &result)
	return result, err
}
