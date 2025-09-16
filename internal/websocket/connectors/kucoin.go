package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"PumpWatch/internal/models"
)

// KuCoinConnector는 쿠코인 WebSocket 연결자
type KuCoinConnector struct {
	BaseConnector
	token        string
	pingInterval int64
	connectId    string
}

// KuCoinTokenResponse는 토큰 API 응답 구조
type KuCoinTokenResponse struct {
	Code string `json:"code"`
	Data struct {
		Token           string `json:"token"`
		InstanceServers []struct {
			Endpoint     string `json:"endpoint"`
			Encrypt      bool   `json:"encrypt"`
			Protocol     string `json:"protocol"`
			PingInterval int64  `json:"pingInterval"`
			PingTimeout  int64  `json:"pingTimeout"`
		} `json:"instanceServers"`
	} `json:"data"`
}

// NewKuCoinConnector는 새로운 쿠코인 Connector 생성
func NewKuCoinConnector(marketType string, maxSymbols int) WebSocketConnector {
	return &KuCoinConnector{
		BaseConnector: BaseConnector{
			Exchange:   "kucoin",
			MarketType: marketType,
			Endpoint:   "", // 동적으로 설정됨
			MaxSymbols: maxSymbols,
		},
	}
}

// getToken은 KuCoin WebSocket 토큰을 획득
func (kc *KuCoinConnector) getToken() (*KuCoinTokenResponse, error) {
	var apiURL string
	if kc.MarketType == "spot" {
		apiURL = "https://api.kucoin.com/api/v1/bullet-public"
	} else {
		apiURL = "https://api-futures.kucoin.com/api/v1/bullet-public"
	}

	resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return nil, fmt.Errorf("토큰 요청 실패: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("응답 읽기 실패: %v", err)
	}

	var tokenResp KuCoinTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
	}

	if tokenResp.Code != "200000" {
		return nil, fmt.Errorf("토큰 획득 실패: %s", tokenResp.Code)
	}

	return &tokenResp, nil
}

// Connect는 WebSocket 연결
func (kc *KuCoinConnector) Connect(ctx context.Context, symbols []string) error {
	// 1. 토큰 획득
	tokenResp, err := kc.getToken()
	if err != nil {
		return fmt.Errorf("토큰 획득 실패: %v", err)
	}

	if len(tokenResp.Data.InstanceServers) == 0 {
		return fmt.Errorf("사용 가능한 WebSocket 서버가 없음")
	}

	// 2. WebSocket URL 구성
	server := tokenResp.Data.InstanceServers[0] // 첫 번째 서버 사용
	connectId := fmt.Sprintf("%d", time.Now().UnixNano())
	wsURL := fmt.Sprintf("%s?token=%s&connectId=%s",
		server.Endpoint, tokenResp.Data.Token, connectId)

	kc.token = tokenResp.Data.Token
	kc.pingInterval = server.PingInterval
	kc.connectId = connectId
	kc.Endpoint = wsURL

	// 3. WebSocket 연결
	if err := kc.connectWebSocket(wsURL); err != nil {
		return fmt.Errorf("쿠코인 WebSocket 연결 실패: %v", err)
	}

	// 4. KuCoin 전용 ping 루프 시작 (서버에서 받은 interval 사용)
	kc.startKuCoinPingLoop(ctx)

	// 5. 심볼 구독
	if len(symbols) > 0 {
		if err := kc.Subscribe(symbols); err != nil {
			kc.Disconnect()
			return fmt.Errorf("구독 실패: %v", err)
		}
	}

	return nil
}

// startKuCoinPingLoop은 KuCoin 전용 ping 루프
func (kc *KuCoinConnector) startKuCoinPingLoop(ctx context.Context) {
	if kc.pingInterval <= 0 {
		kc.pingInterval = 18000 // 기본 18초
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				if kc.logger != nil {
					kc.logger.Error("KuCoin ping loop panic: %v", r)
				}
			}
		}()

		ticker := time.NewTicker(time.Duration(kc.pingInterval) * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !kc.IsConnected() {
					continue
				}

				pingMsg := map[string]interface{}{
					"id":   fmt.Sprintf("%d", time.Now().UnixMilli()),
					"type": "ping",
				}

				if err := kc.sendMessage(pingMsg); err != nil {
					if kc.logger != nil {
						kc.logger.Warn("KuCoin ping 전송 실패: %v", err)
					}
					if kc.OnError != nil {
						kc.OnError(err)
					}
				}
			}
		}
	}()
}

