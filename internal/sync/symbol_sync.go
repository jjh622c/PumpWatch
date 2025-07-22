package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"noticepumpcatch/internal/latency"
	"noticepumpcatch/internal/logger"
	"noticepumpcatch/internal/memory"
	"noticepumpcatch/internal/websocket"
)

// BinanceExchangeInfo 바이낸스 거래소 정보 응답
type BinanceExchangeInfo struct {
	Symbols []BinanceSymbol `json:"symbols"`
}

// BinanceSymbol 바이낸스 심볼 정보
type BinanceSymbol struct {
	Symbol               string `json:"symbol"`
	Status               string `json:"status"`
	BaseAsset            string `json:"baseAsset"`
	QuoteAsset           string `json:"quoteAsset"`
	QuotePrecision       int    `json:"quotePrecision"`
	BaseAssetPrecision   int    `json:"baseAssetPrecision"`
	IsSpotTradingAllowed bool   `json:"isSpotTradingAllowed"`
}

// SymbolSyncManager 심볼 동기화 관리자
type SymbolSyncManager struct {
	config         *Config
	logger         *logger.Logger
	memManager     *memory.Manager
	websocket      *websocket.BinanceWebSocket
	latencyMonitor *latency.LatencyMonitor
	upbitAPI       *UpbitAPI // NEW: 업비트 API 클라이언트

	mu                sync.RWMutex
	currentSymbols    map[string]bool
	upbitSymbols      map[string]bool // NEW: 업비트 KRW 심볼 캐시
	lastSyncTime      time.Time
	lastUpbitSync     time.Time // NEW: 업비트 마지막 동기화 시간
	syncInterval      time.Duration
	upbitSyncInterval time.Duration // NEW: 업비트 동기화 간격
	isRunning         bool
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
}

// Config 심볼 동기화 설정
type Config struct {
	AutoSyncSymbols     bool
	SyncIntervalMinutes int
	SyncEnabled         bool
	BaseURL             string
	EnableUpbitFilter   bool // NEW: 업비트 필터링 활성화 여부
	UpbitSyncMinutes    int  // NEW: 업비트 동기화 간격 (분)
}

// NewSymbolSyncManager 새 심볼 동기화 관리자 생성
func NewSymbolSyncManager(
	config *Config,
	logger *logger.Logger,
	memManager *memory.Manager,
	websocket *websocket.BinanceWebSocket,
	latencyMonitor *latency.LatencyMonitor,
) *SymbolSyncManager {
	ctx, cancel := context.WithCancel(context.Background())

	// 기본값 설정
	if config.UpbitSyncMinutes == 0 {
		config.UpbitSyncMinutes = 30 // 기본 30분마다 업비트 동기화
	}

	return &SymbolSyncManager{
		config:            config,
		logger:            logger,
		memManager:        memManager,
		websocket:         websocket,
		latencyMonitor:    latencyMonitor,
		upbitAPI:          NewUpbitAPI(), // NEW: 업비트 API 클라이언트 초기화
		currentSymbols:    make(map[string]bool),
		upbitSymbols:      make(map[string]bool), // NEW: 업비트 심볼 캐시 초기화
		syncInterval:      time.Duration(config.SyncIntervalMinutes) * time.Minute,
		upbitSyncInterval: time.Duration(config.UpbitSyncMinutes) * time.Minute, // NEW
		ctx:               ctx,
		cancel:            cancel,
	}
}

// Start 심볼 동기화 시작
func (ssm *SymbolSyncManager) Start() error {
	if !ssm.config.SyncEnabled {
		ssm.logger.LogInfo("심볼 동기화가 비활성화되어 있습니다")
		return nil
	}

	ssm.mu.Lock()
	if ssm.isRunning {
		ssm.mu.Unlock()
		return fmt.Errorf("심볼 동기화가 이미 실행 중입니다")
	}
	ssm.isRunning = true
	ssm.mu.Unlock()

	ssm.logger.LogInfo("심볼 동기화 시작 (바이낸스: %v분, 업비트: %v분)",
		ssm.config.SyncIntervalMinutes, ssm.config.UpbitSyncMinutes)

	// 초기 업비트 동기화 (필터링 활성화시)
	if ssm.config.EnableUpbitFilter {
		if err := ssm.syncUpbitSymbols(); err != nil {
			ssm.logger.LogError("초기 업비트 동기화 실패: %v", err)
		}
	}

	// 초기 바이낸스 동기화 수행
	if err := ssm.syncSymbols(); err != nil {
		ssm.logger.LogError("초기 심볼 동기화 실패: %v", err)
	}

	// 주기적 동기화 시작
	ssm.wg.Add(1)
	go ssm.syncLoop()

	return nil
}

