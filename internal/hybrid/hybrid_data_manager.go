package hybrid

import (
	"context"
	"fmt"
	"time"

	"PumpWatch/internal/buffer"
	"PumpWatch/internal/config"
	"PumpWatch/internal/database"
	"PumpWatch/internal/logging"
	"PumpWatch/internal/models"
	"PumpWatch/internal/storage"
)

// HybridDataManager manages dual data storage systems for safe migration
// 기존 시스템(안전망) + 새 시스템(성능) 병렬 운영으로 점진적 전환 지원
type HybridDataManager struct {
	ctx    context.Context
	cancel context.CancelFunc
	logger *logging.Logger

	// 기존 시스템 (안전망)
	circularBuffer *buffer.CircularTradeBuffer
	storageManager *storage.Manager

	// 새 시스템 (성능)
	questDBManager *database.QuestDBManager

	// 설정
	config         config.QuestDBConfig
	useQuestDB     bool    // 점진적 전환용
	compareResults bool    // 결과 비교 모드
	migrationPhase int     // 1-4: 점진적 전환 단계

	// 성능 통계
	stats HybridStats
}

// HybridStats holds performance comparison statistics
type HybridStats struct {
	// 처리량 통계
	LegacyWrites       int64 `json:"legacy_writes"`
	QuestDBWrites      int64 `json:"questdb_writes"`
	QuestDBDropped     int64 `json:"questdb_dropped"`

	// 응답시간 통계
	LegacyAvgTime      time.Duration `json:"legacy_avg_time"`
	QuestDBAvgTime     time.Duration `json:"questdb_avg_time"`

	// 비교 모드 통계
	ComparisonCount    int64 `json:"comparison_count"`
	ComparisonMismatch int64 `json:"comparison_mismatch"`

	// 마이그레이션 단계
	CurrentPhase       int   `json:"current_phase"`
	QuestDBPercentage  int   `json:"questdb_percentage"`
}

// NewHybridDataManager creates a new hybrid data management system
func NewHybridDataManager(
	ctx context.Context,
	questDBConfig config.QuestDBConfig,
	circularBuffer *buffer.CircularTradeBuffer,
	storageManager *storage.Manager,
) (*HybridDataManager, error) {

	hybridCtx, cancel := context.WithCancel(ctx)

	hdm := &HybridDataManager{
		ctx:            hybridCtx,
		cancel:         cancel,
		logger:         logging.GetGlobalLogger(),
		circularBuffer: circularBuffer,
		storageManager: storageManager,
		config:         questDBConfig,
		useQuestDB:     questDBConfig.Enabled,
		compareResults: questDBConfig.CompareResults,
		migrationPhase: questDBConfig.MigrationPhase,
		stats: HybridStats{
			CurrentPhase:      questDBConfig.MigrationPhase,
			QuestDBPercentage: getPhasePercentage(questDBConfig.MigrationPhase),
		},
	}

	// QuestDB Manager 초기화 (enabled인 경우만)
	if questDBConfig.Enabled {
		hdm.logger.Info("🔄 Initializing HybridDataManager with QuestDB enabled")

		// 문서 사양대로 QuestDBManagerConfig 생성
		questManagerConfig := database.QuestDBManagerConfig{
			Host:            questDBConfig.Host,
			Port:            questDBConfig.Port,
			Database:        questDBConfig.Database,
			User:            questDBConfig.User,
			Password:        questDBConfig.Password,
			BatchSize:       questDBConfig.BatchSize,
			FlushInterval:   questDBConfig.FlushInterval,
			BufferSize:      questDBConfig.BufferSize,
			WorkerCount:     questDBConfig.WorkerCount,
			MaxOpenConns:    questDBConfig.MaxOpenConns,
			MaxIdleConns:    questDBConfig.MaxIdleConns,
			ConnMaxLifetime: questDBConfig.ConnMaxLifetime,
			MaxRetries:      questDBConfig.MaxRetries,
			BaseDelay:       questDBConfig.BaseDelay,
			MaxDelay:        questDBConfig.MaxDelay,
			BackoffFactor:   questDBConfig.BackoffFactor,
		}

		var err error
		hdm.questDBManager, err = database.NewQuestDBManager(questManagerConfig)
		if err != nil {
			hdm.logger.Warn("⚠️ Failed to initialize QuestDB manager: %v. Running in legacy-only mode.", err)
			hdm.useQuestDB = false
		} else {
			hdm.logger.Info("✅ HybridDataManager: QuestDB manager initialized (Phase %d: %d%%)",
				hdm.migrationPhase, hdm.stats.QuestDBPercentage)
		}
	} else {
		hdm.logger.Info("ℹ️ HybridDataManager: QuestDB disabled - using legacy system only")
	}

	return hdm, nil
}

