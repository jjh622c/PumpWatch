package sync

import (
	"fmt"
	"log"
	"path/filepath"
	"time"

	"PumpWatch/internal/config"
	"PumpWatch/internal/models"
)

// SymbolCoordinator는 심볼 관리의 중앙 조정자
// YAML 설정과 심볼 필터링을 통합 관리
type SymbolCoordinator struct {
	yamlManager    *config.YAMLConfigManager
	filterService  *SymbolFilterService
	updateTimer    *time.Ticker
	configPath     string
	isRunning      bool
	
	// 콜백 함수들
	onUpdate       func(*models.SymbolsConfig)     // 설정 업데이트 콜백
	onError        func(error)                     // 에러 발생 콜백
	onSyncComplete func(models.SymbolConfigStats)  // 동기화 완료 콜백
}

// NewSymbolCoordinator는 새로운 Symbol Coordinator 생성
func NewSymbolCoordinator(configPath string) *SymbolCoordinator {
	// 절대 경로로 변환
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		absConfigPath = configPath
	}

	yamlManager := config.NewYAMLConfigManager(absConfigPath)
	
	return &SymbolCoordinator{
		yamlManager:   yamlManager,
		configPath:    absConfigPath,
		isRunning:     false,
	}
}

// Initialize는 시스템 초기화
func (sc *SymbolCoordinator) Initialize() error {
	log.Printf("🚀 Symbol Coordinator 초기화 시작...")

	// 1. YAML 설정 로드
	if err := sc.yamlManager.LoadConfig(); err != nil {
		log.Printf("⚠️ YAML 설정 로드 실패, 기본 설정 사용: %v", err)
		
		// 기본 설정으로 초기화 후 저장
		config := models.NewSymbolsConfig()
		config.InitializeWithDefaults()
		
		if err := sc.yamlManager.UpdateConfig(config); err != nil {
			return fmt.Errorf("기본 설정 저장 실패: %v", err)
		}
	}

	// 2. 심볼 필터링 서비스 초기화
	sc.filterService = NewSymbolFilterService(sc.yamlManager.GetConfig())

	// 3. 자동 저장 시작
	sc.yamlManager.StartAutoSave()

	log.Printf("✅ Symbol Coordinator 초기화 완료")
	return nil
}

// Start는 자동 동기화 시작 (24시간 간격)
func (sc *SymbolCoordinator) Start() error {
	if sc.isRunning {
		return fmt.Errorf("이미 실행 중입니다")
	}

	log.Printf("🔄 Symbol Coordinator 시작 - 24시간 간격 자동 동기화")

	// 초기 동기화 실행
	if err := sc.SyncNow(); err != nil {
		log.Printf("⚠️ 초기 동기화 실패: %v", err)
		if sc.onError != nil {
			sc.onError(err)
		}
	}

	// 24시간 간격 타이머 시작
	sc.updateTimer = time.NewTicker(24 * time.Hour)
	sc.isRunning = true

	go sc.autoSyncLoop()

	log.Printf("✅ Symbol Coordinator 시작 완료")
	return nil
}

// Stop은 자동 동기화 중단
func (sc *SymbolCoordinator) Stop() error {
	if !sc.isRunning {
		return nil
	}

	log.Printf("🛑 Symbol Coordinator 중단 중...")

	if sc.updateTimer != nil {
		sc.updateTimer.Stop()
		sc.updateTimer = nil
	}

	sc.yamlManager.StopAutoSave()
	sc.isRunning = false

	log.Printf("✅ Symbol Coordinator 중단 완료")
	return nil
}

