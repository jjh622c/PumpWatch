package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"PumpWatch/internal/logging"
	"PumpWatch/internal/models"
	"github.com/gorilla/websocket"
)

// WebSocketConnector는 거래소별 WebSocket 연결의 기본 인터페이스
type WebSocketConnector interface {
	// 연결 관리
	Connect(ctx context.Context, symbols []string) error
	Disconnect() error
	IsConnected() bool

	// 구독 관리
	Subscribe(symbols []string) error
	Unsubscribe(symbols []string) error
	GetSubscribedSymbols() []string

	// 메시지 처리
	StartMessageLoop(ctx context.Context, messageChan chan<- models.TradeEvent) error
	ParseTradeMessage([]byte) ([]models.TradeEvent, error)

	// WebSocket 연결 객체 접근
	GetConnection() *websocket.Conn

	// 상태 정보
	GetConnectionInfo() ConnectionInfo
}

// BaseConnector는 공통 WebSocket 기능을 제공하는 기본 구조체
type BaseConnector struct {
	Exchange   string
	MarketType string
	Endpoint   string

	Connection *websocket.Conn
	IsActive   bool
	mu         sync.RWMutex // 연결 상태 동기화용

	// 구독 정보
	SubscribedSymbols []string
	MaxSymbols        int

	// 통계
	LastMessageTime time.Time
	TotalMessages   int64
	ReconnectCount  int

	// 로깅
	logger *logging.Logger

	// 콜백
	OnMessage   func([]byte)
	OnError     func(error)
	OnReconnect func()
}

// ConnectionInfo는 연결 상태 정보
type ConnectionInfo struct {
	Exchange          string    `json:"exchange"`
	MarketType        string    `json:"market_type"`
	Endpoint          string    `json:"endpoint"`
	IsConnected       bool      `json:"is_connected"`
	SubscribedSymbols []string  `json:"subscribed_symbols"`
	MaxSymbols        int       `json:"max_symbols"`
	LastMessageTime   time.Time `json:"last_message_time"`
	TotalMessages     int64     `json:"total_messages"`
	ReconnectCount    int       `json:"reconnect_count"`
}

// GetConnectionInfo는 연결 정보 반환
func (bc *BaseConnector) GetConnectionInfo() ConnectionInfo {
	return ConnectionInfo{
		Exchange:          bc.Exchange,
		MarketType:        bc.MarketType,
		Endpoint:          bc.Endpoint,
		IsConnected:       bc.IsActive,
		SubscribedSymbols: bc.SubscribedSymbols,
		MaxSymbols:        bc.MaxSymbols,
		LastMessageTime:   bc.LastMessageTime,
		TotalMessages:     bc.TotalMessages,
		ReconnectCount:    bc.ReconnectCount,
	}
}

// IsConnected는 연결 상태 확인 (thread-safe)
func (bc *BaseConnector) IsConnected() bool {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.IsActive && bc.Connection != nil
}

// GetSubscribedSymbols는 구독 중인 심볼 목록 반환
func (bc *BaseConnector) GetSubscribedSymbols() []string {
	return bc.SubscribedSymbols
}

// GetConnection은 WebSocket 연결 객체 반환 (thread-safe)
func (bc *BaseConnector) GetConnection() *websocket.Conn {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if bc.IsActive && bc.Connection != nil {
		return bc.Connection
	}
	return nil
}

// SetOnError sets the error callback function
func (bc *BaseConnector) SetOnError(callback func(error)) {
	bc.OnError = callback
}

// InitLogger initializes the connector's logger
func (bc *BaseConnector) InitLogger() {
	if bc.logger == nil {
		globalLogger := logging.GetGlobalLogger()
		if globalLogger != nil {
			bc.logger = globalLogger.WebSocketLogger(bc.Exchange, bc.MarketType)
		}
	}
}

// Disconnect는 연결 종료 (thread-safe)
func (bc *BaseConnector) Disconnect() error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.Connection != nil {
		bc.Connection.Close()
		bc.Connection = nil
	}
	bc.IsActive = false
	bc.SubscribedSymbols = []string{}

	log.Printf("🔌 %s %s 연결 종료", bc.Exchange, bc.MarketType)
	return nil
}

