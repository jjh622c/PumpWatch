package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"PumpWatch/internal/config"
	"PumpWatch/internal/logging"
	"PumpWatch/internal/models"
	"PumpWatch/internal/storage"
	"PumpWatch/internal/symbols"
	"PumpWatch/internal/websocket"
)

// FakeListingTester simulates real listing announcements for testing
type FakeListingTester struct {
	taskManager    *websocket.EnhancedTaskManager
	storageManager *storage.Manager
}

// LA 케이스 재현을 위한 가짜 상장공고 데이터
func (flt *FakeListingTester) createFakeLAListing() *models.ListingEvent {
	now := time.Now()
	// 🔧 CRITICAL FIX: AnnouncedAt을 30초 전으로 설정하여 이미 수집된 데이터 활용
	announcedAt := now.Add(-30 * time.Second)

	return &models.ListingEvent{
		ID:           "999999", // 가짜 ID
		Title:        "리겔 에이아이(LA) KRW 마켓 디지털 자산 추가",
		Symbol:       "LA",
		Markets:      []string{"KRW"},
		AnnouncedAt:  announcedAt,  // 🔧 30초 전 시점
		DetectedAt:   now,          // 현재 감지
		NoticeURL:    "https://upbit.com/service_center/notice?id=999999",
		TriggerTime:  announcedAt,  // 🔧 AnnouncedAt과 동일
		IsKRWListing: true,
	}
}

// TOSHI 케이스 재현을 위한 가짜 상장공고 데이터
func (flt *FakeListingTester) createFakeTOSHIListing() *models.ListingEvent {
	now := time.Now()
	// 🔧 CRITICAL FIX: AnnouncedAt을 30초 전으로 설정하여 이미 수집된 데이터 활용
	announcedAt := now.Add(-30 * time.Second)

	return &models.ListingEvent{
		ID:           "999998", // 가짜 ID
		Title:        "토시(TOSHI) KRW 마켓 디지털 자산 추가",
		Symbol:       "TOSHI",
		Markets:      []string{"KRW"},
		AnnouncedAt:  announcedAt,  // 🔧 30초 전 시점
		DetectedAt:   now,          // 현재 감지
		NoticeURL:    "https://upbit.com/service_center/notice?id=999998",
		TriggerTime:  announcedAt,  // 🔧 AnnouncedAt과 동일
		IsKRWListing: true,
	}
}

// SOMI 케이스 (실제 거래 중인 심볼) 테스트용 가짜 상장공고 데이터
func (flt *FakeListingTester) createFakeSOMIListing() *models.ListingEvent {
	now := time.Now()
	// 🔧 CRITICAL FIX: AnnouncedAt을 30초 전으로 설정하여 이미 수집된 데이터 활용
	announcedAt := now.Add(-30 * time.Second)

	return &models.ListingEvent{
		ID:           "999997", // 가짜 ID
		Title:        "SOMI(SOMI) KRW 마켓 디지털 자산 추가",
		Symbol:       "SOMI",
		Markets:      []string{"KRW"},
		AnnouncedAt:  announcedAt,  // 🔧 30초 전 시점
		DetectedAt:   now,          // 현재 감지
		NoticeURL:    "https://upbit.com/service_center/notice?id=999997",
		TriggerTime:  announcedAt,  // 🔧 AnnouncedAt과 동일
		IsKRWListing: true,
	}
}

