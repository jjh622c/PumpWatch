package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
	"PumpWatch/internal/models"
)

// YAMLConfigManager는 YAML 기반 설정 관리자
type YAMLConfigManager struct {
	configPath     string                    // YAML 파일 경로
	config         *models.SymbolsConfig     // 현재 설정
	autoSaveTimer  *time.Ticker             // 자동 저장 타이머
	lastModified   time.Time                // 파일 마지막 수정 시간
}

// NewYAMLConfigManager는 새로운 YAML Config Manager 생성
func NewYAMLConfigManager(configPath string) *YAMLConfigManager {
	return &YAMLConfigManager{
		configPath: configPath,
		config:     models.NewSymbolsConfig(),
	}
}

// LoadConfig는 YAML 파일에서 설정 로드
func (ycm *YAMLConfigManager) LoadConfig() error {
	// 파일 존재 확인
	if _, err := os.Stat(ycm.configPath); os.IsNotExist(err) {
		// 파일이 없으면 기본 설정으로 생성
		ycm.config.InitializeWithDefaults()
		return ycm.SaveConfig()
	}

	// 파일 정보 확인
	fileInfo, err := os.Stat(ycm.configPath)
	if err != nil {
		return fmt.Errorf("파일 정보 확인 실패: %v", err)
	}
	ycm.lastModified = fileInfo.ModTime()

	// YAML 파일 읽기
	data, err := os.ReadFile(ycm.configPath)
	if err != nil {
		return fmt.Errorf("파일 읽기 실패: %v", err)
	}

	// YAML 파싱
	config := models.NewSymbolsConfig()
	if err := yaml.Unmarshal(data, config); err != nil {
		return fmt.Errorf("YAML 파싱 실패: %v", err)
	}

	// 설정 유효성 검증
	if err := config.Validate(); err != nil {
		return fmt.Errorf("설정 유효성 검증 실패: %v", err)
	}

	ycm.config = config
	fmt.Printf("📂 YAML 설정 로드 완료: %s (version %s)\n", 
		ycm.configPath, config.Version)

	return nil
}

// SaveConfig는 설정을 YAML 파일로 저장
func (ycm *YAMLConfigManager) SaveConfig() error {
	// 디렉터리 생성
	dir := filepath.Dir(ycm.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("디렉터리 생성 실패: %v", err)
	}

	// 업데이트 시간 설정
	ycm.config.UpdatedAt = time.Now()

	// YAML 마샬링
	data, err := yaml.Marshal(ycm.config)
	if err != nil {
		return fmt.Errorf("YAML 마샬링 실패: %v", err)
	}

	// 임시 파일에 쓰기 (원자적 업데이트)
	tempPath := ycm.configPath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("임시 파일 쓰기 실패: %v", err)
	}

	// 원본 파일로 이동
	if err := os.Rename(tempPath, ycm.configPath); err != nil {
		os.Remove(tempPath) // 임시 파일 정리
		return fmt.Errorf("파일 이동 실패: %v", err)
	}

	// 수정 시간 업데이트
	if fileInfo, err := os.Stat(ycm.configPath); err == nil {
		ycm.lastModified = fileInfo.ModTime()
	}

	fmt.Printf("💾 YAML 설정 저장 완료: %s\n", ycm.configPath)
	return nil
}

// GetConfig는 현재 설정 반환
func (ycm *YAMLConfigManager) GetConfig() *models.SymbolsConfig {
	return ycm.config
}

// UpdateConfig는 설정 업데이트 및 저장
func (ycm *YAMLConfigManager) UpdateConfig(newConfig *models.SymbolsConfig) error {
	// 설정 유효성 검증
	if err := newConfig.Validate(); err != nil {
		return fmt.Errorf("설정 유효성 검증 실패: %v", err)
	}

	ycm.config = newConfig
	return ycm.SaveConfig()
}

// StartAutoSave는 자동 저장 시작 (5분 간격)
func (ycm *YAMLConfigManager) StartAutoSave() {
	if ycm.autoSaveTimer != nil {
		ycm.autoSaveTimer.Stop()
	}

	ycm.autoSaveTimer = time.NewTicker(5 * time.Minute)
	go func() {
		for range ycm.autoSaveTimer.C {
			if err := ycm.SaveConfig(); err != nil {
				fmt.Printf("❌ 자동 저장 실패: %v\n", err)
			}
		}
	}()

	fmt.Printf("⏰ 자동 저장 시작: 5분 간격\n")
}

// StopAutoSave는 자동 저장 중단
func (ycm *YAMLConfigManager) StopAutoSave() {
	if ycm.autoSaveTimer != nil {
		ycm.autoSaveTimer.Stop()
		ycm.autoSaveTimer = nil
		fmt.Printf("⏰ 자동 저장 중단\n")
	}
}

// CheckFileChange는 파일 변경 확인
func (ycm *YAMLConfigManager) CheckFileChange() (bool, error) {
	fileInfo, err := os.Stat(ycm.configPath)
	if err != nil {
		return false, fmt.Errorf("파일 정보 확인 실패: %v", err)
	}

	if fileInfo.ModTime().After(ycm.lastModified) {
		fmt.Printf("📝 YAML 파일 외부 변경 감지: %s\n", ycm.configPath)
		return true, nil
	}

	return false, nil
}

// ReloadIfChanged는 파일이 변경되었으면 재로드
func (ycm *YAMLConfigManager) ReloadIfChanged() error {
	changed, err := ycm.CheckFileChange()
	if err != nil {
		return err
	}

	if changed {
		fmt.Printf("🔄 YAML 설정 재로드 중...\n")
		return ycm.LoadConfig()
	}

	return nil
}

