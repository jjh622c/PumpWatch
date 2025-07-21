package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"noticepumpcatch/internal/memory"
)

// Manager 알림 관리자
type Manager struct {
	mu                sync.RWMutex
	slackWebhook      string
	telegramToken     string
	telegramChatID    string
	notificationLevel string // INFO/WARNING/CRITICAL
	enabled           bool
	rateLimit         time.Duration
	lastNotification  time.Time
}

// Notification 알림 메시지
type Notification struct {
	Level     string                 `json:"level"` // INFO/WARNING/CRITICAL
	Title     string                 `json:"title"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data"`
	Timestamp time.Time              `json:"timestamp"`
}

// NewManager 알림 관리자 생성
func NewManager(slackWebhook, telegramToken, telegramChatID string) *Manager {
	return &Manager{
		slackWebhook:     slackWebhook,
		telegramToken:    telegramToken,
		telegramChatID:   telegramChatID,
		enabled:          true,
		rateLimit:        30 * time.Second,             // 30초 간격 제한
		lastNotification: time.Now().Add(-time.Minute), // 초기값
	}
}

// SendNotification 알림 전송
func (nm *Manager) SendNotification(notification *Notification) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if !nm.enabled {
		return nil
	}

	// 레이트 리밋 확인
	if time.Since(nm.lastNotification) < nm.rateLimit {
		return fmt.Errorf("레이트 리밋: %v", nm.rateLimit)
	}

	// 슬랙 알림
	if nm.slackWebhook != "" {
		if err := nm.sendSlackNotification(notification); err != nil {
			log.Printf("❌ 슬랙 알림 실패: %v", err)
		}
	}

	// 텔레그램 알림
	if nm.telegramToken != "" && nm.telegramChatID != "" {
		if err := nm.sendTelegramNotification(notification); err != nil {
			log.Printf("❌ 텔레그램 알림 실패: %v", err)
		}
	}

	nm.lastNotification = time.Now()
	return nil
}

// sendSlackNotification 슬랙 알림 전송
func (nm *Manager) sendSlackNotification(notification *Notification) error {
	// 슬랙 메시지 포맷
	slackMessage := map[string]interface{}{
		"text": notification.Title,
		"attachments": []map[string]interface{}{
			{
				"color":     nm.getColorByLevel(notification.Level),
				"title":     notification.Title,
				"text":      notification.Message,
				"fields":    nm.formatSlackFields(notification.Data),
				"timestamp": notification.Timestamp.Unix(),
			},
		},
	}

	jsonData, err := json.Marshal(slackMessage)
	if err != nil {
		return err
	}

	resp, err := http.Post(nm.slackWebhook, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("슬랙 API 오류: %d", resp.StatusCode)
	}

	log.Printf("📤 슬랙 알림 전송: %s", notification.Title)
	return nil
}

// sendTelegramNotification 텔레그램 알림 전송
func (nm *Manager) sendTelegramNotification(notification *Notification) error {
	// 텔레그램 메시지 포맷
	message := fmt.Sprintf("*%s*\n%s\n\n%s",
		notification.Title,
		notification.Message,
		nm.formatTelegramData(notification.Data))

	// 텔레그램 Bot API URL
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", nm.telegramToken)

	telegramMessage := map[string]interface{}{
		"chat_id":    nm.telegramChatID,
		"text":       message,
		"parse_mode": "Markdown",
	}

	jsonData, err := json.Marshal(telegramMessage)
	if err != nil {
		return err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("텔레그램 API 오류: %d", resp.StatusCode)
	}

	log.Printf("📤 텔레그램 알림 전송: %s", notification.Title)
	return nil
}

// getColorByLevel 레벨별 색상 반환
func (nm *Manager) getColorByLevel(level string) string {
	switch level {
	case "INFO":
		return "#36a64f" // 녹색
	case "WARNING":
		return "#ff9500" // 주황색
	case "CRITICAL":
		return "#ff0000" // 빨간색
	default:
		return "#0000ff" // 파란색
	}
}

// formatSlackFields 슬랙 필드 포맷
func (nm *Manager) formatSlackFields(data map[string]interface{}) []map[string]interface{} {
	fields := make([]map[string]interface{}, 0)

	for key, value := range data {
		fields = append(fields, map[string]interface{}{
			"title": key,
			"value": fmt.Sprintf("%v", value),
			"short": true,
		})
	}

	return fields
}

// formatTelegramData 텔레그램 데이터 포맷
func (nm *Manager) formatTelegramData(data map[string]interface{}) string {
	if len(data) == 0 {
		return ""
	}

	result := "*상세 정보:*\n"
	for key, value := range data {
		result += fmt.Sprintf("• %s: %v\n", key, value)
	}

	return result
}

// SendPumpSignal 펌핑 시그널 알림
func (nm *Manager) SendPumpSignal(signal *memory.AdvancedPumpSignal) error {
	notification := &Notification{
		Level: "WARNING",
		Title: "🚀 펌핑 시그널 감지!",
		Message: fmt.Sprintf("심볼: %s\n거래소: %s\n종합 점수: %.2f",
			signal.Symbol, signal.Exchange, signal.CompositeScore),
		Timestamp: signal.Timestamp,
		Data: map[string]interface{}{
			"거래량 변화율": fmt.Sprintf("%.2f%%", signal.VolumeChange),
			"가격 변화율":  fmt.Sprintf("%.2f%%", signal.PriceChange),
			"거래대금":    fmt.Sprintf("%.2f USDT", signal.TradeAmount),
			"스프레드 변화": fmt.Sprintf("%.2f%%", signal.SpreadChange),
			"오더북 불균형": fmt.Sprintf("%.2f", signal.OrderBookImbalance),
			"권장 액션":   signal.Action,
		},
	}

	return nm.SendNotification(notification)
}

// SendSystemAlert 시스템 알림
func (nm *Manager) SendSystemAlert(level, title, message string, data map[string]interface{}) error {
	notification := &Notification{
		Level:     level,
		Title:     title,
		Message:   message,
		Timestamp: time.Now(),
		Data:      data,
	}

	return nm.SendNotification(notification)
}

// SendErrorAlert 에러 알림
func (nm *Manager) SendErrorAlert(err error, context string) error {
	notification := &Notification{
		Level:     "CRITICAL",
		Title:     "🚨 시스템 에러 발생",
		Message:   fmt.Sprintf("컨텍스트: %s\n에러: %v", context, err),
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"에러 타입": fmt.Sprintf("%T", err),
			"발생 시간": time.Now().Format("2006-01-02 15:04:05"),
		},
	}

	return nm.SendNotification(notification)
}

// SendPerformanceAlert 성능 알림
func (nm *Manager) SendPerformanceAlert(stats map[string]interface{}) error {
	notification := &Notification{
		Level:     "INFO",
		Title:     "📊 성능 리포트",
		Message:   "시스템 성능 현황",
		Timestamp: time.Now(),
		Data:      stats,
	}

	return nm.SendNotification(notification)
}

// GetLastNotificationTime 마지막 알림 시간 조회
func (nm *Manager) GetLastNotificationTime() time.Time {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return nm.lastNotification
}

// IsEnabled 활성화 상태 조회
func (nm *Manager) IsEnabled() bool {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return nm.enabled
}
