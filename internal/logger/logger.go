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
	Level                       LogLevel `json:"level"`
	OutputFile                  string   `json:"output_file"`
	MaxSize                     int      `json:"max_size"` // MB
	MaxBackups                  int      `json:"max_backups"`
	LatencyWarnSeconds          float64  `json:"latency_warn_seconds"`
	LatencyCriticalSeconds      float64  `json:"latency_critical_seconds"`
	LatencyStatsIntervalSeconds int      `json:"latency_stats_interval_seconds"`
	LogRotationIntervalMinutes  int      `json:"log_rotation_interval_minutes"`
	CriticalLogFile             string   `json:"critical_log_file"`
	EnableCriticalSeparation    bool     `json:"enable_critical_separation"`
}

// Logger 로거 구조체
type Logger struct {
	config           LoggerConfig
	file             *os.File
	criticalFile     *os.File
	fileLogger       *log.Logger
	criticalLogger   *log.Logger
	consoleLogger    *log.Logger
	mu               sync.Mutex
	criticalMu       sync.Mutex
	lastStatus       time.Time
	statusInterval   time.Duration
	lastRotation     time.Time
	rotationInterval time.Duration
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
		config:           config,
		file:             file,
		fileLogger:       log.New(file, "", log.LstdFlags),
		consoleLogger:    log.New(multiWriter, "", log.LstdFlags),
		lastStatus:       time.Now(),
		statusInterval:   30 * time.Second,
		lastRotation:     time.Now(),
		rotationInterval: time.Duration(config.LogRotationIntervalMinutes) * time.Minute,
	}

	// 중요 이벤트 로그 파일 초기화
	if config.EnableCriticalSeparation && config.CriticalLogFile != "" {
		criticalFile, err := os.OpenFile(config.CriticalLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err == nil {
			logger.criticalFile = criticalFile
			logger.criticalLogger = log.New(criticalFile, "", log.LstdFlags)
		}
	}

	return logger, nil
}

// log 내부 로깅 함수
func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	if level < l.config.Level {
		return
	}

	// 크기 기반 로그 롤링 확인 (시간 기반보다 우선)
	l.checkSizeBasedRotation()

	levelStr := LogLevelString[level]
	timestamp := time.Now().Format("2006/01/02 15:04:05")
	message := fmt.Sprintf(format, args...)
	logEntry := fmt.Sprintf("%s %s: %s", timestamp, levelStr, message)

	// 파일에는 모든 레벨 기록
	l.mu.Lock()
	l.fileLogger.Println(logEntry)
	l.mu.Unlock()

	// 중요 이벤트 별도 저장 (레이턴시, 재시작, 에러, 크리티컬)
	l.logCriticalIfNeeded(level, message, logEntry)

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

// LogLatency 지연 감지 로그
func (l *Logger) LogLatency(format string, args ...interface{}) {
	l.log(WARNING, format, args...)
}

// LogLatencyStats 지연 통계 로그
func (l *Logger) LogLatencyStats(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// LogLatencyCritical 심각한 지연 로그
func (l *Logger) LogLatencyCritical(format string, args ...interface{}) {
	l.log(CRITICAL, format, args...)
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
		l.file.Close()
	}

	if l.criticalFile != nil {
		l.criticalFile.Close()
	}

	return nil
}

// checkSizeBasedRotation 크기 기반 로그 롤링 확인
func (l *Logger) checkSizeBasedRotation() {
	if l.file == nil {
		return
	}

	info, err := l.file.Stat()
	if err != nil {
		return
	}

	// 최대 크기 초과시 로테이션 (시간 기반보다 우선)
	if info.Size() > int64(l.config.MaxSize*1024*1024) {
		l.rotateBySize()
	}
}

// rotateBySize 크기 기반 로그 롤링 수행
func (l *Logger) rotateBySize() {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 현재 파일 닫기
	if l.file != nil {
		l.file.Close()
	}

	// 타임스탬프가 포함된 백업 파일명 생성
	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("%s.%s", l.config.OutputFile, timestamp)

	// 기존 파일을 백업으로 이동
	if err := os.Rename(l.config.OutputFile, backupPath); err != nil {
		// 파일이 없거나 이동 실패시 무시
		return
	}

	// 새 파일 열기
	file, err := os.OpenFile(l.config.OutputFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return
	}

	l.file = file
	l.fileLogger = log.New(file, "", log.LstdFlags)
}

// logCriticalIfNeeded 중요 이벤트 감지하여 별도 파일에 저장
func (l *Logger) logCriticalIfNeeded(level LogLevel, message, logEntry string) {
	if !l.config.EnableCriticalSeparation || l.criticalLogger == nil {
		return
	}

	// 중요 이벤트 감지 조건
	isCritical := false

	// 1. 레벨이 ERROR 이상
	if level >= ERROR {
		isCritical = true
	}

	// 2. 특정 키워드 포함 검사
	criticalKeywords := []string{
		// 기존 키워드들
		"밀림 감지", "latency", "지연", "재시작", "restart", "panic", "crash",
		"버그", "bug", "오류", "error", "실패", "failed", "종료", "shutdown",
		"연결 끊김", "connection", "timeout", "메모리 부족", "memory",

		// 🚀 WebSocket 및 좀비 연결 관련 키워드 추가
		"critical", "좀비", "zombie", "건강성", "health", "preemptive",
		"websocket", "웹소켓", "reconnect", "disconnect", "끊어짐", "중단",
		"alert", "경고", "감지", "detect", "goroutine", "고루틴", "leak", "누수",
		"overflow", "오버플로우", "buffer", "버퍼", "channel", "채널",
		"pump", "펌핑", "signal", "시그널", "hft", "감지기",

		// 🔥 성능 및 시스템 관련 키워드
		"performance", "성능", "slow", "느림", "hang", "멈춤", "freeze", "정지",
		"deadlock", "데드락", "race", "경합", "corruption", "손상",
		"disk", "디스크", "space", "공간", "full", "가득",
	}

	messageLower := strings.ToLower(message)
	for _, keyword := range criticalKeywords {
		if strings.Contains(messageLower, strings.ToLower(keyword)) {
			isCritical = true
			break
		}
	}

	// 중요 이벤트인 경우 별도 파일에 저장
	if isCritical {
		l.criticalMu.Lock()
		l.criticalLogger.Println(logEntry)
		l.criticalMu.Unlock()
	}
}

// checkTimeBasedRotation 시간 기반 로그 롤링 확인
func (l *Logger) checkTimeBasedRotation() {
	now := time.Now()
	if now.Sub(l.lastRotation) >= l.rotationInterval {
		l.rotateByTime()
		l.lastRotation = now
	}
}

// rotateByTime 시간 기반 로그 롤링 수행
func (l *Logger) rotateByTime() {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 현재 파일 닫기
	if l.file != nil {
		l.file.Close()
	}

	// 타임스탬프가 포함된 백업 파일명 생성
	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("%s.%s", l.config.OutputFile, timestamp)

	// 기존 파일을 백업으로 이동
	if err := os.Rename(l.config.OutputFile, backupPath); err != nil {
		// 파일이 없거나 이동 실패시 무시
		return
	}

	// 새 파일 열기
	file, err := os.OpenFile(l.config.OutputFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return
	}

	l.file = file
	l.fileLogger = log.New(file, "", log.LstdFlags)
}

// Rotate 로그 파일 로테이션 (크기 기반)
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
