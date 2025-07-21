package filemanager

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FileManager 10분 단위 파일 분할 관리자
type FileManager struct {
	baseDir        string
	maxFileSize    int64 // 100MB
	bufferSize     int
	useCompression bool

	// 파일 핸들러 캐시 (market/symbol/datatype별)
	handlers map[string]*FileHandler
	mu       sync.RWMutex

	// 정기 flush 고루틴
	flushInterval time.Duration
}

// FileHandler 파일 핸들러
type FileHandler struct {
	file       *os.File
	writer     *bufio.Writer
	gzipWriter *gzip.Writer
	mu         sync.Mutex

	// 파일 정보
	market   string
	symbol   string
	dataType string
	timeSlot string // YYYYMMDD_HH10
	partNum  int    // 파일 분할 번호

	// 상태 정보
	lastFlush   time.Time
	currentSize int64
	lineCount   int64
}

// NewFileManager 파일 관리자 생성
func NewFileManager(baseDir string, maxFileSize int64, bufferSize int, useCompression bool) *FileManager {
	fm := &FileManager{
		baseDir:        baseDir,
		maxFileSize:    maxFileSize,
		bufferSize:     bufferSize,
		useCompression: useCompression,
		handlers:       make(map[string]*FileHandler),
		flushInterval:  5 * time.Second,
	}

	// 정기 flush 고루틴 시작
	go fm.flushRoutine()

	return fm
}

// WriteRecord 레코드를 파일에 기록
func (fm *FileManager) WriteRecord(market, symbol, dataType string, record interface{}) error {
	handler, err := fm.getOrCreateHandler(market, symbol, dataType)
	if err != nil {
		return fmt.Errorf("핸들러 생성 실패: %v", err)
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()

	// JSON 직렬화
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("JSON 직렬화 실패: %v", err)
	}

	// newline 추가
	data = append(data, '\n')

	// 파일에 기록
	var writer *bufio.Writer
	if handler.gzipWriter != nil {
		writer = bufio.NewWriter(handler.gzipWriter)
	} else {
		writer = handler.writer
	}

	if _, err := writer.Write(data); err != nil {
		return fmt.Errorf("파일 기록 실패: %v", err)
	}

	// 크기 및 라인 수 업데이트
	handler.currentSize += int64(len(data))
	handler.lineCount++

	// 파일 크기 제한 확인
	if handler.currentSize >= fm.maxFileSize {
		if err := fm.rotateFile(handler); err != nil {
			return fmt.Errorf("파일 로테이션 실패: %v", err)
		}
	}

	// 버퍼가 가득 찼으면 즉시 flush
	if writer.Available() < fm.bufferSize {
		if err := writer.Flush(); err != nil {
			return fmt.Errorf("버퍼 flush 실패: %v", err)
		}
		handler.lastFlush = time.Now()
	}

	return nil
}

// getOrCreateHandler 파일 핸들러 생성 또는 가져오기
func (fm *FileManager) getOrCreateHandler(market, symbol, dataType string) (*FileHandler, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	// 핸들러 키 생성
	key := fmt.Sprintf("%s/%s/%s", market, symbol, dataType)

	// 기존 핸들러 확인
	if handler, exists := fm.handlers[key]; exists {
		// 시간 슬롯이 바뀌었는지 확인
		currentTimeSlot := fm.getCurrentTimeSlot()
		if handler.timeSlot != currentTimeSlot {
			handler.Close()
			delete(fm.handlers, key)
		} else {
			return handler, nil
		}
	}

	// 새 핸들러 생성
	handler, err := fm.createFileHandler(market, symbol, dataType)
	if err != nil {
		return nil, err
	}

	fm.handlers[key] = handler
	return handler, nil
}