// Stop 심볼 동기화 중지
func (ssm *SymbolSyncManager) Stop() error {
	ssm.mu.Lock()
	if !ssm.isRunning {
		ssm.mu.Unlock()
		return nil
	}
	ssm.isRunning = false
	ssm.mu.Unlock()

	ssm.logger.LogInfo("심볼 동기화 중지 요청")
	ssm.cancel()
	ssm.wg.Wait()
	ssm.logger.LogInfo("심볼 동기화 중지 완료")

	return nil
}

// syncLoop 주기적 동기화 루프
func (ssm *SymbolSyncManager) syncLoop() {
	defer ssm.wg.Done()

	binanceTicker := time.NewTicker(ssm.syncInterval)
	defer binanceTicker.Stop()

	// 업비트 동기화 타이머 (필터링 활성화시에만)
	var upbitTicker *time.Ticker
	if ssm.config.EnableUpbitFilter {
		upbitTicker = time.NewTicker(ssm.upbitSyncInterval)
		defer upbitTicker.Stop()
	}

	for {
		select {
		case <-ssm.ctx.Done():
			return
		case <-binanceTicker.C:
			if err := ssm.syncSymbols(); err != nil {
				ssm.logger.LogError("바이낸스 심볼 동기화 실패: %v", err)
			}
		case <-upbitTicker.C:
			if ssm.config.EnableUpbitFilter {
				if err := ssm.syncUpbitSymbols(); err != nil {
					ssm.logger.LogError("업비트 심볼 동기화 실패: %v", err)
				}
			}
		}
	}
}

// syncUpbitSymbols 업비트 심볼 동기화
func (ssm *SymbolSyncManager) syncUpbitSymbols() error {
	ssm.logger.LogInfo("업비트 KRW 심볼 목록 동기화 시작")

	// 업비트 API에서 KRW 마켓 목록 가져오기
	krwSymbols, err := ssm.upbitAPI.GetKRWSymbols()
	if err != nil {
		return fmt.Errorf("업비트 KRW 마켓 조회 실패: %v", err)
	}

	ssm.logger.LogInfo("업비트에서 %d개의 KRW 마켓 발견", len(krwSymbols))

	// 업비트 심볼 캐시 업데이트
	ssm.mu.Lock()
	ssm.upbitSymbols = make(map[string]bool)
	for _, symbol := range krwSymbols {
		ssm.upbitSymbols[symbol] = true
	}
	ssm.lastUpbitSync = time.Now()
	ssm.mu.Unlock()

	ssm.logger.LogInfo("업비트 심볼 캐시 업데이트 완료")
	return nil
}

// syncSymbols 바이낸스에서 심볼 목록 동기화
func (ssm *SymbolSyncManager) syncSymbols() error {
	ssm.logger.LogInfo("바이낸스 심볼 목록 동기화 시작")

	// 바이낸스 API에서 거래소 정보 가져오기
	exchangeInfo, err := ssm.fetchExchangeInfo()
	if err != nil {
		return fmt.Errorf("거래소 정보 조회 실패: %v", err)
	}

	// 활성화된 USDT 페어만 필터링
	activeSymbols := ssm.filterActiveUSDTPairs(exchangeInfo.Symbols)

	// 업비트 필터링 적용 (활성화시)
	if ssm.config.EnableUpbitFilter {
		activeSymbols = ssm.filterByUpbitExclusion(activeSymbols)
	}

	ssm.logger.LogInfo("필터링 후 %d개의 심볼 선택", len(activeSymbols))

	// 현재 심볼과 비교하여 변경사항 확인
	addedSymbols, removedSymbols := ssm.compareSymbols(activeSymbols)

	// 변경사항이 있으면 처리
	if len(addedSymbols) > 0 || len(removedSymbols) > 0 {
		ssm.logger.LogInfo("심볼 변경 감지: 추가 %d개, 제거 %d개", len(addedSymbols), len(removedSymbols))

		// 새 심볼 추가
		if len(addedSymbols) > 0 {
			ssm.addSymbols(addedSymbols)
		}

		// 제거된 심볼 처리
		if len(removedSymbols) > 0 {
			ssm.removeSymbols(removedSymbols)
		}

		// WebSocket 재연결 대신 로그만 출력 (main.go에서 처리)
		ssm.logger.LogInfo("심볼 변경 완료: WebSocket 재연결은 main에서 처리됩니다")
	} else {
		ssm.logger.LogInfo("심볼 변경사항 없음")
	}

	ssm.lastSyncTime = time.Now()
	return nil
}

