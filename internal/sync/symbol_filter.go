package sync

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"PumpWatch/internal/models"
)

// SymbolFilterService는 심볼 필터링 및 동기화 서비스
type SymbolFilterService struct {
	config     *models.SymbolsConfig
	httpClient *http.Client
	lastSync   map[string]time.Time // 거래소별 마지막 동기화 시간
}

// NewSymbolFilterService는 새로운 Symbol Filter Service 생성
func NewSymbolFilterService(config *models.SymbolsConfig) *SymbolFilterService {
	return &SymbolFilterService{
		config:     config,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		lastSync:   make(map[string]time.Time),
	}
}

// SyncAllSymbols는 모든 거래소 심볼 동기화 및 필터링
func (sfs *SymbolFilterService) SyncAllSymbols() error {
	log.Printf("🔄 전체 심볼 동기화 시작...")

	// 1. 업비트 KRW 심볼 가져오기
	upbitSymbols, err := sfs.fetchUpbitKRWSymbols()
	if err != nil {
		return fmt.Errorf("업비트 심볼 가져오기 실패: %v", err)
	}
	log.Printf("📊 업비트 KRW 심볼: %d개", len(upbitSymbols))

	// 2. 설정 업데이트
	sfs.config.UpdateUpbitKRWSymbols(upbitSymbols)

	// 3. 각 거래소 심볼 동기화
	for exchange := range models.DefaultExchangeConfigs {
		if err := sfs.syncExchangeSymbols(exchange); err != nil {
			log.Printf("❌ %s 심볼 동기화 실패: %v", exchange, err)
			continue
		}
		sfs.lastSync[exchange] = time.Now()
		log.Printf("✅ %s 심볼 동기화 완료", exchange)
	}

	// 4. 구독 목록 생성 (업비트 KRW 제외)
	sfs.config.GenerateSubscriptionLists()

	log.Printf("🎯 심볼 필터링 완료 - 구독 목록 생성됨")
	return nil
}

// fetchUpbitKRWSymbols는 업비트 KRW 마켓 심볼 가져오기
func (sfs *SymbolFilterService) fetchUpbitKRWSymbols() ([]string, error) {
	url := "https://api.upbit.com/v1/market/all"

	resp, err := sfs.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("API 호출 실패: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d 응답", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("응답 읽기 실패: %v", err)
	}

	var markets []struct {
		Market      string `json:"market"`
		KoreanName  string `json:"korean_name"`
		EnglishName string `json:"english_name"`
	}

	if err := json.Unmarshal(body, &markets); err != nil {
		return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
	}

	var krwSymbols []string
	for _, market := range markets {
		// KRW- 마켓만 추출
		if strings.HasPrefix(market.Market, "KRW-") {
			symbol := strings.TrimPrefix(market.Market, "KRW-")
			krwSymbols = append(krwSymbols, symbol)
		}
	}

	return krwSymbols, nil
}

// syncExchangeSymbols는 특정 거래소의 심볼 동기화
func (sfs *SymbolFilterService) syncExchangeSymbols(exchange string) error {
	apiConfig, exists := models.DefaultExchangeAPIConfigs[exchange]
	if !exists {
		return fmt.Errorf("지원하지 않는 거래소: %s", exchange)
	}

	// Rate limit 준수
	time.Sleep(apiConfig.RateLimit)

	// Spot 심볼 가져오기
	var spotSymbols []string
	var futuresSymbols []string
	var err error

	if apiConfig.SpotSymbolsAPI != "" {
		spotSymbols, err = sfs.fetchSymbolsFromAPI(exchange, "spot", apiConfig.SpotSymbolsAPI)
		if err != nil {
			log.Printf("⚠️ %s spot 심볼 가져오기 실패: %v", exchange, err)
		}
	}

	if apiConfig.FuturesSymbolsAPI != "" {
		futuresSymbols, err = sfs.fetchSymbolsFromAPI(exchange, "futures", apiConfig.FuturesSymbolsAPI)
		if err != nil {
			log.Printf("⚠️ %s futures 심볼 가져오기 실패: %v", exchange, err)
		}
	}

	// 설정 업데이트
	sfs.config.UpdateExchangeSymbols(exchange, spotSymbols, futuresSymbols)

	log.Printf("📊 %s - Spot: %d개, Futures: %d개", exchange, len(spotSymbols), len(futuresSymbols))
	return nil
}

