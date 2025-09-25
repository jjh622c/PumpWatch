package models

import "time"

// ListingMetadata는 업비트 상장공고 메타데이터
type ListingMetadata struct {
	Symbol          string `json:"symbol"`           // 상장 심볼 (예: "TIA")
	ListingTime     int64  `json:"listing_time"`     // 상장공고 시점 (Unix milliseconds)
	CollectionStart int64  `json:"collection_start"` // 수집 시작 시점 (-20초)
	CollectionEnd   int64  `json:"collection_end"`   // 수집 종료 시점 (상장공고 시점)

	// 업비트 상장 정보
	Markets     []string `json:"markets"`      // 상장 마켓 ["KRW", "BTC", "USDT"]
	NoticeTitle string   `json:"notice_title"` // 공지사항 제목
	NoticeURL   string   `json:"notice_url"`   // 공지사항 URL

	// 수집 통계
	Exchanges      map[string]ExchangeStats `json:"exchanges"`       // 거래소별 수집 통계
	TotalTrades    int                      `json:"total_trades"`    // 총 체결 건수
	CollectionTime int64                    `json:"collection_time"` // 실제 수집 소요 시간 (ms)

	// 메타데이터
	CreatedAt   time.Time `json:"created_at"`   // 수집 시작 시간
	CompletedAt time.Time `json:"completed_at"` // 수집 완료 시간
	Version     string    `json:"version"`      // PumpWatch 버전
}

// ExchangeStats는 거래소별 수집 통계
type ExchangeStats struct {
	SpotTrades    int   `json:"spot_trades"`    // 현물 체결 수
	FuturesTrades int   `json:"futures_trades"` // 선물 체결 수
	FirstTrade    int64 `json:"first_trade"`    // 첫 체결 시간 (Unix milliseconds)
	LastTrade     int64 `json:"last_trade"`     // 마지막 체결 시간 (Unix milliseconds)
	Connected     bool  `json:"connected"`      // 연결 성공 여부

	// 연결 품질 정보
	ConnectionTime  int64   `json:"connection_time"`  // 연결 소요 시간 (ms)
	DisconnectCount int     `json:"disconnect_count"` // 연결 끊김 횟수
	ReconnectCount  int     `json:"reconnect_count"`  // 재연결 횟수
	DataQuality     float64 `json:"data_quality"`     // 데이터 품질 점수 (0-1)
	LatencyAvg      float64 `json:"latency_avg"`      // 평균 지연시간 (ms)
	LatencyMax      float64 `json:"latency_max"`      // 최대 지연시간 (ms)
}

// NewListingMetadata는 새로운 상장공고 메타데이터를 생성
func NewListingMetadata(symbol string, listingTime int64) *ListingMetadata {
	return &ListingMetadata{
		Symbol:          symbol,
		ListingTime:     listingTime,
		CollectionStart: listingTime - 20000, // -20초 (milliseconds)
		CollectionEnd:   listingTime,
		Markets:         make([]string, 0),
		Exchanges:       make(map[string]ExchangeStats),
		CreatedAt:       time.Now(),
		Version:         "PumpWatch-v2.0",
	}
}

// AddExchangeStats는 거래소 통계를 추가
func (lm *ListingMetadata) AddExchangeStats(exchange string, stats ExchangeStats) {
	lm.Exchanges[exchange] = stats
	lm.TotalTrades += stats.SpotTrades + stats.FuturesTrades
}

// Complete는 수집 완료 처리
func (lm *ListingMetadata) Complete() {
	lm.CompletedAt = time.Now()
	lm.CollectionTime = lm.CompletedAt.UnixMilli() - lm.CreatedAt.UnixMilli()
}

// GetCollectionDuration은 실제 수집 소요 시간(초)을 반환
func (lm *ListingMetadata) GetCollectionDuration() float64 {
	if lm.CompletedAt.IsZero() {
		return 0
	}
	return lm.CompletedAt.Sub(lm.CreatedAt).Seconds()
}

// GetSuccessfulExchanges는 성공적으로 연결된 거래소 목록을 반환
func (lm *ListingMetadata) GetSuccessfulExchanges() []string {
	successful := make([]string, 0)
	for exchange, stats := range lm.Exchanges {
		if stats.Connected && (stats.SpotTrades > 0 || stats.FuturesTrades > 0) {
			successful = append(successful, exchange)
		}
	}
	return successful
}

// GetDataQualityScore는 전체 데이터 품질 점수를 계산
func (lm *ListingMetadata) GetDataQualityScore() float64 {
	if len(lm.Exchanges) == 0 {
		return 0
	}

	totalScore := 0.0
	for _, stats := range lm.Exchanges {
		totalScore += stats.DataQuality
	}

	return totalScore / float64(len(lm.Exchanges))
}

// AnalysisMetadata는 펌프 분석 메타데이터
type AnalysisMetadata struct {
	Symbol          string `json:"symbol"`
	OriginalData    string `json:"original_data"`    // Raw 데이터 경로
	AnalysisVersion string `json:"analysis_version"` // 분석기 버전

	// 분석 설정
	PumpThreshold float64 `json:"pump_threshold"` // 펌프 임계값 (%)
	TimeWindow    int64   `json:"time_window"`    // 분석 시간 윈도우 (ms)
	MinDuration   int64   `json:"min_duration"`   // 최소 펌프 지속시간 (ms)

	// 분석 결과 요약
	TotalPumpsFound int `json:"total_pumps_found"`
	ExchangeCount   int `json:"exchange_count"` // 분석한 거래소 수
	MarketCount     int `json:"market_count"`   // 분석한 마켓 수

	// 시간 정보
	AnalysisStart  time.Time `json:"analysis_start"`
	AnalysisEnd    time.Time `json:"analysis_end"`
	ProcessingTime int64     `json:"processing_time"` // 처리 시간 (ms)
}