// ExportConfig는 설정을 다른 파일로 내보내기
func (ycm *YAMLConfigManager) ExportConfig(exportPath string) error {
	data, err := yaml.Marshal(ycm.config)
	if err != nil {
		return fmt.Errorf("YAML 마샬링 실패: %v", err)
	}

	dir := filepath.Dir(exportPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("디렉터리 생성 실패: %v", err)
	}

	if err := os.WriteFile(exportPath, data, 0644); err != nil {
		return fmt.Errorf("파일 쓰기 실패: %v", err)
	}

	fmt.Printf("📤 설정 내보내기 완료: %s\n", exportPath)
	return nil
}

// ImportConfig는 다른 파일에서 설정 가져오기
func (ycm *YAMLConfigManager) ImportConfig(importPath string) error {
	data, err := os.ReadFile(importPath)
	if err != nil {
		return fmt.Errorf("파일 읽기 실패: %v", err)
	}

	config := models.NewSymbolsConfig()
	if err := yaml.Unmarshal(data, config); err != nil {
		return fmt.Errorf("YAML 파싱 실패: %v", err)
	}

	if err := config.Validate(); err != nil {
		return fmt.Errorf("설정 유효성 검증 실패: %v", err)
	}

	ycm.config = config
	fmt.Printf("📥 설정 가져오기 완료: %s\n", importPath)
	
	return ycm.SaveConfig()
}

// BackupConfig는 현재 설정 백업
func (ycm *YAMLConfigManager) BackupConfig() (string, error) {
	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("%s.backup_%s", ycm.configPath, timestamp)
	
	if err := ycm.ExportConfig(backupPath); err != nil {
		return "", fmt.Errorf("백업 생성 실패: %v", err)
	}

	fmt.Printf("💾 설정 백업 생성: %s\n", backupPath)
	return backupPath, nil
}

// RestoreFromBackup는 백업에서 설정 복원
func (ycm *YAMLConfigManager) RestoreFromBackup(backupPath string) error {
	// 현재 설정 백업
	currentBackup, err := ycm.BackupConfig()
	if err != nil {
		fmt.Printf("⚠️ 현재 설정 백업 실패: %v\n", err)
	} else {
		fmt.Printf("📦 현재 설정 백업: %s\n", currentBackup)
	}

	// 백업에서 복원
	if err := ycm.ImportConfig(backupPath); err != nil {
		return fmt.Errorf("백업 복원 실패: %v", err)
	}

	fmt.Printf("🔄 백업에서 복원 완료: %s\n", backupPath)
	return nil
}

// GetConfigSummary는 설정 요약 정보 반환
func (ycm *YAMLConfigManager) GetConfigSummary() ConfigSummary {
	stats := ycm.config.GetStats()
	
	return ConfigSummary{
		Version:                ycm.config.Version,
		LastUpdated:           ycm.config.UpdatedAt,
		FilePath:              ycm.configPath,
		FileSize:              ycm.getFileSize(),
		TotalExchanges:        stats.TotalExchanges,
		TotalUpbitKRWSymbols:  stats.TotalUpbitKRWSymbols,
		TotalSpotSymbols:      stats.TotalSpotSymbols,
		TotalFuturesSymbols:   stats.TotalFuturesSymbols,
		TotalSubscriptions:    stats.TotalSubscriptions,
	}
}

// getFileSize는 파일 크기 반환
func (ycm *YAMLConfigManager) getFileSize() int64 {
	if fileInfo, err := os.Stat(ycm.configPath); err == nil {
		return fileInfo.Size()
	}
	return 0
}

// ConfigSummary는 설정 요약 정보
type ConfigSummary struct {
	Version               string    `json:"version"`
	LastUpdated           time.Time `json:"last_updated"`
	FilePath              string    `json:"file_path"`
	FileSize              int64     `json:"file_size"`
	TotalExchanges        int       `json:"total_exchanges"`
	TotalUpbitKRWSymbols  int       `json:"total_upbit_krw_symbols"`
	TotalSpotSymbols      int       `json:"total_spot_symbols"`
	TotalFuturesSymbols   int       `json:"total_futures_symbols"`
	TotalSubscriptions    int       `json:"total_subscriptions"`
}

// WriteConfigTemplate는 설정 템플릿 생성
func WriteConfigTemplate(templatePath string) error {
	template := models.NewSymbolsConfig()
	template.InitializeWithDefaults()

	// 예시 데이터 추가
	template.UpbitKRWSymbols = []string{"BTC", "ETH", "XRP", "ADA", "DOT"}
	
	// 바이낸스 예시 심볼 추가
	if config, exists := template.Exchanges["binance"]; exists {
		config.SpotSymbols = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
		config.FuturesSymbols = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
		template.Exchanges["binance"] = config
	}
	
	// 구독 목록 생성
	template.GenerateSubscriptionLists()

	data, err := yaml.Marshal(template)
	if err != nil {
		return fmt.Errorf("YAML 마샬링 실패: %v", err)
	}

	dir := filepath.Dir(templatePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("디렉터리 생성 실패: %v", err)
	}

	if err := os.WriteFile(templatePath, data, 0644); err != nil {
		return fmt.Errorf("파일 쓰기 실패: %v", err)
	}

	fmt.Printf("📄 설정 템플릿 생성: %s\n", templatePath)
	return nil
}

// ValidateConfigFile은 YAML 파일 유효성 검증
func ValidateConfigFile(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("파일 읽기 실패: %v", err)
	}

	config := models.NewSymbolsConfig()
	if err := yaml.Unmarshal(data, config); err != nil {
		return fmt.Errorf("YAML 파싱 실패: %v", err)
	}

	if err := config.Validate(); err != nil {
		return fmt.Errorf("설정 유효성 검증 실패: %v", err)
	}

	fmt.Printf("✅ YAML 설정 유효성 검증 통과: %s\n", configPath)
	return nil
}