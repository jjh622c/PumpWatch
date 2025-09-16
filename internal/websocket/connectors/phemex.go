package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"PumpWatch/internal/models"
)

// PhemexConnector는 Phemex WebSocket 연결자 (공식 문서 기반)
type PhemexConnector struct {
	BaseConnector
	priceScales map[string]int64 // 심볼별 가격 스케일
}

// NewPhemexConnector는 새로운 Phemex Connector 생성
func NewPhemexConnector(marketType string, maxSymbols int) WebSocketConnector {
	// 하드코딩된 엔드포인트 (하위 호환성을 위해 유지)
	endpoint := "wss://ws.phemex.com" // 공식 엔드포인트
	return NewPhemexConnectorWithEndpoint(marketType, maxSymbols, endpoint)
}

// NewPhemexConnectorWithEndpoint는 엔드포인트를 지정하여 Connector 생성
func NewPhemexConnectorWithEndpoint(marketType string, maxSymbols int, endpoint string) WebSocketConnector {

	// 주요 futures 심볼들의 priceScale (공식 문서 기반)
	priceScales := map[string]int64{
		"BTCUSD":  10000,  // BTC 가격 스케일
		"ETHUSD":  10000,  // ETH 가격 스케일
		"XRPUSD":  100000, // XRP 가격 스케일
		"LTCUSD":  10000,  // LTC 가격 스케일
		"ADAUSD":  100000, // ADA 가격 스케일
		"LINKUSD": 10000,  // LINK 가격 스케일
		"DOTUSD":  10000,  // DOT 가격 스케일
		"UNIUSD":  10000,  // UNI 가격 스케일
	}

	return &PhemexConnector{
		BaseConnector: BaseConnector{
			Exchange:   "phemex",
			MarketType: marketType,
			Endpoint:   endpoint,
			MaxSymbols: maxSymbols,
		},
		priceScales: priceScales,
	}
}

// Connect는 WebSocket 연결 및 초기 설정
func (pc *PhemexConnector) Connect(ctx context.Context, symbols []string) error {
	// WebSocket 연결
	if err := pc.connectWebSocket(pc.Endpoint); err != nil {
		return fmt.Errorf("Phemex 연결 실패: %v", err)
	}

	// Phemex 전용 heartbeat 시작 (5초마다 server.ping)
	pc.startPhemexHeartbeat(ctx)

	// 심볼 구독
	if len(symbols) > 0 {
		if err := pc.Subscribe(symbols); err != nil {
			pc.Disconnect()
			return fmt.Errorf("구독 실패: %v", err)
		}
	}

	return nil
}

// startPhemexHeartbeat는 Phemex 전용 heartbeat 시작
func (pc *PhemexConnector) startPhemexHeartbeat(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !pc.IsConnected() {
					continue
				}

				// server.ping 메시지 전송
				pingMsg := map[string]interface{}{
					"id":     pc.generateID(),
					"method": "server.ping",
					"params": []interface{}{},
				}

				if err := pc.sendMessage(pingMsg); err != nil {
					fmt.Printf("⚠️ Phemex heartbeat 전송 실패: %v\n", err)
				}
			}
		}
	}()
}

// Subscribe는 심볼 구독 (공식 문서 기반)
func (pc *PhemexConnector) Subscribe(symbols []string) error {
	if !pc.IsConnected() {
		return fmt.Errorf("연결되지 않음")
	}

	// 심볼 개수 제한 확인
	if len(pc.SubscribedSymbols)+len(symbols) > pc.MaxSymbols {
		return fmt.Errorf("최대 구독 개수 초과: %d/%d",
			len(pc.SubscribedSymbols)+len(symbols), pc.MaxSymbols)
	}

	// Phemex futures 심볼 형식으로 변환
	var phemexSymbols []string
	for _, symbol := range symbols {
		phemexSymbol := pc.formatPhemexSymbol(symbol)
		phemexSymbols = append(phemexSymbols, phemexSymbol)
	}

	// Phemex spot과 futures는 다른 구독 방식 사용
	var subMessage map[string]interface{}

	if pc.MarketType == "spot" {
		// Spot: 공식 문서 기준 trade_p.subscribe 사용
		subMessage = map[string]interface{}{
			"id":     pc.generateID(),
			"method": "trade_p.subscribe",
			"params": phemexSymbols, // spot은 배열 직접 전달
		}
	} else {
		// Futures: 객체 배열 형식 사용
		var params []map[string]interface{}
		for _, phemexSymbol := range phemexSymbols {
			params = append(params, map[string]interface{}{
				"symbol": phemexSymbol,
			})
		}
		subMessage = map[string]interface{}{
			"id":     pc.generateID(),
			"method": "trades.subscribe",
			"params": params,
		}
	}

	if err := pc.sendMessage(subMessage); err != nil {
		return fmt.Errorf("구독 메시지 전송 실패: %v", err)
	}

	// 구독 목록 업데이트
	pc.SubscribedSymbols = append(pc.SubscribedSymbols, symbols...)

	fmt.Printf("📊 Phemex %s 구독: %d개 심볼 (%v)\n", pc.MarketType, len(symbols), phemexSymbols)
	return nil
}

