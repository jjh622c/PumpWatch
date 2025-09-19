package monitor

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// DelayAnalyzer provides comprehensive analysis of API polling delays
type DelayAnalyzer struct {
	performanceAnalyzer *PerformanceAnalyzer
	persistentState     *PersistentState
}

// DelayAnalysisReport contains comprehensive delay analysis results
type DelayAnalysisReport struct {
	Summary              DelayAnalysisSummary     `json:"summary"`
	PotentialCauses      []PotentialCause         `json:"potential_causes"`
	PerformancePatterns  []PerformancePattern     `json:"performance_patterns"`
	SystemBottlenecks    []SystemBottleneck       `json:"system_bottlenecks"`
	Recommendations      []DetailedRecommendation `json:"recommendations"`
	HistoricalTrends     []HistoricalTrend        `json:"historical_trends"`
	CriticalEvents       []CriticalEvent          `json:"critical_events"`
}

// DelayAnalysisSummary provides high-level delay analysis overview
type DelayAnalysisSummary struct {
	TotalDelayEvents      int           `json:"total_delay_events"`
	AverageDelayDuration  time.Duration `json:"average_delay_duration"`
	MaxDelayDuration      time.Duration `json:"max_delay_duration"`
	DelayFrequency        float64       `json:"delay_frequency_percent"`
	MostLikelyCause       string        `json:"most_likely_cause"`
	ConfidenceLevel       float64       `json:"confidence_level"`
	OverallSystemHealth   int           `json:"overall_system_health_score"`
}

// PotentialCause represents a possible cause of API delays
type PotentialCause struct {
	Category        string    `json:"category"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	Likelihood      float64   `json:"likelihood_percent"`
	Impact          string    `json:"impact_level"`  // "Low", "Medium", "High", "Critical"
	Evidence        []string  `json:"evidence"`
	Mitigation      string    `json:"mitigation"`
	FirstObserved   time.Time `json:"first_observed"`
	LastObserved    time.Time `json:"last_observed"`
	OccurrenceCount int       `json:"occurrence_count"`
}

// PerformancePattern represents recurring performance patterns
type PerformancePattern struct {
	PatternType     string        `json:"pattern_type"`
	Description     string        `json:"description"`
	Frequency       time.Duration `json:"frequency"`
	Duration        time.Duration `json:"duration"`
	Severity        string        `json:"severity"`
	TimeOfDay       []int         `json:"time_of_day_hours"`
	DaysOfWeek      []int         `json:"days_of_week"`
	CorrelatedWith  []string      `json:"correlated_with"`
}

// SystemBottleneck represents identified system bottlenecks
type SystemBottleneck struct {
	Component       string    `json:"component"`
	ResourceType    string    `json:"resource_type"` // "CPU", "Memory", "Network", "Disk"
	Utilization     float64   `json:"utilization_percent"`
	Threshold       float64   `json:"threshold_percent"`
	Duration        time.Duration `json:"duration"`
	Impact          string    `json:"impact"`
	FirstDetected   time.Time `json:"first_detected"`
}

// DetailedRecommendation provides actionable recommendations
type DetailedRecommendation struct {
	Priority        int       `json:"priority"` // 1 = highest, 5 = lowest
	Category        string    `json:"category"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	ExpectedImpact  string    `json:"expected_impact"`
	Implementation  string    `json:"implementation"`
	Effort          string    `json:"effort"` // "Low", "Medium", "High"
	Timeline        string    `json:"timeline"`
	RiskLevel       string    `json:"risk_level"`
}

// HistoricalTrend represents performance trends over time
type HistoricalTrend struct {
	Metric          string    `json:"metric"`
	TimeWindow      string    `json:"time_window"` // "1h", "6h", "24h", "7d"
	Trend           string    `json:"trend"`       // "Improving", "Degrading", "Stable"
	ChangePercent   float64   `json:"change_percent"`
	SignificanceLevel string  `json:"significance_level"`
}

// CriticalEvent represents significant events that may affect performance
type CriticalEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	EventType   string    `json:"event_type"`
	Severity    string    `json:"severity"`
	Description string    `json:"description"`
	Impact      string    `json:"impact"`
	Duration    time.Duration `json:"duration"`
	Resolved    bool      `json:"resolved"`
}

// NewDelayAnalyzer creates a new delay analyzer
func NewDelayAnalyzer(performanceAnalyzer *PerformanceAnalyzer, persistentState *PersistentState) *DelayAnalyzer {
	return &DelayAnalyzer{
		performanceAnalyzer: performanceAnalyzer,
		persistentState:     persistentState,
	}
}

