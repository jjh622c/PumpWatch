package symbols

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// SymbolsConfig represents the symbols configuration structure
type SymbolsConfig struct {
	Version         string                     `yaml:"version"`
	UpdatedAt       time.Time                  `yaml:"updated_at"`
	UpbitKRWSymbols []string                   `yaml:"upbit_krw_symbols"`
	Exchanges       map[string]ExchangeSymbols `yaml:"exchanges"`
	SubscriptionLists map[string][]string      `yaml:"subscription_lists"`
}

// ExchangeSymbols represents exchange-specific symbol configuration
type ExchangeSymbols struct {
	SpotEndpoint               string        `yaml:"spot_endpoint"`
	FuturesEndpoint           string        `yaml:"futures_endpoint"`
	MaxSymbolsPerConnection   int           `yaml:"max_symbols_per_connection"`
	RetryCooldown            time.Duration `yaml:"retry_cooldown"`
	MaxRetries               int           `yaml:"max_retries"`
	SpotSymbols              []string      `yaml:"spot_symbols"`
	FuturesSymbols           []string      `yaml:"futures_symbols"`
	LastUpdated              time.Time     `yaml:"last_updated"`
}

// LoadConfig loads symbols configuration from YAML file
func LoadConfig(configPath string) (*SymbolsConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read symbols config: %w", err)
	}

	var config SymbolsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse symbols YAML: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("symbols config validation failed: %w", err)
	}

	return &config, nil
}

// SaveConfig saves symbols configuration to YAML file
func SaveConfig(config *SymbolsConfig, configPath string) error {
	config.UpdatedAt = time.Now()
	
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal symbols config: %w", err)
	}

	// Atomic write using temp file
	tempPath := configPath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp symbols config: %w", err)
	}

	if err := os.Rename(tempPath, configPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to move symbols config: %w", err)
	}

	return nil
}

// Validate validates the symbols configuration
func (sc *SymbolsConfig) Validate() error {
	if sc.Version == "" {
		return fmt.Errorf("version cannot be empty")
	}

	if len(sc.Exchanges) == 0 {
		return fmt.Errorf("at least one exchange must be configured")
	}

	for name, exchange := range sc.Exchanges {
		if exchange.MaxSymbolsPerConnection <= 0 {
			return fmt.Errorf("exchange %s max_symbols_per_connection must be positive", name)
		}
		if exchange.MaxRetries < 0 {
			return fmt.Errorf("exchange %s max_retries cannot be negative", name)
		}
	}

	return nil
}

// GetFilteredSymbols returns symbols for a specific exchange and market type, 
// filtered to exclude Upbit KRW symbols
func (sc *SymbolsConfig) GetFilteredSymbols(exchange, marketType string) []string {
	exchangeConfig, exists := sc.Exchanges[exchange]
	if !exists {
		return []string{}
	}

	var symbols []string
	switch marketType {
	case "spot":
		symbols = exchangeConfig.SpotSymbols
	case "futures":
		symbols = exchangeConfig.FuturesSymbols
	default:
		return []string{}
	}

	// Filter out Upbit KRW symbols
	upbitSymbolsMap := make(map[string]bool)
	for _, symbol := range sc.UpbitKRWSymbols {
		upbitSymbolsMap[symbol] = true
	}

	var filtered []string
	for _, symbol := range symbols {
		// Extract base symbol (e.g., BTC from BTCUSDT)
		baseSymbol := extractBaseSymbol(symbol)
		if !upbitSymbolsMap[baseSymbol] {
			filtered = append(filtered, symbol)
		}
	}

	return filtered
}

// GetSubscriptionList returns pre-filtered subscription list for exchange-market combination
func (sc *SymbolsConfig) GetSubscriptionList(exchange, marketType string) []string {
	key := fmt.Sprintf("%s_%s", exchange, marketType)
	if symbols, exists := sc.SubscriptionLists[key]; exists {
		return symbols
	}
	return []string{}
}

// GetAllExchanges returns list of all configured exchanges
func (sc *SymbolsConfig) GetAllExchanges() []string {
	var exchanges []string
	for name := range sc.Exchanges {
		exchanges = append(exchanges, name)
	}
	return exchanges
}

// GetExchangeConfig returns configuration for a specific exchange
func (sc *SymbolsConfig) GetExchangeConfig(exchange string) (ExchangeSymbols, bool) {
	config, exists := sc.Exchanges[exchange]
	return config, exists
}

// extractBaseSymbol extracts the base symbol from a trading pair
func extractBaseSymbol(symbol string) string {
	symbol = strings.ToUpper(symbol)

	// Handle dash-separated format (OKX: BTC-USDT, BTC-USDT-SWAP)
	if strings.Contains(symbol, "-") {
		parts := strings.Split(symbol, "-")
		if len(parts) >= 2 {
			return parts[0] // Return first part (base symbol)
		}
	}

	// Handle underscore-separated format (Gate.io: BTC_USDT)
	if strings.Contains(symbol, "_") {
		parts := strings.Split(symbol, "_")
		if len(parts) >= 2 {
			return parts[0] // Return first part (base symbol)
		}
	}

	// Handle concatenated format (Binance/Bybit: BTCUSDT)
	quoteAssets := []string{"USDT", "USDC", "BTC", "ETH", "BNB"}
	for _, quote := range quoteAssets {
		if strings.HasSuffix(symbol, quote) {
			return strings.TrimSuffix(symbol, quote)
		}
	}

	// Default: return original symbol
	return symbol
}