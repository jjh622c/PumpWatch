package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"PumpWatch/internal/models"
)

// BybitConnector는 바이비트 WebSocket 연결자
type BybitConnector struct {
	BaseConnector
}

// NewBybitConnector는 새로운 바이비트 Connector 생성
func NewBybitConnector(marketType string, maxSymbols int) WebSocketConnector {
	// 하드코딩된 엔드포인트 (하위 호환성을 위해 유지)
	endpoint := ""
	if marketType == "spot" {
		endpoint = "wss://stream.bybit.com/v5/public/spot"
	} else {
		endpoint = "wss://stream.bybit.com/v5/public/linear"
	}
	return NewBybitConnectorWithEndpoint(marketType, maxSymbols, endpoint)
}

// NewBybitConnectorWithEndpoint는 엔드포인트를 지정하여 Connector 생성
func NewBybitConnectorWithEndpoint(marketType string, maxSymbols int, endpoint string) WebSocketConnector {

	return &BybitConnector{
		BaseConnector: BaseConnector{
			Exchange:   "bybit",
			MarketType: marketType,
			Endpoint:   endpoint,
			MaxSymbols: maxSymbols,
		},
	}
}

// Connect는 WebSocket 연결 및 초기 설정
func (bc *BybitConnector) Connect(ctx context.Context, symbols []string) error {
	// WebSocket 연결
	if err := bc.connectWebSocket(bc.Endpoint); err != nil {
		return fmt.Errorf("바이비트 연결 실패: %v", err)
	}

	// Ping 루프 시작 (바이비트는 20초마다 Ping)
	bc.startPingLoop(ctx, 20*time.Second)

	// 심볼 구독
	if len(symbols) > 0 {
		if err := bc.Subscribe(symbols); err != nil {
			bc.Disconnect()
			return fmt.Errorf("구독 실패: %v", err)
		}
	}

	return nil
}

// Subscribe는 심볼 구독 (10개씩 배치로 처리)
func (bc *BybitConnector) Subscribe(symbols []string) error {
	if !bc.IsConnected() {
		return fmt.Errorf("연결되지 않음")
	}

	// 심볼 개수 제한 확인
	if len(bc.SubscribedSymbols)+len(symbols) > bc.MaxSymbols {
		return fmt.Errorf("최대 구독 개수 초과: %d/%d",
			len(bc.SubscribedSymbols)+len(symbols), bc.MaxSymbols)
	}

	// 바이비트는 한 번에 최대 10개 args만 허용하므로 배치 처리
	const batchSize = 10

	for i := 0; i < len(symbols); i += batchSize {
		end := i + batchSize
		if end > len(symbols) {
			end = len(symbols)
		}

		batch := symbols[i:end]

		// 바이비트 형식으로 토픽 생성
		var args []string
		for _, symbol := range batch {
			formattedSymbol := formatSymbol(symbol, "bybit", bc.MarketType)
			topic := fmt.Sprintf("publicTrade.%s", formattedSymbol)
			args = append(args, topic)
		}

		// 구독 메시지 전송
		subMessage := map[string]interface{}{
			"op":   "subscribe",
			"args": args,
		}

		if err := bc.sendMessage(subMessage); err != nil {
			return fmt.Errorf("구독 메시지 전송 실패 (배치 %d): %v", (i/batchSize)+1, err)
		}

		fmt.Printf("📊 바이비트 %s 구독 배치 %d: %d개 심볼 (%v)\n",
			bc.MarketType, (i/batchSize)+1, len(batch), batch)

		// 배치 간 짧은 대기 (API 제한 고려)
		time.Sleep(100 * time.Millisecond)
	}

	// 구독 목록 업데이트
	bc.SubscribedSymbols = append(bc.SubscribedSymbols, symbols...)

	fmt.Printf("✅ 바이비트 %s 전체 구독 완료: %d개 심볼\n", bc.MarketType, len(symbols))
	return nil
}

// Unsubscribe는 심볼 구독 해제 (10개씩 배치로 처리)
func (bc *BybitConnector) Unsubscribe(symbols []string) error {
	if !bc.IsConnected() {
		return fmt.Errorf("연결되지 않음")
	}

	// 바이비트는 한 번에 최대 10개 args만 허용하므로 배치 처리
	const batchSize = 10

	for i := 0; i < len(symbols); i += batchSize {
		end := i + batchSize
		if end > len(symbols) {
			end = len(symbols)
		}

		batch := symbols[i:end]

		// 바이비트 형식으로 토픽 생성
		var args []string
		for _, symbol := range batch {
			formattedSymbol := formatSymbol(symbol, "bybit", bc.MarketType)
			topic := fmt.Sprintf("publicTrade.%s", formattedSymbol)
			args = append(args, topic)
		}

		// 구독 해제 메시지 전송
		unsubMessage := map[string]interface{}{
			"op":   "unsubscribe",
			"args": args,
		}

		if err := bc.sendMessage(unsubMessage); err != nil {
			return fmt.Errorf("구독 해제 메시지 전송 실패 (배치 %d): %v", (i/batchSize)+1, err)
		}

		fmt.Printf("📊 바이비트 %s 구독 해제 배치 %d: %d개 심볼\n",
			bc.MarketType, (i/batchSize)+1, len(batch))

		// 배치 간 짧은 대기
		time.Sleep(100 * time.Millisecond)
	}

	// 구독 목록에서 제거
	for _, symbol := range symbols {
		for i, subscribed := range bc.SubscribedSymbols {
			if subscribed == symbol {
				bc.SubscribedSymbols = append(bc.SubscribedSymbols[:i], bc.SubscribedSymbols[i+1:]...)
				break
			}
		}
	}

	fmt.Printf("✅ 바이비트 %s 전체 구독 해제 완료: %d개 심볼\n", bc.MarketType, len(symbols))
	return nil
}

