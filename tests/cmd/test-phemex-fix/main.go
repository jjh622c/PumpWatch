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

// Phemex subscription method 수정 테스트
func main() {
	fmt.Println("🔍 Phemex trade_p.subscribe 수정 테스트")
	fmt.Println("공식 문서 기준 올바른 메서드로 BTC 체결 데이터 수신 테스트")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Phemex spot 커넥터 생성
	connector := connectors.NewPhemexConnector("spot", 10)

	// BTC 심볼로 연결
	symbols := []string{"BTCUSDT"}

	if err := connector.Connect(ctx, symbols); err != nil {
		log.Printf("❌ Phemex 연결 실패: %v", err)
		return
	}

	fmt.Println("✅ Phemex 연결 성공")

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
			fmt.Printf("📊 Phemex 메시지 #%d: %s %s@%s (%.8f x %.2f)\n",
				messageCount, trade.Symbol, trade.Side, trade.Price,
				parseFloat(trade.Quantity), parseFloat(trade.Price))

			if messageCount >= 3 {
				fmt.Printf("✅ Phemex 3개 메시지 수신 성공! trade_p.subscribe 수정 완료!\n")
				connector.Disconnect()
				return
			}

		case <-timeout:
			fmt.Printf("⏰ Phemex 20초 타임아웃 - 수신된 메시지: %d개\n", messageCount)
			if messageCount > 0 {
				fmt.Println("✅ Phemex 메시지 수신 성공! trade_p.subscribe 수정 완료!")
			} else {
				fmt.Println("❌ Phemex 여전히 메시지 수신 실패 - 추가 디버깅 필요")
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