// createFileHandler 새 파일 핸들러 생성
func (fm *FileManager) createFileHandler(market, symbol, dataType string) (*FileHandler, error) {
	// 디렉토리 경로 생성
	dirPath := filepath.Join(fm.baseDir, market, symbol, dataType)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return nil, fmt.Errorf("디렉토리 생성 실패: %v", err)
	}

	// 시간 슬롯 및 파일명 생성
	timeSlot := fm.getCurrentTimeSlot()
	filename := fmt.Sprintf("%s.jsonl", timeSlot)
	if fm.useCompression {
		filename += ".gz"
	}

	filePath := filepath.Join(dirPath, filename)

	// 파일 생성 (append 모드)
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("파일 생성 실패: %v", err)
	}

	// 파일 크기 확인 (기존 파일이 있는 경우)
	fileInfo, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("파일 정보 조회 실패: %v", err)
	}

	// 기존 파일 크기가 제한을 초과하면 새 파트 생성
	partNum := 1
	if fileInfo.Size() >= fm.maxFileSize {
		partNum = fm.getNextPartNumber(dirPath, timeSlot)
		filename = fmt.Sprintf("%s_part%d.jsonl", timeSlot, partNum)
		if fm.useCompression {
			filename += ".gz"
		}
		filePath = filepath.Join(dirPath, filename)
		file.Close()

		file, err = os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("새 파트 파일 생성 실패: %v", err)
		}
		fileInfo, _ = file.Stat()
	}

	handler := &FileHandler{
		file:        file,
		market:      market,
		symbol:      symbol,
		dataType:    dataType,
		timeSlot:    timeSlot,
		partNum:     partNum,
		lastFlush:   time.Now(),
		currentSize: fileInfo.Size(),
	}

	// 압축 설정
	if fm.useCompression {
		handler.gzipWriter = gzip.NewWriter(file)
		handler.writer = bufio.NewWriter(handler.gzipWriter)
	} else {
		handler.writer = bufio.NewWriter(file)
	}

	log.Printf("📁 새 파일 핸들러 생성: %s", filePath)
	return handler, nil
}

// getCurrentTimeSlot 현재 시간 슬롯 반환 (YYYYMMDD_HH10)
func (fm *FileManager) getCurrentTimeSlot() string {
	now := time.Now()
	// 10분 단위로 반올림
	rounded := now.Truncate(10 * time.Minute)
	return rounded.Format("20060102_1504")
}

// getNextPartNumber 다음 파트 번호 계산
func (fm *FileManager) getNextPartNumber(dirPath, timeSlot string) int {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return 1
	}

	maxPart := 0
	pattern := fmt.Sprintf("%s_part", timeSlot)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), pattern) {
			// 파일명에서 파트 번호 추출: YYYYMMDD_HH10_part2.jsonl
			parts := strings.Split(entry.Name(), "_part")
			if len(parts) == 2 {
				partStr := strings.Split(parts[1], ".")[0]
				if partNum, err := strconv.Atoi(partStr); err == nil && partNum > maxPart {
					maxPart = partNum
				}
			}
		}
	}

	return maxPart + 1
}

// rotateFile 파일 로테이션 (크기 제한 초과 시)
func (fm *FileManager) rotateFile(handler *FileHandler) error {
	// 현재 파일 닫기
	handler.Close()

	// 새 파트 번호 계산
	dirPath := filepath.Join(fm.baseDir, handler.market, handler.symbol, handler.dataType)
	nextPart := fm.getNextPartNumber(dirPath, handler.timeSlot)

	// 새 파일명 생성
	filename := fmt.Sprintf("%s_part%d.jsonl", handler.timeSlot, nextPart)
	if fm.useCompression {
		filename += ".gz"
	}
	filepath := filepath.Join(dirPath, filename)

	// 새 파일 생성
	file, err := os.OpenFile(filepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("새 파트 파일 생성 실패: %v", err)
	}

	// 핸들러 업데이트
	handler.file = file
	handler.partNum = nextPart
	handler.currentSize = 0
	handler.lineCount = 0
	handler.lastFlush = time.Now()

	// 압축 설정
	if fm.useCompression {
		handler.gzipWriter = gzip.NewWriter(file)
		handler.writer = bufio.NewWriter(handler.gzipWriter)
	} else {
		handler.writer = bufio.NewWriter(file)
	}

	log.Printf("🔄 파일 로테이션: %s (파트 %d)", filepath, nextPart)
	return nil
}

// flushRoutine 정기 flush 고루틴
func (fm *FileManager) flushRoutine() {
	ticker := time.NewTicker(fm.flushInterval)
	defer ticker.Stop()

	for range ticker.C {
		fm.flushAll()
	}
}

// flushAll 모든 핸들러 flush
func (fm *FileManager) flushAll() {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	for key, handler := range fm.handlers {
		handler.mu.Lock()

		// 마지막 flush 이후 일정 시간이 지났으면 flush
		if time.Since(handler.lastFlush) >= fm.flushInterval {
			if err := handler.writer.Flush(); err != nil {
				log.Printf("❌ 버퍼 flush 실패: %s - %v", key, err)
			} else {
				handler.lastFlush = time.Now()
			}
		}

		handler.mu.Unlock()
	}
}

// Close 모든 핸들러 닫기
func (fm *FileManager) Close() {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	for key, handler := range fm.handlers {
		handler.Close()
		log.Printf("📁 파일 핸들러 닫기: %s", key)
	}
}

// Close 파일 핸들러 닫기
func (h *FileHandler) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.writer != nil {
		h.writer.Flush()
	}
	if h.gzipWriter != nil {
		h.gzipWriter.Close()
	}
	if h.file != nil {
		h.file.Close()
	}
}