// fetchSymbolsFromAPI는 API에서 심볼 목록 가져오기
func (sfs *SymbolFilterService) fetchSymbolsFromAPI(exchange, marketType, apiURL string) ([]string, error) {
	resp, err := sfs.httpClient.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("API 호출 실패: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d 응답", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("응답 읽기 실패: %v", err)
	}

	// 거래소별 파싱 로직
	switch exchange {
	case "binance":
		return sfs.parseBinanceSymbols(body, marketType)
	case "bybit":
		return sfs.parseBybitSymbols(body)
	case "kucoin":
		return sfs.parseKuCoinSymbols(body, marketType)
	case "okx":
		return sfs.parseOKXSymbols(body)
	case "phemex":
		return sfs.parsePhemexSymbols(body, marketType)
	case "gate":
		return sfs.parseGateSymbols(body, marketType)
	default:
		return nil, fmt.Errorf("지원하지 않는 거래소: %s", exchange)
	}
}

// parseBinanceSymbols는 바이낸스 심볼 파싱
func (sfs *SymbolFilterService) parseBinanceSymbols(body []byte, marketType string) ([]string, error) {
	var response struct {
		Symbols []struct {
			Symbol string `json:"symbol"`
			Status string `json:"status"`
		} `json:"symbols"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
	}

	var symbols []string
	for _, symbol := range response.Symbols {
		if symbol.Status == "TRADING" {
			// USDT 페어만 선택
			if strings.HasSuffix(symbol.Symbol, "USDT") {
				symbols = append(symbols, symbol.Symbol)
			}
		}
	}

	return symbols, nil
}

// parseBybitSymbols는 바이비트 심볼 파싱
func (sfs *SymbolFilterService) parseBybitSymbols(body []byte) ([]string, error) {
	var response struct {
		Result struct {
			List []struct {
				Symbol string `json:"symbol"`
				Status string `json:"status"`
			} `json:"list"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
	}

	var symbols []string
	for _, symbol := range response.Result.List {
		if symbol.Status == "Trading" {
			// USDT 페어만 선택
			if strings.HasSuffix(symbol.Symbol, "USDT") {
				symbols = append(symbols, symbol.Symbol)
			}
		}
	}

	return symbols, nil
}

// parseKuCoinSymbols는 쿠코인 심볼 파싱
func (sfs *SymbolFilterService) parseKuCoinSymbols(body []byte, marketType string) ([]string, error) {
	if marketType == "spot" {
		var response struct {
			Data []struct {
				Symbol        string `json:"symbol"`
				EnableTrading bool   `json:"enableTrading"`
			} `json:"data"`
		}

		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
		}

		var symbols []string
		for _, symbol := range response.Data {
			if symbol.EnableTrading {
				// USDT 페어만 선택
				if strings.HasSuffix(symbol.Symbol, "-USDT") {
					// KuCoin은 "-" 형식이므로 바이낸스 형식으로 변환
					converted := strings.Replace(symbol.Symbol, "-", "", -1)
					symbols = append(symbols, converted)
				}
			}
		}
		return symbols, nil
	} else {
		// Futures
		var response struct {
			Data []struct {
				Symbol string `json:"symbol"`
				Status string `json:"status"`
			} `json:"data"`
		}

		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
		}

		var symbols []string
		for _, symbol := range response.Data {
			if symbol.Status == "Open" {
				// USDT 페어만 선택
				if strings.HasSuffix(symbol.Symbol, "USDTM") {
					// USDTM을 USDT로 변환
					converted := strings.Replace(symbol.Symbol, "USDTM", "USDT", -1)
					symbols = append(symbols, converted)
				}
			}
		}
		return symbols, nil
	}
}