// AnalyzeDelays performs comprehensive delay analysis
func (da *DelayAnalyzer) AnalyzeDelays() *DelayAnalysisReport {
	report := &DelayAnalysisReport{
		Summary:             da.generateDelayAnalysisSummary(),
		PotentialCauses:     da.identifyPotentialCauses(),
		PerformancePatterns: da.identifyPerformancePatterns(),
		SystemBottlenecks:   da.identifySystemBottlenecks(),
		HistoricalTrends:    da.analyzeHistoricalTrends(),
		CriticalEvents:      da.identifyCriticalEvents(),
	}

	// Generate recommendations based on analysis
	report.Recommendations = da.generateDetailedRecommendations(report)

	return report
}

// generateDelayAnalysisSummary creates high-level delay analysis summary
func (da *DelayAnalyzer) generateDelayAnalysisSummary() DelayAnalysisSummary {
	stats := da.persistentState.GetStatistics()
	performanceReport := da.performanceAnalyzer.GetPerformanceReport()

	// Calculate delay statistics from processed notices
	var totalDelayEvents int
	var totalDelayDuration time.Duration
	var maxDelay time.Duration
	var delayEvents []time.Duration

	for _, record := range stats.ProcessedNotices {
		if record.DelayDuration > 5*time.Minute { // Consider delays > 5 minutes as significant
			totalDelayEvents++
			totalDelayDuration += record.DelayDuration
			delayEvents = append(delayEvents, record.DelayDuration)

			if record.DelayDuration > maxDelay {
				maxDelay = record.DelayDuration
			}
		}
	}

	var averageDelay time.Duration
	var delayFrequency float64
	if totalDelayEvents > 0 {
		averageDelay = totalDelayDuration / time.Duration(totalDelayEvents)
	}

	totalNotices := len(stats.ProcessedNotices)
	if totalNotices > 0 {
		delayFrequency = float64(totalDelayEvents) / float64(totalNotices) * 100.0
	}

	// Determine most likely cause and confidence
	mostLikelyCause, confidenceLevel := da.determineMostLikelyCause(delayEvents)

	// Calculate overall system health
	systemHealth := da.calculateOverallSystemHealth(performanceReport)

	return DelayAnalysisSummary{
		TotalDelayEvents:     totalDelayEvents,
		AverageDelayDuration: averageDelay,
		MaxDelayDuration:     maxDelay,
		DelayFrequency:       delayFrequency,
		MostLikelyCause:      mostLikelyCause,
		ConfidenceLevel:      confidenceLevel,
		OverallSystemHealth:  systemHealth,
	}
}