// Unsubscribe는 심볼 구독 해제
func (pc *PhemexConnector) Unsubscribe(symbols []string) error {
	for _, symbol := range symbols {
		formattedSymbol := formatSymbol(symbol, "phemex", pc.MarketType)
		topic := fmt.Sprintf("trade.%s", formattedSymbol)

		unsubMessage := map[string]interface{}{
			"id":     time.Now().UnixNano(),
			"method": "trade.unsubscribe",
			"params": []string{topic},
		}

		pc.sendMessage(unsubMessage)
	}

	// 구독 목록에서 제거
	for _, symbol := range symbols {
		for i, subscribed := range pc.SubscribedSymbols {
			if subscribed == symbol {
				pc.SubscribedSymbols = append(pc.SubscribedSymbols[:i], pc.SubscribedSymbols[i+1:]...)
				break
			}
		}
	}

	return nil
}

// StartMessageLoop는 메시지 수신 루프
func (pc *PhemexConnector) StartMessageLoop(ctx context.Context, messageChan chan<- models.TradeEvent) error {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if !pc.IsConnected() {
					time.Sleep(1 * time.Second)
					continue
				}

				message, err := pc.readMessage()
				if err != nil {
					if pc.OnError != nil {
						pc.OnError(err)
					}
					continue
				}

				tradeEvents, err := pc.parseTradeMessage(message)
				if err != nil {
					// 구독/heartbeat 응답은 무시 (정상적인 동작)
					if strings.Contains(err.Error(), "구독/heartbeat 응답") {
						continue
					}
					// 실제 파싱 에러만 로깅
					fmt.Printf("🔧 Phemex 파싱 실패: %v\n메시지: %s\n---\n", err, string(message))
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
func (pc *PhemexConnector) ParseTradeMessage(data []byte) ([]models.TradeEvent, error) {
	return pc.parseTradeMessage(data)
}

// parseTradeMessage는 Phemex 거래 메시지 파싱 (공식 문서 기반)
func (pc *PhemexConnector) parseTradeMessage(data []byte) ([]models.TradeEvent, error) {
	// Phemex WebSocket 응답 구조
	var response struct {
		ID     interface{} `json:"id"`
		Result interface{} `json:"result"`
		Error  interface{} `json:"error"`
		// Trade stream 메시지 (trade_p.subscribe 사용 시 trades_p 필드)
		TradesP  [][]interface{} `json:"trades_p"`
		Sequence int64           `json:"sequence"`
		Symbol   string          `json:"symbol"`
		Type     string          `json:"type"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
	}

	// 구독 응답이나 heartbeat 응답 처리
	if response.ID != nil {
		if response.Error != nil {
			return nil, fmt.Errorf("Phemex 에러 응답: %v", response.Error)
		}
		// 구독 성공/heartbeat 응답은 무시
		return nil, fmt.Errorf("구독/heartbeat 응답")
	}

	// trades_p 배열이 없으면 거래 데이터가 아님
	if len(response.TradesP) == 0 {
		return nil, fmt.Errorf("거래 데이터 없음")
	}

	var tradeEvents []models.TradeEvent

	for _, trade := range response.TradesP {
		if len(trade) < 4 {
			continue
		}

		// 거래 데이터: [timestamp, side, price, size] - trade_p.subscribe 형식
		timestampNs, ok := trade[0].(float64)
		if !ok {
			continue
		}

		sideStr, ok := trade[1].(string)
		if !ok {
			continue
		}

		priceStr, ok := trade[2].(string)
		if !ok {
			continue
		}

		sizeStr, ok := trade[3].(string)
		if !ok {
			continue
		}

		// 가격은 이미 실제 가격 (priceEp 변환 불필요)
		actualPrice := priceStr

		// 거래 방향 정규화
		side := strings.ToLower(sideStr)

		// 타임스탬프 변환 (나노초 -> 밀리초)
		timestamp := int64(timestampNs) / 1000000

		tradeEvent := models.TradeEvent{
			Exchange:   "phemex",
			MarketType: pc.MarketType,
			Symbol:     normalizeSymbol(response.Symbol),
			Price:      actualPrice,
			Quantity:   sizeStr,
			Side:       side,
			TradeID:    fmt.Sprintf("%d_%d", response.Sequence, int64(timestampNs)),
			Timestamp:  timestamp,
		}

		tradeEvents = append(tradeEvents, tradeEvent)
	}

	return tradeEvents, nil
}

// convertPriceEp는 priceEp를 실제 가격으로 변환
func (pc *PhemexConnector) convertPriceEp(symbol string, priceEp int64) string {
	priceScale, exists := pc.priceScales[symbol]
	if !exists {
		// 기본값 사용
		priceScale = 10000
	}

	actualPrice := float64(priceEp) / float64(priceScale)
	return fmt.Sprintf("%.8f", actualPrice)
}

// formatPhemexSymbol는 심볼을 Phemex 형식으로 변환
func (pc *PhemexConnector) formatPhemexSymbol(symbol string) string {
	// BTCUSDT -> BTCUSD 변환 (futures의 경우)
	if strings.HasSuffix(symbol, "USDT") && pc.MarketType == "futures" {
		base := strings.TrimSuffix(symbol, "USDT")
		return base + "USD"
	}

	// 이미 올바른 형식이면 그대로 반환
	return symbol
}

// generateID는 고유 ID 생성
func (pc *PhemexConnector) generateID() int64 {
	return time.Now().UnixNano() / 1000000
}

