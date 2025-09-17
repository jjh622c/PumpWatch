package main

import (
	"context"
	"fmt"
	"time"

	"PumpWatch/internal/models"
	"PumpWatch/internal/websocket/connectors"
)

// KuCoin 실제 모니터링 심볼로 테스트
func main() {
	fmt.Println("🔍 KuCoin 실제 모니터링 심볼 테스트")
	fmt.Println("XMR-USDT로 실제 메시지가 들어오는지 확인 (모네로 - 거래량 많은 코인)")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// KuCoin spot 커넥터 생성
	connector := connectors.NewKuCoinConnector("spot", 10)

	// XMR-USDT 심볼로 연결 (모네로 - 거래량이 많은 코인)
	symbols := []string{"XMR-USDT"}

	if err := connector.Connect(ctx, symbols); err != nil {
		fmt.Printf("❌ KuCoin 연결 실패: %v\n", err)
		return
	}

	fmt.Println("✅ KuCoin 연결 성공")

	// 메시지 수신 테스트
	messageChan := make(chan models.TradeEvent, 100)

	if err := connector.StartMessageLoop(ctx, messageChan); err != nil {
		fmt.Printf("❌ 메시지 루프 시작 실패: %v\n", err)
		return
	}

	fmt.Println("📡 CHR-USDT 메시지 수신 대기 중 (30초)...")

	messageCount := 0
	timeout := time.After(30 * time.Second)

	for {
		select {
		case trade := <-messageChan:
			messageCount++
			fmt.Printf("📊 KuCoin CHR 메시지 #%d: %s %s@%s (%.8f)\n",
				messageCount, trade.Symbol, trade.Side, trade.Price, parseFloat(trade.Quantity))

			if messageCount >= 3 {
				fmt.Printf("✅ KuCoin 3개 메시지 수신 완료!\n")
				connector.Disconnect()
				return
			}

		case <-timeout:
			fmt.Printf("⏰ KuCoin 30초 타임아웃 - 수신된 메시지: %d개\n", messageCount)
			if messageCount > 0 {
				fmt.Println("✅ KuCoin 메시지 수신 성공!")
			} else {
				fmt.Println("❌ KuCoin 메시지 수신 실패!")
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
	// 간단한 float 파싱
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}