// parseOKXSymbols는 OKX 심볼 파싱
func (sfs *SymbolFilterService) parseOKXSymbols(body []byte) ([]string, error) {
	var response struct {
		Data []struct {
			InstId string `json:"instId"`
			State  string `json:"state"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
	}

	var symbols []string
	for _, symbol := range response.Data {
		if symbol.State == "live" {
			// USDT 페어만 선택
			if strings.HasSuffix(symbol.InstId, "-USDT") {
				// OKX는 "-" 형식이므로 바이낸스 형식으로 변환
				converted := strings.Replace(symbol.InstId, "-", "", -1)
				symbols = append(symbols, converted)
			}
		}
	}

	return symbols, nil
}

// parsePhemexSymbols는 피멕스 심볼 파싱
func (sfs *SymbolFilterService) parsePhemexSymbols(body []byte, marketType string) ([]string, error) {
	var response struct {
		Data struct {
			Products []struct {
				Symbol string `json:"symbol"`
				Status string `json:"status"`
				Type   string `json:"type"`
			} `json:"products"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
	}

	var symbols []string
	for _, product := range response.Data.Products {
		if product.Status == "Listed" {
			if marketType == "spot" && product.Type == "Spot" {
				// USDT 페어만 선택
				if strings.HasSuffix(product.Symbol, "USDT") {
					symbols = append(symbols, product.Symbol)
				}
			} else if marketType == "futures" && product.Type == "Perpetual" {
				// USDT 페어만 선택
				if strings.HasSuffix(product.Symbol, "USD") {
					// USD를 USDT로 변환 (Phemex는 USD 표기)
					converted := strings.Replace(product.Symbol, "USD", "USDT", -1)
					symbols = append(symbols, converted)
				}
			}
		}
	}

	return symbols, nil
}

// parseGateSymbols는 게이트 심볼 파싱
func (sfs *SymbolFilterService) parseGateSymbols(body []byte, marketType string) ([]string, error) {
	if marketType == "spot" {
		var symbols []struct {
			ID          string `json:"id"`
			TradeStatus string `json:"trade_status"`
		}

		if err := json.Unmarshal(body, &symbols); err != nil {
			return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
		}

		var result []string
		for _, symbol := range symbols {
			if symbol.TradeStatus == "tradable" {
				// USDT 페어만 선택
				if strings.HasSuffix(symbol.ID, "_usdt") {
					// Gate.io는 "_" 형식이므로 바이낸스 형식으로 변환
					converted := strings.ToUpper(strings.Replace(symbol.ID, "_", "", -1))
					result = append(result, converted)
				}
			}
		}
		return result, nil
	} else {
		// Futures
		var response []struct {
			Name        string `json:"name"`
			InDelisting bool   `json:"in_delisting"`
		}

		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
		}

		var result []string
		for _, contract := range response {
			if !contract.InDelisting {
				// USDT 페어만 선택
				if strings.HasSuffix(contract.Name, "_USDT") {
					// "_USDT"를 "USDT"로 변환
					converted := strings.Replace(contract.Name, "_", "", -1)
					result = append(result, converted)
				}
			}
		}
		return result, nil
	}
}

// GetFilteredSymbols는 필터링된 심볼 목록 반환
func (sfs *SymbolFilterService) GetFilteredSymbols(exchange, marketType string) []string {
	return sfs.config.GetSubscriptionList(exchange, marketType)
}

// GetStats는 필터링 통계 반환
func (sfs *SymbolFilterService) GetStats() models.SymbolConfigStats {
	return sfs.config.GetStats()
}

// NeedsUpdate는 업데이트 필요 여부 확인 (24시간마다)
func (sfs *SymbolFilterService) NeedsUpdate(exchange string) bool {
	lastSync, exists := sfs.lastSync[exchange]
	if !exists {
		return true
	}
	return time.Since(lastSync) > 24*time.Hour
}

// GetConfig는 현재 설정 반환
func (sfs *SymbolFilterService) GetConfig() *models.SymbolsConfig {
	return sfs.config
}