// Subscribe는 심볼 구독
func (kc *KuCoinConnector) Subscribe(symbols []string) error {
	if !kc.IsConnected() {
		return fmt.Errorf("연결되지 않음")
	}

	if len(kc.SubscribedSymbols)+len(symbols) > kc.MaxSymbols {
		return fmt.Errorf("최대 구독 개수 초과: %d/%d",
			len(kc.SubscribedSymbols)+len(symbols), kc.MaxSymbols)
	}

	// KuCoin 형식으로 토픽 생성 및 구독
	for _, symbol := range symbols {
		formattedSymbol := formatSymbol(symbol, "kucoin", kc.MarketType)

		var topic string
		if kc.MarketType == "spot" {
			// KuCoin Spot: 실제 거래 매칭 데이터를 위한 올바른 채널
			topic = fmt.Sprintf("/market/match:%s", formattedSymbol)
		} else {
			// KuCoin Futures: 실제 체결 데이터를 위한 올바른 채널
			topic = fmt.Sprintf("/contractMarket/execution:%s", formattedSymbol)
		}

		// KuCoin 구독 메시지 (response=true 복원)
		subMessage := map[string]interface{}{
			"id":       time.Now().UnixMilli(),
			"type":     "subscribe",
			"topic":    topic,
			"response": true, // ACK 수신을 위해 필요
		}

		fmt.Printf("📡 KuCoin %s 구독 전송: topic=%s\n", kc.MarketType, topic)
		if err := kc.sendMessage(subMessage); err != nil {
			return fmt.Errorf("구독 메시지 전송 실패: %v", err)
		}
		fmt.Printf("✅ KuCoin %s 구독 메시지 전송 완료\n", kc.MarketType)
	}

	kc.SubscribedSymbols = append(kc.SubscribedSymbols, symbols...)
	fmt.Printf("📊 쿠코인 %s 구독: %d개 심볼\n", kc.MarketType, len(symbols))
	return nil
}

// Unsubscribe는 심볼 구독 해제
func (kc *KuCoinConnector) Unsubscribe(symbols []string) error {
	// 구독 해제 구현
	for _, symbol := range symbols {
		formattedSymbol := formatSymbol(symbol, "kucoin", kc.MarketType)

		var topic string
		if kc.MarketType == "spot" {
			// KuCoin Spot: 실제 거래 매칭 데이터를 위한 올바른 채널
			topic = fmt.Sprintf("/market/match:%s", formattedSymbol)
		} else {
			// KuCoin Futures: 실제 체결 데이터를 위한 올바른 채널
			topic = fmt.Sprintf("/contractMarket/execution:%s", formattedSymbol)
		}

		unsubMessage := map[string]interface{}{
			"id":             fmt.Sprintf("%d", time.Now().UnixMilli()),
			"type":           "unsubscribe",
			"topic":          topic,
			"privateChannel": false,
			"response":       true,
		}

		kc.sendMessage(unsubMessage)
	}

	// 구독 목록에서 제거
	for _, symbol := range symbols {
		for i, subscribed := range kc.SubscribedSymbols {
			if subscribed == symbol {
				kc.SubscribedSymbols = append(kc.SubscribedSymbols[:i], kc.SubscribedSymbols[i+1:]...)
				break
			}
		}
	}

	return nil
}

// StartMessageLoop는 메시지 수신 루프
func (kc *KuCoinConnector) StartMessageLoop(ctx context.Context, messageChan chan<- models.TradeEvent) error {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("❌ 쿠코인 메시지 루프 패닉: %v\n", r)
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			default:
				if !kc.IsConnected() {
					time.Sleep(1 * time.Second)
					continue
				}

				message, err := kc.readMessage()
				if err != nil {
					if kc.OnError != nil {
						kc.OnError(err)
					}
					time.Sleep(1 * time.Second)
					continue
				}

				tradeEvent, err := kc.parseTradeMessage(message)
				if err != nil {
					continue
				}

				select {
				case messageChan <- tradeEvent:
				default:
				}
			}
		}
	}()

	return nil
}

// ParseTradeMessage implements the interface method for trade message parsing
func (kc *KuCoinConnector) ParseTradeMessage(data []byte) ([]models.TradeEvent, error) {
	tradeEvent, err := kc.parseTradeMessage(data)
	if err != nil {
		return nil, err
	}
	return []models.TradeEvent{tradeEvent}, nil
}

