package monitor

import (
	"regexp"
	"strings"
)

// ListingParser parses Upbit announcement titles to extract listing information
type ListingParser struct {
	patterns []*ListingPattern
}

// ListingPattern represents a pattern for parsing listing announcements
type ListingPattern struct {
	Name        string
	Regex       *regexp.Regexp
	Description string
}

// ListingResult holds the parsed result
type ListingResult struct {
	Symbol       string
	Markets      []string
	IsKRWListing bool
	Pattern      string
}

// NewListingParser creates a new listing parser with Flash-upbit patterns
func NewListingParser() *ListingParser {
	patterns := []*ListingPattern{
		// Pattern 1: multiple coin(market) - 셀레스티아(TIA)(KRW, BTC, USDT 마켓)
		{
			Name:        "multiple_coin_market",
			Regex:       regexp.MustCompile(`([^(]+)\(([A-Z0-9]+)\)\s*\(([^)]+)\s*마켓\)`),
			Description: "Multiple coin with markets in parentheses",
		},

		// Pattern 2: coins with parenthesis markets - 비체인(VET), 알고랜드(ALGO) (BTC, USDT 마켓)
		{
			Name:        "coins_with_parenthesis_markets",
			Regex:       regexp.MustCompile(`([^(]+)\(([A-Z0-9]+)\)[^(]*\(([^)]+)\s*마켓\)`),
			Description: "Coins with parenthesis and markets",
		},

		// Pattern 3: market outside parenthesis - 썬더코어(TT), 카바(KAVA) KRW 마켓
		{
			Name:        "market_outside_parenthesis",
			Regex:       regexp.MustCompile(`([^(]+)\(([A-Z0-9]+)\)[^A-Z]*([A-Z]+)\s*마켓`),
			Description: "Market specified outside parentheses",
		},

		// Pattern 4: single coin, single market - 봉크(BONK) KRW 마켓
		{
			Name:        "single_coin_single_market",
			Regex:       regexp.MustCompile(`([^(]+)\(([A-Z0-9]+)\)\s*([A-Z]+)\s*마켓`),
			Description: "Single coin with single market",
		},

		// Pattern 5: coin list in parenthesis - KRW 마켓 디지털 자산 추가 (WAXP, CARV)
		{
			Name:        "coin_list_in_parenthesis",
			Regex:       regexp.MustCompile(`([A-Z]+)\s*마켓[^(]*\(([^)]+)\)`),
			Description: "Coin list in parentheses after market type",
		},
	}

	return &ListingParser{
		patterns: patterns,
	}
}

// ParseListing attempts to parse a listing announcement using all patterns
func (lp *ListingParser) ParseListing(title string) *ListingResult {
	// Clean up title
	cleanTitle := strings.TrimSpace(title)

	// Try each pattern
	for _, pattern := range lp.patterns {
		if result := lp.tryPattern(pattern, cleanTitle); result != nil {
			result.Pattern = pattern.Name
			return result
		}
	}

	return nil
}

// tryPattern attempts to match a specific pattern
func (lp *ListingParser) tryPattern(pattern *ListingPattern, title string) *ListingResult {
	matches := pattern.Regex.FindStringSubmatch(title)
	if len(matches) < 3 {
		return nil
	}

	switch pattern.Name {
	case "multiple_coin_market":
		return lp.parseMultipleCoinMarket(matches)
	case "coins_with_parenthesis_markets":
		return lp.parseCoinsWithParenthesisMarkets(matches)
	case "market_outside_parenthesis":
		return lp.parseMarketOutsideParenthesis(matches)
	case "single_coin_single_market":
		return lp.parseSingleCoinSingleMarket(matches)
	case "coin_list_in_parenthesis":
		return lp.parseCoinListInParenthesis(matches)
	}

	return nil
}

// parseMultipleCoinMarket parses pattern 1: 셀레스티아(TIA)(KRW, BTC, USDT 마켓)
func (lp *ListingParser) parseMultipleCoinMarket(matches []string) *ListingResult {
	if len(matches) < 4 {
		return nil
	}

	symbol := strings.TrimSpace(matches[2])
	marketsStr := strings.TrimSpace(matches[3])
	markets := lp.parseMarkets(marketsStr)

	return &ListingResult{
		Symbol:       symbol,
		Markets:      markets,
		IsKRWListing: lp.containsKRW(markets),
	}
}

