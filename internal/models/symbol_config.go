package models

import (
	"fmt"
	"strings"
	"time"
)

// SymbolsConfig는 YAML 설정 파일의 전체 구조
type SymbolsConfig struct {
	Version   string    `yaml:"version"`
	UpdatedAt time.Time `yaml:"updated_at"`
	
	// 업비트 KRW 상장 심볼들
	UpbitKRWSymbols []string `yaml:"upbit_krw_symbols"`
	
	// 거래소별 설정
	Exchanges map[string]ExchangeConfig `yaml:"exchanges"`
	
	// 실제 구독할 심볼 목록 (업비트 KRW 제외 후)
	SubscriptionLists map[string][]string `yaml:"subscription_lists"`
}

// ExchangeConfig는 거래소별 설정 정보
type ExchangeConfig struct {
	// WebSocket Endpoints
	SpotEndpoint    string `yaml:"spot_endpoint"`
	FuturesEndpoint string `yaml:"futures_endpoint"`
	
	// 연결 제한 설정
	MaxSymbolsPerConnection int           `yaml:"max_symbols_per_connection"`
	RetryCooldown          time.Duration `yaml:"retry_cooldown"`
	MaxRetries             int           `yaml:"max_retries"`
	
	// 심볼 목록
	SpotSymbols    []string `yaml:"spot_symbols"`
	FuturesSymbols []string `yaml:"futures_symbols"`
	
	// 메타데이터
	LastUpdated time.Time `yaml:"last_updated"`
}

// DefaultExchangeConfigs는 기본 거래소 설정들
var DefaultExchangeConfigs = map[string]ExchangeConfig{
	"binance": {
		SpotEndpoint:            "wss://stream.binance.com:9443/ws",
		FuturesEndpoint:         "wss://fstream.binance.com/ws",
		MaxSymbolsPerConnection: 100,
		RetryCooldown:          30 * time.Second,
		MaxRetries:             5,
	},
	"bybit": {
		SpotEndpoint:            "wss://stream.bybit.com/v5/public/spot",
		FuturesEndpoint:         "wss://stream.bybit.com/v5/public/linear",
		MaxSymbolsPerConnection: 50,
		RetryCooldown:          60 * time.Second,
		MaxRetries:             3,
	},
	"kucoin": {
		SpotEndpoint:            "wss://ws-api.kucoin.com/endpoint", // 실제로는 토큰 기반
		FuturesEndpoint:         "wss://ws-api-futures.kucoin.com/endpoint",
		MaxSymbolsPerConnection: 100,
		RetryCooldown:          45 * time.Second,
		MaxRetries:             4,
	},
	"okx": {
		SpotEndpoint:            "wss://ws.okx.com:8443/ws/v5/public",
		FuturesEndpoint:         "wss://ws.okx.com:8443/ws/v5/public",
		MaxSymbolsPerConnection: 100,
		RetryCooldown:          30 * time.Second,
		MaxRetries:             5,
	},
	"phemex": {
		SpotEndpoint:            "wss://phemex.com/ws",
		FuturesEndpoint:         "wss://phemex.com/ws",
		MaxSymbolsPerConnection: 20,
		RetryCooldown:          90 * time.Second,
		MaxRetries:             3,
	},
	"gate": {
		SpotEndpoint:            "wss://api.gateio.ws/ws/v4/",
		FuturesEndpoint:         "wss://fx-ws.gateio.ws/v4/ws",
		MaxSymbolsPerConnection: 100,
		RetryCooldown:          30 * time.Second,
		MaxRetries:             5,
	},
}

// NewSymbolsConfig는 기본 설정으로 새로운 SymbolsConfig 생성
func NewSymbolsConfig() *SymbolsConfig {
	return &SymbolsConfig{
		Version:           "2.0",
		UpdatedAt:         time.Now(),
		UpbitKRWSymbols:   make([]string, 0),
		Exchanges:         make(map[string]ExchangeConfig),
		SubscriptionLists: make(map[string][]string),
	}
}

// InitializeWithDefaults는 기본 거래소 설정으로 초기화
func (sc *SymbolsConfig) InitializeWithDefaults() {
	for exchange, config := range DefaultExchangeConfigs {
		config.SpotSymbols = make([]string, 0)
		config.FuturesSymbols = make([]string, 0)
		config.LastUpdated = time.Now()
		sc.Exchanges[exchange] = config
	}
	
	// 기본 구독 목록 초기화
	for exchange := range DefaultExchangeConfigs {
		sc.SubscriptionLists[exchange+"_spot"] = make([]string, 0)
		sc.SubscriptionLists[exchange+"_futures"] = make([]string, 0)
	}
}