// fetchExchangeInfo 바이낸스 API에서 거래소 정보 가져오기
func (ssm *SymbolSyncManager) fetchExchangeInfo() (*BinanceExchangeInfo, error) {
	url := "https://api.binance.com/api/v3/exchangeInfo"

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API 요청 실패: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var exchangeInfo BinanceExchangeInfo
	if err := json.Unmarshal(body, &exchangeInfo); err != nil {
		return nil, err
	}

	return &exchangeInfo, nil
}

// filterActiveUSDTPairs 활성화된 USDT 페어만 필터링
func (ssm *SymbolSyncManager) filterActiveUSDTPairs(symbols []BinanceSymbol) []string {
	var activeSymbols []string

	for _, symbol := range symbols {
		// USDT 페어이고 활성 상태인 것만 필터링
		if symbol.QuoteAsset == "USDT" &&
			symbol.Status == "TRADING" &&
			symbol.IsSpotTradingAllowed {
			activeSymbols = append(activeSymbols, symbol.Symbol)
		}
	}

	// 알파벳 순으로 정렬
	sort.Strings(activeSymbols)
	return activeSymbols
}

// filterByUpbitExclusion 업비트 KRW 미상장 페어만 필터링
func (ssm *SymbolSyncManager) filterByUpbitExclusion(binanceSymbols []string) []string {
	ssm.mu.RLock()
	defer ssm.mu.RUnlock()

	if len(ssm.upbitSymbols) == 0 {
		ssm.logger.LogWarn("업비트 심볼 캐시가 비어있습니다. 모든 바이낸스 심볼을 포함합니다.")
		return binanceSymbols
	}

	var filtered []string
	excludedCount := 0

	for _, symbol := range binanceSymbols {
		// BTCUSDT -> BTC 추출
		baseAsset := strings.TrimSuffix(symbol, "USDT")

		// 업비트 KRW 마켓에 없는 것만 포함
		if !ssm.upbitSymbols[baseAsset] {
			filtered = append(filtered, symbol)
		} else {
			excludedCount++
		}
	}

	ssm.logger.LogInfo("업비트 필터링: %d개 제외, %d개 포함", excludedCount, len(filtered))
	return filtered
}

// compareSymbols 현재 심볼과 새로운 심볼 목록 비교
func (ssm *SymbolSyncManager) compareSymbols(newSymbols []string) ([]string, []string) {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()

	// 새 심볼을 맵으로 변환
	newSymbolMap := make(map[string]bool)
	for _, symbol := range newSymbols {
		newSymbolMap[symbol] = true
	}

	var addedSymbols, removedSymbols []string

	// 추가된 심볼 찾기
	for _, symbol := range newSymbols {
		if !ssm.currentSymbols[symbol] {
			addedSymbols = append(addedSymbols, symbol)
		}
	}

	// 제거된 심볼 찾기
	for symbol := range ssm.currentSymbols {
		if !newSymbolMap[symbol] {
			removedSymbols = append(removedSymbols, symbol)
		}
	}

	// 현재 심볼 맵 업데이트
	ssm.currentSymbols = newSymbolMap

	return addedSymbols, removedSymbols
}

// addSymbols 새 심볼 추가
func (ssm *SymbolSyncManager) addSymbols(symbols []string) {
	ssm.logger.LogInfo("새 심볼 추가: %v", symbols)

	// 메모리 관리자에 새 심볼 초기화
	for _, symbol := range symbols {
		ssm.memManager.InitializeSymbol(symbol)
		ssm.logger.LogInfo("심볼 초기화 완료: %s", symbol)
	}
}