// HandleTradeEvent processes trade event using hybrid approach
func (hdm *HybridDataManager) HandleTradeEvent(exchange, marketType string, tradeEvent models.TradeEvent) {
	// Phase 5 문서 사양: 기존 시스템에 항상 저장 (안전망)
	start := time.Now()
	exchangeKey := fmt.Sprintf("%s_%s", exchange, marketType)

	if err := hdm.circularBuffer.StoreTradeEvent(exchangeKey, tradeEvent); err != nil {
		hdm.logger.Warn("⚠️ Failed to store in legacy circular buffer: %v", err)
	} else {
		hdm.stats.LegacyWrites++
	}
	legacyTime := time.Since(start)

	// QuestDB에도 저장 (점진적 전환 로직)
	if hdm.useQuestDB && hdm.questDBManager != nil && hdm.shouldUseQuestDB() {
		start = time.Now()
		if success := hdm.questDBManager.AddTrade(tradeEvent); success {
			hdm.stats.QuestDBWrites++
			questdbTime := time.Since(start)

			// 성능 통계 업데이트 (단순 이동 평균)
			hdm.stats.LegacyAvgTime = (hdm.stats.LegacyAvgTime + legacyTime) / 2
			hdm.stats.QuestDBAvgTime = (hdm.stats.QuestDBAvgTime + questdbTime) / 2
		} else {
			hdm.stats.QuestDBDropped++
		}
	}
}

// shouldUseQuestDB determines whether to use QuestDB based on migration phase
func (hdm *HybridDataManager) shouldUseQuestDB() bool {
	if !hdm.useQuestDB {
		return false
	}

	// Phase 5 문서 사양: 점진적 전환 로직
	switch hdm.migrationPhase {
	case 1: // 10% QuestDB 사용
		return (time.Now().UnixNano() % 10) == 0
	case 2: // 50% QuestDB 사용
		return (time.Now().UnixNano() % 2) == 0
	case 3, 4: // 100% QuestDB 사용
		return true
	default:
		return false
	}
}

// GetListingData retrieves listing data using hybrid approach
func (hdm *HybridDataManager) GetListingData(symbol string, triggerTime time.Time) (*models.ListingData, error) {
	startTime := triggerTime.Add(-20 * time.Second)
	endTime := triggerTime.Add(20 * time.Second)

	if hdm.compareResults && hdm.questDBManager != nil {
		// 비교 모드: 양쪽 결과 비교 후 레거시 결과 반환
		return hdm.compareAndReturnLegacy(symbol, startTime, endTime)
	}

	if hdm.useQuestDB && hdm.questDBManager != nil && hdm.migrationPhase >= 3 {
		// Phase 3-4: QuestDB 우선 사용
		return hdm.getFromQuestDB(symbol, startTime, endTime)
	}

	// 기본: 레거시 시스템 사용
	return hdm.getFromCircularBuffer(symbol, startTime, endTime)
}

// compareAndReturnLegacy compares both systems and returns legacy result
func (hdm *HybridDataManager) compareAndReturnLegacy(symbol string, startTime, endTime time.Time) (*models.ListingData, error) {
	// 양쪽에서 동시에 데이터 조회
	legacyResult, legacyErr := hdm.getFromCircularBuffer(symbol, startTime, endTime)
	questdbResult, questdbErr := hdm.getFromQuestDB(symbol, startTime, endTime)

	hdm.stats.ComparisonCount++

	// 결과 비교 (간단한 거래 수 비교)
	if legacyErr == nil && questdbErr == nil {
		legacyCount := hdm.countTotalTrades(legacyResult)
		questdbCount := hdm.countTotalTrades(questdbResult)

		if abs(legacyCount-questdbCount) > 10 { // 10개 이상 차이 시 불일치로 간주
			hdm.stats.ComparisonMismatch++
			hdm.logger.Warn("📊 Data mismatch detected: Legacy=%d, QuestDB=%d trades",
				legacyCount, questdbCount)
		} else {
			hdm.logger.Info("📊 Data consistency verified: Legacy=%d, QuestDB=%d trades",
				legacyCount, questdbCount)
		}
	}

	// 안전을 위해 항상 레거시 결과 반환
	return legacyResult, legacyErr
}

