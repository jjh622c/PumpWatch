package websocket

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"PumpWatch/internal/logging"
	"PumpWatch/internal/models"
	"PumpWatch/internal/websocket/connectors"
)

// SafeWorker는 메모리 누수 없는 단일 고루틴 워커
// "무식하게 때려박기" 철학: 단순하고 확실한 구조
type SafeWorker struct {
	// 기본 정보
	ID         int
	Exchange   string
	MarketType string
	Symbols    []string

	// Context 기반 생명주기 관리 (메모리 누수 방지)
	ctx    context.Context
	cancel context.CancelFunc

	// 연결 관리 (단일 뮤텍스로 안전성 보장)
	mu        sync.RWMutex
	conn      *websocket.Conn
	connected bool

	// 통합 채널들 (백프레셔 처리)
	messageChan chan []byte              // WebSocket 원시 메시지
	tradeChan   chan models.TradeEvent   // 파싱된 거래 데이터

	// 타이머들 (통합 루프에서 관리)
	pingTicker     *time.Ticker
	reconnectTimer *time.Timer

	// 통계 (atomic 연산으로 안전성 보장)
	stats SafeWorkerStats

	// 거래소별 커넥터
	connector connectors.WebSocketConnector

	// 콜백
	onTradeEvent func(models.TradeEvent)
	onError      func(error)
	onConnected  func()

	// 로깅
	logger *logging.Logger

	// 상태 플래그
	running int32 // atomic
}

// SafeWorkerStats는 원자적 연산으로 안전한 통계
type SafeWorkerStats struct {
	MessageCount     int64 // atomic
	TradeCount       int64 // atomic
	ErrorCount       int64 // atomic
	ReconnectCount   int64 // atomic
	LastMessageTime  int64 // atomic (unix nano)
	ConnectionUptime int64 // atomic (unix nano)
}

// NewSafeWorker는 새로운 안전한 워커 생성
func NewSafeWorker(id int, exchange, marketType string, symbols []string, connector connectors.WebSocketConnector) *SafeWorker {
	ctx, cancel := context.WithCancel(context.Background())

	return &SafeWorker{
		ID:         id,
		Exchange:   exchange,
		MarketType: marketType,
		Symbols:    symbols,
		ctx:        ctx,
		cancel:     cancel,

		// 백프레셔 처리를 위한 버퍼 크기
		messageChan: make(chan []byte, 2000),    // 원시 메시지용
		tradeChan:   make(chan models.TradeEvent, 500000), // 거래 데이터용 - 상장 펌핑 대응

		connector: connector,

		// 25초 간격으로 ping (대부분 거래소는 30초 타임아웃)
		pingTicker: time.NewTicker(25 * time.Second),
	}
}

// Start는 워커 시작 (단일 고루틴으로 모든 처리)
func (w *SafeWorker) Start() {
	if !atomic.CompareAndSwapInt32(&w.running, 0, 1) {
		return // 이미 실행 중
	}

	w.initLogger()
	w.logger.Info("🚀 SafeWorker %d 시작: %s %s, %d 심볼",
		w.ID, w.Exchange, w.MarketType, len(w.Symbols))

	// Context 취소 시 정리 작업 예약
	defer func() {
		w.cleanup()
		atomic.StoreInt32(&w.running, 0)
		w.logger.Info("✅ SafeWorker %d 완전 종료", w.ID)
	}()

	// 통합 이벤트 루프 (메모리 누수 방지)
	w.eventLoop()
}

// eventLoop는 모든 이벤트를 단일 루프에서 처리 (핵심 메소드)
func (w *SafeWorker) eventLoop() {
	connectTimer := time.NewTimer(0) // 즉시 첫 연결 시도
	defer connectTimer.Stop()

	for {
		select {
		case <-w.ctx.Done():
			w.logger.Info("📥 SafeWorker %d Context 취소됨", w.ID)
			return

		case <-connectTimer.C:
			w.attemptConnection()
			// 재연결 타이머 설정 (연결 실패 시)
			if !w.isConnected() {
				backoff := time.Duration(atomic.LoadInt64(&w.stats.ReconnectCount)+1) * time.Second
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
				connectTimer.Reset(backoff)
			}

		case <-w.pingTicker.C:
			if w.isConnected() {
				w.sendPing()
			}

		case rawMsg := <-w.messageChan:
			w.processRawMessage(rawMsg)

		case tradeEvent := <-w.tradeChan:
			atomic.AddInt64(&w.stats.TradeCount, 1)
			if w.onTradeEvent != nil {
				w.onTradeEvent(tradeEvent)
			}
		}
	}
}

// attemptConnection은 연결 시도 (안전한 뮤텍스 패턴)
func (w *SafeWorker) attemptConnection() {
	if w.isConnected() {
		return
	}

	w.logger.Debug("🔌 SafeWorker %d 연결 시도", w.ID)

	err := w.connector.Connect(w.ctx, w.Symbols)
	if err != nil {
		atomic.AddInt64(&w.stats.ErrorCount, 1)
		w.logger.Warn("❌ SafeWorker %d 연결 실패: %v", w.ID, err)
		if w.onError != nil {
			w.onError(fmt.Errorf("worker %d connection failed: %w", w.ID, err))
		}
		return
	}

	// 연결 상태 업데이트 (단일 쓰기 락)
	w.mu.Lock()
	w.connected = true
	w.conn = w.connector.GetConnection() // 실제 WebSocket 연결 객체 저장
	atomic.StoreInt64(&w.stats.ConnectionUptime, time.Now().UnixNano())
	atomic.AddInt64(&w.stats.ReconnectCount, 1)
	w.mu.Unlock()

	w.logger.Info("✅ SafeWorker %d 연결 성공", w.ID)

	if w.onConnected != nil {
		w.onConnected()
	}

	// 메시지 수신 시작 (동일한 고루틴에서)
	go w.receiveMessages()
}

