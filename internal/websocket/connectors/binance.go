package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"PumpWatch/internal/models"
)

// BinanceConnector는 바이낸스 WebSocket 연결자
type BinanceConnector struct {
	BaseConnector
}

// NewBinanceConnector는 새로운 바이낸스 Connector 생성
func NewBinanceConnector(marketType string, maxSymbols int) WebSocketConnector {
	endpoint := ""
	if marketType == "spot" {
		endpoint = "wss://stream.binance.com:9443/ws"
	} else {
		endpoint = "wss://fstream.binance.com/ws"
	}
	
	return &BinanceConnector{
		BaseConnector: BaseConnector{
			Exchange:   "binance",
			MarketType: marketType,
			Endpoint:   endpoint,
			MaxSymbols: maxSymbols,
		},
	}
}

// Connect는 WebSocket 연결 및 초기 설정
func (bc *BinanceConnector) Connect(ctx context.Context, symbols []string) error {
	// WebSocket 연결
	if err := bc.connectWebSocket(bc.Endpoint); err != nil {
		return fmt.Errorf("바이낸스 연결 실패: %v", err)
	}
	
	// Ping 루프 시작 (바이낸스는 30초마다 Ping)
	bc.startPingLoop(ctx, 25*time.Second)
	
	// 심볼 구독
	if len(symbols) > 0 {
		if err := bc.Subscribe(symbols); err != nil {
			bc.Disconnect()
			return fmt.Errorf("구독 실패: %v", err)
		}
	}
	
	return nil
}

// Subscribe는 심볼 구독
func (bc *BinanceConnector) Subscribe(symbols []string) error {
	if !bc.IsConnected() {
		return fmt.Errorf("연결되지 않음")
	}
	
	// 심볼 개수 제한 확인
	if len(bc.SubscribedSymbols)+len(symbols) > bc.MaxSymbols {
		return fmt.Errorf("최대 구독 개수 초과: %d/%d", 
			len(bc.SubscribedSymbols)+len(symbols), bc.MaxSymbols)
	}
	
	// 바이낸스 형식으로 스트림 이름 생성
	var streams []string
	for _, symbol := range symbols {
		formattedSymbol := formatSymbol(symbol, "binance", bc.MarketType)
		streamName := formattedSymbol + "@trade" // 거래 데이터 스트림
		streams = append(streams, streamName)
	}
	
	// 구독 메시지 전송
	subMessage := map[string]interface{}{
		"method": "SUBSCRIBE",
		"params": streams,
		"id":     1,
	}
	
	if err := bc.sendMessage(subMessage); err != nil {
		return fmt.Errorf("구독 메시지 전송 실패: %v", err)
	}
	
	// 구독 목록 업데이트
	bc.SubscribedSymbols = append(bc.SubscribedSymbols, symbols...)
	
	fmt.Printf("📊 바이낸스 %s 구독: %d개 심볼\n", bc.MarketType, len(symbols))
	return nil
}

