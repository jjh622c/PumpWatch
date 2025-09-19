package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"PumpWatch/internal/buffer"
	"PumpWatch/internal/config"
	"PumpWatch/internal/models"
	"PumpWatch/internal/symbols"
	"PumpWatch/internal/websocket"
)

// QuickFunctionalityTester: 빠른 기능 테스트
type QuickFunctionalityTester struct {
	taskManager    *websocket.EnhancedTaskManager
	circularBuffer *buffer.CircularTradeBuffer
	ctx            context.Context
	cancel         context.CancelFunc
	testSymbol     string
	startTime      time.Time
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: ./test-quick-functionality <SYMBOL>")
		fmt.Println("Example: ./test-quick-functionality SOMI")
		os.Exit(1)
	}

	testSymbol := os.Args[1]

	fmt.Printf("🧪 === PumpWatch v2.0 빠른 기능 테스트 ===\n")
	fmt.Printf("🎯 테스트 심볼: %s\n", testSymbol)
	fmt.Printf("📅 테스트 시작: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("⏱️  총 소요시간: 약 3분 (데이터 수집 2분 + 테스트 1분)\n\n")

	tester, err := NewQuickFunctionalityTester(testSymbol)
	if err != nil {
		fmt.Printf("❌ 테스터 초기화 실패: %v\n", err)
		os.Exit(1)
	}
	defer tester.Close()

	// 빠른 테스트 단계
	fmt.Printf("🚀 1단계: 2분간 데이터 수집\n")
	if err := tester.runQuickDataCollection(); err != nil {
		fmt.Printf("❌ 데이터 수집 실패: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("🔍 2단계: 1분 전 데이터 접근 테스트\n")
	if err := tester.testRecentAccess(); err != nil {
		fmt.Printf("❌ 과거 데이터 접근 실패: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("💾 3단계: 백업/복원 테스트\n")
	if err := tester.testBackupRestore(); err != nil {
		fmt.Printf("❌ 백업/복원 실패: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n🎉 모든 빠른 테스트 통과! PumpWatch v2.0 기본 기능이 정상 작동합니다.\n")
	fmt.Printf("💡 실제 17분 테스트가 필요하면 ./test-functionality 를 사용하세요.\n")
}

func NewQuickFunctionalityTester(testSymbol string) (*QuickFunctionalityTester, error) {
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

	// EnhancedTaskManager 초기화
	taskManager, err := websocket.NewEnhancedTaskManager(ctx, cfg.Exchanges, symbolsConfig, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("TaskManager 초기화 실패: %v", err)
	}

	// TaskManager의 CircularBuffer 사용 (별도 생성하지 않음)
	circularBuffer := taskManager.GetCircularBuffer()

	fmt.Printf("✅ 빠른 기능 테스터 초기화 완료\n")

	return &QuickFunctionalityTester{
		taskManager:    taskManager,
		circularBuffer: circularBuffer,
		ctx:            ctx,
		cancel:         cancel,
		testSymbol:     testSymbol,
		startTime:      time.Now(),
	}, nil
}

// runQuickDataCollection: 2분간 빠른 데이터 수집
func (qft *QuickFunctionalityTester) runQuickDataCollection() error {
	fmt.Printf("📡 WebSocket 연결 및 데이터 수집 시작...\n")

	// TaskManager 시작
	if err := qft.taskManager.Start(); err != nil {
		return fmt.Errorf("TaskManager 시작 실패: %v", err)
	}

	// 30초마다 상태 출력
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	duration := 2 * time.Minute
	endTime := time.Now().Add(duration)

	for time.Now().Before(endTime) {
		select {
		case <-ticker.C:
			stats := qft.circularBuffer.GetStats()
			fmt.Printf("📊 수집된 거래: %d개, 메모리: %.1fMB\n",
				stats.TotalEvents, float64(stats.MemoryUsage)/1024/1024)
		case <-qft.ctx.Done():
			return fmt.Errorf("컨텍스트 취소됨")
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}

	stats := qft.circularBuffer.GetStats()
	fmt.Printf("✅ 2분간 데이터 수집 완료: 총 %d개 거래\n", stats.TotalEvents)
	return nil
}

// testRecentAccess: 최근 데이터 접근 테스트 (30초 전)
func (qft *QuickFunctionalityTester) testRecentAccess() error {
	fmt.Printf("🔍 최근 데이터 접근 테스트 (30초 전)...\n")

	// 30초 전 시점 계산 (데이터가 확실히 있는 시간대)
	now := time.Now()
	targetTime := now.Add(-30 * time.Second)
	startTime := targetTime.Add(-10 * time.Second)
	endTime := targetTime.Add(10 * time.Second)

	fmt.Printf("🎯 타겟 시간: %s\n", targetTime.Format("15:04:05"))
	fmt.Printf("📅 데이터 범위: %s ~ %s\n",
		startTime.Format("15:04:05"), endTime.Format("15:04:05"))

	// 과거 데이터 추출 (거래소별 spot + futures 키 사용)
	var trades []models.TradeEvent
	exchangeKeys := []string{
		"binance_spot", "binance_futures",
		"bybit_spot", "bybit_futures",
		"okx_spot", "okx_futures",
		"kucoin_spot", "kucoin_futures",
		"gate_spot", "gate_futures",
	}
	for _, exchangeKey := range exchangeKeys {
		exchangeTrades, err := qft.circularBuffer.GetTradeEvents(exchangeKey, startTime, endTime)
		if err != nil {
			fmt.Printf("⚠️ %s 거래소 데이터 추출 실패: %v\n", exchangeKey, err)
			continue
		}
		trades = append(trades, exchangeTrades...)
	}

	if len(trades) == 0 {
		return fmt.Errorf("30초 전 데이터가 없음 - CircularBuffer 조회 문제 가능성")
	}

	fmt.Printf("✅ 30초 전 데이터 추출 성공: %d개 거래\n", len(trades))

	// 심볼 필터링
	symbolTrades := qft.filterTradesBySymbol(trades, qft.testSymbol)
	fmt.Printf("🎯 %s 심볼 거래: %d개\n", qft.testSymbol, len(symbolTrades))

	// 파일 저장 테스트
	if err := qft.saveDataToFile(symbolTrades, "quick_access_test"); err != nil {
		return fmt.Errorf("파일 저장 실패: %v", err)
	}

	fmt.Printf("💾 파일 저장 성공: data/test_results/quick_access_test.json\n")
	return nil
}

// testBackupRestore: 백업/복원 테스트
func (qft *QuickFunctionalityTester) testBackupRestore() error {
	fmt.Printf("💾 백업 생성 테스트...\n")

	// 원본 통계 기록
	originalStats := qft.circularBuffer.GetStats()
	fmt.Printf("📊 원본 데이터: %d개 거래, %.1fMB\n",
		originalStats.TotalEvents, float64(originalStats.MemoryUsage)/1024/1024)

	// 백업 생성
	if err := qft.circularBuffer.SaveToBackup(); err != nil {
		return fmt.Errorf("백업 생성 실패: %v", err)
	}

	// 백업 파일 확인
	backupPath := "data/buffer/circular_backup.json"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("백업 파일이 생성되지 않음: %s", backupPath)
	}

	fmt.Printf("✅ 백업 생성 완료: %s\n", backupPath)

	// 메모리 초기화 시뮬레이션 (새 버퍼 생성)
	fmt.Printf("🔄 메모리 초기화 시뮬레이션...\n")
	newBuffer := buffer.NewCircularTradeBuffer(qft.ctx)
	qft.circularBuffer = newBuffer

	emptyStats := qft.circularBuffer.GetStats()
	fmt.Printf("📊 초기화 후: %d개 거래 (메모리 초기화 확인)\n", emptyStats.TotalEvents)

	// 백업에서 복원
	fmt.Printf("📥 백업에서 복원...\n")
	if err := qft.circularBuffer.LoadFromBackup(); err != nil {
		return fmt.Errorf("백업 복원 실패: %v", err)
	}

	// 복원된 통계 확인
	restoredStats := qft.circularBuffer.GetStats()
	fmt.Printf("📊 복원 후: %d개 거래, %.1fMB\n",
		restoredStats.TotalEvents, float64(restoredStats.MemoryUsage)/1024/1024)

	// 데이터 일치성 검증
	if restoredStats.TotalEvents != originalStats.TotalEvents {
		return fmt.Errorf("백업/복원 데이터 불일치: 원본 %d vs 복원 %d",
			originalStats.TotalEvents, restoredStats.TotalEvents)
	}

	fmt.Printf("✅ 백업/복원 테스트 완료: 데이터 일치성 확인됨\n")
	return nil
}

// filterTradesBySymbol: 심볼별 거래 필터링
func (qft *QuickFunctionalityTester) filterTradesBySymbol(trades []models.TradeEvent, symbol string) []models.TradeEvent {
	var filtered []models.TradeEvent
	for _, trade := range trades {
		if qft.isTargetSymbol(trade.Symbol, symbol) {
			filtered = append(filtered, trade)
		}
	}
	return filtered
}

// isTargetSymbol: 심볼 매칭
func (qft *QuickFunctionalityTester) isTargetSymbol(tradeSymbol, targetSymbol string) bool {
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
func (qft *QuickFunctionalityTester) saveDataToFile(trades []models.TradeEvent, filename string) error {
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

// Close: 리소스 정리
func (qft *QuickFunctionalityTester) Close() {
	fmt.Printf("🧹 리소스 정리 중...\n")

	if qft.taskManager != nil {
		qft.taskManager.Stop()
	}

	if qft.circularBuffer != nil {
		qft.circularBuffer.Close()
	}

	if qft.cancel != nil {
		qft.cancel()
	}

	fmt.Printf("✅ 리소스 정리 완료\n")
}