// parseCoinsWithParenthesisMarkets parses pattern 2: 비체인(VET), 알고랜드(ALGO) (BTC, USDT 마켓)
func (lp *ListingParser) parseCoinsWithParenthesisMarkets(matches []string) *ListingResult {
	if len(matches) < 4 {
		return nil
	}

	// Extract the last symbol from multiple symbols
	symbol := strings.TrimSpace(matches[2])
	marketsStr := strings.TrimSpace(matches[3])
	markets := lp.parseMarkets(marketsStr)

	return &ListingResult{
		Symbol:       symbol,
		Markets:      markets,
		IsKRWListing: lp.containsKRW(markets),
	}
}

// parseMarketOutsideParenthesis parses pattern 3: 썬더코어(TT), 카바(KAVA) KRW 마켓
func (lp *ListingParser) parseMarketOutsideParenthesis(matches []string) *ListingResult {
	if len(matches) < 4 {
		return nil
	}

	symbol := strings.TrimSpace(matches[2])
	market := strings.TrimSpace(matches[3])

	return &ListingResult{
		Symbol:       symbol,
		Markets:      []string{market},
		IsKRWListing: market == "KRW",
	}
}

// parseSingleCoinSingleMarket parses pattern 4: 봉크(BONK) KRW 마켓
func (lp *ListingParser) parseSingleCoinSingleMarket(matches []string) *ListingResult {
	if len(matches) < 4 {
		return nil
	}

	symbol := strings.TrimSpace(matches[2])
	market := strings.TrimSpace(matches[3])

	return &ListingResult{
		Symbol:       symbol,
		Markets:      []string{market},
		IsKRWListing: market == "KRW",
	}
}

// parseCoinListInParenthesis parses pattern 5: KRW 마켓 디지털 자산 추가 (WAXP, CARV)
func (lp *ListingParser) parseCoinListInParenthesis(matches []string) *ListingResult {
	if len(matches) < 3 {
		return nil
	}

	market := strings.TrimSpace(matches[1])
	coinsStr := strings.TrimSpace(matches[2])

	// Parse multiple symbols
	symbols := lp.parseSymbols(coinsStr)
	if len(symbols) == 0 {
		return nil
	}

	// For multiple symbols, return the first one as primary
	// In practice, we might need to handle multiple symbols differently
	return &ListingResult{
		Symbol:       symbols[0],
		Markets:      []string{market},
		IsKRWListing: market == "KRW",
	}
}

// parseMarkets parses market string like "KRW, BTC, USDT 마켓" into []string{"KRW", "BTC", "USDT"}
func (lp *ListingParser) parseMarkets(marketsStr string) []string {
	// Remove "마켓" suffix
	marketsStr = strings.ReplaceAll(marketsStr, "마켓", "")
	marketsStr = strings.ReplaceAll(marketsStr, "market", "")

	// Split by comma and clean up
	parts := strings.Split(marketsStr, ",")
	var markets []string

	for _, part := range parts {
		market := strings.TrimSpace(part)
		if market != "" {
			markets = append(markets, strings.ToUpper(market))
		}
	}

	return markets
}

// parseSymbols parses symbol string like "WAXP, CARV" into []string{"WAXP", "CARV"}
func (lp *ListingParser) parseSymbols(symbolsStr string) []string {
	parts := strings.Split(symbolsStr, ",")
	var symbols []string

	for _, part := range parts {
		symbol := strings.TrimSpace(part)
		if symbol != "" {
			symbols = append(symbols, strings.ToUpper(symbol))
		}
	}

	return symbols
}

// containsKRW checks if markets list contains KRW
func (lp *ListingParser) containsKRW(markets []string) bool {
	for _, market := range markets {
		if strings.ToUpper(market) == "KRW" {
			return true
		}
	}
	return false
}

// TestPattern tests a specific pattern against a title (for debugging)
func (lp *ListingParser) TestPattern(patternName, title string) *ListingResult {
	for _, pattern := range lp.patterns {
		if pattern.Name == patternName {
			if result := lp.tryPattern(pattern, title); result != nil {
				result.Pattern = pattern.Name
				return result
			}
		}
	}
	return nil
}

// GetPatterns returns all available patterns (for debugging)
func (lp *ListingParser) GetPatterns() []*ListingPattern {
	return lp.patterns
}