// getFromCircularBuffer retrieves data from legacy circular buffer
func (hdm *HybridDataManager) getFromCircularBuffer(symbol string, startTime, endTime time.Time) (*models.ListingData, error) {
	listingData := &models.ListingData{
		Symbol:      symbol,
		TriggerTime: endTime.Add(-20 * time.Second), // 원래 트리거 시간
		StartTime:   startTime,
		EndTime:     endTime,
		Trades:      make(map[string][]models.TradeEvent),
	}

	// 모든 거래소에서 데이터 수집 (기존 로직과 동일)
	exchangeKeys := []string{
		"binance_spot", "binance_futures",
		"bybit_spot", "bybit_futures",
		"okx_spot", "okx_futures",
		"kucoin_spot", "kucoin_futures",
		"gate_spot", "gate_futures",
		"phemex_spot", "phemex_futures",
	}

	for _, exchangeKey := range exchangeKeys {
		trades, err := hdm.circularBuffer.GetTradeEvents(exchangeKey, startTime, endTime)
		if err != nil {
			hdm.logger.Warn("⚠️ Failed to get trades from %s: %v", exchangeKey, err)
			continue
		}

		// 심볼 필터링
		filteredTrades := hdm.filterTradesBySymbol(trades, symbol)
		if len(filteredTrades) > 0 {
			listingData.Trades[exchangeKey] = filteredTrades
		}
	}

	return listingData, nil
}

// getFromQuestDB retrieves data from QuestDB (placeholder - actual implementation needed)
func (hdm *HybridDataManager) getFromQuestDB(symbol string, startTime, endTime time.Time) (*models.ListingData, error) {
	// TODO: Phase 4에서 QuestDB 조회 로직 구현
	// 지금은 기본 구조만 제공
	listingData := &models.ListingData{
		Symbol:      symbol,
		TriggerTime: endTime.Add(-20 * time.Second),
		StartTime:   startTime,
		EndTime:     endTime,
		Trades:      make(map[string][]models.TradeEvent),
	}

	hdm.logger.Info("🚧 QuestDB query not implemented yet - returning empty result")
	return listingData, nil
}

// Utility functions
func (hdm *HybridDataManager) filterTradesBySymbol(trades []models.TradeEvent, targetSymbol string) []models.TradeEvent {
	var filtered []models.TradeEvent
	for _, trade := range trades {
		if hdm.isTargetSymbol(trade.Symbol, targetSymbol) {
			filtered = append(filtered, trade)
		}
	}
	return filtered
}

func (hdm *HybridDataManager) isTargetSymbol(tradeSymbol, targetSymbol string) bool {
	// 기존 CollectionEvent.isTargetSymbol과 동일한 로직
	// 간단화를 위해 기본 매칭만 구현
	return tradeSymbol == targetSymbol ||
		   tradeSymbol == targetSymbol+"USDT" ||
		   tradeSymbol == targetSymbol+"-USDT"
}

func (hdm *HybridDataManager) countTotalTrades(data *models.ListingData) int {
	if data == nil {
		return 0
	}
	count := 0
	for _, trades := range data.Trades {
		count += len(trades)
	}
	return count
}

func getPhasePercentage(phase int) int {
	switch phase {
	case 1:
		return 10
	case 2:
		return 50
	case 3, 4:
		return 100
	default:
		return 0
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// GetStats returns current hybrid manager statistics
func (hdm *HybridDataManager) GetStats() HybridStats {
	return hdm.stats
}

// Close gracefully shuts down hybrid data manager
func (hdm *HybridDataManager) Close() error {
	hdm.logger.Info("🔌 Shutting down HybridDataManager...")

	// QuestDB Manager 정리
	if hdm.questDBManager != nil {
		if err := hdm.questDBManager.Close(); err != nil {
			hdm.logger.Error("❌ Failed to close QuestDB Manager: %v", err)
		}
	}

	// Context 취소
	hdm.cancel()

	// 최종 통계 출력
	hdm.logger.Info("📊 HybridDataManager Final Stats: Legacy=%d, QuestDB=%d (dropped=%d), Comparisons=%d (mismatches=%d)",
		hdm.stats.LegacyWrites, hdm.stats.QuestDBWrites, hdm.stats.QuestDBDropped,
		hdm.stats.ComparisonCount, hdm.stats.ComparisonMismatch)

	hdm.logger.Info("✅ HybridDataManager gracefully stopped")
	return nil
}