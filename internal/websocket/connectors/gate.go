package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"PumpWatch/internal/models"
)

// GateConnector는 게이트 WebSocket 연결자 (공식 API v4 기준 재구현)
type GateConnector struct {
	BaseConnector
	pingInterval time.Duration
}

// Gate.io 공식 API 응답 구조체들
type GateSubscriptionResponse struct {
	Time    int64  `json:"time"`
	Channel string `json:"channel"`
	Event   string `json:"event"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Result *struct {
		Status string `json:"status"`
	} `json:"result,omitempty"`
}

type GateTradeResponse struct {
	Time    int64  `json:"time"`
	Channel string `json:"channel"`
	Event   string `json:"event"`
	Result  struct {
		Id           int64  `json:"id"`           // Gate.io sends as number
		CreateTime   int64  `json:"create_time"`   // Gate.io sends as number
		CreateTimeMs string `json:"create_time_ms"` // This is still string with decimals
		Side         string `json:"side"`
		// Spot 필드들
		CurrencyPair string `json:"currency_pair,omitempty"`
		Amount       string `json:"amount,omitempty"`
		Price        string `json:"price,omitempty"`
		// Futures 필드들
		Contract string `json:"contract,omitempty"`
		Size     string `json:"size,omitempty"`
	} `json:"result"`
}

// NewGateConnector는 새로운 게이트 Connector 생성 (공식 API 기준)
func NewGateConnector(marketType string, maxSymbols int) WebSocketConnector {
	// 하드코딩된 엔드포인트 (하위 호환성을 위해 유지)
	var endpoint string
	if marketType == "spot" {
		endpoint = "wss://api.gateio.ws/ws/v4/"
	} else {
		// USDT Futures 사용 (가장 일반적)
		endpoint = "wss://fx-ws.gateio.ws/v4/ws/usdt"
	}
	return NewGateConnectorWithEndpoint(marketType, maxSymbols, endpoint)
}

// NewGateConnectorWithEndpoint는 엔드포인트를 지정하여 Connector 생성
func NewGateConnectorWithEndpoint(marketType string, maxSymbols int, endpoint string) WebSocketConnector {

	return &GateConnector{
		BaseConnector: BaseConnector{
			Exchange:   "gate",
			MarketType: marketType,
			Endpoint:   endpoint,
			MaxSymbols: maxSymbols,
		},
		pingInterval: 30 * time.Second, // 30초마다 ping
	}
}

// Connect는 WebSocket 연결 (공식 API 기준)
func (gc *GateConnector) Connect(ctx context.Context, symbols []string) error {
	// 1. WebSocket 연결
	if err := gc.connectWebSocket(gc.Endpoint); err != nil {
		return fmt.Errorf("게이트 WebSocket 연결 실패: %v", err)
	}

	// 2. Gate.io 전용 ping 루프 시작
	gc.startGatePingLoop(ctx)

	// 3. 심볼 구독 (있는 경우)
	if len(symbols) > 0 {
		if err := gc.Subscribe(symbols); err != nil {
			gc.Disconnect()
			return fmt.Errorf("구독 실패: %v", err)
		}
	}

	return nil
}

// startGatePingLoop는 Gate.io 전용 ping 루프
func (gc *GateConnector) startGatePingLoop(ctx context.Context) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				if gc.logger != nil {
					gc.logger.Error("Gate.io ping loop panic: %v", r)
				}
			}
		}()

		ticker := time.NewTicker(gc.pingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !gc.IsConnected() {
					continue
				}

				// Gate.io application-level ping
				pingMsg := map[string]interface{}{
					"time":    time.Now().Unix(),
					"channel": "spot.ping", // futures의 경우 "futures.ping"
					"event":   "subscribe",
				}

				if gc.MarketType == "futures" {
					pingMsg["channel"] = "futures.ping"
				}

				if err := gc.sendMessage(pingMsg); err != nil {
					if gc.logger != nil {
						gc.logger.Warn("Gate.io ping 전송 실패: %v", err)
					}
					if gc.OnError != nil {
						gc.OnError(err)
					}
				}
			}
		}
	}()
}

// Subscribe는 심볼 구독 (공식 API 기준 올바른 형식)
func (gc *GateConnector) Subscribe(symbols []string) error {
	if !gc.IsConnected() {
		return fmt.Errorf("연결되지 않음")
	}

	if len(gc.SubscribedSymbols)+len(symbols) > gc.MaxSymbols {
		return fmt.Errorf("최대 구독 개수 초과: %d/%d",
			len(gc.SubscribedSymbols)+len(symbols), gc.MaxSymbols)
	}

	// Gate.io는 각 심볼마다 별도 구독 메시지 필요
	for _, symbol := range symbols {
		formattedSymbol := formatSymbol(symbol, "gate", gc.MarketType)

		var channel string
		if gc.MarketType == "spot" {
			channel = "spot.trades"
		} else {
			channel = "futures.trades"
		}

		// 공식 API 형식: payload 배열에 심볼 지정
		subMessage := map[string]interface{}{
			"time":    time.Now().Unix(),
			"channel": channel,
			"event":   "subscribe",
			"payload": []string{formattedSymbol},
		}

		fmt.Printf("📡 Gate %s 구독 전송: channel=%s, symbol=%s, payload=%v\n",
			gc.MarketType, channel, formattedSymbol, []string{formattedSymbol})

		if err := gc.sendMessage(subMessage); err != nil {
			return fmt.Errorf("구독 메시지 전송 실패 (%s): %v", symbol, err)
		}
		fmt.Printf("✅ Gate %s 구독 메시지 전송 완료\n", gc.MarketType)

		// 구독 응답 대기 및 처리를 위한 짧은 지연
		time.Sleep(100 * time.Millisecond)
	}

	gc.SubscribedSymbols = append(gc.SubscribedSymbols, symbols...)
	fmt.Printf("📊 게이트 %s 구독: %d개 심볼 (공식 API 형식)\n", gc.MarketType, len(symbols))
	return nil
}

// Unsubscribe는 심볼 구독 해제 (공식 API 기준)
func (gc *GateConnector) Unsubscribe(symbols []string) error {
	for _, symbol := range symbols {
		formattedSymbol := formatSymbol(symbol, "gate", gc.MarketType)

		var channel string
		if gc.MarketType == "spot" {
			channel = "spot.trades"
		} else {
			channel = "futures.trades"
		}

		unsubMessage := map[string]interface{}{
			"time":    time.Now().Unix(),
			"channel": channel,
			"event":   "unsubscribe",
			"payload": []string{formattedSymbol},
		}

		gc.sendMessage(unsubMessage)
	}

	// 구독 목록에서 제거
	for _, symbol := range symbols {
		for i, subscribed := range gc.SubscribedSymbols {
			if subscribed == symbol {
				gc.SubscribedSymbols = append(gc.SubscribedSymbols[:i], gc.SubscribedSymbols[i+1:]...)
				break
			}
		}
	}

	return nil
}

// StartMessageLoop는 메시지 수신 루프 (응답 처리 강화)
func (gc *GateConnector) StartMessageLoop(ctx context.Context, messageChan chan<- models.TradeEvent) error {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("❌ 게이트 메시지 루프 패닉: %v\n", r)
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			default:
				if !gc.IsConnected() {
					time.Sleep(1 * time.Second)
					continue
				}

				message, err := gc.readMessage()
				if err != nil {
					if gc.OnError != nil {
						gc.OnError(err)
					}
					time.Sleep(1 * time.Second)
					continue
				}

				// 먼저 구독 응답 처리
				if gc.handleSubscriptionResponse(message) {
					continue
				}

				// 거래 데이터 파싱
				tradeEvents, err := gc.parseTradeMessage(message)
				if err != nil {
					continue // 거래 메시지가 아니거나 파싱 실패
				}

				// 거래 이벤트 전송
				for _, tradeEvent := range tradeEvents {
					select {
					case messageChan <- tradeEvent:
					default:
					}
				}
			}
		}
	}()

	return nil
}

// handleSubscriptionResponse는 구독 응답 처리
func (gc *GateConnector) handleSubscriptionResponse(data []byte) bool {
	var subResp GateSubscriptionResponse
	if err := json.Unmarshal(data, &subResp); err != nil {
		return false
	}

	// 구독 관련 이벤트인지 확인
	if subResp.Event == "subscribe" || subResp.Event == "unsubscribe" {
		if subResp.Error != nil {
			if gc.logger != nil {
				gc.logger.Error("Gate.io 구독 에러 (%s): %s",
					subResp.Channel, subResp.Error.Message)
			}
		} else if subResp.Result != nil && subResp.Result.Status == "success" {
			if gc.logger != nil {
				gc.logger.Debug("Gate.io 구독 성공: %s", subResp.Channel)
			}
		}
		return true
	}

	// Ping/Pong 응답 처리
	if strings.Contains(subResp.Channel, "ping") || strings.Contains(subResp.Channel, "pong") {
		return true
	}

	return false
}

// ParseTradeMessage implements the interface method for trade message parsing
func (gc *GateConnector) ParseTradeMessage(data []byte) ([]models.TradeEvent, error) {
	return gc.parseTradeMessage(data)
}

// parseTradeMessage는 게이트 거래 메시지 파싱 (공식 API 형식)
func (gc *GateConnector) parseTradeMessage(data []byte) ([]models.TradeEvent, error) {

	var response GateTradeResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
	}

	// 구독 확인 응답 체크 (별도 구조체로 파싱 시도)
	var subResponse GateSubscriptionResponse
	if json.Unmarshal(data, &subResponse) == nil && subResponse.Event == "subscribe" {
		fmt.Printf("📋 Gate %s 구독 응답: channel=%s, event=%s\n",
			gc.MarketType, subResponse.Channel, subResponse.Event)
		return nil, fmt.Errorf("구독 확인 메시지")
	}

	// 거래 업데이트 이벤트인지 확인
	if response.Event != "update" || !strings.Contains(response.Channel, "trades") {
		fmt.Printf("🔧 Gate %s: 거래 업데이트 아님 - event: %s, channel: %s\n",
			gc.MarketType, response.Event, response.Channel)
		return nil, fmt.Errorf("거래 업데이트 아님: %s/%s", response.Event, response.Channel)
	}

	// Gate.io는 단일 result 객체 사용 (배열이 아님)
	trade := response.Result

	// 타임스탬프 파싱
	var timestamp int64
	if trade.CreateTimeMs != "" {
		// create_time_ms는 소수점 포함된 문자열 (예: "1758037893114.020000")
		if dotIndex := strings.Index(trade.CreateTimeMs, "."); dotIndex > 0 {
			timestampStr := trade.CreateTimeMs[:dotIndex] // 소수점 앞 부분만 사용
			timestamp, _ = strconv.ParseInt(timestampStr, 10, 64)
		} else {
			timestamp, _ = strconv.ParseInt(trade.CreateTimeMs, 10, 64)
		}
	} else if trade.CreateTime > 0 {
		timestamp = trade.CreateTime * 1000 // 초를 밀리초로 변환 (이제 int64)
	}
	if timestamp == 0 {
		timestamp = time.Now().UnixMilli()
	}

	// 심볼과 수량 결정
	var symbol, quantity string
	if gc.MarketType == "spot" {
		symbol = trade.CurrencyPair
		quantity = trade.Amount
	} else {
		symbol = trade.Contract
		quantity = trade.Size
	}

	if symbol == "" || quantity == "" {
		return nil, fmt.Errorf("필수 필드 누락: symbol=%s, quantity=%s", symbol, quantity)
	}

	tradeEvent := models.TradeEvent{
		Exchange:   "gate",
		MarketType: gc.MarketType,
		Symbol:     normalizeSymbol(symbol),
		Price:      trade.Price,
		Quantity:   quantity,
		Side:       strings.ToLower(trade.Side),
		TradeID:    fmt.Sprintf("%d", trade.Id), // Convert int64 to string
		Timestamp:  timestamp,
	}

	return []models.TradeEvent{tradeEvent}, nil
}

