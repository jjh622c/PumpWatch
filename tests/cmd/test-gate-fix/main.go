package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"PumpWatch/internal/models"
	"PumpWatch/internal/websocket/connectors"
)

// Gate.io 메시지 수신 디버깅 테스트
func main() {
	fmt.Println("🔍 Gate.io 메시지 수신 디버깅 테스트")
	fmt.Println("BTC로 실제 메시지가 들어오는지 확인")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Gate spot 커넥터 생성
	connector := connectors.NewGateConnector("spot", 10)

	// BTC 심볼로 연결 (Gate 형식: BTC_USDT)
	symbols := []string{"BTC_USDT"}

	if err := connector.Connect(ctx, symbols); err != nil {
		log.Printf("❌ Gate 연결 실패: %v", err)
		return
	}

	fmt.Println("✅ Gate 연결 성공")

	// 메시지 수신 테스트
	messageChan := make(chan models.TradeEvent, 100)

	if err := connector.StartMessageLoop(ctx, messageChan); err != nil {
		log.Printf("❌ 메시지 루프 시작 실패: %v", err)
		return
	}

	fmt.Println("📡 BTC 메시지 수신 대기 중 (20초)...")

	messageCount := 0
	timeout := time.After(20 * time.Second)

	for {
		select {
		case trade := <-messageChan:
			messageCount++
			fmt.Printf("📊 Gate 메시지 #%d: %s %s@%s (%.8f x %.2f)\n",
				messageCount, trade.Symbol, trade.Side, trade.Price,
				parseFloat(trade.Quantity), parseFloat(trade.Price))

			if messageCount >= 3 {
				fmt.Printf("✅ Gate 3개 메시지 수신 성공!\n")
				connector.Disconnect()
				return
			}

		case <-timeout:
			fmt.Printf("⏰ Gate 20초 타임아웃 - 수신된 메시지: %d개\n", messageCount)
			if messageCount > 0 {
				fmt.Println("✅ Gate 메시지 수신 성공!")
			} else {
				fmt.Println("❌ Gate 여전히 메시지 수신 실패 - 구독 또는 파싱 문제 추정")
			}
			connector.Disconnect()
			return

		case <-ctx.Done():
			connector.Disconnect()
			return
		}
	}
}

func parseFloat(s string) float64 {
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return 0.0
}