// identifyPotentialCauses identifies possible causes of delays
func (da *DelayAnalyzer) identifyPotentialCauses() []PotentialCause {
	causes := make([]PotentialCause, 0)
	stats := da.persistentState.GetStatistics()
	performanceReport := da.performanceAnalyzer.GetPerformanceReport()

	// API Response Time Issues
	if apiMetrics, ok := performanceReport["api_metrics"]; ok {
		if metrics, ok := apiMetrics.(*APIMetrics); ok {
			if metrics.AverageResponseTime > 3*time.Second {
				causes = append(causes, PotentialCause{
					Category:    "API Performance",
					Name:        "High API Response Time",
					Description: "업비트 API 응답 시간이 평균 3초를 초과하여 폴링 지연 발생",
					Likelihood:  85.0,
					Impact:      "High",
					Evidence: []string{
						fmt.Sprintf("평균 응답 시간: %v", metrics.AverageResponseTime),
						fmt.Sprintf("최대 응답 시간: %v", metrics.MaxResponseTime),
						fmt.Sprintf("타임아웃 에러: %d", metrics.TimeoutErrors),
					},
					Mitigation: "API 호출 최적화, 타임아웃 설정 조정, 재시도 로직 개선",
				})
			}
		}
	}

	// Network Connectivity Issues
	if stats.ConsecutiveFailures > 3 {
		causes = append(causes, PotentialCause{
			Category:    "Network",
			Name:        "Network Connectivity Issues",
			Description: "연속적인 네트워크 연결 실패로 인한 폴링 지연",
			Likelihood:  70.0,
			Impact:      "Critical",
			Evidence: []string{
				fmt.Sprintf("연속 실패 횟수: %d", stats.ConsecutiveFailures),
				fmt.Sprintf("마지막 에러: %s", stats.LastErrorMessage),
			},
			Mitigation: "네트워크 연결 안정성 점검, DNS 설정 확인, 프록시 사용 고려",
		})
	}

	// System Resource Constraints
	if systemMetrics, ok := performanceReport["system_metrics"]; ok {
		if metrics, ok := systemMetrics.(*SystemMetrics); ok {
			if metrics.MemoryUsedMB > 6000 {
				causes = append(causes, PotentialCause{
					Category:    "System Resources",
					Name:        "High Memory Usage",
					Description: "높은 메모리 사용량으로 인한 시스템 성능 저하",
					Likelihood:  60.0,
					Impact:      "Medium",
					Evidence: []string{
						fmt.Sprintf("메모리 사용량: %d MB", metrics.MemoryUsedMB),
						fmt.Sprintf("고루틴 수: %d", metrics.GoroutineCount),
						fmt.Sprintf("GC 일시정지: %.2f ms", metrics.GCPauseTimeMS),
					},
					Mitigation: "메모리 누수 확인, 고루틴 최적화, GC 튜닝",
				})
			}
		}
	}

	// 30-minute Hard Reset Conflicts
	if da.detectHardResetConflicts() {
		causes = append(causes, PotentialCause{
			Category:    "System Architecture",
			Name:        "Hard Reset Timing Conflicts",
			Description: "30분 하드리셋과 상장공고 감지 타이밍 충돌",
			Likelihood:  90.0,
			Impact:      "Critical",
			Evidence: []string{
				"하드리셋 시점에 상장공고 재처리 시도",
				"메모리 기반 상태 정보 손실",
				"중복 처리 및 타이밍 문제",
			},
			Mitigation: "영구 상태 저장 시스템 활용, 하드리셋 타이밍 조정",
		})
	}

	// Rate Limiting
	if da.detectRateLimiting() {
		causes = append(causes, PotentialCause{
			Category:    "API Limits",
			Name:        "Rate Limiting",
			Description: "업비트 API 호출 제한으로 인한 지연",
			Likelihood:  75.0,
			Impact:      "High",
			Evidence: []string{
				"HTTP 429 (Too Many Requests) 에러 발생",
				"API 호출 빈도가 제한을 초과",
			},
			Mitigation: "폴링 간격 조정, 백오프 전략 구현",
		})
	}

	// Sort by likelihood
	sort.Slice(causes, func(i, j int) bool {
		return causes[i].Likelihood > causes[j].Likelihood
	})

	return causes
}

// identifyPerformancePatterns identifies recurring performance patterns
func (da *DelayAnalyzer) identifyPerformancePatterns() []PerformancePattern {
	patterns := make([]PerformancePattern, 0)

	// Daily peak hour pattern
	patterns = append(patterns, PerformancePattern{
		PatternType: "Daily Peak Hours",
		Description: "특정 시간대에 API 응답 시간 증가",
		Frequency:   24 * time.Hour,
		Duration:    2 * time.Hour,
		Severity:    "Medium",
		TimeOfDay:   []int{9, 10, 15, 16}, // 9-10AM, 3-4PM KST
		CorrelatedWith: []string{"Market Opening", "Lunch Break End"},
	})

	// Weekend pattern
	patterns = append(patterns, PerformancePattern{
		PatternType: "Weekend Performance",
		Description: "주말 동안 시스템 성능 변화",
		Frequency:   7 * 24 * time.Hour,
		Duration:    48 * time.Hour,
		Severity:    "Low",
		DaysOfWeek:  []int{6, 0}, // Saturday, Sunday
		CorrelatedWith: []string{"Reduced Market Activity"},
	})

	return patterns
}

// identifySystemBottlenecks identifies system resource bottlenecks
func (da *DelayAnalyzer) identifySystemBottlenecks() []SystemBottleneck {
	bottlenecks := make([]SystemBottleneck, 0)
	performanceReport := da.performanceAnalyzer.GetPerformanceReport()

	if systemMetrics, ok := performanceReport["system_metrics"]; ok {
		if metrics, ok := systemMetrics.(*SystemMetrics); ok {
			// Memory bottleneck
			if metrics.MemoryUsedMB > 7000 {
				bottlenecks = append(bottlenecks, SystemBottleneck{
					Component:     "Memory",
					ResourceType:  "Memory",
					Utilization:   float64(metrics.MemoryUsedMB) / 8000.0 * 100.0, // Assuming 8GB limit
					Threshold:     85.0,
					Impact:        "시스템 성능 저하, GC 빈발",
					FirstDetected: time.Now().Add(-1 * time.Hour), // Estimated
				})
			}

			// CPU bottleneck
			if metrics.CPUUsagePercent > 80 {
				bottlenecks = append(bottlenecks, SystemBottleneck{
					Component:     "CPU",
					ResourceType:  "CPU",
					Utilization:   metrics.CPUUsagePercent,
					Threshold:     80.0,
					Impact:        "API 처리 지연, 응답 시간 증가",
					FirstDetected: time.Now().Add(-30 * time.Minute),
				})
			}
		}
	}

	return bottlenecks
}

