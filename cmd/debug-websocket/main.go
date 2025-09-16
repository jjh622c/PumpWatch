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

// 웹소켓 메시지 수신 디버깅 테스트
func main() {
	fmt.Println("🔍 실패한 거래소 메시지 수신 디버깅 테스트")
	fmt.Println("KuCoin, Phemex, Gate에서 BTC 메시지가 들어오는지 확인")

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// KuCoin Spot BTC 테스트
	fmt.Println("\n=== KuCoin Spot BTC 테스트 ===")
	testKuCoinSpotBTC(ctx)

	// Phemex Spot BTC 테스트
	fmt.Println("\n=== Phemex Spot BTC 테스트 ===")
	testPhemexSpotBTC(ctx)

	// Gate Spot BTC 테스트
	fmt.Println("\n=== Gate Spot BTC 테스트 ===")
	testGateSpotBTC(ctx)
}

func testKuCoinSpotBTC(ctx context.Context) {
	connector := connectors.NewKuCoinConnector("spot", 10)

	// BTC 심볼로 연결
	symbols := []string{"BTC-USDT"}

	if err := connector.Connect(ctx, symbols); err != nil {
		log.Printf("❌ KuCoin 연결 실패: %v", err)
		return
	}

	fmt.Println("✅ KuCoin 연결 성공")

	// 메시지 수신 테스트
	messageChan := make(chan models.TradeEvent, 100)

	if err := connector.StartMessageLoop(ctx, messageChan); err != nil {
		log.Printf("❌ 메시지 루프 시작 실패: %v", err)
		return
	}

	fmt.Println("📡 메시지 수신 대기 중 (15초)...")

	messageCount := 0
	timeout := time.After(15 * time.Second)

	for {
		select {
		case trade := <-messageChan:
			messageCount++
			fmt.Printf("📊 KuCoin 메시지 #%d: %s %s@%s (%.8f x %.2f)\n",
				messageCount, trade.Symbol, trade.Side, trade.Price,
				parseFloat(trade.Quantity), parseFloat(trade.Price))

			if messageCount >= 3 {
				fmt.Printf("✅ KuCoin 3개 메시지 수신 완료!\n")
				return
			}

		case <-timeout:
			fmt.Printf("⏰ KuCoin 15초 타임아웃 - 수신된 메시지: %d개\n", messageCount)
			if messageCount == 0 {
				fmt.Println("❌ KuCoin 메시지 수신 실패!")
			}
			return

		case <-ctx.Done():
			return
		}
	}
}

func testPhemexSpotBTC(ctx context.Context) {
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

	fmt.Println("📡 메시지 수신 대기 중 (15초)...")

	messageCount := 0
	timeout := time.After(15 * time.Second)

	for {
		select {
		case trade := <-messageChan:
			messageCount++
			fmt.Printf("📊 Phemex 메시지 #%d: %s %s@%s (%.8f x %.2f)\n",
				messageCount, trade.Symbol, trade.Side, trade.Price,
				parseFloat(trade.Quantity), parseFloat(trade.Price))

			if messageCount >= 3 {
				fmt.Printf("✅ Phemex 3개 메시지 수신 완료!\n")
				return
			}

		case <-timeout:
			fmt.Printf("⏰ Phemex 15초 타임아웃 - 수신된 메시지: %d개\n", messageCount)
			if messageCount == 0 {
				fmt.Println("❌ Phemex 메시지 수신 실패!")
			}
			return

		case <-ctx.Done():
			return
		}
	}
}

func testGateSpotBTC(ctx context.Context) {
	connector := connectors.NewGateConnector("spot", 10)

	// BTC 심볼로 연결
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

	fmt.Println("📡 메시지 수신 대기 중 (15초)...")

	messageCount := 0
	timeout := time.After(15 * time.Second)

	for {
		select {
		case trade := <-messageChan:
			messageCount++
			fmt.Printf("📊 Gate 메시지 #%d: %s %s@%s (%.8f x %.2f)\n",
				messageCount, trade.Symbol, trade.Side, trade.Price,
				parseFloat(trade.Quantity), parseFloat(trade.Price))

			if messageCount >= 3 {
				fmt.Printf("✅ Gate 3개 메시지 수신 완료!\n")
				return
			}

		case <-timeout:
			fmt.Printf("⏰ Gate 15초 타임아웃 - 수신된 메시지: %d개\n", messageCount)
			if messageCount == 0 {
				fmt.Println("❌ Gate 메시지 수신 실패!")
			}
			return

		case <-ctx.Done():
			return
		}
	}
}

func parseFloat(s string) float64 {
	// 간단한 float 파싱 (에러 처리 생략)
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return 0.0
}