// SyncNow는 즉시 동기화 실행
func (sc *SymbolCoordinator) SyncNow() error {
	log.Printf("🔄 심볼 동기화 시작...")
	startTime := time.Now()

	// 1. 외부 파일 변경 확인 및 재로드
	if err := sc.yamlManager.ReloadIfChanged(); err != nil {
		log.Printf("⚠️ YAML 재로드 실패: %v", err)
	}

	// 2. 심볼 필터링 서비스 업데이트 (최신 설정 적용)
	sc.filterService = NewSymbolFilterService(sc.yamlManager.GetConfig())

	// 3. 전체 심볼 동기화
	if err := sc.filterService.SyncAllSymbols(); err != nil {
		return fmt.Errorf("심볼 동기화 실패: %v", err)
	}

	// 4. 업데이트된 설정 저장
	if err := sc.yamlManager.UpdateConfig(sc.filterService.GetConfig()); err != nil {
		return fmt.Errorf("설정 저장 실패: %v", err)
	}

	syncDuration := time.Since(startTime)
	stats := sc.filterService.GetStats()
	
	log.Printf("✅ 심볼 동기화 완료 (소요시간: %.2f초)", syncDuration.Seconds())
	log.Printf("📊 동기화 결과 - 거래소: %d개, 구독: %d개, 업비트 KRW: %d개",
		stats.TotalExchanges, stats.TotalSubscriptions, stats.TotalUpbitKRWSymbols)

	// 5. 콜백 호출
	if sc.onUpdate != nil {
		sc.onUpdate(sc.filterService.GetConfig())
	}
	if sc.onSyncComplete != nil {
		sc.onSyncComplete(stats)
	}

	return nil
}

// autoSyncLoop는 자동 동기화 루프
func (sc *SymbolCoordinator) autoSyncLoop() {
	for range sc.updateTimer.C {
		log.Printf("⏰ 자동 동기화 트리거됨")
		
		if err := sc.SyncNow(); err != nil {
			log.Printf("❌ 자동 동기화 실패: %v", err)
			if sc.onError != nil {
				sc.onError(err)
			}
		}
	}
}

// GetConfig는 현재 설정 반환
func (sc *SymbolCoordinator) GetConfig() *models.SymbolsConfig {
	return sc.yamlManager.GetConfig()
}

// GetFilteredSymbols는 필터링된 심볼 목록 반환
func (sc *SymbolCoordinator) GetFilteredSymbols(exchange, marketType string) []string {
	if sc.filterService == nil {
		return []string{}
	}
	return sc.filterService.GetFilteredSymbols(exchange, marketType)
}

// GetStats는 통계 정보 반환
func (sc *SymbolCoordinator) GetStats() models.SymbolConfigStats {
	if sc.filterService == nil {
		return models.SymbolConfigStats{}
	}
	return sc.filterService.GetStats()
}

// GetConfigSummary는 설정 요약 반환
func (sc *SymbolCoordinator) GetConfigSummary() config.ConfigSummary {
	return sc.yamlManager.GetConfigSummary()
}

// BackupConfig는 현재 설정 백업
func (sc *SymbolCoordinator) BackupConfig() (string, error) {
	return sc.yamlManager.BackupConfig()
}

// RestoreFromBackup는 백업에서 설정 복원
func (sc *SymbolCoordinator) RestoreFromBackup(backupPath string) error {
	if err := sc.yamlManager.RestoreFromBackup(backupPath); err != nil {
		return err
	}

	// 복원 후 필터 서비스 업데이트
	sc.filterService = NewSymbolFilterService(sc.yamlManager.GetConfig())
	
	// 콜백 호출
	if sc.onUpdate != nil {
		sc.onUpdate(sc.filterService.GetConfig())
	}

	return nil
}

// ExportConfig는 설정 내보내기
func (sc *SymbolCoordinator) ExportConfig(exportPath string) error {
	return sc.yamlManager.ExportConfig(exportPath)
}

// ImportConfig는 설정 가져오기
func (sc *SymbolCoordinator) ImportConfig(importPath string) error {
	if err := sc.yamlManager.ImportConfig(importPath); err != nil {
		return err
	}

	// 가져오기 후 필터 서비스 업데이트
	sc.filterService = NewSymbolFilterService(sc.yamlManager.GetConfig())
	
	// 콜백 호출
	if sc.onUpdate != nil {
		sc.onUpdate(sc.filterService.GetConfig())
	}

	return nil
}