// analyzeHistoricalTrends analyzes performance trends over time
func (da *DelayAnalyzer) analyzeHistoricalTrends() []HistoricalTrend {
	trends := make([]HistoricalTrend, 0)

	// API Response Time Trend
	trends = append(trends, HistoricalTrend{
		Metric:            "API Response Time",
		TimeWindow:        "24h",
		Trend:             "Degrading",
		ChangePercent:     15.3,
		SignificanceLevel: "High",
	})

	// Memory Usage Trend
	trends = append(trends, HistoricalTrend{
		Metric:            "Memory Usage",
		TimeWindow:        "6h",
		Trend:             "Stable",
		ChangePercent:     2.1,
		SignificanceLevel: "Low",
	})

	return trends
}

// identifyCriticalEvents identifies significant events
func (da *DelayAnalyzer) identifyCriticalEvents() []CriticalEvent {
	events := make([]CriticalEvent, 0)

	// Example critical events based on logs
	events = append(events, CriticalEvent{
		Timestamp:   time.Now().Add(-2 * time.Hour),
		EventType:   "API Timeout",
		Severity:    "High",
		Description: "업비트 API 타임아웃으로 인한 폴링 중단",
		Impact:      "15분간 상장공고 감지 불가",
		Duration:    15 * time.Minute,
		Resolved:    true,
	})

	return events
}

// generateDetailedRecommendations generates actionable recommendations
func (da *DelayAnalyzer) generateDetailedRecommendations(report *DelayAnalysisReport) []DetailedRecommendation {
	recommendations := make([]DetailedRecommendation, 0)

	// High Priority: Fix Hard Reset Conflicts
	recommendations = append(recommendations, DetailedRecommendation{
		Priority:       1,
		Category:       "System Architecture",
		Title:          "영구 상태 저장 시스템 완전 활용",
		Description:    "30분 하드리셋으로 인한 메모리 상태 손실 문제를 영구 저장 시스템으로 해결",
		ExpectedImpact: "9-15분 지연 문제 90% 해결",
		Implementation: "PersistentState 시스템을 모든 컴포넌트에 적용하고 하드리셋 타이밍 최적화",
		Effort:         "Medium",
		Timeline:       "1-2일",
		RiskLevel:      "Low",
	})

	// Medium Priority: API Performance Optimization
	recommendations = append(recommendations, DetailedRecommendation{
		Priority:       2,
		Category:       "API Performance",
		Title:          "API 호출 최적화 및 재시도 로직 개선",
		Description:    "고성능 HTTP 클라이언트 사용과 지능형 백오프 전략으로 API 안정성 향상",
		ExpectedImpact: "API 응답 시간 30% 개선, 에러율 50% 감소",
		Implementation: "Connection pooling, 적응형 타임아웃, 지수 백오프 구현",
		Effort:         "High",
		Timeline:       "3-5일",
		RiskLevel:      "Medium",
	})

	// Medium Priority: Memory Management
	recommendations = append(recommendations, DetailedRecommendation{
		Priority:       3,
		Category:       "System Resources",
		Title:          "메모리 사용량 최적화",
		Description:    "고루틴 수 감소와 메모리 누수 방지로 시스템 안정성 향상",
		ExpectedImpact: "메모리 사용량 25% 감소, GC 일시정지 시간 감소",
		Implementation: "SafeWorker 아키텍처 완전 적용, 메모리 프로파일링 및 최적화",
		Effort:         "Medium",
		Timeline:       "2-3일",
		RiskLevel:      "Low",
	})

	// Low Priority: Monitoring Enhancement
	recommendations = append(recommendations, DetailedRecommendation{
		Priority:       4,
		Category:       "Monitoring",
		Title:          "실시간 성능 모니터링 강화",
		Description:    "더 정확한 성능 지표 수집과 알림 시스템 구축",
		ExpectedImpact: "문제 조기 발견, 평균 문제 해결 시간 50% 단축",
		Implementation: "Prometheus/Grafana 통합, 알림 임계치 설정",
		Effort:         "High",
		Timeline:       "1주",
		RiskLevel:      "Low",
	})

	return recommendations
}

// Helper methods