// connectWebSocket는 기본 WebSocket 연결 (thread-safe)
func (bc *BaseConnector) connectWebSocket(endpoint string) error {
	dialer := websocket.Dialer{
		Proxy:            websocket.DefaultDialer.Proxy,
		HandshakeTimeout: 45 * time.Second,
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("URL 파싱 실패: %v", err)
	}

	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("WebSocket 연결 실패: %v", err)
	}

	bc.mu.Lock()
	bc.Connection = conn
	bc.IsActive = true
	bc.LastMessageTime = time.Now()
	bc.mu.Unlock()

	log.Printf("🔌 %s %s WebSocket 연결 성공: %s", bc.Exchange, bc.MarketType, endpoint)
	return nil
}

// sendMessage는 메시지 전송 (thread-safe with panic recovery)
func (bc *BaseConnector) sendMessage(message interface{}) error {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	if !bc.IsActive || bc.Connection == nil {
		return fmt.Errorf("연결되지 않음")
	}

	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("JSON 마샬링 실패: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("⚠️ %s %s sendMessage panic recovered: %v", bc.Exchange, bc.MarketType, r)
			// 패닉 발생 시 연결 상태를 비활성화
			bc.IsActive = false
			// 지능형 복구 시스템에 에러 보고
			if bc.OnError != nil {
				bc.OnError(fmt.Errorf("sendMessage panic: %v", r))
			}
		}
	}()

	if err := bc.Connection.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("메시지 전송 실패: %v", err)
	}

	return nil
}

// readMessage는 메시지 수신 (thread-safe with panic recovery)
func (bc *BaseConnector) readMessage() ([]byte, error) {
	bc.mu.RLock()

	if !bc.IsActive || bc.Connection == nil {
		bc.mu.RUnlock()
		return nil, fmt.Errorf("연결되지 않음")
	}

	conn := bc.Connection // 로컬 복사본 보관
	bc.mu.RUnlock()

	defer func() {
		if r := recover(); r != nil {
			log.Printf("⚠️ %s %s readMessage panic recovered: %v", bc.Exchange, bc.MarketType, r)
			// 패닉 발생 시 쓰기 락으로 연결 상태를 비활성화
			bc.mu.Lock()
			bc.IsActive = false
			bc.mu.Unlock()
			// 지능형 복구 시스템에 에러 보고
			if bc.OnError != nil {
				bc.OnError(fmt.Errorf("readMessage panic: %v", r))
			}
		}
	}()

	messageType, message, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("메시지 읽기 실패: %v", err)
	}

	if messageType != websocket.TextMessage {
		return nil, fmt.Errorf("지원하지 않는 메시지 타입: %d", messageType)
	}

	// 통계 업데이트는 쓰기 락으로 보호
	bc.mu.Lock()
	bc.LastMessageTime = time.Now()
	bc.TotalMessages++
	bc.mu.Unlock()

	return message, nil
}

// startPingLoop는 Ping 루프 시작 (연결 유지용, thread-safe with panic recovery)
func (bc *BaseConnector) startPingLoop(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				bc.mu.RLock()
				isConnected := bc.IsActive && bc.Connection != nil
				conn := bc.Connection
				bc.mu.RUnlock()

				if isConnected && conn != nil {
					func() {
						defer func() {
							if r := recover(); r != nil {
								log.Printf("⚠️ %s %s Ping panic recovered: %v", bc.Exchange, bc.MarketType, r)
								// 패닉 발생 시 쓰기 락으로 연결 상태를 비활성화
								bc.mu.Lock()
								bc.IsActive = false
								bc.mu.Unlock()
								// 지능형 복구 시스템에 에러 보고
								if bc.OnError != nil {
									bc.OnError(fmt.Errorf("ping panic: %v", r))
								}
							}
						}()

						if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
							log.Printf("⚠️ %s %s Ping 전송 실패: %v", bc.Exchange, bc.MarketType, err)
							if bc.OnError != nil {
								bc.OnError(err)
							}
						}
					}()
				}
			}
		}
	}()
}

// chunkSymbols는 심볼을 연결 제한에 따라 청크로 분할
func (bc *BaseConnector) chunkSymbols(symbols []string, maxPerChunk int) [][]string {
	var chunks [][]string

	for i := 0; i < len(symbols); i += maxPerChunk {
		end := i + maxPerChunk
		if end > len(symbols) {
			end = len(symbols)
		}
		chunks = append(chunks, symbols[i:end])
	}

	return chunks
}

