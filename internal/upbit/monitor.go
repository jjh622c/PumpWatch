package upbit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Monitor는 업비트 상장공고를 모니터링하는 단순화된 구조체
type Monitor struct {
	client    *http.Client
	apiURL    string
	lastCheck map[string]bool // 이미 처리된 공지 추적용
	parser    *NoticeParser

	// 콜백 함수
	onNewListing func(listing ListingEvent)
}

// ListingEvent는 새로운 상장 이벤트
type ListingEvent struct {
	Symbol       string    `json:"symbol"`        // 상장 심볼 (예: "TIA")
	Markets      []string  `json:"markets"`       // 상장 마켓 ["KRW", "BTC", "USDT"]
	Title        string    `json:"title"`         // 공지사항 제목
	ListedTime   time.Time `json:"listed_time"`   // 상장공고 시점
	DetectedTime time.Time `json:"detected_time"` // 감지 시점

	// 메타데이터
	NoticeURL   string `json:"notice_url"`    // 공지사항 URL
	IsNewNotice bool   `json:"is_new_notice"` // 신규 공지 여부
}

// NewMonitor는 새로운 업비트 모니터를 생성 (프록시 없는 단순 HTTP 클라이언트)
func NewMonitor() *Monitor {
	return &Monitor{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		apiURL:    "https://api-manager.upbit.com/api/v1/announcements?os=web&page=1&per_page=20&category=trade",
		lastCheck: make(map[string]bool),
		parser:    NewNoticeParser(),
	}
}

// SetNewListingCallback은 새로운 상장 감지시 호출할 콜백 설정
func (m *Monitor) SetNewListingCallback(callback func(ListingEvent)) {
	m.onNewListing = callback
}

// Start는 5초 간격으로 업비트 상장공고 모니터링 시작
func (m *Monitor) Start(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// 초기 체크
	if err := m.checkNotices(); err != nil {
		return fmt.Errorf("초기 공지 체크 실패: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := m.checkNotices(); err != nil {
				// 에러 로그만 찍고 계속 진행
				fmt.Printf("업비트 공지 체크 오류: %v\n", err)
			}
		}
	}
}

// checkNotices는 업비트 공지사항을 체크하고 새로운 상장 공지를 탐지 (flash-upbit 로직 이식)
func (m *Monitor) checkNotices() error {
	notices, err := m.fetchNotices()
	if err != nil {
		return fmt.Errorf("공지사항 조회 실패: %v", err)
	}

	// flash-upbit 로직: 가장 최신 공지만 체크
	if len(notices) == 0 {
		return nil
	}

	notice := notices[0] // 가장 최신 공지
	now := time.Now()

	// 이미 처리된 공지인지 확인
	if m.lastCheck[notice.Title] {
		return nil
	}

	// 상장 시간 파싱
	listedTime, err := m.parseListedTime(notice.ListedAt)
	if err != nil {
		// 시간 파싱 실패시 현재 시간으로 대체하고 계속 진행
		listedTime = now
	}

	// flash-upbit 핵심 로직: 15초 이내 + NewBadge + !UpdateBadge 체크
	if now.Sub(listedTime) < 15*time.Second {
		if notice.NewBadge && !notice.UpdateBadge {
			// 상장 공지인지 파싱 시도
			listings := m.parser.ParseListings(notice.Title)
			if len(listings) > 0 {
				// 새로운 상장 공지 발견!
				fmt.Printf("🚨 [UPBIT] 새로운 상장공지 감지!\n")
				fmt.Printf("  📋 제목: %s\n", notice.Title)
				fmt.Printf("  🕐 시간: %s (감지까지 %v)\n",
					listedTime.Format("15:04:05"),
					now.Sub(listedTime).Round(time.Second))

				m.lastCheck[notice.Title] = true

				// 각 심볼별로 이벤트 발생
				for _, listing := range listings {
					event := ListingEvent{
						Symbol:       listing.Symbol,
						Markets:      listing.Markets,
						Title:        notice.Title,
						ListedTime:   listedTime,
						DetectedTime: now,
						NoticeURL:    fmt.Sprintf("https://upbit.com/service_center/notice"), // 업비트 공지사항 페이지
						IsNewNotice:  notice.NewBadge,
					}

					fmt.Printf("  💎 심볼: %s (마켓: %v)\n", listing.Symbol, listing.Markets)

					// 콜백 호출
					if m.onNewListing != nil {
						m.onNewListing(event)
					}
				}
				return nil
			}
		}
	}

	// 상장공지가 아니거나 조건에 맞지 않으면 처리 완료로 마킹
	if !m.lastCheck[notice.Title] {
		m.lastCheck[notice.Title] = true
	}

	return nil
}

