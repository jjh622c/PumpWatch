package models

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// GetCurrentUnixMilli는 현재 시간을 Unix milliseconds로 반환
func GetCurrentUnixMilli() int64 {
	return time.Now().UnixMilli()
}

// parseFloat는 string을 float64로 변환 (에러 시 0 반환)
func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0.0
	}
	return f
}

// formatFloat는 float64를 string으로 변환 (소수점 8자리)
func formatFloat(f float64) string {
	return fmt.Sprintf("%.8f", f)
}

// calculatePercentChange는 두 가격 간의 변동률을 계산
func calculatePercentChange(startPrice, endPrice string) string {
	start := parseFloat(startPrice)
	end := parseFloat(endPrice)
	
	if start == 0 {
		return "0.00"
	}
	
	change := ((end - start) / start) * 100
	return fmt.Sprintf("%.2f", change)
}

// normalizeSymbol 관련 헬퍼 함수들은 이미 trade_event.go에서 import 필요
// 여기서는 중복 정의 방지를 위해 제거하고 필요시 trade_event.go에서 import

// TimeRangeCheck는 시간 범위 체크를 위한 헬퍼
type TimeRangeCheck struct {
	StartTime int64
	EndTime   int64
}

// NewTimeRangeCheck는 시간 범위 체커를 생성
func NewTimeRangeCheck(startTime, endTime int64) *TimeRangeCheck {
	return &TimeRangeCheck{
		StartTime: startTime,
		EndTime:   endTime,
	}
}

// IsInRange는 주어진 시간이 범위 내에 있는지 확인
func (trc *TimeRangeCheck) IsInRange(timestamp int64) bool {
	return timestamp >= trc.StartTime && timestamp <= trc.EndTime
}

// GetDurationMs는 범위의 지속시간을 milliseconds로 반환
func (trc *TimeRangeCheck) GetDurationMs() int64 {
	return trc.EndTime - trc.StartTime
}

// GetDurationSeconds는 범위의 지속시간을 초로 반환
func (trc *TimeRangeCheck) GetDurationSeconds() float64 {
	return float64(trc.GetDurationMs()) / 1000.0
}

// FormatTimestamp는 Unix milliseconds를 읽기 쉬운 형식으로 변환
func FormatTimestamp(timestamp int64) string {
	t := time.Unix(timestamp/1000, (timestamp%1000)*1000000)
	return t.Format("2006-01-02 15:04:05.000")
}

// ParseTimestamp는 문자열을 Unix milliseconds로 변환
func ParseTimestamp(timeStr string) (int64, error) {
	t, err := time.Parse("2006-01-02 15:04:05.000", timeStr)
	if err != nil {
		return 0, err
	}
	return t.UnixMilli(), nil
}

// GenerateFilePath는 데이터 파일 경로를 생성
func GenerateFilePath(symbol string, timestamp time.Time, dataType string) string {
	dateStr := timestamp.Format("20060102_150405")
	return fmt.Sprintf("%s_%s", symbol, dateStr)
}

// GetExchangeList는 지원되는 거래소 목록을 반환
func GetExchangeList() []string {
	return []string{"binance", "okx", "kucoin", "phemex", "gate", "bybit"}
}

// GetMarketTypes는 지원되는 마켓 타입 목록을 반환
func GetMarketTypes() []string {
	return []string{"spot", "futures"}
}

// IsValidExchange는 유효한 거래소인지 확인
func IsValidExchange(exchange string) bool {
	exchanges := GetExchangeList()
	for _, e := range exchanges {
		if e == exchange {
			return true
		}
	}
	return false
}

// IsValidMarketType는 유효한 마켓 타입인지 확인
func IsValidMarketType(marketType string) bool {
	types := GetMarketTypes()
	for _, t := range types {
		if t == marketType {
			return true
		}
	}
	return false
}

// CleanSymbol은 심볼을 정리 (대문자, 공백 제거 등)
func CleanSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

// ExtractBaseQuote는 심볼에서 base와 quote asset을 추출
func ExtractBaseQuote(symbol string) (base, quote string) {
	symbol = CleanSymbol(symbol)
	
	// 일반적인 USDT, USDC, BTC 등의 패턴으로 분리 시도
	quotes := []string{"USDT", "USDC", "BUSD", "BTC", "ETH", "BNB", "USD", "EUR"}
	
	for _, q := range quotes {
		if strings.HasSuffix(symbol, q) {
			base = symbol[:len(symbol)-len(q)]
			quote = q
			return
		}
	}
	
	// 패턴 매칭 실패시 기본값 반환
	if len(symbol) > 3 {
		return symbol[:len(symbol)-3], symbol[len(symbol)-3:]
	}
	
	return symbol, ""
}

// CalculateSpread는 bid-ask 스프레드를 계산
func CalculateSpread(bid, ask string) string {
	bidPrice := parseFloat(bid)
	askPrice := parseFloat(ask)
	
	if bidPrice == 0 || askPrice == 0 {
		return "0.00"
	}
	
	spread := askPrice - bidPrice
	spreadPercent := (spread / bidPrice) * 100
	
	return fmt.Sprintf("%.4f", spreadPercent)
}

// GetMemoryUsageEstimate는 예상 메모리 사용량을 계산 (bytes)
func GetMemoryUsageEstimate(tradeCount int) int64 {
	// TradeEvent 구조체 크기 추정: ~200 bytes per trade
	const tradeEventSize = 200
	
	// 12개 거래소 * 마켓 조합에서 각각 tradeCount만큼
	totalTrades := tradeCount * 12
	
	// 기본 구조체 오버헤드 + 실제 데이터
	overhead := int64(1024 * 1024) // 1MB 오버헤드
	dataSize := int64(totalTrades * tradeEventSize)
	
	return overhead + dataSize
}

// FormatMemorySize는 바이트 수를 읽기 쉬운 형식으로 변환
func FormatMemorySize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}