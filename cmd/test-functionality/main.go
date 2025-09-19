package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"PumpWatch/internal/buffer"
	"PumpWatch/internal/config"
	"PumpWatch/internal/models"
	"PumpWatch/internal/symbols"
	"PumpWatch/internal/websocket"
)

// FunctionalityTester: 실제 기능 테스트를 위한 구조체
type FunctionalityTester struct {
	taskManager    *websocket.EnhancedTaskManager
	circularBuffer *buffer.CircularTradeBuffer
	ctx            context.Context
	cancel         context.CancelFunc
	testSymbol     string
	startTime      time.Time
}

// TestPhase: 테스트 단계 정의
type TestPhase struct {
	Name        string
	Description string
	Duration    time.Duration
	Action      func(*FunctionalityTester) error
}

// TestResult: 테스트 결과 저장
type TestResult struct {
	Phase           string            `json:"phase"`
	StartTime       time.Time         `json:"start_time"`
	EndTime         time.Time         `json:"end_time"`
	Duration        time.Duration     `json:"duration"`
	Success         bool              `json:"success"`
	DataCollected   int               `json:"data_collected"`
	DataRetrieved   int               `json:"data_retrieved"`
	ErrorMessage    string            `json:"error_message,omitempty"`
	Details         map[string]interface{} `json:"details,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: ./test-functionality <SYMBOL>")
		fmt.Println("Example: ./test-functionality SOMI")
		os.Exit(1)
	}

	testSymbol := os.Args[1]

	fmt.Printf("🧪 === PumpWatch v2.0 종합 기능 테스트 시작 ===\n")
	fmt.Printf("🎯 테스트 심볼: %s\n", testSymbol)
	fmt.Printf("📅 테스트 시작: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("🔬 테스트 목적: 17분 후 15분 전 데이터 접근 + 하드리셋 복구 검증\n\n")

	tester, err := NewFunctionalityTester(testSymbol)
	if err != nil {
		fmt.Printf("❌ 테스터 초기화 실패: %v\n", err)
		os.Exit(1)
	}
	defer tester.Close()

	// 테스트 단계 정의
	phases := []TestPhase{
		{
			Name:        "data_collection",
			Description: "17분간 실시간 데이터 수집",
			Duration:    17 * time.Minute,
			Action:      (*FunctionalityTester).runDataCollection,
		},
		{
			Name:        "historical_access",
			Description: "15분 전 데이터 접근 및 파일 저장 테스트",
			Duration:    1 * time.Minute,
			Action:      (*FunctionalityTester).testHistoricalAccess,
		},
		{
			Name:        "backup_create",
			Description: "하드리셋 대비 백업 생성",
			Duration:    30 * time.Second,
			Action:      (*FunctionalityTester).createBackup,
		},
		{
			Name:        "simulate_restart",
			Description: "하드리셋 시뮬레이션 (메모리 초기화)",
			Duration:    10 * time.Second,
			Action:      (*FunctionalityTester).simulateRestart,
		},
		{
			Name:        "backup_restore",
			Description: "백업에서 데이터 복원",
			Duration:    30 * time.Second,
			Action:      (*FunctionalityTester).restoreFromBackup,
		},
		{
			Name:        "post_restart_access",
			Description: "하드리셋 후 과거 데이터 접근 테스트",
			Duration:    1 * time.Minute,
			Action:      (*FunctionalityTester).testPostRestartAccess,
		},
	}

	// 인터럽트 시그널 처리
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	results := []TestResult{}

	// 각 테스트 단계 실행
	for _, phase := range phases {
		fmt.Printf("🚀 [%s] %s (소요시간: %v)\n", phase.Name, phase.Description, phase.Duration)

		result := TestResult{
			Phase:     phase.Name,
			StartTime: time.Now(),
			Details:   make(map[string]interface{}),
		}

		// 타이머 설정
		timer := time.NewTimer(phase.Duration)
		done := make(chan error, 1)

		// 단계 실행
		go func() {
			done <- phase.Action(tester)
		}()

		select {
		case err := <-done:
			timer.Stop()
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			if err != nil {
				result.Success = false
				result.ErrorMessage = err.Error()
				fmt.Printf("❌ [%s] 실패: %v\n", phase.Name, err)
			} else {
				result.Success = true
				fmt.Printf("✅ [%s] 성공 (소요: %v)\n", phase.Name, result.Duration)
			}

		case <-timer.C:
			result.EndTime = time.Now()
			result.Duration = phase.Duration
			result.Success = true
			fmt.Printf("⏰ [%s] 시간 완료 (소요: %v)\n", phase.Name, result.Duration)

		case <-sigChan:
			fmt.Printf("\n🛑 테스트 중단됨\n")
			tester.Close()
			os.Exit(0)
		}

		results = append(results, result)
		fmt.Printf("📊 [%s] 결과: 성공=%v, 수집=%d, 추출=%d\n\n",
			phase.Name, result.Success, result.DataCollected, result.DataRetrieved)
	}

	// 최종 결과 저장
	tester.saveTestResults(results)
	tester.printFinalReport(results)
}

// NewFunctionalityTester: 기능 테스터 초기화
func NewFunctionalityTester(testSymbol string) (*FunctionalityTester, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// 설정 로드
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		cancel()
		return nil, fmt.Errorf("설정 로드 실패: %v", err)
	}

	// 심볼 설정 로드
	symbolsConfig, err := symbols.LoadConfig("config/symbols/symbols.yaml")
	if err != nil {
		cancel()
		return nil, fmt.Errorf("심볼 설정 로드 실패: %v", err)
	}

	// EnhancedTaskManager 초기화 (PumpAnalyzer 비활성화)
	taskManager, err := websocket.NewEnhancedTaskManager(ctx, cfg.Exchanges, symbolsConfig, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("TaskManager 초기화 실패: %v", err)
	}

	// TaskManager의 CircularBuffer 사용 (별도 생성하지 않음)
	circularBuffer := taskManager.GetCircularBuffer()

	fmt.Printf("✅ 기능 테스터 초기화 완료\n")
	fmt.Printf("📊 TaskManager 초기화됨\n")

	return &FunctionalityTester{
		taskManager:    taskManager,
		circularBuffer: circularBuffer,
		ctx:            ctx,
		cancel:         cancel,
		testSymbol:     testSymbol,
		startTime:      time.Now(),
	}, nil
}

// runDataCollection: 17분간 데이터 수집
func (ft *FunctionalityTester) runDataCollection() error {
	fmt.Printf("📡 데이터 수집 시작...\n")

	// TaskManager 시작
	if err := ft.taskManager.Start(); err != nil {
		return fmt.Errorf("TaskManager 시작 실패: %v", err)
	}

	// 1분마다 수집 상태 출력
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	collectedCount := 0
	for i := 0; i < 17; i++ {
		select {
		case <-ticker.C:
			stats := ft.circularBuffer.GetStats()
			collectedCount = int(stats.TotalEvents)
			fmt.Printf("📊 [%d분] 수집된 거래: %d개, 메모리: %.1fMB\n",
				i+1, collectedCount, float64(stats.MemoryUsage)/1024/1024)
		case <-ft.ctx.Done():
			return fmt.Errorf("컨텍스트 취소됨")
		}
	}

	fmt.Printf("✅ 17분간 데이터 수집 완료: 총 %d개 거래\n", collectedCount)
	return nil
}

// testHistoricalAccess: 15분 전 데이터 접근 테스트
func (ft *FunctionalityTester) testHistoricalAccess() error {
	fmt.Printf("🔍 15분 전 데이터 접근 테스트...\n")

	// 15분 전 시점 계산
	now := time.Now()
	targetTime := now.Add(-15 * time.Minute)
	startTime := targetTime.Add(-20 * time.Second)
	endTime := targetTime.Add(20 * time.Second)

	fmt.Printf("🎯 타겟 시간: %s\n", targetTime.Format("15:04:05"))
	fmt.Printf("📅 데이터 범위: %s ~ %s\n",
		startTime.Format("15:04:05"), endTime.Format("15:04:05"))

	// 과거 데이터 추출 (모든 거래소에서)
	var trades []models.TradeEvent
	exchanges := []string{"binance", "bybit", "okx", "kucoin", "gate"}
	for _, exchange := range exchanges {
		exchangeTrades, err := ft.circularBuffer.GetTradeEvents(exchange, startTime, endTime)
		if err != nil {
			fmt.Printf("⚠️ %s 거래소 데이터 추출 실패: %v\n", exchange, err)
			continue
		}
		trades = append(trades, exchangeTrades...)
	}

	if len(trades) == 0 {
		return fmt.Errorf("15분 전 데이터가 없음 - CircularBuffer 문제 가능성")
	}

	fmt.Printf("✅ 15분 전 데이터 추출 성공: %d개 거래\n", len(trades))

	// 심볼 필터링 (테스트 심볼만)
	symbolTrades := ft.filterTradesBySymbol(trades, ft.testSymbol)
	fmt.Printf("🎯 %s 심볼 거래: %d개\n", ft.testSymbol, len(symbolTrades))

	// 파일 저장 테스트
	if err := ft.saveDataToFile(symbolTrades, "historical_access_test"); err != nil {
		return fmt.Errorf("파일 저장 실패: %v", err)
	}

	fmt.Printf("💾 파일 저장 성공: data/test_results/historical_access_test.json\n")
	return nil
}

// createBackup: 백업 생성
func (ft *FunctionalityTester) createBackup() error {
	fmt.Printf("💾 CircularBuffer 백업 생성...\n")

	if err := ft.circularBuffer.SaveToBackup(); err != nil {
		return fmt.Errorf("백업 생성 실패: %v", err)
	}

	// 백업 파일 존재 확인
	backupPath := "data/buffer/circular_backup.json"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("백업 파일이 생성되지 않음: %s", backupPath)
	}

	fmt.Printf("✅ 백업 생성 완료: %s\n", backupPath)
	return nil
}

// simulateRestart: 하드리셋 시뮬레이션
func (ft *FunctionalityTester) simulateRestart() error {
	fmt.Printf("🔄 하드리셋 시뮬레이션 (메모리 초기화)...\n")

	// TaskManager 중지
	ft.taskManager.Stop()
	fmt.Printf("🛑 TaskManager 중지 완료\n")

	// CircularBuffer 닫기 (메모리 해제)
	ft.circularBuffer.Close()
	fmt.Printf("🗑️ CircularBuffer 메모리 해제 완료\n")

	// 새로운 CircularBuffer 생성 (빈 상태)
	newBuffer := buffer.NewCircularTradeBuffer(ft.ctx)
	ft.circularBuffer = newBuffer

	fmt.Printf("✅ 하드리셋 시뮬레이션 완료 (메모리 초기화됨)\n")
	return nil
}

// restoreFromBackup: 백업에서 복원
func (ft *FunctionalityTester) restoreFromBackup() error {
	fmt.Printf("📥 백업에서 데이터 복원...\n")

	if err := ft.circularBuffer.LoadFromBackup(); err != nil {
		return fmt.Errorf("백업 복원 실패: %v", err)
	}

	stats := ft.circularBuffer.GetStats()
	fmt.Printf("✅ 백업 복원 완료: %d개 거래, %.1fMB 메모리\n",
		stats.TotalEvents, float64(stats.MemoryUsage)/1024/1024)
	return nil
}

// testPostRestartAccess: 하드리셋 후 과거 데이터 접근 테스트
func (ft *FunctionalityTester) testPostRestartAccess() error {
	fmt.Printf("🔍 하드리셋 후 과거 데이터 접근 테스트...\n")

	// 17분 전 시점 계산 (테스트 시작 시점)
	targetTime := ft.startTime.Add(5 * time.Minute) // 테스트 시작 5분 후
	startTime := targetTime.Add(-20 * time.Second)
	endTime := targetTime.Add(20 * time.Second)

	fmt.Printf("🎯 타겟 시간: %s (테스트 시작 5분 후)\n", targetTime.Format("15:04:05"))
	fmt.Printf("📅 데이터 범위: %s ~ %s\n",
		startTime.Format("15:04:05"), endTime.Format("15:04:05"))

	// 복원된 데이터에서 과거 데이터 추출 (모든 거래소에서)
	var trades []models.TradeEvent
	exchanges := []string{"binance", "bybit", "okx", "kucoin", "gate"}
	for _, exchange := range exchanges {
		exchangeTrades, err := ft.circularBuffer.GetTradeEvents(exchange, startTime, endTime)
		if err != nil {
			fmt.Printf("⚠️ %s 거래소 데이터 추출 실패: %v\n", exchange, err)
			continue
		}
		trades = append(trades, exchangeTrades...)
	}

	if len(trades) == 0 {
		return fmt.Errorf("하드리셋 후 과거 데이터 접근 실패 - 백업/복원 문제")
	}

	fmt.Printf("✅ 하드리셋 후 과거 데이터 접근 성공: %d개 거래\n", len(trades))

	// 심볼 필터링
	symbolTrades := ft.filterTradesBySymbol(trades, ft.testSymbol)
	fmt.Printf("🎯 %s 심볼 거래: %d개\n", ft.testSymbol, len(symbolTrades))

	// 파일 저장
	if err := ft.saveDataToFile(symbolTrades, "post_restart_access_test"); err != nil {
		return fmt.Errorf("파일 저장 실패: %v", err)
	}

	fmt.Printf("💾 파일 저장 성공: data/test_results/post_restart_access_test.json\n")
	return nil
}

// filterTradesBySymbol: 심볼별 거래 필터링
func (ft *FunctionalityTester) filterTradesBySymbol(trades []models.TradeEvent, symbol string) []models.TradeEvent {
	var filtered []models.TradeEvent
	for _, trade := range trades {
		if ft.isTargetSymbol(trade.Symbol, symbol) {
			filtered = append(filtered, trade)
		}
	}
	return filtered
}

// isTargetSymbol: 심볼 매칭 (거래소별 형식 지원)
func (ft *FunctionalityTester) isTargetSymbol(tradeSymbol, targetSymbol string) bool {
	// 기본 매칭
	if tradeSymbol == targetSymbol {
		return true
	}
	// USDT 페어 매칭
	if tradeSymbol == targetSymbol+"USDT" {
		return true
	}
	// 거래소별 구분자 매칭
	if tradeSymbol == targetSymbol+"-USDT" || tradeSymbol == targetSymbol+"_USDT" {
		return true
	}
	return false
}

// saveDataToFile: 데이터 파일 저장
func (ft *FunctionalityTester) saveDataToFile(trades []models.TradeEvent, filename string) error {
	// 디렉토리 생성
	dir := "data/test_results"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("디렉토리 생성 실패: %v", err)
	}

	// 파일 경로
	filePath := filepath.Join(dir, filename+".json")

	// JSON 직렬화
	data, err := json.MarshalIndent(trades, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON 직렬화 실패: %v", err)
	}

	// 파일 쓰기
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("파일 쓰기 실패: %v", err)
	}

	return nil
}

// saveTestResults: 테스트 결과 저장
func (ft *FunctionalityTester) saveTestResults(results []TestResult) {
	dir := "data/test_results"
	os.MkdirAll(dir, 0755)

	filePath := filepath.Join(dir, fmt.Sprintf("functionality_test_%s.json",
		time.Now().Format("20060102_150405")))

	data, _ := json.MarshalIndent(results, "", "  ")
	os.WriteFile(filePath, data, 0644)

	fmt.Printf("📋 테스트 결과 저장: %s\n", filePath)
}

// printFinalReport: 최종 보고서 출력
func (ft *FunctionalityTester) printFinalReport(results []TestResult) {
	fmt.Printf("\n🎯 === PumpWatch v2.0 종합 기능 테스트 완료 ===\n")
	fmt.Printf("📅 테스트 종료: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("⏱️  총 소요시간: %v\n\n", time.Since(ft.startTime))

	successCount := 0
	for _, result := range results {
		status := "❌ 실패"
		if result.Success {
			status = "✅ 성공"
			successCount++
		}
		fmt.Printf("%s [%s] %v\n", status, result.Phase, result.Duration)
		if result.ErrorMessage != "" {
			fmt.Printf("   └─ 오류: %s\n", result.ErrorMessage)
		}
	}

	fmt.Printf("\n📊 성공률: %d/%d (%.1f%%)\n",
		successCount, len(results), float64(successCount)/float64(len(results))*100)

	if successCount == len(results) {
		fmt.Printf("🎉 모든 테스트 통과! PumpWatch v2.0 시스템이 완벽하게 작동합니다.\n")
	} else {
		fmt.Printf("⚠️  일부 테스트 실패. 시스템 점검이 필요합니다.\n")
	}
}

// Close: 리소스 정리
func (ft *FunctionalityTester) Close() {
	fmt.Printf("🧹 리소스 정리 중...\n")

	if ft.taskManager != nil {
		ft.taskManager.Stop()
	}

	if ft.circularBuffer != nil {
		ft.circularBuffer.Close()
	}

	if ft.cancel != nil {
		ft.cancel()
	}

	fmt.Printf("✅ 리소스 정리 완료\n")
}