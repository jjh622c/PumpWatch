package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LogLevel 로그 레벨 정의
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARNING
	ERROR
	CRITICAL
)

// LogLevelString 로그 레벨 문자열 매핑
var LogLevelString = map[LogLevel]string{
	DEBUG:    "DEBUG",
	INFO:     "INFO",
	WARNING:  "WARNING",
	ERROR:    "ERROR",
	CRITICAL: "CRITICAL",
}

// LogLevelFromString 문자열에서 로그 레벨 변환
func LogLevelFromString(level string) LogLevel {
	switch strings.ToLower(level) {
	case "debug":
		return DEBUG
	case "info":
		return INFO
	case "warning", "warn":
		return WARNING
	case "error":
		return ERROR
	case "critical":
		return CRITICAL
	default:
		return INFO
	}
}

// LoggerConfig 로거 설정
type LoggerConfig struct {
	Level      LogLevel `json:"level"`
	OutputFile string   `json:"output_file"`
	MaxSize    int      `json:"max_size"` // MB
	MaxBackups int      `json:"max_backups"`
}

// Logger 로거 구조체
type Logger struct {
	config         LoggerConfig
	file           *os.File
	fileLogger     *log.Logger
	consoleLogger  *log.Logger
	mu             sync.Mutex
	lastStatus     time.Time
	statusInterval time.Duration
}

// NewLogger 새 로거 생성
func NewLogger(config LoggerConfig) (*Logger, error) {
	// 로그 디렉토리 생성
	logDir := filepath.Dir(config.OutputFile)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("로그 디렉토리 생성 실패: %v", err)
	}

	// 로그 파일 열기
	file, err := os.OpenFile(config.OutputFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, fmt.Errorf("로그 파일 열기 실패: %v", err)
	}

	// 멀티 라이터 생성 (콘솔 + 파일)
	multiWriter := io.MultiWriter(os.Stdout, file)

	logger := &Logger{
		config:         config,
		file:           file,
		fileLogger:     log.New(file, "", log.LstdFlags),
		consoleLogger:  log.New(multiWriter, "", log.LstdFlags),
		lastStatus:     time.Now(),
		statusInterval: 30 * time.Second,
	}

	return logger, nil
}

// log 내부 로깅 함수
func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	if level < l.config.Level {
		return
	}

	levelStr := LogLevelString[level]
	timestamp := time.Now().Format("2006/01/02 15:04:05")
	message := fmt.Sprintf(format, args...)
	logEntry := fmt.Sprintf("%s %s: %s", timestamp, levelStr, message)

	// 파일에는 모든 레벨 기록
	l.mu.Lock()
	l.fileLogger.Println(logEntry)
	l.mu.Unlock()

	// 콘솔에는 WARNING 이상만 출력 (INFO는 상태 요약에서만)
	if level >= WARNING {
		l.consoleLogger.Println(logEntry)
	}
}

// LogDebug 디버그 로그
func (l *Logger) LogDebug(format string, args ...interface{}) {
	l.log(DEBUG, format, args...)
}

