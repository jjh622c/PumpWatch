package storage

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"noticepumpcatch/internal/memory"
	"noticepumpcatch/internal/raw"
)

// SignalDataHandler 시그널 데이터 저장 핸들러
type SignalDataHandler struct {
	storageManager *StorageManager
	memManager     *memory.Manager
	rawManager     *raw.RawManager // raw 데이터 관리자 추가
}

// NewSignalDataHandler 시그널 데이터 핸들러 생성
func NewSignalDataHandler(storageManager *StorageManager, memManager *memory.Manager, rawManager *raw.RawManager) *SignalDataHandler {
	return &SignalDataHandler{
		storageManager: storageManager,
		memManager:     memManager,
		rawManager:     rawManager, // raw 데이터 관리자 주입
	}
}

// SaveSignalData 시그널 발생 시 ±60초 범위 데이터 저장 (핵심 함수)
// 시그널 발생 시점을 기준으로 전후 60초 범위의 모든 체결/오더북 데이터를 즉시 파일로 저장
func (h *SignalDataHandler) SaveSignalData(symbol, exchange string, signalTime time.Time) error {
	log.Printf("💾 시그널 데이터 저장 시작: %s (시점: %s)", symbol, signalTime.Format("2006-01-02 15:04:05"))

	// ±60초 범위 계산
	startTime := signalTime.Add(-60 * time.Second)
	endTime := signalTime.Add(60 * time.Second)

	// 🚨 핵심: raw 데이터에서 해당 범위 데이터 추출
	trades, orderbooks, err := h.rawManager.ExtractTimeRangeData(symbol, startTime, endTime)
	if err != nil {
		return fmt.Errorf("raw 데이터 추출 실패: %v", err)
	}

	log.Printf("📊 raw 데이터 추출 완료: %s (오더북 %d개, 체결 %d개)", symbol, len(orderbooks), len(trades))

	// ISO8601 형식의 타임스탬프 생성 (파일명용)
	timestamp := signalTime.UTC().Format("20060102T150405Z")

	// 체결 데이터 저장
	if err := h.saveTradeDataFromRaw(exchange, symbol, timestamp, trades); err != nil {
		return fmt.Errorf("체결 데이터 저장 실패: %v", err)
	}

	// 오더북 데이터 저장
	if err := h.saveOrderbookDataFromRaw(exchange, symbol, timestamp, orderbooks); err != nil {
		return fmt.Errorf("오더북 데이터 저장 실패: %v", err)
	}

	log.Printf("✅ 시그널 데이터 저장 완료: %s (체결 %d개, 오더북 %d개)", symbol, len(trades), len(orderbooks))
	return nil
}

// saveTradeDataFromRaw raw 체결 데이터를 파일로 저장
// 경로: trades/{exchange}_{symbol}_{timestamp}.json
func (h *SignalDataHandler) saveTradeDataFromRaw(exchange, symbol, timestamp string, trades []raw.TradeRecord) error {
	if len(trades) == 0 {
		log.Printf("⚠️  체결 데이터 없음: %s", symbol)
		return nil
	}

	// 파일명 생성: trades/binance_btcusdt_20250721T160012Z.json
	filename := fmt.Sprintf("%s_%s_%s.json", exchange, symbol, timestamp)
	filepath := filepath.Join(h.storageManager.baseDir, "trades", filename)

	// JSON 배열로 저장
	data := map[string]interface{}{
		"metadata": map[string]interface{}{
			"exchange":    exchange,
			"symbol":      symbol,
			"timestamp":   timestamp,
			"trade_count": len(trades),
			"created_at":  time.Now().UTC(),
			"data_type":   "trade_data",
			"source":      "raw_data_extraction",
		},
		"trades": trades,
	}

	// 파일 저장
	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("파일 생성 실패: %v", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("JSON 인코딩 실패: %v", err)
	}

	log.Printf("💾 체결 데이터 저장: %s (%d개)", filename, len(trades))
	return nil
}