// parseTradeMessage는 거래소별로 구현해야 하는 메시지 파싱 함수
type TradeMessageParser func([]byte) (models.TradeEvent, error)

// SubscriptionMessage는 구독 메시지 기본 구조
type SubscriptionMessage struct {
	Method string      `json:"method"`
	Params interface{} `json:"params"`
	ID     int         `json:"id"`
}

// createSubscriptionMessage는 기본 구독 메시지 생성
func createSubscriptionMessage(method string, params interface{}, id int) SubscriptionMessage {
	return SubscriptionMessage{
		Method: method,
		Params: params,
		ID:     id,
	}
}

// formatSymbol은 거래소별 심볼 형식 변환
func formatSymbol(symbol, exchange, marketType string) string {
	symbol = strings.ToUpper(symbol)

	switch exchange {
	case "binance":
		return strings.ToLower(symbol) // binance는 소문자
	case "bybit":
		return symbol // bybit은 대문자
	case "kucoin":
		if strings.Contains(symbol, "USDT") && !strings.Contains(symbol, "-USDT") {
			return strings.Replace(symbol, "USDT", "-USDT", -1)
		}
		return symbol
	case "okx":
		// OKX futures는 이미 올바른 형식 (BTC-USDT-SWAP)이므로 변환 안 함
		// OKX spot만 BTCUSDT -> BTC-USDT 변환 필요
		if marketType == "spot" && strings.Contains(symbol, "USDT") && !strings.Contains(symbol, "-") {
			return strings.Replace(symbol, "USDT", "-USDT", -1)
		}
		return symbol
	case "phemex":
		return symbol // phemex는 대문자
	case "gate":
		symbol = strings.ToUpper(symbol) // Gate.io uses uppercase symbols
		if strings.Contains(symbol, "USDT") && !strings.Contains(symbol, "_USDT") {
			return strings.Replace(symbol, "USDT", "_USDT", -1)
		}
		return symbol
	default:
		return symbol
	}
}

// normalizeSymbol은 심볼을 표준 형식으로 정규화
func normalizeSymbol(symbol string) string {
	// 모든 심볼을 BTCUSDT 형식으로 정규화
	symbol = strings.ToUpper(symbol)
	symbol = strings.Replace(symbol, "-", "", -1)
	symbol = strings.Replace(symbol, "_", "", -1)
	return symbol
}

// ConnectorFactory는 커넥터 생성자 함수 타입
type ConnectorFactory func(marketType string, maxSymbols int) WebSocketConnector

// 커넥터 레지스트리
var connectorRegistry = make(map[string]ConnectorFactory)
var registryMutex sync.RWMutex

// init 함수에서 기본 커넥터들 등록
func init() {
	RegisterConnector("binance", NewBinanceConnector)
	RegisterConnector("bybit", NewBybitConnector)
	RegisterConnector("kucoin", NewKuCoinConnector)
	RegisterConnector("okx", NewOKXConnector)
	RegisterConnector("phemex", NewPhemexConnector)
	RegisterConnector("gate", NewGateConnector)
}

// RegisterConnector는 새로운 커넥터를 레지스트리에 등록
func RegisterConnector(exchange string, factory ConnectorFactory) {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	connectorRegistry[exchange] = factory
}

// GetRegisteredExchanges는 등록된 거래소 목록 반환
func GetRegisteredExchanges() []string {
	registryMutex.RLock()
	defer registryMutex.RUnlock()

	exchanges := make([]string, 0, len(connectorRegistry))
	for exchange := range connectorRegistry {
		exchanges = append(exchanges, exchange)
	}
	return exchanges
}

// GetConnectorFactory는 거래소별 Connector Factory 반환 (레지스트리 기반)
func GetConnectorFactory(exchange string) (ConnectorFactory, error) {
	registryMutex.RLock()
	defer registryMutex.RUnlock()

	factory, exists := connectorRegistry[exchange]
	if !exists {
		return nil, fmt.Errorf("지원하지 않는 거래소: %s (지원 가능: %v)", exchange, GetRegisteredExchanges())
	}
	return factory, nil
}
