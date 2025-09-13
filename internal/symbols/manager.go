package symbols

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Manager handles symbol filtering and configuration management
type Manager struct {
	config     *SymbolsConfig
	httpClient *http.Client
}

// NewManager creates a new symbols manager
func NewManager() (*Manager, error) {
	return &Manager{
		config: &SymbolsConfig{
			Version:           "2.0",
			UpdatedAt:         time.Now(),
			UpbitKRWSymbols:   []string{},
			Exchanges:         make(map[string]ExchangeSymbols),
			SubscriptionLists: make(map[string][]string),
		},
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// UpdateFromExchanges fetches latest symbol lists from all exchanges and Upbit KRW
func (m *Manager) UpdateFromExchanges() error {
	// 1. Update Upbit KRW symbols first (for filtering)
	if err := m.updateUpbitKRWSymbols(); err != nil {
		return fmt.Errorf("failed to update Upbit KRW symbols: %w", err)
	}

	// 2. Update symbols from each exchange
	exchanges := []string{"binance", "bybit", "okx", "kucoin", "phemex", "gate"}
	
	for _, exchange := range exchanges {
		fmt.Printf("📡 Updating symbols for %s...\n", exchange)
		
		switch exchange {
		case "binance":
			if err := m.updateBinanceSymbols(); err != nil {
				fmt.Printf("⚠️ Failed to update %s symbols: %v\n", exchange, err)
			}
		case "bybit":
			if err := m.updateBybitSymbols(); err != nil {
				fmt.Printf("⚠️ Failed to update %s symbols: %v\n", exchange, err)
			}
		case "okx":
			if err := m.updateOKXSymbols(); err != nil {
				fmt.Printf("⚠️ Failed to update %s symbols: %v\n", exchange, err)
			}
		case "kucoin":
			if err := m.updateKuCoinSymbols(); err != nil {
				fmt.Printf("⚠️ Failed to update %s symbols: %v\n", exchange, err)
			}
		case "phemex":
			if err := m.updatePhemexSymbols(); err != nil {
				fmt.Printf("⚠️ Failed to update %s symbols: %v\n", exchange, err)
			}
		case "gate":
			if err := m.updateGateSymbols(); err != nil {
				fmt.Printf("⚠️ Failed to update %s symbols: %v\n", exchange, err)
			}
		}
	}

	// 3. Generate subscription lists after all symbols are updated
	m.generateSubscriptionLists()

	fmt.Printf("✅ Symbol update complete - Total Upbit KRW: %d\n", len(m.config.UpbitKRWSymbols))
	return nil
}

// SaveToFile saves the configuration to a YAML file
func (m *Manager) SaveToFile(filePath string) error {
	return SaveConfig(m.config, filePath)
}

// GetConfig returns the current configuration
func (m *Manager) GetConfig() *SymbolsConfig {
	return m.config
}

// updateUpbitKRWSymbols fetches current Upbit KRW market symbols
func (m *Manager) updateUpbitKRWSymbols() error {
	url := "https://api.upbit.com/v1/market/all"
	
	resp, err := m.httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch Upbit markets: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Upbit API returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var markets []struct {
		Market string `json:"market"`
	}

	if err := json.Unmarshal(body, &markets); err != nil {
		return fmt.Errorf("failed to parse markets JSON: %w", err)
	}

	// Extract KRW symbols
	var krwSymbols []string
	for _, market := range markets {
		if strings.HasPrefix(market.Market, "KRW-") {
			symbol := strings.TrimPrefix(market.Market, "KRW-")
			krwSymbols = append(krwSymbols, symbol)
		}
	}

	m.config.UpbitKRWSymbols = krwSymbols
	fmt.Printf("📊 Updated Upbit KRW symbols: %d symbols\n", len(krwSymbols))
	
	return nil
}

// updateBinanceSymbols fetches real Binance symbols from API
func (m *Manager) updateBinanceSymbols() error {
	// Fetch spot symbols
	spotSymbols, err := m.fetchBinanceSpotSymbols()
	if err != nil {
		fmt.Printf("⚠️ Failed to fetch Binance spot symbols: %v\n", err)
		spotSymbols = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"} // Fallback
	}

	// Fetch futures symbols
	futuresSymbols, err := m.fetchBinanceFuturesSymbols()
	if err != nil {
		fmt.Printf("⚠️ Failed to fetch Binance futures symbols: %v\n", err)
		futuresSymbols = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"} // Fallback
	}

	// Initialize Binance config with real symbols
	m.config.Exchanges["binance"] = ExchangeSymbols{
		SpotEndpoint:            "wss://stream.binance.com:9443/ws",
		FuturesEndpoint:         "wss://fstream.binance.com/ws",
		MaxSymbolsPerConnection: 100,
		RetryCooldown:          30 * time.Second,
		MaxRetries:             5,
		SpotSymbols:            spotSymbols,
		FuturesSymbols:         futuresSymbols,
		LastUpdated:            time.Now(),
	}

	fmt.Printf("🔸 Binance: %d spot, %d futures symbols fetched\n", len(spotSymbols), len(futuresSymbols))
	return nil
}

// updateBybitSymbols updates Bybit symbols
func (m *Manager) updateBybitSymbols() error {
	// Fetch spot symbols
	spotSymbols, err := m.fetchBybitSpotSymbols()
	if err != nil {
		fmt.Printf("⚠️ Failed to fetch Bybit spot symbols: %v\n", err)
		spotSymbols = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"} // Fallback
	}

	// Fetch futures symbols
	futuresSymbols, err := m.fetchBybitFuturesSymbols()
	if err != nil {
		fmt.Printf("⚠️ Failed to fetch Bybit futures symbols: %v\n", err)
		futuresSymbols = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"} // Fallback
	}

	// Initialize Bybit config with real symbols
	m.config.Exchanges["bybit"] = ExchangeSymbols{
		SpotEndpoint:            "wss://stream.bybit.com/v5/public/spot",
		FuturesEndpoint:         "wss://stream.bybit.com/v5/public/linear",
		MaxSymbolsPerConnection: 50,
		RetryCooldown:          60 * time.Second,
		MaxRetries:             3,
		SpotSymbols:            spotSymbols,
		FuturesSymbols:         futuresSymbols,
		LastUpdated:            time.Now(),
	}

	fmt.Printf("🔸 Bybit: %d spot, %d futures symbols fetched\n", len(spotSymbols), len(futuresSymbols))
	return nil
}

// updateOKXSymbols updates OKX symbols
func (m *Manager) updateOKXSymbols() error {
	// Fetch spot symbols
	spotSymbols, err := m.fetchOKXSpotSymbols()
	if err != nil {
		fmt.Printf("⚠️ Failed to fetch OKX spot symbols: %v\n", err)
		spotSymbols = []string{"BTC-USDT", "ETH-USDT", "SOL-USDT"} // Fallback
	}

	// Fetch futures symbols
	futuresSymbols, err := m.fetchOKXFuturesSymbols()
	if err != nil {
		fmt.Printf("⚠️ Failed to fetch OKX futures symbols: %v\n", err)
		futuresSymbols = []string{"BTC-USDT-SWAP", "ETH-USDT-SWAP", "SOL-USDT-SWAP"} // Fallback
	}

	// Initialize OKX config with real symbols
	m.config.Exchanges["okx"] = ExchangeSymbols{
		SpotEndpoint:            "wss://ws.okx.com:8443/ws/v5/public",
		FuturesEndpoint:         "wss://ws.okx.com:8443/ws/v5/public",
		MaxSymbolsPerConnection: 100,
		RetryCooldown:          30 * time.Second,
		MaxRetries:             5,
		SpotSymbols:            spotSymbols,
		FuturesSymbols:         futuresSymbols,
		LastUpdated:            time.Now(),
	}

	fmt.Printf("🔸 OKX: %d spot, %d futures symbols fetched\n", len(spotSymbols), len(futuresSymbols))
	return nil
}

// updateKuCoinSymbols updates KuCoin symbols
func (m *Manager) updateKuCoinSymbols() error {
	// Fetch spot symbols
	spotSymbols, err := m.fetchKuCoinSpotSymbols()
	if err != nil {
		fmt.Printf("⚠️ Failed to fetch KuCoin spot symbols: %v\n", err)
		spotSymbols = []string{"BTC-USDT", "ETH-USDT", "SOL-USDT"} // Fallback
	}

	// Fetch futures symbols
	futuresSymbols, err := m.fetchKuCoinFuturesSymbols()
	if err != nil {
		fmt.Printf("⚠️ Failed to fetch KuCoin futures symbols: %v\n", err)
		futuresSymbols = []string{"XBTUSDTM", "ETHUSDTM", "SOLUSDTM"} // Fallback
	}

	// Initialize KuCoin config with real symbols
	m.config.Exchanges["kucoin"] = ExchangeSymbols{
		SpotEndpoint:            "wss://ws-api.kucoin.com/endpoint",
		FuturesEndpoint:         "wss://ws-api-futures.kucoin.com/endpoint",
		MaxSymbolsPerConnection: 100,
		RetryCooldown:          45 * time.Second,
		MaxRetries:             4,
		SpotSymbols:            spotSymbols,
		FuturesSymbols:         futuresSymbols,
		LastUpdated:            time.Now(),
	}

	fmt.Printf("🔸 KuCoin: %d spot, %d futures symbols fetched\n", len(spotSymbols), len(futuresSymbols))
	return nil
}

// updatePhemexSymbols updates Phemex symbols
func (m *Manager) updatePhemexSymbols() error {
	m.config.Exchanges["phemex"] = ExchangeSymbols{
		SpotEndpoint:            "wss://ws.phemex.com",
		FuturesEndpoint:         "wss://ws.phemex.com",
		MaxSymbolsPerConnection: 20,
		RetryCooldown:          90 * time.Second,
		MaxRetries:             3,
		SpotSymbols:            []string{},
		FuturesSymbols:         []string{},
		LastUpdated:            time.Now(),
	}
	return nil
}

// updateGateSymbols updates Gate.io symbols
func (m *Manager) updateGateSymbols() error {
	// Fetch spot symbols
	spotSymbols, err := m.fetchGateSpotSymbols()
	if err != nil {
		fmt.Printf("⚠️ Failed to fetch Gate.io spot symbols: %v\n", err)
		spotSymbols = []string{"BTC_USDT", "ETH_USDT", "SOL_USDT"} // Fallback
	}

	// Fetch futures symbols
	futuresSymbols, err := m.fetchGateFuturesSymbols()
	if err != nil {
		fmt.Printf("⚠️ Failed to fetch Gate.io futures symbols: %v\n", err)
		futuresSymbols = []string{"BTC_USDT", "ETH_USDT", "SOL_USDT"} // Fallback
	}

	// Initialize Gate.io config with real symbols
	m.config.Exchanges["gate"] = ExchangeSymbols{
		SpotEndpoint:            "wss://api.gateio.ws/ws/v4/",
		FuturesEndpoint:         "wss://fx-ws.gateio.ws/v4/ws",
		MaxSymbolsPerConnection: 100,
		RetryCooldown:          30 * time.Second,
		MaxRetries:             5,
		SpotSymbols:            spotSymbols,
		FuturesSymbols:         futuresSymbols,
		LastUpdated:            time.Now(),
	}

	fmt.Printf("🔸 Gate.io: %d spot, %d futures symbols fetched\n", len(spotSymbols), len(futuresSymbols))
	return nil
}

// generateSubscriptionLists creates filtered subscription lists for each exchange-market combination
func (m *Manager) generateSubscriptionLists() {
	upbitSymbolsMap := make(map[string]bool)
	for _, symbol := range m.config.UpbitKRWSymbols {
		upbitSymbolsMap[symbol] = true
	}

	for exchangeName, exchangeConfig := range m.config.Exchanges {
		// Spot subscription list - CORRECT: Include symbols that are NOT in Upbit KRW (for pump detection)
		var spotFiltered []string
		for _, symbol := range exchangeConfig.SpotSymbols {
			baseSymbol := extractBaseSymbol(symbol)
			if !upbitSymbolsMap[baseSymbol] {  // NOT in Upbit = potential new listing
				spotFiltered = append(spotFiltered, symbol)
			}
		}
		m.config.SubscriptionLists[fmt.Sprintf("%s_spot", exchangeName)] = spotFiltered

		// Futures subscription list - CORRECT: Include symbols that are NOT in Upbit KRW
		var futuresFiltered []string
		for _, symbol := range exchangeConfig.FuturesSymbols {
			baseSymbol := extractBaseSymbol(symbol)
			if !upbitSymbolsMap[baseSymbol] {  // NOT in Upbit = potential new listing
				futuresFiltered = append(futuresFiltered, symbol)
			}
		}
		m.config.SubscriptionLists[fmt.Sprintf("%s_futures", exchangeName)] = futuresFiltered

		fmt.Printf("🔹 %s: spot=%d, futures=%d symbols (filtered)\n",
			exchangeName, len(spotFiltered), len(futuresFiltered))
	}
}

// fetchBinanceSpotSymbols fetches all USDT pairs from Binance Spot API
func (m *Manager) fetchBinanceSpotSymbols() ([]string, error) {
	url := "https://api.binance.com/api/v3/exchangeInfo"

	resp, err := m.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Binance spot info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Binance spot API returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var exchangeInfo struct {
		Symbols []struct {
			Symbol      string `json:"symbol"`
			Status      string `json:"status"`
			BaseAsset   string `json:"baseAsset"`
			QuoteAsset  string `json:"quoteAsset"`
		} `json:"symbols"`
	}

	if err := json.Unmarshal(body, &exchangeInfo); err != nil {
		return nil, fmt.Errorf("failed to parse exchange info: %w", err)
	}

	var usdtSymbols []string
	for _, symbol := range exchangeInfo.Symbols {
		// Only include USDT pairs that are actively trading
		if symbol.QuoteAsset == "USDT" && symbol.Status == "TRADING" {
			usdtSymbols = append(usdtSymbols, symbol.Symbol)
		}
	}

	return usdtSymbols, nil
}

// fetchBinanceFuturesSymbols fetches all USDT pairs from Binance Futures API
func (m *Manager) fetchBinanceFuturesSymbols() ([]string, error) {
	url := "https://fapi.binance.com/fapi/v1/exchangeInfo"

	resp, err := m.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Binance futures info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Binance futures API returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var exchangeInfo struct {
		Symbols []struct {
			Symbol      string `json:"symbol"`
			Status      string `json:"status"`
			BaseAsset   string `json:"baseAsset"`
			QuoteAsset  string `json:"quoteAsset"`
		} `json:"symbols"`
	}

	if err := json.Unmarshal(body, &exchangeInfo); err != nil {
		return nil, fmt.Errorf("failed to parse futures exchange info: %w", err)
	}

	var usdtSymbols []string
	for _, symbol := range exchangeInfo.Symbols {
		// Only include USDT pairs that are actively trading
		if symbol.QuoteAsset == "USDT" && symbol.Status == "TRADING" {
			usdtSymbols = append(usdtSymbols, symbol.Symbol)
		}
	}

	return usdtSymbols, nil
}


// fetchBybitSpotSymbols fetches all USDT pairs from Bybit Spot API
func (m *Manager) fetchBybitSpotSymbols() ([]string, error) {
	url := "https://api.bybit.com/v5/market/instruments-info?category=spot"

	resp, err := m.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Bybit spot info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Bybit spot API returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var instrumentsInfo struct {
		Result struct {
			List []struct {
				Symbol       string `json:"symbol"`
				Status       string `json:"status"`
				BaseCoin     string `json:"baseCoin"`
				QuoteCoin    string `json:"quoteCoin"`
			} `json:"list"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &instrumentsInfo); err != nil {
		return nil, fmt.Errorf("failed to parse Bybit instruments info: %w", err)
	}

	var usdtSymbols []string
	for _, instrument := range instrumentsInfo.Result.List {
		// Only include USDT pairs that are actively trading
		if instrument.QuoteCoin == "USDT" && instrument.Status == "Trading" {
			usdtSymbols = append(usdtSymbols, instrument.Symbol)
		}
	}

	return usdtSymbols, nil
}

// fetchBybitFuturesSymbols fetches all USDT pairs from Bybit Linear Futures API
func (m *Manager) fetchBybitFuturesSymbols() ([]string, error) {
	url := "https://api.bybit.com/v5/market/instruments-info?category=linear"

	resp, err := m.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Bybit futures info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Bybit futures API returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var instrumentsInfo struct {
		Result struct {
			List []struct {
				Symbol       string `json:"symbol"`
				Status       string `json:"status"`
				BaseCoin     string `json:"baseCoin"`
				QuoteCoin    string `json:"quoteCoin"`
			} `json:"list"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &instrumentsInfo); err != nil {
		return nil, fmt.Errorf("failed to parse Bybit futures instruments info: %w", err)
	}

	var usdtSymbols []string
	for _, instrument := range instrumentsInfo.Result.List {
		// Only include USDT pairs that are actively trading
		if instrument.QuoteCoin == "USDT" && instrument.Status == "Trading" {
			usdtSymbols = append(usdtSymbols, instrument.Symbol)
		}
	}

	return usdtSymbols, nil
}

// fetchOKXSpotSymbols fetches all USDT pairs from OKX Spot API
func (m *Manager) fetchOKXSpotSymbols() ([]string, error) {
	url := "https://www.okx.com/api/v5/public/instruments?instType=SPOT"

	resp, err := m.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OKX spot info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OKX spot API returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var instrumentsInfo struct {
		Code string `json:"code"`
		Data []struct {
			InstId    string `json:"instId"`
			State     string `json:"state"`
			InstType  string `json:"instType"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &instrumentsInfo); err != nil {
		return nil, fmt.Errorf("failed to parse OKX instruments info: %w", err)
	}

	var usdtSymbols []string
	for _, instrument := range instrumentsInfo.Data {
		// Only include USDT pairs that are live
		if strings.HasSuffix(instrument.InstId, "-USDT") && instrument.State == "live" {
			usdtSymbols = append(usdtSymbols, instrument.InstId)
		}
	}

	return usdtSymbols, nil
}

// fetchOKXFuturesSymbols fetches all USDT SWAP pairs from OKX Futures API
func (m *Manager) fetchOKXFuturesSymbols() ([]string, error) {
	url := "https://www.okx.com/api/v5/public/instruments?instType=SWAP"

	resp, err := m.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OKX futures info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OKX futures API returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var instrumentsInfo struct {
		Code string `json:"code"`
		Data []struct {
			InstId    string `json:"instId"`
			State     string `json:"state"`
			InstType  string `json:"instType"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &instrumentsInfo); err != nil {
		return nil, fmt.Errorf("failed to parse OKX futures instruments info: %w", err)
	}

	var usdtSymbols []string
	for _, instrument := range instrumentsInfo.Data {
		// Only include USDT-SWAP pairs that are live
		if strings.HasSuffix(instrument.InstId, "-USDT-SWAP") && instrument.State == "live" {
			usdtSymbols = append(usdtSymbols, instrument.InstId)
		}
	}

	return usdtSymbols, nil
}

// fetchKuCoinSpotSymbols fetches all USDT pairs from KuCoin Spot API
func (m *Manager) fetchKuCoinSpotSymbols() ([]string, error) {
	url := "https://api.kucoin.com/api/v1/symbols"

	resp, err := m.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch KuCoin spot info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("KuCoin spot API returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var symbolsInfo struct {
		Code string `json:"code"`
		Data []struct {
			Symbol      string `json:"symbol"`
			Name        string `json:"name"`
			BaseCurrency string `json:"baseCurrency"`
			QuoteCurrency string `json:"quoteCurrency"`
			EnableTrading bool  `json:"enableTrading"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &symbolsInfo); err != nil {
		return nil, fmt.Errorf("failed to parse KuCoin symbols info: %w", err)
	}

	var usdtSymbols []string
	for _, symbol := range symbolsInfo.Data {
		// Only include USDT pairs that are actively trading
		if symbol.QuoteCurrency == "USDT" && symbol.EnableTrading {
			usdtSymbols = append(usdtSymbols, symbol.Symbol)
		}
	}

	return usdtSymbols, nil
}

// fetchKuCoinFuturesSymbols fetches all USDT pairs from KuCoin Futures API
func (m *Manager) fetchKuCoinFuturesSymbols() ([]string, error) {
	url := "https://api-futures.kucoin.com/api/v1/contracts/active"

	resp, err := m.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch KuCoin futures info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("KuCoin futures API returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var contractsInfo struct {
		Code string `json:"code"`
		Data []struct {
			Symbol         string `json:"symbol"`
			Status         string `json:"status"`
			BaseCurrency   string `json:"baseCurrency"`
			QuoteCurrency  string `json:"quoteCurrency"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &contractsInfo); err != nil {
		return nil, fmt.Errorf("failed to parse KuCoin futures contracts info: %w", err)
	}

	var usdtSymbols []string
	for _, contract := range contractsInfo.Data {
		// Only include USDT pairs that are open for trading
		if strings.HasSuffix(contract.Symbol, "USDTM") && contract.Status == "Open" {
			usdtSymbols = append(usdtSymbols, contract.Symbol)
		}
	}

	return usdtSymbols, nil
}

// fetchGateSpotSymbols fetches all USDT pairs from Gate.io Spot API
func (m *Manager) fetchGateSpotSymbols() ([]string, error) {
	url := "https://api.gateio.ws/api/v4/spot/currency_pairs"

	resp, err := m.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Gate.io spot info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Gate.io spot API returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var pairs []struct {
		ID             string `json:"id"`
		Base           string `json:"base"`
		Quote          string `json:"quote"`
		Fee            string `json:"fee"`
		TradeStatus    string `json:"trade_status"`
	}

	if err := json.Unmarshal(body, &pairs); err != nil {
		return nil, fmt.Errorf("failed to parse Gate.io currency pairs: %w", err)
	}

	var usdtSymbols []string
	for _, pair := range pairs {
		// Only include USDT pairs that are actively trading
		if pair.Quote == "USDT" && pair.TradeStatus == "tradable" {
			usdtSymbols = append(usdtSymbols, pair.ID)
		}
	}

	return usdtSymbols, nil
}

// fetchGateFuturesSymbols fetches all USDT pairs from Gate.io Futures API
func (m *Manager) fetchGateFuturesSymbols() ([]string, error) {
	url := "https://api.gateio.ws/api/v4/futures/usdt/contracts"

	resp, err := m.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Gate.io futures info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Gate.io futures API returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var contracts []struct {
		Name               string `json:"name"`
		Type               string `json:"type"`
		QuantoMultiplier   string `json:"quanto_multiplier"`
		LeverageMin        string `json:"leverage_min"`
		LeverageMax        string `json:"leverage_max"`
		MaintenanceRate    string `json:"maintenance_rate"`
		MarkType           string `json:"mark_type"`
		MarkPrice          string `json:"mark_price"`
		IndexPrice         string `json:"index_price"`
		LastPrice          string `json:"last_price"`
		InDelisting        bool   `json:"in_delisting"`
	}

	if err := json.Unmarshal(body, &contracts); err != nil {
		return nil, fmt.Errorf("failed to parse Gate.io futures contracts: %w", err)
	}

	var usdtSymbols []string
	for _, contract := range contracts {
		// Only include USDT contracts that are not being delisted
		if strings.HasSuffix(contract.Name, "_USDT") && !contract.InDelisting {
			usdtSymbols = append(usdtSymbols, contract.Name)
		}
	}

	return usdtSymbols, nil
}
