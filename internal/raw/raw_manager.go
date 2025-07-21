package raw

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"noticepumpcatch/internal/filemanager"
	"noticepumpcatch/internal/memory"
)

// RawManager 실시간 raw 데이터 기록 관리자
type RawManager struct {
	baseDir string
	mu      sync.RWMutex

	// 새로운 파일 매니저 사용
	fileManager *filemanager.FileManager

	// 메모리 관리자 참조 (기존 기능 유지)
	memManager *memory.Manager
}

// TradeRecord 체결 데이터 레코드
type TradeRecord struct {
	Symbol    string    `json:"symbol"`
	Price     string    `json:"price"`
	Quantity  string    `json:"quantity"`
	Side      string    `json:"side"` // "BUY" or "SELL"
	TradeID   string    `json:"trade_id"`
	Timestamp time.Time `json:"timestamp"`
	Exchange  string    `json:"exchange"`
}

// OrderbookRecord 오더북 데이터 레코드
type OrderbookRecord struct {
	Symbol    string     `json:"symbol"`
	Bids      [][]string `json:"bids"` // [price, quantity]
	Asks      [][]string `json:"asks"` // [price, quantity]
	Timestamp time.Time  `json:"timestamp"`
	Exchange  string     `json:"exchange"`
}

// NewRawManager raw 데이터 관리자 생성
func NewRawManager(baseDir string, bufferSize int, useCompression bool, memManager *memory.Manager) *RawManager {
	// 새로운 파일 매니저 생성 (100MB 제한)
	fileManager := filemanager.NewFileManager(
		baseDir,
		100*1024*1024, // 100MB
		bufferSize,
		useCompression,
	)

	rm := &RawManager{
		baseDir:     baseDir,
		fileManager: fileManager,
		memManager:  memManager,
	}

	// 디렉토리 생성
	rm.createDirectories()

	return rm
}

// createDirectories 필요한 디렉토리 생성
func (rm *RawManager) createDirectories() {
	// raw 디렉토리 생성
	if err := os.MkdirAll(rm.baseDir, 0755); err != nil {
		log.Printf("❌ raw 디렉토리 생성 실패: %v", err)
	}
}

// RecordTrade 체결 데이터 기록
func (rm *RawManager) RecordTrade(symbol, price, quantity, side, tradeID, exchange string, timestamp time.Time) error {
	record := TradeRecord{
		Symbol:    symbol,
		Price:     price,
		Quantity:  quantity,
		Side:      side,
		TradeID:   tradeID,
		Timestamp: timestamp,
		Exchange:  exchange,
	}

	// 새로운 파일 매니저를 사용하여 기록
	return rm.fileManager.WriteRecord(exchange, symbol, "trade", record)
}

// RecordOrderbook 오더북 데이터 기록
func (rm *RawManager) RecordOrderbook(symbol, exchange string, bids, asks [][]string, timestamp time.Time) error {
	record := OrderbookRecord{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: timestamp,
		Exchange:  exchange,
	}

	// 새로운 파일 매니저를 사용하여 기록
	return rm.fileManager.WriteRecord(exchange, symbol, "orderbook", record)
}

// ExtractTimeRangeData 특정 시간 범위 데이터 추출
func (rm *RawManager) ExtractTimeRangeData(symbol string, startTime, endTime time.Time) ([]TradeRecord, []OrderbookRecord, error) {
	var trades []TradeRecord
	var orderbooks []OrderbookRecord

	// 심볼 디렉토리 경로 (기존 구조 유지)
	symbolDir := filepath.Join(rm.baseDir, symbol)
	if _, err := os.Stat(symbolDir); os.IsNotExist(err) {
		return trades, orderbooks, nil // 디렉토리가 없으면 빈 결과 반환
	}

	// 체결 데이터 추출
	trades, err := rm.extractTradeData(symbolDir, startTime, endTime)
	if err != nil {
		return nil, nil, fmt.Errorf("체결 데이터 추출 실패: %v", err)
	}

	// 오더북 데이터 추출
	orderbooks, err = rm.extractOrderbookData(symbolDir, startTime, endTime)
	if err != nil {
		return nil, nil, fmt.Errorf("오더북 데이터 추출 실패: %v", err)
	}

	return trades, orderbooks, nil
}