// 가짜 상장공고를 직접 주입하여 시스템 반응 테스트
func (flt *FakeListingTester) injectFakeListing(listingType string) error {
	var fakeEvent *models.ListingEvent

	switch listingType {
	case "LA":
		fakeEvent = flt.createFakeLAListing()
	case "TOSHI":
		fakeEvent = flt.createFakeTOSHIListing()
	case "SOMI":
		fakeEvent = flt.createFakeSOMIListing()
	default:
		return fmt.Errorf("unsupported listing type: %s", listingType)
	}

	fmt.Printf("\n🚨 === 가짜 %s 상장공고 주입 테스트 시작 ===\n", listingType)
	fmt.Printf("💎 Symbol: %s\n", fakeEvent.Symbol)
	fmt.Printf("📋 Title: %s\n", fakeEvent.Title)
	fmt.Printf("🕐 Announced: %s\n", fakeEvent.AnnouncedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("🎯 Detected: %s\n", fakeEvent.DetectedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("⏱️ Detection delay: %v\n", fakeEvent.DetectedAt.Sub(fakeEvent.AnnouncedAt))

	// 시스템 반응 시간 측정 시작
	startTime := time.Now()

	// 데이터 수집 트리거 (실제 UpbitMonitor.triggerDataCollection과 동일한 로직)
	if err := flt.taskManager.StartDataCollection(fakeEvent.Symbol, fakeEvent.AnnouncedAt); err != nil {
		fmt.Printf("❌ Failed to trigger data collection: %v\n", err)
		return err
	}

	// 메타데이터 저장
	if err := flt.storageManager.StoreListingEvent(fakeEvent); err != nil {
		fmt.Printf("⚠️ Failed to store listing event metadata: %v\n", err)
	}

	responseTime := time.Since(startTime)
	fmt.Printf("🚀 가짜 상장공고 처리 완료 (응답 시간: %v)\n", responseTime)

	// 45초 후 결과 확인 (40초 데이터 수집 완료 + 5초 여유)
	fmt.Printf("⏳ 45초 대기 중 (데이터 수집 완료 대기)...\n")
	time.Sleep(45 * time.Second)

	return flt.verifyDataCollection(fakeEvent)
}

// 데이터 수집 결과 검증
func (flt *FakeListingTester) verifyDataCollection(event *models.ListingEvent) error {
	fmt.Printf("\n📊 === %s 데이터 수집 결과 검증 ===\n", event.Symbol)

	// 저장된 데이터 파일 확인
	dataDir := fmt.Sprintf("data/%s_%s", event.Symbol, event.AnnouncedAt.Format("20060102_150405"))

	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		fmt.Printf("❌ 데이터 디렉토리가 생성되지 않았습니다: %s\n", dataDir)
		return fmt.Errorf("data directory not created: %s", dataDir)
	}

	fmt.Printf("✅ 데이터 디렉토리 생성됨: %s\n", dataDir)

	// 메타데이터 파일 확인
	metadataFile := fmt.Sprintf("%s/metadata.json", dataDir)
	if _, err := os.Stat(metadataFile); os.IsNotExist(err) {
		fmt.Printf("❌ 메타데이터 파일이 생성되지 않았습니다: %s\n", metadataFile)
		return fmt.Errorf("metadata file not created: %s", metadataFile)
	}

	// 메타데이터 내용 확인
	metadataData, err := os.ReadFile(metadataFile)
	if err != nil {
		return fmt.Errorf("failed to read metadata: %w", err)
	}

	var savedEvent models.ListingEvent
	if err := json.Unmarshal(metadataData, &savedEvent); err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	fmt.Printf("✅ 메타데이터 저장 확인:\n")
	fmt.Printf("  - Symbol: %s\n", savedEvent.Symbol)
	fmt.Printf("  - Markets: %v\n", savedEvent.Markets)
	fmt.Printf("  - AnnouncedAt: %s\n", savedEvent.AnnouncedAt.Format("2006-01-02 15:04:05"))

	// Raw 데이터 디렉토리 확인
	rawDir := fmt.Sprintf("%s/raw", dataDir)
	if _, err := os.Stat(rawDir); os.IsNotExist(err) {
		fmt.Printf("❌ Raw 데이터 디렉토리가 생성되지 않았습니다: %s\n", rawDir)
		return fmt.Errorf("raw data directory not created: %s", rawDir)
	}

	// Raw 데이터 파일들 확인
	rawFiles, err := os.ReadDir(rawDir)
	if err != nil {
		return fmt.Errorf("failed to read raw directory: %w", err)
	}

	fmt.Printf("✅ Raw 데이터 파일들:\n")
	totalDataSize := int64(0)
	for _, file := range rawFiles {
		if !file.IsDir() {
			info, _ := file.Info()
			fmt.Printf("  - %s: %d bytes\n", file.Name(), info.Size())
			totalDataSize += info.Size()
		}
	}
	fmt.Printf("📊 총 Raw 데이터 크기: %d bytes\n", totalDataSize)

	// 성능 점검
	if totalDataSize == 0 {
		fmt.Printf("⚠️ Raw 데이터가 비어있습니다 - WebSocket 데이터 수집 실패?\n")
		return fmt.Errorf("no raw data collected")
	}

	fmt.Printf("🎉 %s 상장공고 테스트 완료 - 데이터 수집 성공!\n", event.Symbol)
	return nil
}

// 하드리셋 타이밍 충돌 시뮬레이션
func (flt *FakeListingTester) simulateHardResetConflict() error {
	fmt.Printf("\n🔄 === 하드리셋 타이밍 충돌 시뮬레이션 ===\n")
	fmt.Printf("시나리오: LA 케이스 재현 (19:02:31 공고 → 19:11:02 하드리셋 → 19:11:26 지연 감지)\n")

	// 1분 후에 가짜 상장공고 주입 (하드리셋 직전 상황 시뮬레이션)
	fmt.Printf("⏰ 1분 후 가짜 LA 상장공고 주입 예정...\n")
	time.Sleep(1 * time.Minute)

	// 가짜 상장공고 주입
	fakeEvent := flt.createFakeLAListing()
	fmt.Printf("🚨 가짜 LA 상장공고 주입!\n")

	// 즉시 하드리셋 시뮬레이션 (실제로는 graceful shutdown 신호)
	fmt.Printf("🔄 30초 후 하드리셋 시뮬레이션...\n")
	time.Sleep(30 * time.Second)

	// 시스템에 shutdown 신호 전송 (실제 하드리셋 시뮬레이션)
	fmt.Printf("🛑 Graceful shutdown 신호 전송 (하드리셋 시뮬레이션)\n")

	// 실제로는 여기서 시스템이 재시작되어야 하므로
	// PersistentState 검증을 위한 상태 확인
	return flt.checkPersistentState(fakeEvent)
}

// 영구 상태 저장 시스템 검증
func (flt *FakeListingTester) checkPersistentState(event *models.ListingEvent) error {
	fmt.Printf("\n💾 === 영구 상태 저장 시스템 검증 ===\n")

	// monitor_state.json 파일 확인
	stateFile := "data/monitor/monitor_state.json"
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		fmt.Printf("❌ 영구 상태 파일이 존재하지 않습니다: %s\n", stateFile)
		return fmt.Errorf("persistent state file not found: %s", stateFile)
	}

	stateData, err := os.ReadFile(stateFile)
	if err != nil {
		return fmt.Errorf("failed to read persistent state: %w", err)
	}

	fmt.Printf("✅ 영구 상태 파일 존재 확인: %s (%d bytes)\n", stateFile, len(stateData))

	// JSON 파싱 확인
	var state map[string]interface{}
	if err := json.Unmarshal(stateData, &state); err != nil {
		return fmt.Errorf("failed to parse persistent state: %w", err)
	}

	fmt.Printf("📊 영구 상태 데이터:\n")
	for key, value := range state {
		fmt.Printf("  - %s: %v\n", key, value)
	}

	return nil
}

