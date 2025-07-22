package latency

import (
	"fmt"
	"sync"
	"time"
)

// LatencyRecord 개별 지연 기록
type LatencyRecord struct {
	Symbol         string
	Type           string // "trade" or "orderbook"
	ExchangeTime   time.Time
	ReceiveTime    time.Time
	LatencySeconds float64
}

// LatencyStats 심볼별 지연 통계
type LatencyStats struct {
	Symbol         string
	Type           string
	Count          int
	TotalLatency   float64
	AverageLatency float64
	MaxLatency     float64
	MinLatency     float64
	LastUpdate     time.Time
}

// LatencyMonitor 지연 모니터링 관리자
type LatencyMonitor struct {
	mu                sync.RWMutex
	records           map[string][]LatencyRecord // key: "symbol_type"
	stats             map[string]*LatencyStats   // key: "symbol_type"
	warnThreshold     float64
	criticalThreshold float64
	statsInterval     time.Duration
	lastStatsTime     time.Time
}

// NewLatencyMonitor 새 지연 모니터 생성
func NewLatencyMonitor(warnThreshold, criticalThreshold float64, statsIntervalSeconds int) *LatencyMonitor {
	return &LatencyMonitor{
		records:           make(map[string][]LatencyRecord),
		stats:             make(map[string]*LatencyStats),
		warnThreshold:     warnThreshold,
		criticalThreshold: criticalThreshold,
		statsInterval:     time.Duration(statsIntervalSeconds) * time.Second,
		lastStatsTime:     time.Now(),
	}
}

// RecordLatency 지연 기록 추가
func (lm *LatencyMonitor) RecordLatency(symbol, dataType string, exchangeTime, receiveTime time.Time) (float64, bool) {
	latency := receiveTime.Sub(exchangeTime).Seconds()

	record := LatencyRecord{
		Symbol:         symbol,
		Type:           dataType,
		ExchangeTime:   exchangeTime,
		ReceiveTime:    receiveTime,
		LatencySeconds: latency,
	}

	key := fmt.Sprintf("%s_%s", symbol, dataType)

	lm.mu.Lock()
	defer lm.mu.Unlock()

	// 기록 추가 (최근 100개만 유지)
	if len(lm.records[key]) >= 100 {
		lm.records[key] = lm.records[key][1:]
	}
	lm.records[key] = append(lm.records[key], record)

	// 통계 업데이트
	lm.updateStats(key, record)

	// 경고 여부 반환
	isWarning := latency >= lm.warnThreshold
	return latency, isWarning
}

// updateStats 통계 업데이트
func (lm *LatencyMonitor) updateStats(key string, record LatencyRecord) {
	stats, exists := lm.stats[key]
	if !exists {
		stats = &LatencyStats{
			Symbol:     record.Symbol,
			Type:       record.Type,
			MinLatency: record.LatencySeconds,
		}
		lm.stats[key] = stats
	}

	stats.Count++
	stats.TotalLatency += record.LatencySeconds
	stats.AverageLatency = stats.TotalLatency / float64(stats.Count)
	stats.LastUpdate = time.Now()

	if record.LatencySeconds > stats.MaxLatency {
		stats.MaxLatency = record.LatencySeconds
	}
	if record.LatencySeconds < stats.MinLatency {
		stats.MinLatency = record.LatencySeconds
	}
}

// GetLatencyStats 모든 지연 통계 조회
func (lm *LatencyMonitor) GetLatencyStats() map[string]*LatencyStats {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	result := make(map[string]*LatencyStats)
	for key, stats := range lm.stats {
		// 통계 복사본 생성
		statsCopy := *stats
		result[key] = &statsCopy
	}
	return result
}

// ShouldPrintStats 통계 출력 여부 확인
func (lm *LatencyMonitor) ShouldPrintStats() bool {
	now := time.Now()
	if now.Sub(lm.lastStatsTime) >= lm.statsInterval {
		lm.lastStatsTime = now
		return true
	}
	return false
}

// GetCriticalSymbols 심각한 지연이 있는 심볼들 조회
func (lm *LatencyMonitor) GetCriticalSymbols() []string {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	var criticalSymbols []string
	symbolSet := make(map[string]bool)

	for _, stats := range lm.stats {
		if stats.MaxLatency >= lm.criticalThreshold {
			if !symbolSet[stats.Symbol] {
				symbolSet[stats.Symbol] = true
				criticalSymbols = append(criticalSymbols, stats.Symbol)
			}
		}
	}

	return criticalSymbols
}

// GetWarnSymbols 경고 수준 지연이 있는 심볼들 조회
func (lm *LatencyMonitor) GetWarnSymbols() []string {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	var warnSymbols []string
	symbolSet := make(map[string]bool)

	for _, stats := range lm.stats {
		if stats.AverageLatency >= lm.warnThreshold {
			if !symbolSet[stats.Symbol] {
				symbolSet[stats.Symbol] = true
				warnSymbols = append(warnSymbols, stats.Symbol)
			}
		}
	}

	return warnSymbols
}

// GetOverallStats 전체 통계 요약
func (lm *LatencyMonitor) GetOverallStats() map[string]interface{} {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	totalRecords := 0
	totalLatency := 0.0
	maxLatency := 0.0
	minLatency := 999999.0
	symbolCount := 0

	for _, stats := range lm.stats {
		totalRecords += stats.Count
		totalLatency += stats.TotalLatency
		if stats.MaxLatency > maxLatency {
			maxLatency = stats.MaxLatency
		}
		if stats.MinLatency < minLatency {
			minLatency = stats.MinLatency
		}
		symbolCount++
	}

	avgLatency := 0.0
	if totalRecords > 0 {
		avgLatency = totalLatency / float64(totalRecords)
	}

	return map[string]interface{}{
		"total_records":    totalRecords,
		"symbol_count":     symbolCount,
		"avg_latency":      avgLatency,
		"max_latency":      maxLatency,
		"min_latency":      minLatency,
		"warn_symbols":     len(lm.GetWarnSymbols()),
		"critical_symbols": len(lm.GetCriticalSymbols()),
	}
}