// saveOrderbookDataFromRaw raw 오더북 데이터를 파일로 저장
// 경로: orderbooks/{exchange}_{symbol}_{timestamp}.json
func (h *SignalDataHandler) saveOrderbookDataFromRaw(exchange, symbol, timestamp string, orderbooks []raw.OrderbookRecord) error {
	if len(orderbooks) == 0 {
		log.Printf("⚠️  오더북 데이터 없음: %s", symbol)
		return nil
	}

	// 파일명 생성: orderbooks/binance_btcusdt_20250721T160012Z.json
	filename := fmt.Sprintf("%s_%s_%s.json", exchange, symbol, timestamp)
	filepath := filepath.Join(h.storageManager.baseDir, "orderbooks", filename)

	// JSON 배열로 저장
	data := map[string]interface{}{
		"metadata": map[string]interface{}{
			"exchange":        exchange,
			"symbol":          symbol,
			"timestamp":       timestamp,
			"orderbook_count": len(orderbooks),
			"created_at":      time.Now().UTC(),
			"data_type":       "orderbook_data",
			"source":          "raw_data_extraction",
		},
		"orderbooks": orderbooks,
	}

	// 파일 저장
	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("파일 생성 실패: %v", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("JSON 인코딩 실패: %v", err)
	}

	log.Printf("💾 오더북 데이터 저장: %s (%d개)", filename, len(orderbooks))
	return nil
}

// SavePumpSignalData 펌핑 시그널 발생 시 데이터 저장
func (h *SignalDataHandler) SavePumpSignalData(signal *memory.AdvancedPumpSignal) error {
	return h.SaveSignalData(signal.Symbol, "binance", signal.Timestamp)
}

// SaveListingSignalData 상장공시 시그널 발생 시 데이터 저장
func (h *SignalDataHandler) SaveListingSignalData(symbol string, signalTime time.Time) error {
	return h.SaveSignalData(symbol, "binance", signalTime)
}

// SaveCustomTriggerData 커스텀 트리거 발생 시 데이터 저장
func (h *SignalDataHandler) SaveCustomTriggerData(symbol, exchange string, triggerTime time.Time) error {
	return h.SaveSignalData(symbol, exchange, triggerTime)
}

// ExtractAndSaveTimeRangeData 특정 시간 범위 데이터 추출 및 저장 (120초 백업)
func (h *SignalDataHandler) ExtractAndSaveTimeRangeData(symbol, exchange string, triggerTime time.Time, preSeconds, postSeconds int) error {
	log.Printf("📦 시간 범위 데이터 추출 및 저장: %s (±%d초)", symbol, preSeconds)

	// 시간 범위 계산
	startTime := triggerTime.Add(-time.Duration(preSeconds) * time.Second)
	endTime := triggerTime.Add(time.Duration(postSeconds) * time.Second)

	// raw 데이터에서 추출
	trades, orderbooks, err := h.rawManager.ExtractTimeRangeData(symbol, startTime, endTime)
	if err != nil {
		return fmt.Errorf("raw 데이터 추출 실패: %v", err)
	}

	log.Printf("📊 시간 범위 데이터 추출: %s (오더북 %d개, 체결 %d개)", symbol, len(orderbooks), len(trades))

	// ISO8601 형식의 타임스탬프 생성
	timestamp := triggerTime.UTC().Format("20060102T150405Z")

	// 백업 파일명 생성 (시간 범위 포함)
	backupFilename := fmt.Sprintf("%s_%s_%s_%ds_%ds.json", exchange, symbol, timestamp, preSeconds, postSeconds)
	backupFilepath := filepath.Join(h.storageManager.baseDir, "snapshots", backupFilename)

	// 통합 데이터 구조
	backupData := map[string]interface{}{
		"metadata": map[string]interface{}{
			"exchange":        exchange,
			"symbol":          symbol,
			"trigger_time":    triggerTime.UTC(),
			"timestamp":       timestamp,
			"pre_seconds":     preSeconds,
			"post_seconds":    postSeconds,
			"trade_count":     len(trades),
			"orderbook_count": len(orderbooks),
			"created_at":      time.Now().UTC(),
			"data_type":       "time_range_backup",
			"source":          "raw_data_extraction",
		},
		"trades":     trades,
		"orderbooks": orderbooks,
	}

	// 백업 파일 저장
	file, err := os.Create(backupFilepath)
	if err != nil {
		return fmt.Errorf("백업 파일 생성 실패: %v", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(backupData); err != nil {
		return fmt.Errorf("백업 JSON 인코딩 실패: %v", err)
	}

	log.Printf("💾 시간 범위 백업 저장: %s (체결 %d개, 오더북 %d개)", backupFilename, len(trades), len(orderbooks))
	return nil
}