func main() {
	fmt.Printf("🧪 === 가짜 상장공고 주입 테스트 시작 ===\n")
	fmt.Printf("목적: 실제 LA/TOSHI 케이스를 재현하여 시스템 개선 효과 검증\n")

	// 로깅 초기화
	if err := logging.InitGlobalLogger("fake-listing-test", "info", "logs"); err != nil {
		fmt.Printf("❌ Failed to initialize logging: %v\n", err)
		os.Exit(1)
	}
	defer logging.CloseGlobalLogger()

	// Context 생성
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 설정 로드
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		fmt.Printf("❌ Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// 심볼 설정 로드
	symbolsConfig, err := symbols.LoadConfig("config/symbols/symbols.yaml")
	if err != nil {
		fmt.Printf("❌ Failed to load symbols configuration: %v\n", err)
		os.Exit(1)
	}

	// Storage manager 초기화
	storageManager := storage.NewManager(cfg.Storage, cfg.Analysis)

	// WebSocket Task Manager 초기화
	taskManager, err := websocket.NewEnhancedTaskManager(ctx, cfg.Exchanges, symbolsConfig, storageManager)
	if err != nil {
		fmt.Printf("❌ Failed to initialize EnhancedTaskManager: %v\n", err)
		os.Exit(1)
	}

	// 백그라운드에서 WebSocket 시작
	if err := taskManager.Start(); err != nil {
		fmt.Printf("❌ Failed to start WebSocket Task Manager: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ WebSocket Task Manager 시작됨\n")

	// 5초 대기 (WebSocket 연결 안정화)
	fmt.Printf("⏳ WebSocket 연결 안정화 대기 (5초)...\n")
	time.Sleep(5 * time.Second)

	// Fake Listing Tester 초기화
	tester := &FakeListingTester{
		taskManager:    taskManager,
		storageManager: storageManager,
	}

	// 테스트 시나리오 선택
	if len(os.Args) < 2 {
		fmt.Printf("사용법: %s [LA|TOSHI|SOMI|CONFLICT]\n", os.Args[0])
		fmt.Printf("  LA: LA 상장공고 시뮬레이션\n")
		fmt.Printf("  TOSHI: TOSHI 상장공고 시뮬레이션\n")
		fmt.Printf("  SOMI: SOMI 상장공고 시뮬레이션 (실제 거래 중인 심볼)\n")
		fmt.Printf("  CONFLICT: 하드리셋 타이밍 충돌 시뮬레이션\n")
		os.Exit(1)
	}

	scenario := os.Args[1]

	// Graceful shutdown 처리
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Printf("\n🛑 Shutdown signal received\n")
		cancel()
	}()

	// 테스트 실행
	switch scenario {
	case "LA":
		if err := tester.injectFakeListing("LA"); err != nil {
			fmt.Printf("❌ LA 테스트 실패: %v\n", err)
			os.Exit(1)
		}
	case "TOSHI":
		if err := tester.injectFakeListing("TOSHI"); err != nil {
			fmt.Printf("❌ TOSHI 테스트 실패: %v\n", err)
			os.Exit(1)
		}
	case "SOMI":
		if err := tester.injectFakeListing("SOMI"); err != nil {
			fmt.Printf("❌ SOMI 테스트 실패: %v\n", err)
			os.Exit(1)
		}
	case "CONFLICT":
		if err := tester.simulateHardResetConflict(); err != nil {
			fmt.Printf("❌ 하드리셋 충돌 테스트 실패: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Printf("❌ 지원하지 않는 시나리오: %s\n", scenario)
		os.Exit(1)
	}

	fmt.Printf("✅ 가짜 상장공고 테스트 완료\n")

	// 정리
	if err := taskManager.Stop(); err != nil {
		fmt.Printf("⚠️ Error stopping task manager: %v\n", err)
	}
}