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

// OKXConnector는 OKX WebSocket 연결자
type OKXConnector struct {
	BaseConnector
}

// NewOKXConnector는 새로운 OKX Connector 생성
func NewOKXConnector(marketType string, maxSymbols int) WebSocketConnector {
	endpoint := "wss://ws.okx.com:8443/ws/v5/public"
	
	return &OKXConnector{
		BaseConnector: BaseConnector{
			Exchange:   "okx",
			MarketType: marketType,
			Endpoint:   endpoint,
			MaxSymbols: maxSymbols,
		},
	}
}

// Connect는 WebSocket 연결
func (oc *OKXConnector) Connect(ctx context.Context, symbols []string) error {
	if err := oc.connectWebSocket(oc.Endpoint); err != nil {
		return fmt.Errorf("OKX 연결 실패: %v", err)
	}
	
	oc.startPingLoop(ctx, 35*time.Second)
	
	if len(symbols) > 0 {
		if err := oc.Subscribe(symbols); err != nil {
			oc.Disconnect()
			return fmt.Errorf("구독 실패: %v", err)
		}
	}
	
	return nil
}

// Subscribe는 심볼 구독
func (oc *OKXConnector) Subscribe(symbols []string) error {
	if !oc.IsConnected() {
		return fmt.Errorf("연결되지 않음")
	}
	
	if len(oc.SubscribedSymbols)+len(symbols) > oc.MaxSymbols {
		return fmt.Errorf("최대 구독 개수 초과: %d/%d", 
			len(oc.SubscribedSymbols)+len(symbols), oc.MaxSymbols)
	}
	
	var args []map[string]string
	for _, symbol := range symbols {
		formattedSymbol := formatSymbol(symbol, "okx", oc.MarketType)
		args = append(args, map[string]string{
			"channel": "trades",
			"instId":  formattedSymbol,
		})
	}
	
	subMessage := map[string]interface{}{
		"op":   "subscribe",
		"args": args,
	}
	
	if err := oc.sendMessage(subMessage); err != nil {
		return fmt.Errorf("구독 메시지 전송 실패: %v", err)
	}
	
	oc.SubscribedSymbols = append(oc.SubscribedSymbols, symbols...)
	fmt.Printf("📊 OKX %s 구독: %d개 심볼\n", oc.MarketType, len(symbols))
	return nil
}

// Unsubscribe는 심볼 구독 해제
func (oc *OKXConnector) Unsubscribe(symbols []string) error {
	var args []map[string]string
	for _, symbol := range symbols {
		formattedSymbol := formatSymbol(symbol, "okx", oc.MarketType)
		args = append(args, map[string]string{
			"channel": "trades",
			"instId":  formattedSymbol,
		})
	}
	
	unsubMessage := map[string]interface{}{
		"op":   "unsubscribe",
		"args": args,
	}
	
	oc.sendMessage(unsubMessage)
	
	// 구독 목록에서 제거
	for _, symbol := range symbols {
		for i, subscribed := range oc.SubscribedSymbols {
			if subscribed == symbol {
				oc.SubscribedSymbols = append(oc.SubscribedSymbols[:i], oc.SubscribedSymbols[i+1:]...)
				break
			}
		}
	}
	
	return nil
}

// StartMessageLoop는 메시지 수신 루프
func (oc *OKXConnector) StartMessageLoop(ctx context.Context, messageChan chan<- models.TradeEvent) error {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if !oc.IsConnected() {
					time.Sleep(1 * time.Second)
					continue
				}
				
				message, err := oc.readMessage()
				if err != nil {
					if oc.OnError != nil {
						oc.OnError(err)
					}
					continue
				}
				
				tradeEvents, err := oc.parseTradeMessage(message)
				if err != nil {
					// 파싱 실패 로그 출력 (디버깅용)
					messageStr := string(message)
					if len(messageStr) == 0 {
						fmt.Printf("🔧 OKX 파싱 실패: %v (빈 메시지)\n", err)
					} else if len(messageStr) < 10 {
						fmt.Printf("🔧 OKX 파싱 실패: %v (짧은 메시지: %q)\n", err, messageStr)
					} else {
						fmt.Printf("🔧 OKX 파싱 실패: %v (메시지 길이: %d, 내용: %q)\n", err, len(messageStr), messageStr)
					}
					continue
				}
				
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

// ParseTradeMessage implements the interface method for trade message parsing
func (oc *OKXConnector) ParseTradeMessage(data []byte) ([]models.TradeEvent, error) {
	return oc.parseTradeMessage(data)
}

// parseTradeMessage는 OKX 거래 메시지 파싱
func (oc *OKXConnector) parseTradeMessage(data []byte) ([]models.TradeEvent, error) {
	var response struct {
		Arg struct {
			Channel string `json:"channel"`
			InstId  string `json:"instId"`
		} `json:"arg"`
		Data []struct {
			InstId  string `json:"instId"`
			TradeId string `json:"tradeId"`
			Px      string `json:"px"`      // 가격
			Sz      string `json:"sz"`      // 수량
			Side    string `json:"side"`    // 거래 방향
			Ts      string `json:"ts"`      // 타임스탬프
		} `json:"data"`
	}
	
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
	}
	
	if response.Arg.Channel != "trades" {
		return nil, fmt.Errorf("거래 채널 아님")
	}
	
	var tradeEvents []models.TradeEvent
	for _, trade := range response.Data {
		timestamp, _ := strconv.ParseInt(trade.Ts, 10, 64)
		
		tradeEvent := models.TradeEvent{
			Exchange:   "okx",
			MarketType: oc.MarketType,
			Symbol:     normalizeSymbol(trade.InstId),
			Price:      trade.Px,
			Quantity:   trade.Sz,
			Side:       strings.ToLower(trade.Side),
			TradeID:    trade.TradeId,
			Timestamp:  timestamp,
		}
		tradeEvents = append(tradeEvents, tradeEvent)
	}
	
	return tradeEvents, nil
}

func NewOKXSpotConnector(maxSymbols int) WebSocketConnector {
	return NewOKXConnector("spot", maxSymbols)
}

func NewOKXFuturesConnector(maxSymbols int) WebSocketConnector {
	return NewOKXConnector("futures", maxSymbols)
}