// receiveMessages는 메시지 수신 (별도 고루틴이지만 Context로 관리)
func (w *SafeWorker) receiveMessages() {
	defer func() {
		// 연결 끊어짐 처리
		w.mu.Lock()
		w.connected = false
		w.mu.Unlock()
		w.logger.Debug("📡 SafeWorker %d 메시지 수신 종료", w.ID)
	}()

	for {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		if !w.isConnected() {
			return
		}

		// WebSocket에서 직접 읽기 (타임아웃 설정)
		conn := w.getConnection()
		if conn == nil {
			return
		}

		conn.SetReadDeadline(time.Now().Add(35 * time.Second)) // ping 간격보다 길게
		_, message, err := conn.ReadMessage()
		if err != nil {
			atomic.AddInt64(&w.stats.ErrorCount, 1)
			w.logger.Warn("📡 SafeWorker %d 메시지 읽기 실패: %v", w.ID, err)
			return // 연결 끊어짐
		}

		atomic.StoreInt64(&w.stats.LastMessageTime, time.Now().UnixNano())
		atomic.AddInt64(&w.stats.MessageCount, 1)

		// 백프레셔 처리
		select {
		case w.messageChan <- message:
		default:
			// 채널 가득참 - 로그만 남기고 계속 진행 ("무식하게 때려박기")
			w.logger.Warn("⚠️ SafeWorker %d 메시지 채널 가득참", w.ID)
		}
	}
}

// processRawMessage는 원시 메시지 처리
func (w *SafeWorker) processRawMessage(message []byte) {
	w.logger.Debug("📨 SafeWorker %d 메시지 수신: %d bytes", w.ID, len(message))

	// 거래소별 파싱 로직 실행
	tradeEvents, err := w.connector.ParseTradeMessage(message)
	if err != nil {
		// 파싱 실패는 일반적 (ping/pong, 구독 응답 등) - 디버그 레벨로만 로그
		w.logger.Debug("📨 SafeWorker %d 파싱 건너뜀: %v", w.ID, err)
		return
	}

	// 파싱된 거래 이벤트들을 tradeChan으로 전송
	for _, tradeEvent := range tradeEvents {
		select {
		case w.tradeChan <- tradeEvent:
			// 성공적으로 전송됨
		default:
			// 백프레셔 처리 - 채널 가득참 시 로그만 남기고 버림
			w.logger.Warn("⚠️ SafeWorker %d 거래 채널 가득함 - 데이터 버림", w.ID)
		}
	}
}

// sendPing은 ping 전송 (안전한 뮤텍스 패턴)
func (w *SafeWorker) sendPing() {
	conn := w.getConnection()
	if conn == nil {
		return
	}

	err := conn.WriteMessage(websocket.PingMessage, []byte{})
	if err != nil {
		w.logger.Warn("💔 SafeWorker %d Ping 전송 실패: %v", w.ID, err)
		w.mu.Lock()
		w.connected = false
		w.mu.Unlock()
	}
}

// isConnected는 연결 상태 확인 (thread-safe)
func (w *SafeWorker) isConnected() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.connected && w.conn != nil
}

// getConnection은 연결 객체 안전하게 가져오기
func (w *SafeWorker) getConnection() *websocket.Conn {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.connected && w.conn != nil {
		return w.conn
	}
	return nil
}

// Stop은 워커 중지 (완전한 정리)
func (w *SafeWorker) Stop() {
	w.logger.Info("🛑 SafeWorker %d 중지 시작", w.ID)
	w.cancel() // Context 취소로 모든 고루틴 정리
}

// cleanup은 리소스 정리
func (w *SafeWorker) cleanup() {
	// 타이머 정리
	if w.pingTicker != nil {
		w.pingTicker.Stop()
	}
	if w.reconnectTimer != nil {
		w.reconnectTimer.Stop()
	}

	// 연결 정리
	w.mu.Lock()
	if w.conn != nil {
		w.conn.Close()
		w.conn = nil
	}
	w.connected = false
	w.mu.Unlock()

	// 채널 정리 (채널은 GC가 처리)
	w.logger.Info("🧹 SafeWorker %d 리소스 정리 완료", w.ID)
}

// initLogger는 로거 초기화
func (w *SafeWorker) initLogger() {
	if w.logger == nil {
		globalLogger := logging.GetGlobalLogger()
		if globalLogger != nil {
			w.logger = globalLogger.WebSocketLogger(w.Exchange, w.MarketType)
		}
	}
}

// GetStats는 현재 통계 반환 (atomic 읽기)
func (w *SafeWorker) GetStats() SafeWorkerStats {
	return SafeWorkerStats{
		MessageCount:     atomic.LoadInt64(&w.stats.MessageCount),
		TradeCount:       atomic.LoadInt64(&w.stats.TradeCount),
		ErrorCount:       atomic.LoadInt64(&w.stats.ErrorCount),
		ReconnectCount:   atomic.LoadInt64(&w.stats.ReconnectCount),
		LastMessageTime:  atomic.LoadInt64(&w.stats.LastMessageTime),
		ConnectionUptime: atomic.LoadInt64(&w.stats.ConnectionUptime),
	}
}

// SetOnTradeEvent는 거래 이벤트 콜백 설정
func (w *SafeWorker) SetOnTradeEvent(callback func(models.TradeEvent)) {
	w.onTradeEvent = callback
}

// SetOnError는 에러 콜백 설정
func (w *SafeWorker) SetOnError(callback func(error)) {
	w.onError = callback
}

// SetOnConnected는 연결 성공 콜백 설정
func (w *SafeWorker) SetOnConnected(callback func()) {
	w.onConnected = callback
}