// fetchNotices는 업비트 API에서 공지사항 목록을 가져옴 (flash-upbit 로직 이식)
func (m *Monitor) fetchNotices() ([]UpbitNotice, error) {
	start := time.Now()
	ts := start.UnixNano()

	// flash-upbit과 동일한 캐시 무효화 로직
	url := fmt.Sprintf("%s?cb=%d_%d", m.apiURL, ts, start.UnixNano()%100000)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("요청 생성 실패: %v", err)
	}

	// flash-upbit과 동일한 헤더들 설정
	m.setRequestHeaders(req)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP 요청 실패: %v", err)
	}
	defer resp.Body.Close()

	// 429 Rate Limit 처리 (flash-upbit 로직)
	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("업비트 API Rate Limit (429)")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP 응답 오류: %d", resp.StatusCode)
	}

	var apiResponse UpbitAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, fmt.Errorf("JSON 디코딩 실패: %v", err)
	}

	if !apiResponse.Success {
		return nil, fmt.Errorf("API 응답 실패")
	}

	return apiResponse.Data.Notices, nil
}

// setRequestHeaders는 업비트 API 요청에 필요한 헤더들을 설정 (flash-upbit과 동일)
func (m *Monitor) setRequestHeaders(req *http.Request) {
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ko-KR,ko;q=0.8,en-US;q=0.5,en;q=0.3")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Host", "api-manager.upbit.com")
	req.Header.Set("Priority", "u=0, i")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Expires", "0")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:139.0) Gecko/20100101 Firefox/139.0")
}

// parseListedTime은 업비트의 상장 시간 문자열을 time.Time으로 파싱
func (m *Monitor) parseListedTime(listedAt string) (time.Time, error) {
	// 업비트 시간 형식에 맞게 파싱 (예: "2024-09-04T14:30:00+09:00")
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05+09:00",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, listedAt); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("시간 파싱 실패: %s", listedAt)
}

// GetLastCheckCount는 마지막 체크한 공지 수를 반환 (디버깅용)
func (m *Monitor) GetLastCheckCount() int {
	return len(m.lastCheck)
}

// ClearLastCheck는 마지막 체크 캐시를 지움 (재시작용)
func (m *Monitor) ClearLastCheck() {
	m.lastCheck = make(map[string]bool)
}

// 업비트 API 응답 구조체들 (실제 API 응답에 맞게 업데이트)
type UpbitNotice struct {
	Title       string `json:"title"`
	ListedAt    string `json:"first_listed_at"`
	NewBadge    bool   `json:"need_new_badge"`
	UpdateBadge bool   `json:"need_update_badge"`
	ID          int    `json:"id"`
	Category    string `json:"category"`
}

type UpbitAPIData struct {
	Notices []UpbitNotice `json:"notices"`
}

type UpbitAPIResponse struct {
	Success bool         `json:"success"`
	Data    UpbitAPIData `json:"data"`
}

// UpbitListing은 파싱된 상장 정보 (flash-upbit에서 가져옴)
type UpbitListing struct {
	Symbol  string   `json:"symbol"`
	Markets []string `json:"markets"`
}