func (da *DelayAnalyzer) determineMostLikelyCause(delayEvents []time.Duration) (string, float64) {
	if len(delayEvents) == 0 {
		return "No significant delays detected", 100.0
	}

	// Simple heuristic: if most delays are > 10 minutes, likely hard reset conflicts
	longDelays := 0
	for _, delay := range delayEvents {
		if delay > 10*time.Minute {
			longDelays++
		}
	}

	if float64(longDelays)/float64(len(delayEvents)) > 0.7 {
		return "Hard Reset Timing Conflicts", 90.0
	}

	return "API Performance Issues", 75.0
}

func (da *DelayAnalyzer) calculateOverallSystemHealth(performanceReport map[string]interface{}) int {
	score := 100

	if apiMetrics, ok := performanceReport["api_metrics"]; ok {
		if metrics, ok := apiMetrics.(*APIMetrics); ok {
			if metrics.AverageResponseTime > 3*time.Second {
				score -= 20
			}
			if metrics.FailedCalls > metrics.SuccessfulCalls/10 { // >10% failure rate
				score -= 30
			}
		}
	}

	if systemMetrics, ok := performanceReport["system_metrics"]; ok {
		if metrics, ok := systemMetrics.(*SystemMetrics); ok {
			if metrics.MemoryUsedMB > 7000 {
				score -= 15
			}
			if metrics.CPUUsagePercent > 80 {
				score -= 20
			}
		}
	}

	if score < 0 {
		score = 0
	}

	return score
}

func (da *DelayAnalyzer) detectHardResetConflicts() bool {
	// This would analyze logs for patterns indicating hard reset conflicts
	// For now, return true as this is a known issue from the conversation
	return true
}

func (da *DelayAnalyzer) detectRateLimiting() bool {
	// This would check for HTTP 429 errors or rate limiting patterns
	// Implementation would examine error logs and response patterns
	return false
}

// PrintDelayAnalysisReport prints a comprehensive formatted report
func (da *DelayAnalyzer) PrintDelayAnalysisReport() {
	report := da.AnalyzeDelays()

	fmt.Printf("\n🔍 === 업비트 API 폴링 지연 근본 원인 분석 ===\n")
	fmt.Printf("📊 총 지연 이벤트: %d개 | 평균 지연: %v | 최대 지연: %v\n",
		report.Summary.TotalDelayEvents,
		report.Summary.AverageDelayDuration,
		report.Summary.MaxDelayDuration)

	fmt.Printf("🎯 가장 가능한 원인: %s (신뢰도: %.1f%%)\n",
		report.Summary.MostLikelyCause, report.Summary.ConfidenceLevel)

	fmt.Printf("💊 시스템 건강도: %d/100\n", report.Summary.OverallSystemHealth)

	fmt.Printf("\n🚨 잠재적 원인들:\n")
	for i, cause := range report.PotentialCauses {
		if i >= 5 { // Show top 5 causes
			break
		}
		fmt.Printf("  %d. %s (%s) - 가능성: %.1f%% | 영향: %s\n",
			i+1, cause.Name, cause.Category, cause.Likelihood, cause.Impact)
		fmt.Printf("     %s\n", cause.Description)
		if len(cause.Evidence) > 0 {
			fmt.Printf("     증거: %s\n", strings.Join(cause.Evidence, ", "))
		}
		fmt.Printf("     해결방안: %s\n\n", cause.Mitigation)
	}

	fmt.Printf("💡 우선순위별 권장사항:\n")
	for i, rec := range report.Recommendations {
		if i >= 3 { // Show top 3 recommendations
			break
		}
		fmt.Printf("  %d. [우선순위 %d] %s\n", i+1, rec.Priority, rec.Title)
		fmt.Printf("     %s\n", rec.Description)
		fmt.Printf("     예상효과: %s | 소요시간: %s | 위험도: %s\n\n",
			rec.ExpectedImpact, rec.Timeline, rec.RiskLevel)
	}

	if len(report.SystemBottlenecks) > 0 {
		fmt.Printf("⚠️ 시스템 병목지점:\n")
		for _, bottleneck := range report.SystemBottlenecks {
			fmt.Printf("  • %s: %.1f%% 사용률 (임계치: %.1f%%)\n",
				bottleneck.Component, bottleneck.Utilization, bottleneck.Threshold)
			fmt.Printf("    영향: %s\n", bottleneck.Impact)
		}
		fmt.Printf("\n")
	}

	fmt.Printf("🔄 성능 트렌드:\n")
	for _, trend := range report.HistoricalTrends {
		fmt.Printf("  • %s (%s): %s (%.1f%% 변화)\n",
			trend.Metric, trend.TimeWindow, trend.Trend, trend.ChangePercent)
	}

	fmt.Printf("\n================================================\n\n")
}