// Unsubscribe는 심볼 구독 해제
func (bc *BinanceConnector) Unsubscribe(symbols []string) error {
	if !bc.IsConnected() {
		return fmt.Errorf("연결되지 않음")
	}
	
	// 바이낸스 형식으로 스트림 이름 생성
	var streams []string
	for _, symbol := range symbols {
		formattedSymbol := formatSymbol(symbol, "binance", bc.MarketType)
		streamName := formattedSymbol + "@trade"
		streams = append(streams, streamName)
	}
	
	// 구독 해제 메시지 전송
	unsubMessage := map[string]interface{}{
		"method": "UNSUBSCRIBE", 
		"params": streams,
		"id":     2,
	}
	
	if err := bc.sendMessage(unsubMessage); err != nil {
		return fmt.Errorf("구독 해제 메시지 전송 실패: %v", err)
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
	
	fmt.Printf("📊 바이낸스 %s 구독 해제: %d개 심볼\n", bc.MarketType, len(symbols))
	return nil
}

// StartMessageLoop는 메시지 수신 루프 시작
func (bc *BinanceConnector) StartMessageLoop(ctx context.Context, messageChan chan<- models.TradeEvent) error {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("❌ 바이낸스 메시지 루프 패닉: %v\n", r)
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
					fmt.Printf("⚠️ 바이낸스 메시지 읽기 실패: %v\n", err)
					if bc.OnError != nil {
						bc.OnError(err)
					}
					time.Sleep(1 * time.Second)
					continue
				}
				
				// 거래 데이터 파싱 및 전송
				tradeEvent, err := bc.parseTradeMessage(message)
				if err != nil {
					// 파싱 실패 로그 출력 (디버깅용)
					messageStr := string(message)
					if len(messageStr) == 0 {
						fmt.Printf("🔧 바이낸스 파싱 실패: %v (빈 메시지)\n", err)
					} else if len(messageStr) < 10 {
						fmt.Printf("🔧 바이낸스 파싱 실패: %v (짧은 메시지: %q)\n", err, messageStr)
					} else {
						fmt.Printf("🔧 바이낸스 파싱 실패: %v (메시지 길이: %d)\n", err, len(messageStr))
						// 파싱 실패는 로그만 남기고 계속 진행 (응답 메시지일 수 있음)
					}
					continue
				}
				
				select {
				case messageChan <- tradeEvent:
				default:
					fmt.Printf("⚠️ 바이낸스 메시지 채널이 가득참\n")
				}
			}
		}
	}()
	
	return nil
}

// ParseTradeMessage implements the interface method for trade message parsing
func (bc *BinanceConnector) ParseTradeMessage(data []byte) ([]models.TradeEvent, error) {
	tradeEvent, err := bc.parseTradeMessage(data)
	if err != nil {
		return nil, err
	}
	return []models.TradeEvent{tradeEvent}, nil
}

// parseTradeMessage는 바이낸스 거래 메시지 파싱
func (bc *BinanceConnector) parseTradeMessage(data []byte) (models.TradeEvent, error) {
	// 바이낸스 실제 응답 구조 (직접 trade 이벤트)
	var tradeData struct {
		EventType string `json:"e"` // 이벤트 타입 ("trade")
		EventTime int64  `json:"E"` // 이벤트 시간
		Symbol    string `json:"s"` // 심볼 ("BTCUSDT")
		TradeID   int64  `json:"t"` // 거래 ID
		Price     string `json:"p"` // 가격
		Quantity  string `json:"q"` // 수량
		Timestamp int64  `json:"T"` // 거래 시간
		IsBuyerMaker bool `json:"m"` // 매수자가 메이커인지 (true=매수가 메이커)
	}
	
	if err := json.Unmarshal(data, &tradeData); err != nil {
		return models.TradeEvent{}, fmt.Errorf("JSON 파싱 실패: %v", err)
	}
	
	// 거래 이벤트가 아니면 무시
	if tradeData.EventType != "trade" {
		return models.TradeEvent{}, fmt.Errorf("거래 이벤트 아님")
	}
	
	// 거래 방향 결정 (바이낸스의 m 필드는 매수자가 메이커인지 표시)
	side := "sell"
	if tradeData.IsBuyerMaker {
		side = "buy" // 매수자가 메이커면 매수 거래
	}
	
	return models.TradeEvent{
		Exchange:   "binance",
		MarketType: bc.MarketType,
		Symbol:     normalizeSymbol(tradeData.Symbol),
		Price:      tradeData.Price,
		Quantity:   tradeData.Quantity,
		Side:       side,
		TradeID:    fmt.Sprintf("%d", tradeData.TradeID),
		Timestamp:  tradeData.Timestamp,
	}, nil
}

// BinanceSpotConnector는 바이낸스 현물용 특화 Connector
func NewBinanceSpotConnector(maxSymbols int) WebSocketConnector {
	return NewBinanceConnector("spot", maxSymbols)
}

// BinanceFuturesConnector는 바이낸스 선물용 특화 Connector  
func NewBinanceFuturesConnector(maxSymbols int) WebSocketConnector {
	return NewBinanceConnector("futures", maxSymbols)
}