package logging

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// LogLevel represents the severity level
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

var levelNames = map[LogLevel]string{
	DEBUG: "DEBUG",
	INFO:  "INFO",
	WARN:  "WARN",
	ERROR: "ERROR",
	FATAL: "FATAL",
}

var levelEmojis = map[LogLevel]string{
	DEBUG: "🔍",
	INFO:  "ℹ️",
	WARN:  "⚠️",
	ERROR: "❌",
	FATAL: "💀",
}

// Logger represents the application logger
type Logger struct {
	level      LogLevel
	output     io.Writer
	fileOutput io.Writer
	component  string
	logFile    *os.File
}

// NewLogger creates a new logger instance
func NewLogger(component string, level LogLevel, logDir string) (*Logger, error) {
	logger := &Logger{
		level:     level,
		output:    os.Stdout,
		component: component,
	}

	// Create log directory if it doesn't exist
	if logDir != "" {
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create log directory: %w", err)
		}

		// Create daily log file
		timestamp := time.Now().Format("20060102")
		logFileName := filepath.Join(logDir, fmt.Sprintf("pumpwatch_%s_%s.log", component, timestamp))
		
		logFile, err := os.OpenFile(logFileName, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to create log file: %w", err)
		}

		logger.logFile = logFile
		logger.fileOutput = logFile
	}

	return logger, nil
}

// Close closes the logger and any open files
func (l *Logger) Close() error {
	if l.logFile != nil {
		return l.logFile.Close()
	}
	return nil
}

// formatMessage formats the log message with timestamp, level, component, and caller info
func (l *Logger) formatMessage(level LogLevel, msg string) string {
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	emoji := levelEmojis[level]
	levelName := levelNames[level]
	
	// Get caller information
	_, file, line, ok := runtime.Caller(3)
	caller := "unknown"
	if ok {
		caller = fmt.Sprintf("%s:%d", filepath.Base(file), line)
	}

	return fmt.Sprintf("%s %s [%s] [%s] [%s] %s",
		timestamp, emoji, levelName, l.component, caller, msg)
}

// log is the internal logging method
func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	msg := fmt.Sprintf(format, args...)
	formatted := l.formatMessage(level, msg)

	// Write to console
	if l.output != nil {
		fmt.Fprintln(l.output, formatted)
	}

	// Write to file if available
	if l.fileOutput != nil {
		fmt.Fprintln(l.fileOutput, formatted)
	}

	// For FATAL level, exit the program
	if level == FATAL {
		os.Exit(1)
	}
}

// Debug logs debug messages
func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(DEBUG, format, args...)
}

// Info logs info messages
func (l *Logger) Info(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// Warn logs warning messages
func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(WARN, format, args...)
}

// Error logs error messages
func (l *Logger) Error(format string, args ...interface{}) {
	l.log(ERROR, format, args...)
}

// Fatal logs fatal messages and exits
func (l *Logger) Fatal(format string, args ...interface{}) {
	l.log(FATAL, format, args...)
}

// WebSocketLogger creates a WebSocket-specific logger
func (l *Logger) WebSocketLogger(exchange, marketType string) *Logger {
	component := fmt.Sprintf("%s.%s.%s", l.component, exchange, marketType)
	
	wsLogger := &Logger{
		level:      l.level,
		output:     l.output,
		fileOutput: l.fileOutput,
		component:  component,
		logFile:    l.logFile, // Share the same log file
	}
	
	return wsLogger
}

// WithContext adds context-aware logging
func (l *Logger) WithContext(ctx context.Context) *ContextLogger {
	return &ContextLogger{
		logger: l,
		ctx:    ctx,
	}
}

// ContextLogger provides context-aware logging
type ContextLogger struct {
	logger *Logger
	ctx    context.Context
}

// Debug logs debug messages with context
func (cl *ContextLogger) Debug(format string, args ...interface{}) {
	select {
	case <-cl.ctx.Done():
		return // Context cancelled, don't log
	default:
		cl.logger.Debug(format, args...)
	}
}

// Info logs info messages with context
func (cl *ContextLogger) Info(format string, args ...interface{}) {
	select {
	case <-cl.ctx.Done():
		return
	default:
		cl.logger.Info(format, args...)
	}
}

// Warn logs warning messages with context
func (cl *ContextLogger) Warn(format string, args ...interface{}) {
	select {
	case <-cl.ctx.Done():
		return
	default:
		cl.logger.Warn(format, args...)
	}
}

// Error logs error messages with context
func (cl *ContextLogger) Error(format string, args ...interface{}) {
	select {
	case <-cl.ctx.Done():
		return
	default:
		cl.logger.Error(format, args...)
	}
}

// Global logger instance
var globalLogger *Logger

// InitGlobalLogger initializes the global logger
func InitGlobalLogger(component string, levelStr string, logDir string) error {
	level := parseLogLevel(levelStr)
	
	logger, err := NewLogger(component, level, logDir)
	if err != nil {
		return err
	}
	
	globalLogger = logger
	return nil
}

// GetGlobalLogger returns the global logger
func GetGlobalLogger() *Logger {
	if globalLogger == nil {
		// Fallback to basic logger
		globalLogger, _ = NewLogger("app", INFO, "")
	}
	return globalLogger
}

// CloseGlobalLogger closes the global logger
func CloseGlobalLogger() error {
	if globalLogger != nil {
		return globalLogger.Close()
	}
	return nil
}

// parseLogLevel converts string to LogLevel
func parseLogLevel(levelStr string) LogLevel {
	switch strings.ToUpper(levelStr) {
	case "DEBUG":
		return DEBUG
	case "INFO":
		return INFO
	case "WARN", "WARNING":
		return WARN
	case "ERROR":
		return ERROR
	case "FATAL":
		return FATAL
	default:
		return INFO
	}
}

// Convenience functions for global logger
func Debug(format string, args ...interface{}) {
	GetGlobalLogger().Debug(format, args...)
}

func Info(format string, args ...interface{}) {
	GetGlobalLogger().Info(format, args...)
}

func Warn(format string, args ...interface{}) {
	GetGlobalLogger().Warn(format, args...)
}

func Error(format string, args ...interface{}) {
	GetGlobalLogger().Error(format, args...)
}

func Fatal(format string, args ...interface{}) {
	GetGlobalLogger().Fatal(format, args...)
}

// RecoverPanic logs panics and optionally recovers
func RecoverPanic(component string) {
	if r := recover(); r != nil {
		// Get stack trace
		buf := make([]byte, 4096)
		n := runtime.Stack(buf, false)
		stackTrace := string(buf[:n])
		
		Error("🚨 PANIC in %s: %v\nStack trace:\n%s", component, r, stackTrace)
	}
}

// LogSystemStats logs system statistics
func LogSystemStats(stats interface{}) {
	Info("📊 System Stats: %+v", stats)
}

// LogWebSocketEvent logs WebSocket events with standardized format
func LogWebSocketEvent(exchange, marketType, event string, details interface{}) {
	Info("🔌 [%s.%s] %s: %v", exchange, marketType, event, details)
}

// LogUpbitEvent logs Upbit monitoring events
func LogUpbitEvent(event string, details interface{}) {
	Info("📡 [UPBIT] %s: %v", event, details)
}