// UpdateUpbitKRWSymbols는 업비트 KRW 심볼 목록 업데이트
func (sc *SymbolsConfig) UpdateUpbitKRWSymbols(symbols []string) {
	sc.UpbitKRWSymbols = symbols
	sc.UpdatedAt = time.Now()
}

// UpdateExchangeSymbols는 특정 거래소의 심볼 목록 업데이트
func (sc *SymbolsConfig) UpdateExchangeSymbols(exchange string, spotSymbols, futuresSymbols []string) {
	if config, exists := sc.Exchanges[exchange]; exists {
		config.SpotSymbols = spotSymbols
		config.FuturesSymbols = futuresSymbols
		config.LastUpdated = time.Now()
		sc.Exchanges[exchange] = config
		sc.UpdatedAt = time.Now()
	}
}

// GenerateSubscriptionLists는 업비트 KRW 제외 후 구독 목록 생성
func (sc *SymbolsConfig) GenerateSubscriptionLists() {
	upbitSet := make(map[string]bool)
	for _, symbol := range sc.UpbitKRWSymbols {
		upbitSet[symbol] = true
	}
	
	for exchange, config := range sc.Exchanges {
		// Spot 구독 목록 생성
		spotList := make([]string, 0)
		for _, symbol := range config.SpotSymbols {
			baseSymbol := extractBaseSymbol(symbol)
			if !upbitSet[baseSymbol] {
				spotList = append(spotList, symbol)
			}
		}
		sc.SubscriptionLists[exchange+"_spot"] = spotList
		
		// Futures 구독 목록 생성
		futuresList := make([]string, 0)
		for _, symbol := range config.FuturesSymbols {
			baseSymbol := extractBaseSymbol(symbol)
			if !upbitSet[baseSymbol] {
				futuresList = append(futuresList, symbol)
			}
		}
		sc.SubscriptionLists[exchange+"_futures"] = futuresList
	}
	
	sc.UpdatedAt = time.Now()
}

// GetSubscriptionList는 특정 거래소/마켓의 구독 목록 반환
func (sc *SymbolsConfig) GetSubscriptionList(exchange, marketType string) []string {
	key := exchange + "_" + marketType
	if list, exists := sc.SubscriptionLists[key]; exists {
		return list
	}
	return make([]string, 0)
}

// GetExchangeConfig는 특정 거래소 설정 반환
func (sc *SymbolsConfig) GetExchangeConfig(exchange string) (ExchangeConfig, bool) {
	config, exists := sc.Exchanges[exchange]
	return config, exists
}

// GetAllExchanges는 모든 거래소 목록 반환
func (sc *SymbolsConfig) GetAllExchanges() []string {
	exchanges := make([]string, 0, len(sc.Exchanges))
	for exchange := range sc.Exchanges {
		exchanges = append(exchanges, exchange)
	}
	return exchanges
}

// Validate는 설정의 유효성 검증
func (sc *SymbolsConfig) Validate() error {
	if sc.Version == "" {
		return fmt.Errorf("version is required")
	}
	
	if len(sc.Exchanges) == 0 {
		return fmt.Errorf("at least one exchange is required")
	}
	
	for exchange, config := range sc.Exchanges {
		if config.SpotEndpoint == "" && config.FuturesEndpoint == "" {
			return fmt.Errorf("exchange %s must have at least one endpoint", exchange)
		}
		
		if config.MaxSymbolsPerConnection <= 0 {
			return fmt.Errorf("exchange %s must have positive max_symbols_per_connection", exchange)
		}
		
		if config.MaxRetries <= 0 {
			return fmt.Errorf("exchange %s must have positive max_retries", exchange)
		}
	}
	
	return nil
}

// GetStats는 설정 통계 반환
func (sc *SymbolsConfig) GetStats() SymbolConfigStats {
	totalSpotSymbols := 0
	totalFuturesSymbols := 0
	totalSubscriptions := 0
	
	for _, config := range sc.Exchanges {
		totalSpotSymbols += len(config.SpotSymbols)
		totalFuturesSymbols += len(config.FuturesSymbols)
	}
	
	for _, list := range sc.SubscriptionLists {
		totalSubscriptions += len(list)
	}
	
	return SymbolConfigStats{
		TotalExchanges:       len(sc.Exchanges),
		TotalUpbitKRWSymbols: len(sc.UpbitKRWSymbols),
		TotalSpotSymbols:     totalSpotSymbols,
		TotalFuturesSymbols:  totalFuturesSymbols,
		TotalSubscriptions:   totalSubscriptions,
		LastUpdated:          sc.UpdatedAt,
	}
}

