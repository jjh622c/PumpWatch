package monitor

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// PerformanceMonitor 성능 모니터링
type PerformanceMonitor struct {
	mu                sync.RWMutex
	peakThroughput    int             // 최대 처리량 (초당)
	averageThroughput int             // 평균 처리량 (초당)
	overflowCount     int             // 오버플로우 횟수
	delayCount        int             // 지연 횟수
	lastUpdate        time.Time       // 마지막 업데이트
	processingTimes   []time.Duration // 처리 시간 기록
}

// NewPerformanceMonitor 성능 모니터 생성
func NewPerformanceMonitor() *PerformanceMonitor {
	return &PerformanceMonitor{
		processingTimes: make([]time.Duration, 0, 1000),
		lastUpdate:      time.Now(),
	}
}

// RecordProcessingTime 처리 시간 기록
func (pm *PerformanceMonitor) RecordProcessingTime(duration time.Duration) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.processingTimes = append(pm.processingTimes, duration)

	// 최근 1000개만 유지
	if len(pm.processingTimes) > 1000 {
		pm.processingTimes = pm.processingTimes[1:]
	}

	// 처리량 계산 (1초 단위)
	now := time.Now()
	if now.Sub(pm.lastUpdate) >= time.Second {
		currentThroughput := len(pm.processingTimes)

		if currentThroughput > pm.peakThroughput {
			pm.peakThroughput = currentThroughput
		}

		// 평균 처리량 업데이트 (이동 평균)
		pm.averageThroughput = (pm.averageThroughput + currentThroughput) / 2

		pm.lastUpdate = now
		pm.processingTimes = pm.processingTimes[:0] // 초기화
	}

	// 지연 감지 (100ms 이상)
	if duration > 100*time.Millisecond {
		pm.delayCount++
		log.Printf("⚠️  처리 지연 감지: %v", duration)
	}
}

// RecordOverflow 오버플로우 기록
func (pm *PerformanceMonitor) RecordOverflow() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.overflowCount++
	log.Printf("🚨 오버플로우 발생: 총 %d회", pm.overflowCount)
}

// GetStats 성능 통계 조회
func (pm *PerformanceMonitor) GetStats() map[string]interface{} {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// 평균 처리 시간 계산
	var totalTime time.Duration
	for _, duration := range pm.processingTimes {
		totalTime += duration
	}

	avgProcessingTime := 0.0
	if len(pm.processingTimes) > 0 {
		avgProcessingTime = float64(totalTime.Milliseconds()) / float64(len(pm.processingTimes))
	}

	stats := make(map[string]interface{})
	stats["peak_throughput"] = pm.peakThroughput
	stats["average_throughput"] = pm.averageThroughput
	stats["overflow_count"] = pm.overflowCount
	stats["delay_count"] = pm.delayCount
	stats["avg_processing_time"] = avgProcessingTime
	stats["last_update"] = pm.lastUpdate

	return stats
}

// SystemMonitor 시스템 모니터링
type SystemMonitor struct {
	mu           sync.RWMutex
	startTime    time.Time
	uptime       time.Duration
	errorCount   int
	warningCount int
	lastError    error
	lastWarning  string
	healthStatus string // HEALTHY/WARNING/CRITICAL
	restartCount int
	lastRestart  time.Time
}

// NewSystemMonitor 시스템 모니터 생성
func NewSystemMonitor() *SystemMonitor {
	return &SystemMonitor{
		startTime:    time.Now(),
		healthStatus: "HEALTHY",
	}
}

// RecordError 에러 기록
func (sm *SystemMonitor) RecordError(err error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.errorCount++
	sm.lastError = err
	sm.healthStatus = "CRITICAL"

	log.Printf("🚨 시스템 에러 발생: %v (총 %d회)", err, sm.errorCount)

	// 슬랙/텔레그램 알림 (실제 구현 시)
	sm.sendAlert("ERROR", fmt.Sprintf("시스템 에러: %v", err))
}

// RecordWarning 경고 기록
func (sm *SystemMonitor) RecordWarning(msg string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.warningCount++
	sm.lastWarning = msg

	if sm.healthStatus == "HEALTHY" {
		sm.healthStatus = "WARNING"
	}

	log.Printf("⚠️  시스템 경고: %s (총 %d회)", msg, sm.warningCount)

	// 슬랙/텔레그램 알림 (실제 구현 시)
	sm.sendAlert("WARNING", fmt.Sprintf("시스템 경고: %s", msg))
}

// RecordRestart 재시작 기록
func (sm *SystemMonitor) RecordRestart() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.restartCount++
	sm.lastRestart = time.Now()
	sm.healthStatus = "HEALTHY" // 재시작 후 건강 상태로 복구

	log.Printf("🔄 시스템 재시작: 총 %d회", sm.restartCount)
}

// sendAlert 알림 전송 (실제 구현 시)
func (sm *SystemMonitor) sendAlert(level, message string) {
	// 실제 알림 로직 구현
	log.Printf("📤 알림 전송 [%s]: %s", level, message)
}

// GetHealthStatus 건강 상태 조회
func (sm *SystemMonitor) GetHealthStatus() map[string]interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sm.uptime = time.Since(sm.startTime)

	status := make(map[string]interface{})
	status["health_status"] = sm.healthStatus
	status["uptime"] = sm.uptime.String()
	status["error_count"] = sm.errorCount
	status["warning_count"] = sm.warningCount
	status["restart_count"] = sm.restartCount
	status["last_restart"] = sm.lastRestart

	if sm.lastError != nil {
		status["last_error"] = sm.lastError.Error()
	}

	if sm.lastWarning != "" {
		status["last_warning"] = sm.lastWarning
	}

	return status
}

// AutoRestart 자동 재시작 여부 확인
func (sm *SystemMonitor) AutoRestart() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// 에러가 10회 이상이고 마지막 에러가 5분 이내인 경우
	if sm.errorCount >= 10 && sm.lastError != nil {
		// 실제 구현에서는 더 복잡한 로직 사용
		return true
	}

	return false
}