// parseTradeMessage는 쿠코인 거래 메시지 파싱
func (kc *KuCoinConnector) parseTradeMessage(data []byte) (models.TradeEvent, error) {
	var message struct {
		Type    string `json:"type"`
		Topic   string `json:"topic"`
		Subject string `json:"subject"`
		Data    struct {
			Symbol       string `json:"symbol"`
			Price        string `json:"price"`
			Size         string `json:"size"`
			Side         string `json:"side"`
			TakerOrderId string `json:"takerOrderId"`
			MakerOrderId string `json:"makerOrderId"`
			TradeId      string `json:"tradeId"`
			Time         string `json:"time"` // KuCoin은 나노초 단위 문자열
			Sequence     string `json:"sequence"`
		} `json:"data"`
		Id string `json:"id"`
	}

	if err := json.Unmarshal(data, &message); err != nil {
		return models.TradeEvent{}, fmt.Errorf("JSON 파싱 실패: %v", err)
	}

	// 🔍 DEBUG: 모든 KuCoin 메시지 로깅 (문제 해결용)
	fmt.Printf("🔍 KuCoin %s 메시지: type=%s, topic=%s, subject=%s\n",
		kc.MarketType, message.Type, message.Topic, message.Subject)

	// ACK 메시지 상세 분석 (구독 성공/실패 확인)
	if message.Type == "ack" {
		fmt.Printf("📋 KuCoin %s ACK: id=%s, topic=%s\n", kc.MarketType, message.Id, message.Topic)
		return models.TradeEvent{}, fmt.Errorf("구독 확인 메시지")
	}

	// Pong 메시지나 Welcome 메시지는 무시
	if message.Type == "pong" || message.Type == "welcome" {
		return models.TradeEvent{}, fmt.Errorf("제어 메시지")
	}

	// 거래 메시지인지 확인
	if message.Type != "message" {
		fmt.Printf("🔧 KuCoin %s: 거래 메시지가 아님 (type: %s)\n", kc.MarketType, message.Type)
		return models.TradeEvent{}, fmt.Errorf("거래 메시지 아님")
	}

	// Topic과 Subject 확인 (KuCoin 스펙에 따라)
	var isValidMessage bool
	if kc.MarketType == "spot" {
		// Spot: topic="/market/match:SYMBOL", subject="trade.l3match"
		isValidMessage = strings.Contains(message.Topic, "/market/match:") &&
						strings.Contains(message.Subject, "trade")
	} else {
		// Futures: topic="/contractMarket/execution:SYMBOL", subject="match"
		isValidMessage = strings.Contains(message.Topic, "/contractMarket/execution:") &&
						strings.Contains(message.Subject, "match")
	}

	if !isValidMessage {
		fmt.Printf("🔧 KuCoin %s: 유효하지 않은 거래 메시지 - topic: %s, subject: %s\n",
			kc.MarketType, message.Topic, message.Subject)
		return models.TradeEvent{}, fmt.Errorf("유효하지 않은 거래 메시지: topic=%s, subject=%s",
			message.Topic, message.Subject)
	}

	// 시간 변환 (나노초 문자열을 밀리초로)
	var timestamp int64
	if message.Data.Time != "" {
		// KuCoin의 time은 나노초 단위 문자열
		if timeNano := message.Data.Time; len(timeNano) > 13 {
			timeNano = timeNano[:13] // 밀리초만 사용
		}
		timestamp = parseTimestamp(message.Data.Time)
	}

	return models.TradeEvent{
		Exchange:   "kucoin",
		MarketType: kc.MarketType,
		Symbol:     normalizeSymbol(message.Data.Symbol),
		Price:      message.Data.Price,
		Quantity:   message.Data.Size,
		Side:       strings.ToLower(message.Data.Side),
		TradeID:    message.Data.TradeId,
		Timestamp:  timestamp,
	}, nil
}

// parseTimestamp는 KuCoin의 나노초 타임스탬프를 밀리초로 변환
func parseTimestamp(timeStr string) int64 {
	if len(timeStr) > 13 {
		timeStr = timeStr[:13] // 밀리초 단위로 자름
	}

	if timestamp := parseInt64(timeStr); timestamp > 0 {
		return timestamp
	}

	return time.Now().UnixMilli()
}

// parseInt64는 문자열을 int64로 변환 (에러 시 0 반환)
func parseInt64(s string) int64 {
	if val, err := json.Number(s).Int64(); err == nil {
		return val
	}
	return 0
}

