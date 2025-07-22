package sync

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// UpbitMarket 업비트 마켓 정보
type UpbitMarket struct {
	Market        string `json:"market"`         // 마켓 ID (예: KRW-BTC)
	KoreanName    string `json:"korean_name"`    // 한글명
	EnglishName   string `json:"english_name"`   // 영문명
	MarketWarning string `json:"market_warning"` // 유의종목 여부
}

// UpbitAPI 업비트 API 클라이언트
type UpbitAPI struct {
	baseURL    string
	httpClient *http.Client
}

// NewUpbitAPI 새 업비트 API 클라이언트 생성
func NewUpbitAPI() *UpbitAPI {
	return &UpbitAPI{
		baseURL: "https://api.upbit.com",
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetKRWMarkets 업비트 KRW 마켓 목록 조회
func (u *UpbitAPI) GetKRWMarkets() ([]string, error) {
	url := fmt.Sprintf("%s/v1/market/all", u.baseURL)

	resp, err := u.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("업비트 API 요청 실패: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("업비트 API 응답 오류: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("응답 데이터 읽기 실패: %v", err)
	}

	var markets []UpbitMarket
	if err := json.Unmarshal(body, &markets); err != nil {
		return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
	}

	// KRW 마켓만 필터링하고 심볼명만 추출
	var krwSymbols []string
	for _, market := range markets {
		if strings.HasPrefix(market.Market, "KRW-") {
			// "KRW-BTC" -> "BTC"
			symbol := strings.TrimPrefix(market.Market, "KRW-")
			krwSymbols = append(krwSymbols, symbol)
		}
	}

	return krwSymbols, nil
}

// GetKRWMarketsWithDetails 업비트 KRW 마켓 상세 정보 조회
func (u *UpbitAPI) GetKRWMarketsWithDetails() ([]UpbitMarket, error) {
	url := fmt.Sprintf("%s/v1/market/all", u.baseURL)

	resp, err := u.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("업비트 API 요청 실패: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("업비트 API 응답 오류: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("응답 데이터 읽기 실패: %v", err)
	}

	var markets []UpbitMarket
	if err := json.Unmarshal(body, &markets); err != nil {
		return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
	}

	// KRW 마켓만 필터링
	var krwMarkets []UpbitMarket
	for _, market := range markets {
		if strings.HasPrefix(market.Market, "KRW-") {
			krwMarkets = append(krwMarkets, market)
		}
	}

	return krwMarkets, nil
}

// GetKRWSymbols 업비트 KRW 마켓 심볼명만 조회 (GetKRWMarkets와 유사하지만 심볼명만 반환)
func (u *UpbitAPI) GetKRWSymbols() ([]string, error) {
	url := fmt.Sprintf("%s/v1/market/all", u.baseURL)

	resp, err := u.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("업비트 API 요청 실패: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("업비트 API 응답 오류: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("응답 데이터 읽기 실패: %v", err)
	}

	var markets []UpbitMarket
	if err := json.Unmarshal(body, &markets); err != nil {
		return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
	}

	// KRW 마켓만 필터링하고 심볼명만 추출
	var krwSymbols []string
	for _, market := range markets {
		if strings.HasPrefix(market.Market, "KRW-") {
			// "KRW-BTC" -> "BTC"
			symbol := strings.TrimPrefix(market.Market, "KRW-")
			krwSymbols = append(krwSymbols, symbol)
		}
	}

	return krwSymbols, nil
}