// LogInfo 정보 로그 (파일만)
func (l *Logger) LogInfo(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// LogWarn 경고 로그 (콘솔 + 파일)
func (l *Logger) LogWarn(format string, args ...interface{}) {
	l.log(WARNING, format, args...)
}

// LogError 에러 로그 (콘솔 + 파일)
func (l *Logger) LogError(format string, args ...interface{}) {
	l.log(ERROR, format, args...)
}

// LogCritical 치명적 에러 로그 (콘솔 + 파일)
func (l *Logger) LogCritical(format string, args ...interface{}) {
	l.log(CRITICAL, format, args...)
}

// LogStatus 상태 요약 로그 (콘솔 + 파일, 주기적)
func (l *Logger) LogStatus(format string, args ...interface{}) {
	now := time.Now()
	if now.Sub(l.lastStatus) >= l.statusInterval {
		l.lastStatus = now
		message := fmt.Sprintf(format, args...)
		timestamp := now.Format("2006/01/02 15:04:05")
		statusEntry := fmt.Sprintf("%s STATUS: %s", timestamp, message)

		// 콘솔에 상태 출력
		l.consoleLogger.Println(statusEntry)

		// 파일에도 기록
		l.mu.Lock()
		l.fileLogger.Println(statusEntry)
		l.mu.Unlock()
	}
}

// LogConnection 연결 관련 로그 (INFO 레벨)
func (l *Logger) LogConnection(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// LogWebSocket WebSocket 관련 로그 (DEBUG 레벨)
func (l *Logger) LogWebSocket(format string, args ...interface{}) {
	l.log(DEBUG, format, args...)
}

// LogMemory 메모리 관련 로그 (INFO 레벨)
func (l *Logger) LogMemory(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// LogPerformance 성능 관련 로그 (INFO 레벨)
func (l *Logger) LogPerformance(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// LogTrigger 트리거 관련 로그 (INFO 레벨)
func (l *Logger) LogTrigger(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// LogSignal 시그널 관련 로그 (INFO 레벨)
func (l *Logger) LogSignal(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// LogStorage 스토리지 관련 로그 (INFO 레벨)
func (l *Logger) LogStorage(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// LogCallback 콜백 관련 로그 (INFO 레벨)
func (l *Logger) LogCallback(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// LogShutdown 종료 관련 로그 (WARNING 레벨)
func (l *Logger) LogShutdown(format string, args ...interface{}) {
	l.log(WARNING, format, args...)
}

// LogSuccess 성공 로그 (INFO 레벨)
func (l *Logger) LogSuccess(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// LogFile 파일 관련 로그 (INFO 레벨)
func (l *Logger) LogFile(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// LogGoodbye 종료 메시지 (INFO 레벨)
func (l *Logger) LogGoodbye(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// PrintStatusSummary 상태 요약 출력 (콘솔 전용)
func (l *Logger) PrintStatusSummary(stats map[string]interface{}) {
	now := time.Now()
	if now.Sub(l.lastStatus) >= l.statusInterval {
		l.lastStatus = now

		// 상태 요약 메시지 구성
		summary := fmt.Sprintf("📊 상태 요약 [%s]", now.Format("15:04:05"))

		// 메모리 상태
		if memStats, ok := stats["memory"]; ok {
			if mem, ok := memStats.(map[string]interface{}); ok {
				summary += fmt.Sprintf(" | 메모리: 오더북 %v개, 체결 %v개",
					mem["total_orderbooks"], mem["total_trades"])
			}
		}

		// WebSocket 상태
		if wsStats, ok := stats["websocket"]; ok {
			if ws, ok := wsStats.(map[string]interface{}); ok {
				summary += fmt.Sprintf(" | WebSocket: 연결=%v", ws["is_connected"])
			}
		}

		// 성능 상태
		if perfStats, ok := stats["performance"]; ok {
			if perf, ok := perfStats.(map[string]interface{}); ok {
				summary += fmt.Sprintf(" | 성능: 오버플로우 %v회", perf["overflow_count"])
			}
		}

		// 콘솔에 출력
		l.consoleLogger.Printf("🔄 %s", summary)
	}
}

// Close 로거 종료
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// Rotate 로그 파일 로테이션
func (l *Logger) Rotate() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 현재 파일 크기 확인
	info, err := l.file.Stat()
	if err != nil {
		return err
	}

	// 최대 크기 초과시 로테이션
	if info.Size() > int64(l.config.MaxSize*1024*1024) {
		// 현재 파일 닫기
		l.file.Close()

		// 백업 파일 생성
		backupPath := l.config.OutputFile + ".1"
		if err := os.Rename(l.config.OutputFile, backupPath); err != nil {
			return err
		}

		// 새 파일 열기
		file, err := os.OpenFile(l.config.OutputFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return err
		}

		l.file = file
		l.fileLogger = log.New(file, "", log.LstdFlags)
	}

	return nil
}
