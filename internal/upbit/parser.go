package upbit

import (
	"regexp"
	"strings"
)

// NoticeParser는 업비트 상장공고를 파싱하는 구조체 (flash-upbit 정규식 패턴 활용)
type NoticeParser struct {
	// 5가지 상장공고 패턴 (flash-upbit에서 검증된 패턴들)
	patternMulti       *regexp.Regexp // Pattern 1: 셀레스티아(TIA)(KRW, BTC, USDT 마켓)
	patternCoins       *regexp.Regexp // Pattern 2: coins with parenthesis markets
	patternMarketParen *regexp.Regexp // Pattern 2: (BTC, USDT 마켓)
	patternMarketOut   *regexp.Regexp // Pattern 3: KRW, USDT 마켓 (outside parenthesis)
	patternSingle      *regexp.Regexp // Pattern 4: 봉크(BONK) KRW 마켓
	patternCoinList    *regexp.Regexp // Pattern 5: USDT 마켓 디지털 자산 추가 (AGLD, AHT, ...)
}

// NewNoticeParser는 새로운 공지 파서를 생성하고 정규식 패턴들을 컴파일
func NewNoticeParser() *NoticeParser {
	return &NoticeParser{
		// Pattern 1: multiple coin(market), e.g. 셀레스티아(TIA)(KRW, BTC, USDT 마켓), 아이오넷(IO)(BTC, USDT 마켓)
		patternMulti: regexp.MustCompile(`([가-힣A-Za-z0-9]+)\(([A-Z0-9]+)\)\(([^)]+) 마켓\)`),

		// Pattern 2: coins ... 신규 거래지원 안내 (BTC, USDT 마켓)
		patternCoins:       regexp.MustCompile(`([가-힣A-Za-z0-9]+)\(([A-Z0-9]+)\)`),
		patternMarketParen: regexp.MustCompile(`\(([A-Z, ]+) 마켓\)`),

		// Pattern 3: coins ... KRW, USDT 마켓 디지털 자산 추가 (market outside parenthesis)
		patternMarketOut: regexp.MustCompile(`([A-Z, ]+) 마켓`),

		// Pattern 4: single coin, single market (e.g. 봉크(BONK) KRW 마켓 디지털 자산 추가)
		patternSingle: regexp.MustCompile(`([가-힣A-Za-z0-9]+)\(([A-Z0-9]+)\) ([A-Z, ]+) 마켓`),

		// Pattern 5: market + coin-list in parenthesis
		patternCoinList: regexp.MustCompile(`(KRW|BTC|USDT) 마켓 디지털 자산 추가 \(([^)]+)\)`),
	}
}

// ParseListings는 업비트 공지사항 제목에서 상장 정보를 파싱 (flash-upbit 로직 이식)
func (p *NoticeParser) ParseListings(title string) []UpbitListing {
	listings := []UpbitListing{}

	// Pattern 1: multiple coin(market), e.g. 셀레스티아(TIA)(KRW, BTC, USDT 마켓)
	multiMatches := p.patternMulti.FindAllStringSubmatch(title, -1)
	if len(multiMatches) > 0 {
		for _, m := range multiMatches {
			listings = append(listings, UpbitListing{
				Symbol:  m[2],                 // TIA
				Markets: p.cleanMarkets(m[3]), // KRW, BTC, USDT
			})
		}
		return listings
	}

	// Pattern 2: coins ... 신규 거래지원 안내 (BTC, USDT 마켓)
	coinMatches := p.patternCoins.FindAllStringSubmatch(title, -1)
	marketParenMatch := p.patternMarketParen.FindStringSubmatch(title)
	if len(coinMatches) > 0 && marketParenMatch != nil {
		markets := p.cleanMarkets(marketParenMatch[1])
		for _, c := range coinMatches {
			listings = append(listings, UpbitListing{
				Symbol:  c[2],
				Markets: markets,
			})
		}
		return listings
	}

	// Pattern 3: coins ... KRW, USDT 마켓 디지털 자산 추가 (market outside parenthesis)
	marketOutsideMatch := p.patternMarketOut.FindStringSubmatch(title)
	if len(coinMatches) > 0 && marketOutsideMatch != nil {
		markets := p.cleanMarkets(marketOutsideMatch[1])
		for _, c := range coinMatches {
			listings = append(listings, UpbitListing{
				Symbol:  c[2],
				Markets: markets,
			})
		}
		return listings
	}

	// Pattern 4: single coin, single market (e.g. 봉크(BONK) KRW 마켓 디지털 자산 추가)
	singleMatch := p.patternSingle.FindStringSubmatch(title)
	if len(singleMatch) == 4 {
		listings = append(listings, UpbitListing{
			Symbol:  singleMatch[2],
			Markets: p.cleanMarkets(singleMatch[3]),
		})
		return listings
	}

	// Pattern 5: market + coin-list in parenthesis
	// USDT 마켓 디지털 자산 추가 (AGLD, AHT, ARPA, ...)
	coinListMatch := p.patternCoinList.FindStringSubmatch(title)
	if len(coinListMatch) == 3 {
		market := coinListMatch[1]
		syms := strings.Split(coinListMatch[2], ",")
		for _, sym := range syms {
			s := strings.TrimSpace(sym)
			if s == "" || s == "..." {
				continue
			}
			listings = append(listings, UpbitListing{
				Symbol:  s,
				Markets: []string{market},
			})
		}
		return listings
	}

	// 어떤 패턴에도 매치되지 않음
	return listings
}

// cleanMarkets는 마켓 문자열을 정리하여 배열로 반환 (flash-upbit 헬퍼 함수)
func (p *NoticeParser) cleanMarkets(raw string) []string {
	raw = strings.ReplaceAll(raw, "마켓", "")
	raw = strings.ReplaceAll(raw, "(", "")
	raw = strings.ReplaceAll(raw, ")", "")
	raw = strings.ReplaceAll(raw, ",", " ")
	raw = strings.TrimSpace(raw)
	fields := strings.Fields(raw)
	return fields
}

// IsListingNotice는 제목이 상장공고인지 간단히 판단
func (p *NoticeParser) IsListingNotice(title string) bool {
	keywords := []string{
		"디지털 자산 추가",
		"거래 지원",
		"신규 거래지원",
		"마켓 추가",
		"상장",
	}

	for _, keyword := range keywords {
		if strings.Contains(title, keyword) {
			return true
		}
	}

	return false
}

// GetSupportedMarkets는 업비트에서 지원하는 마켓 목록을 반환
func (p *NoticeParser) GetSupportedMarkets() []string {
	return []string{"KRW", "BTC", "USDT"}
}

// ValidateSymbol은 심볼이 유효한 형식인지 검증
func (p *NoticeParser) ValidateSymbol(symbol string) bool {
	// 대문자와 숫자로만 구성, 2-10자리
	matched, _ := regexp.MatchString(`^[A-Z0-9]{2,10}$`, symbol)
	return matched
}

// ExtractSymbolsFromTitle은 제목에서 모든 가능한 심볼들을 추출 (디버깅용)
func (p *NoticeParser) ExtractSymbolsFromTitle(title string) []string {
	// 괄호 안의 대문자 패턴들을 모두 추출
	re := regexp.MustCompile(`\(([A-Z0-9]+)\)`)
	matches := re.FindAllStringSubmatch(title, -1)

	symbols := make([]string, 0)
	for _, match := range matches {
		if len(match) > 1 && p.ValidateSymbol(match[1]) {
			symbols = append(symbols, match[1])
		}
	}

	return symbols
}