// SymbolConfigStats는 설정 통계 정보
type SymbolConfigStats struct {
	TotalExchanges       int       `json:"total_exchanges"`
	TotalUpbitKRWSymbols int       `json:"total_upbit_krw_symbols"`
	TotalSpotSymbols     int       `json:"total_spot_symbols"`
	TotalFuturesSymbols  int       `json:"total_futures_symbols"`
	TotalSubscriptions   int       `json:"total_subscriptions"`
	LastUpdated          time.Time `json:"last_updated"`
}

// SymbolFilterManager는 심볼 필터링 관리자
type SymbolFilterManager struct {
	config      *SymbolsConfig
	upbitAPI    string // 업비트 API URL
	exchangeAPIs map[string]ExchangeAPIConfig // 거래소별 API 설정
	lastSync    time.Time
}

// ExchangeAPIConfig는 거래소별 API 설정
type ExchangeAPIConfig struct {
	SpotSymbolsAPI    string        // Spot 심볼 목록 API
	FuturesSymbolsAPI string        // Futures 심볼 목록 API
	RateLimit        time.Duration // API 호출 제한
}

// DefaultExchangeAPIConfigs는 기본 거래소 API 설정
var DefaultExchangeAPIConfigs = map[string]ExchangeAPIConfig{
	"binance": {
		SpotSymbolsAPI:    "https://api.binance.com/api/v3/exchangeInfo",
		FuturesSymbolsAPI: "https://fapi.binance.com/fapi/v1/exchangeInfo",
		RateLimit:        1 * time.Second,
	},
	"bybit": {
		SpotSymbolsAPI:    "https://api.bybit.com/v5/market/instruments-info?category=spot",
		FuturesSymbolsAPI: "https://api.bybit.com/v5/market/instruments-info?category=linear",
		RateLimit:        1 * time.Second,
	},
	"kucoin": {
		SpotSymbolsAPI:    "https://api.kucoin.com/api/v1/symbols",
		FuturesSymbolsAPI: "https://api-futures.kucoin.com/api/v1/contracts/active",
		RateLimit:        1 * time.Second,
	},
	"okx": {
		SpotSymbolsAPI:    "https://www.okx.com/api/v5/public/instruments?instType=SPOT",
		FuturesSymbolsAPI: "https://www.okx.com/api/v5/public/instruments?instType=FUTURES",
		RateLimit:        1 * time.Second,
	},
	"phemex": {
		SpotSymbolsAPI:    "https://api.phemex.com/exchange/public/cfg/v2/products",
		FuturesSymbolsAPI: "https://api.phemex.com/exchange/public/cfg/v2/products",
		RateLimit:        2 * time.Second,
	},
	"gate": {
		SpotSymbolsAPI:    "https://api.gateio.ws/api/v4/spot/currency_pairs",
		FuturesSymbolsAPI: "https://api.gateio.ws/api/v4/futures/usdt/contracts",
		RateLimit:        1 * time.Second,
	},
}

// NewSymbolFilterManager는 새로운 Symbol Filter Manager 생성
func NewSymbolFilterManager(config *SymbolsConfig) *SymbolFilterManager {
	return &SymbolFilterManager{
		config:       config,
		upbitAPI:     "https://api.upbit.com/v1/market/all",
		exchangeAPIs: DefaultExchangeAPIConfigs,
		lastSync:     time.Time{},
	}
}

// NeedsUpdate는 업데이트가 필요한지 확인 (24시간마다)
func (sfm *SymbolFilterManager) NeedsUpdate() bool {
	return time.Since(sfm.lastSync) > 24*time.Hour
}

// SetLastSync는 마지막 동기화 시간 설정
func (sfm *SymbolFilterManager) SetLastSync(t time.Time) {
	sfm.lastSync = t
}

// GetConfig는 현재 설정 반환
func (sfm *SymbolFilterManager) GetConfig() *SymbolsConfig {
	return sfm.config
}

// UpdateConfig는 설정 업데이트
func (sfm *SymbolFilterManager) UpdateConfig(config *SymbolsConfig) {
	sfm.config = config
}

// 헬퍼 함수: 심볼에서 기본 심볼 추출 (예: "BTCUSDT" -> "BTC")
func extractBaseSymbol(symbol string) string {
	// 간단한 패턴 매칭으로 기본 심볼 추출
	// 실제로는 더 정교한 로직이 필요할 수 있음
	
	// USDT 페어인 경우
	if strings.HasSuffix(symbol, "USDT") {
		return strings.TrimSuffix(symbol, "USDT")
	}
	
	// BTC 페어인 경우  
	if strings.HasSuffix(symbol, "BTC") {
		return strings.TrimSuffix(symbol, "BTC")
	}
	
	// ETH 페어인 경우
	if strings.HasSuffix(symbol, "ETH") {
		return strings.TrimSuffix(symbol, "ETH")
	}
	
	// 기본적으로 원본 반환
	return symbol
}