// SetCallbacks는 콜백 함수들 설정
func (sc *SymbolCoordinator) SetCallbacks(
	onUpdate func(*models.SymbolsConfig),
	onError func(error),
	onSyncComplete func(models.SymbolConfigStats),
) {
	sc.onUpdate = onUpdate
	sc.onError = onError
	sc.onSyncComplete = onSyncComplete
}

// IsRunning은 실행 상태 확인
func (sc *SymbolCoordinator) IsRunning() bool {
	return sc.isRunning
}

// GetConfigPath는 설정 파일 경로 반환
func (sc *SymbolCoordinator) GetConfigPath() string {
	return sc.configPath
}

// NeedsUpdate는 특정 거래소가 업데이트가 필요한지 확인
func (sc *SymbolCoordinator) NeedsUpdate(exchange string) bool {
	if sc.filterService == nil {
		return true
	}
	return sc.filterService.NeedsUpdate(exchange)
}

// GetUpbitKRWSymbols는 업비트 KRW 심볼 목록 반환
func (sc *SymbolCoordinator) GetUpbitKRWSymbols() []string {
	return sc.yamlManager.GetConfig().UpbitKRWSymbols
}

// GetExchangeSymbols는 특정 거래소의 심볼 목록 반환
func (sc *SymbolCoordinator) GetExchangeSymbols(exchange string) (spot, futures []string) {
	config := sc.yamlManager.GetConfig()
	exchangeConfig, exists := config.GetExchangeConfig(exchange)
	if !exists {
		return []string{}, []string{}
	}
	
	return exchangeConfig.SpotSymbols, exchangeConfig.FuturesSymbols
}

// GetAllExchanges는 지원하는 모든 거래소 목록 반환
func (sc *SymbolCoordinator) GetAllExchanges() []string {
	return sc.yamlManager.GetConfig().GetAllExchanges()
}

// ValidateConfig는 현재 설정 유효성 검증
func (sc *SymbolCoordinator) ValidateConfig() error {
	return sc.yamlManager.GetConfig().Validate()
}

// GetHealthStatus는 시스템 상태 정보 반환
func (sc *SymbolCoordinator) GetHealthStatus() CoordinatorHealthStatus {
	stats := sc.GetStats()
	summary := sc.GetConfigSummary()
	
	return CoordinatorHealthStatus{
		IsRunning:            sc.isRunning,
		LastUpdate:           summary.LastUpdated,
		ConfigPath:           sc.configPath,
		TotalExchanges:       stats.TotalExchanges,
		TotalSubscriptions:   stats.TotalSubscriptions,
		ConfigFileSize:       summary.FileSize,
		HasActiveTimer:       sc.updateTimer != nil,
		NextSyncTime:         sc.getNextSyncTime(),
	}
}

// getNextSyncTime은 다음 동기화 시간 계산
func (sc *SymbolCoordinator) getNextSyncTime() time.Time {
	if !sc.isRunning || sc.updateTimer == nil {
		return time.Time{}
	}
	
	// 마지막 업데이트 시간 + 24시간
	return sc.GetConfigSummary().LastUpdated.Add(24 * time.Hour)
}

// CoordinatorHealthStatus는 Coordinator 상태 정보
type CoordinatorHealthStatus struct {
	IsRunning          bool      `json:"is_running"`
	LastUpdate         time.Time `json:"last_update"`
	ConfigPath         string    `json:"config_path"`
	TotalExchanges     int       `json:"total_exchanges"`
	TotalSubscriptions int       `json:"total_subscriptions"`
	ConfigFileSize     int64     `json:"config_file_size"`
	HasActiveTimer     bool      `json:"has_active_timer"`
	NextSyncTime       time.Time `json:"next_sync_time"`
}