// StartMessageLoop는 메시지 수신 루프 시작
func (bc *BybitConnector) StartMessageLoop(ctx context.Context, messageChan chan<- models.TradeEvent) error {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("❌ 바이비트 메시지 루프 패닉: %v\n", r)
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			default:
				if !bc.IsConnected() {
					time.Sleep(1 * time.Second)
					continue
				}

				message, err := bc.readMessage()
				if err != nil {
					fmt.Printf("⚠️ 바이비트 메시지 읽기 실패: %v\n", err)
					if bc.OnError != nil {
						bc.OnError(err)
					}
					time.Sleep(1 * time.Second)
					continue
				}

				// 거래 데이터 파싱 및 전송
				tradeEvents, err := bc.parseTradeMessage(message)
				if err != nil {
					// 시스템 메시지가 아닌 실제 파싱 에러만 로그 출력
					if !isSystemMessage(message) {
						fmt.Printf("🔧 바이비트 파싱 실패: %v (메시지: %.100s...)\n", err, string(message))
					}
					continue
				}

				// 시스템 메시지로 인한 nil 반환은 조용히 스킵
				if tradeEvents == nil {
					continue
				}

				for _, tradeEvent := range tradeEvents {
					select {
					case messageChan <- tradeEvent:
					default:
						fmt.Printf("⚠️ 바이비트 메시지 채널이 가득참\n")
					}
				}
			}
		}
	}()

	return nil
}

// ParseTradeMessage implements the interface method for trade message parsing
func (bc *BybitConnector) ParseTradeMessage(data []byte) ([]models.TradeEvent, error) {
	return bc.parseTradeMessage(data)
}

// parseTradeMessage는 바이비트 거래 메시지 파싱
func (bc *BybitConnector) parseTradeMessage(data []byte) ([]models.TradeEvent, error) {
	// 먼저 시스템 메시지인지 확인 (subscription 응답, ping/pong 등)
	var systemResponse struct {
		Success bool   `json:"success"`
		RetMsg  string `json:"ret_msg"`
		Op      string `json:"op"`
	}

	// 시스템 메시지 확인
	if err := json.Unmarshal(data, &systemResponse); err == nil {
		if systemResponse.Success || systemResponse.Op != "" {
			// 시스템 메시지는 조용히 스킵 (디버그용으로만 로그)
			return nil, nil
		}
	}

	// 바이비트 V5 API 실제 응답 구조
	var response struct {
		Topic string `json:"topic"`
		Type  string `json:"type"`
		Ts    int64  `json:"ts"`
		Data  []struct {
			TradeId      string `json:"i"`   // 거래 ID
			Symbol       string `json:"s"`   // 심볼
			Price        string `json:"p"`   // 가격
			Volume       string `json:"v"`   // 거래량
			Side         string `json:"S"`   // 거래 방향 (Buy/Sell)
			Timestamp    int64  `json:"T"`   // 타임스탬프 (숫자)
			Seq          int64  `json:"seq"` // 시퀀스 번호
			IsBlockTrade bool   `json:"BT"`  // 블록 거래 여부
		} `json:"data"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
	}

	// publicTrade 토픽이 아니면 조용히 스킵
	if !strings.Contains(response.Topic, "publicTrade") {
		return nil, nil // 에러 대신 nil 반환으로 조용히 스킵
	}

	var tradeEvents []models.TradeEvent

	for _, trade := range response.Data {

		// 타임스탬프 (바이비트는 밀리초 단위 숫자로 제공)
		timestamp := trade.Timestamp

		// 거래 방향 (바이비트는 "Buy"/"Sell"로 제공)
		side := strings.ToLower(trade.Side)

		tradeEvent := models.TradeEvent{
			Exchange:   "bybit",
			MarketType: bc.MarketType,
			Symbol:     normalizeSymbol(trade.Symbol),
			Price:      trade.Price,
			Quantity:   trade.Volume,
			Side:       side,
			TradeID:    trade.TradeId,
			Timestamp:  timestamp,
		}

		tradeEvents = append(tradeEvents, tradeEvent)
	}

	return tradeEvents, nil
}

// isSystemMessage는 시스템 메시지인지 확인 (subscription 응답, ping/pong 등)
func isSystemMessage(data []byte) bool {
	var systemResponse struct {
		Success bool   `json:"success"`
		RetMsg  string `json:"ret_msg"`
		Op      string `json:"op"`
		Topic   string `json:"topic"`
	}

	if err := json.Unmarshal(data, &systemResponse); err == nil {
		// subscription 응답이나 operation 메시지인 경우
		if systemResponse.Success || systemResponse.Op != "" {
			return true
		}
		// publicTrade 토픽이 아닌 경우 (ping/pong 등)
		if systemResponse.Topic != "" && !strings.Contains(systemResponse.Topic, "publicTrade") {
			return true
		}
	}
	return false
}