// removeSymbols 심볼 제거
func (ssm *SymbolSyncManager) removeSymbols(symbols []string) {
	ssm.logger.LogInfo("심볼 제거: %v", symbols)

	// 메모리에서 심볼 데이터 정리
	for _, symbol := range symbols {
		ssm.memManager.CleanupSymbol(symbol)
		ssm.logger.LogInfo("심볼 정리 완료: %s", symbol)
	}
}

// reconnectWebSocket WebSocket 재연결 (새 심볼 반영)
func (ssm *SymbolSyncManager) reconnectWebSocket() error {
	ssm.logger.LogInfo("WebSocket 재연결 시작 (새 심볼 반영)")

	// WebSocket이 nil인지 확인
	if ssm.websocket == nil {
		ssm.logger.LogWarn("WebSocket이 nil이므로 재연결을 건너뜁니다")
		return nil
	}

	// 현재 연결 해제
	ssm.websocket.Disconnect()

	// 잠시 대기
	time.Sleep(2 * time.Second)

	// 새 심볼 목록으로 WebSocket 재연결
	ssm.mu.RLock()
	currentSymbols := make([]string, 0, len(ssm.currentSymbols))
	for symbol := range ssm.currentSymbols {
		currentSymbols = append(currentSymbols, symbol)
	}
	ssm.mu.RUnlock()

	// WebSocket 재연결 (새 심볼 목록으로)
	if err := ssm.websocket.Connect(ssm.ctx); err != nil {
		return fmt.Errorf("WebSocket 재연결 실패: %v", err)
	}

	ssm.logger.LogSuccess("WebSocket 재연결 완료 (%d개 심볼)", len(currentSymbols))
	return nil
}

// GetCurrentSymbols 현재 심볼 목록 조회
func (ssm *SymbolSyncManager) GetCurrentSymbols() []string {
	ssm.mu.RLock()
	defer ssm.mu.RUnlock()

	symbols := make([]string, 0, len(ssm.currentSymbols))
	for symbol := range ssm.currentSymbols {
		symbols = append(symbols, symbol)
	}

	sort.Strings(symbols)
	return symbols
}

// GetSyncStats 동기화 통계 조회
func (ssm *SymbolSyncManager) GetSyncStats() map[string]interface{} {
	ssm.mu.RLock()
	defer ssm.mu.RUnlock()

	return map[string]interface{}{
		"is_running":        ssm.isRunning,
		"last_sync_time":    ssm.lastSyncTime,
		"sync_interval":     ssm.syncInterval.String(),
		"current_symbols":   len(ssm.currentSymbols),
		"auto_sync_enabled": ssm.config.AutoSyncSymbols,
	}
}

// ManualSync 수동 심볼 동기화
func (ssm *SymbolSyncManager) ManualSync() error {
	ssm.logger.LogInfo("수동 심볼 동기화 시작")
	return ssm.syncSymbols()
}

// SetSyncInterval 동기화 간격 설정
func (ssm *SymbolSyncManager) SetSyncInterval(minutes int) {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()

	ssm.syncInterval = time.Duration(minutes) * time.Minute
	ssm.logger.LogInfo("심볼 동기화 간격 변경: %v분", minutes)
}

// EnableAutoSync 자동 동기화 활성화/비활성화
func (ssm *SymbolSyncManager) EnableAutoSync(enabled bool) {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()

	ssm.config.AutoSyncSymbols = enabled
	ssm.logger.LogInfo("자동 심볼 동기화 %s", map[bool]string{true: "활성화", false: "비활성화"}[enabled])
}

// SetWebSocket WebSocket 설정
func (ssm *SymbolSyncManager) SetWebSocket(websocket *websocket.BinanceWebSocket) {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()

	ssm.websocket = websocket
	ssm.logger.LogInfo("WebSocket 설정 완료")
}

// GetFilteredSymbols 필터링된 심볼 목록 반환
func (ssm *SymbolSyncManager) GetFilteredSymbols() []string {
	ssm.mu.RLock()
	defer ssm.mu.RUnlock()

	currentSymbols := make([]string, 0, len(ssm.currentSymbols))
	for symbol := range ssm.currentSymbols {
		currentSymbols = append(currentSymbols, symbol)
	}

	return currentSymbols
}