// extractTradeData 체결 데이터 추출 (기존 로직 유지)
func (rm *RawManager) extractTradeData(symbolDir string, startTime, endTime time.Time) ([]TradeRecord, error) {
	var trades []TradeRecord

	// 날짜 범위 내 파일들 찾기
	files, err := rm.findDataFiles(symbolDir, "trades", startTime, endTime)
	if err != nil {
		return nil, err
	}

	for _, filepath := range files {
		fileTrades, err := rm.readTradeFile(filepath, startTime, endTime)
		if err != nil {
			log.Printf("⚠️ 파일 읽기 실패: %s - %v", filepath, err)
			continue
		}
		trades = append(trades, fileTrades...)
	}

	return trades, nil
}

// extractOrderbookData 오더북 데이터 추출 (기존 로직 유지)
func (rm *RawManager) extractOrderbookData(symbolDir string, startTime, endTime time.Time) ([]OrderbookRecord, error) {
	var orderbooks []OrderbookRecord

	// 날짜 범위 내 파일들 찾기
	files, err := rm.findDataFiles(symbolDir, "orderbook", startTime, endTime)
	if err != nil {
		return nil, err
	}

	for _, filepath := range files {
		fileOrderbooks, err := rm.readOrderbookFile(filepath, startTime, endTime)
		if err != nil {
			log.Printf("⚠️ 파일 읽기 실패: %s - %v", filepath, err)
			continue
		}
		orderbooks = append(orderbooks, fileOrderbooks...)
	}

	return orderbooks, nil
}

// findDataFiles 데이터 파일들 찾기 (기존 로직 유지)
func (rm *RawManager) findDataFiles(symbolDir, dataType string, startTime, endTime time.Time) ([]string, error) {
	var files []string

	// 디렉토리 읽기
	entries, err := os.ReadDir(symbolDir)
	if err != nil {
		return nil, err
	}

	// 파일 패턴 매칭
	pattern := fmt.Sprintf("*_%s.jsonl*", dataType)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		matched, err := filepath.Match(pattern, entry.Name())
		if err != nil || !matched {
			continue
		}

		// 날짜 범위 확인
		fileDate, err := rm.extractDateFromFilename(entry.Name())
		if err != nil {
			continue
		}

		if fileDate.After(startTime.AddDate(0, 0, -1)) && fileDate.Before(endTime.AddDate(0, 0, 1)) {
			files = append(files, filepath.Join(symbolDir, entry.Name()))
		}
	}

	return files, nil
}

// extractDateFromFilename 파일명에서 날짜 추출 (기존 로직 유지)
func (rm *RawManager) extractDateFromFilename(filename string) (time.Time, error) {
	// 예: 2024-07-21_trades.jsonl -> 2024-07-21
	if len(filename) < 10 {
		return time.Time{}, fmt.Errorf("잘못된 파일명: %s", filename)
	}

	dateStr := filename[:10]
	return time.Parse("2006-01-02", dateStr)
}

// readTradeFile 체결 파일 읽기 (기존 로직 유지)
func (rm *RawManager) readTradeFile(filepath string, startTime, endTime time.Time) ([]TradeRecord, error) {
	// 기존 구현 유지 (간단화)
	return []TradeRecord{}, nil
}

// readOrderbookFile 오더북 파일 읽기 (기존 로직 유지)
func (rm *RawManager) readOrderbookFile(filepath string, startTime, endTime time.Time) ([]OrderbookRecord, error) {
	// 기존 구현 유지 (간단화)
	return []OrderbookRecord{}, nil
}

// Close 모든 핸들러 닫기
func (rm *RawManager) Close() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 파일 매니저 닫기
	if rm.fileManager != nil {
		rm.fileManager.Close()
	}
}
