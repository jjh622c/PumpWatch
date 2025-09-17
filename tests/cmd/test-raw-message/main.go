package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// KuCoin 토큰 응답 구조체
type KuCoinTokenResponse struct {
	Code string `json:"code"`
	Data struct {
		Token           string `json:"token"`
		InstanceServers []struct {
			Endpoint     string `json:"endpoint"`
			Encrypt      bool   `json:"encrypt"`
			Protocol     string `json:"protocol"`
			PingInterval int    `json:"pingInterval"`
			PingTimeout  int    `json:"pingTimeout"`
		} `json:"instanceServers"`
	} `json:"data"`
}

func main() {
	fmt.Println("🔍 KuCoin Raw 메시지 수신 테스트")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// KuCoin 토큰 가져오기
	_, endpoint, err := getKuCoinToken()
	if err != nil {
		log.Fatalf("❌ KuCoin 토큰 가져오기 실패: %v", err)
	}

	fmt.Printf("✅ KuCoin 토큰 획득 성공\n")
	fmt.Printf("📡 연결 엔드포인트: %s\n", endpoint)

	// WebSocket 연결
	conn, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		log.Fatalf("❌ WebSocket 연결 실패: %v", err)
	}
	defer conn.Close()

	fmt.Println("🔌 WebSocket 연결 성공")

	// SOMI 구독 메시지 전송
	subscribeMsg := map[string]interface{}{
		"id":             fmt.Sprintf("%d", time.Now().UnixNano()),
		"type":           "subscribe",
		"topic":          "/market/match:SOMI-USDT",
		"privateChannel": false,
		"response":       true,
	}

	if err := conn.WriteJSON(subscribeMsg); err != nil {
		log.Fatalf("❌ 구독 메시지 전송 실패: %v", err)
	}

	fmt.Println("📡 SOMI-USDT 구독 메시지 전송 완료")

	// 메시지 수신 테스트
	messageCount := 0
	parseSuccessCount := 0
	parseFailureCount := 0

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// KuCoin ping 루프 시작 (18초마다)
	go func() {
		ticker := time.NewTicker(18 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingMsg := map[string]interface{}{
					"id":   fmt.Sprintf("%d", time.Now().UnixNano()),
					"type": "ping",
				}
				if err := conn.WriteJSON(pingMsg); err != nil {
					fmt.Printf("⚠️  Ping 전송 실패: %v\n", err)
				} else {
					fmt.Printf("🏓 Ping 전송\n")
				}
			}
		}
	}()

	fmt.Println("⏳ 2분간 메시지 수신 대기...")
	fmt.Println("🏓 Ping/Pong 처리 활성화")
	fmt.Println()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\n📊 테스트 결과:")
			fmt.Printf("   📨 총 수신 메시지: %d개\n", messageCount)
			fmt.Printf("   ✅ 파싱 성공: %d개\n", parseSuccessCount)
			fmt.Printf("   ❌ 파싱 실패: %d개\n", parseFailureCount)
			if messageCount == 0 {
				fmt.Println("🚨 치명적 문제: 메시지가 전혀 수신되지 않음")
			} else if parseSuccessCount == 0 {
				fmt.Println("🚨 치명적 문제: 메시지는 수신되지만 파싱이 모두 실패")
			} else {
				fmt.Println("✅ 메시지 수신 및 파싱 정상")
			}
			return

		default:
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("WebSocket 에러: %v", err)
					return
				}
				continue
			}

			messageCount++
			fmt.Printf("📨 [%d] 메시지 수신 (%d bytes): ", messageCount, len(message))

			// 메시지 파싱 테스트
			var parsedMessage map[string]interface{}
			if err := json.Unmarshal(message, &parsedMessage); err != nil {
				fmt.Printf("❌ JSON 파싱 실패: %v\n", err)
				parseFailureCount++
				continue
			}

			// 메시지 타입 확인
			msgType, _ := parsedMessage["type"].(string)
			subject, _ := parsedMessage["subject"].(string)
			topic, _ := parsedMessage["topic"].(string)

			if msgType == "message" && subject == "trade.l3match" {
				fmt.Printf("🎯 SOMI 거래 메시지!\n")
				fmt.Printf("   📍 Topic: %s\n", topic)
				if data, ok := parsedMessage["data"].(map[string]interface{}); ok {
					symbol, _ := data["symbol"].(string)
					price, _ := data["price"].(string)
					side, _ := data["side"].(string)
					fmt.Printf("   💰 %s: %s (%s)\n", symbol, price, side)
				}
				parseSuccessCount++
			} else if msgType == "ack" {
				fmt.Printf("✅ 구독 확인 메시지\n")
			} else if msgType == "pong" {
				fmt.Printf("🏓 Pong 응답 수신\n")
			} else if msgType == "welcome" {
				fmt.Printf("👋 Welcome 메시지\n")
			} else if msgType == "message" {
				fmt.Printf("📬 거래 메시지 (subject: %s, topic: %s)\n", subject, topic)
				if subject == "trade.l3match" {
					parseSuccessCount++
				}
			} else {
				fmt.Printf("ℹ️  기타 메시지 (type: %s)\n", msgType)
			}

			// 5초마다 상태 출력
			if messageCount%10 == 0 {
				fmt.Printf("\n📊 중간 상태: 수신 %d개, 거래 메시지 %d개\n\n", messageCount, parseSuccessCount)
			}
		}
	}
}

func getKuCoinToken() (string, string, error) {
	// KuCoin spot API에서 토큰 가져오기
	apiURL := "https://api.kucoin.com/api/v1/bullet-public"

	resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return "", "", fmt.Errorf("토큰 요청 실패: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("응답 읽기 실패: %v", err)
	}

	var tokenResp KuCoinTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", "", fmt.Errorf("JSON 파싱 실패: %v", err)
	}

	if tokenResp.Code != "200000" {
		return "", "", fmt.Errorf("토큰 획득 실패: %s", tokenResp.Code)
	}

	if len(tokenResp.Data.InstanceServers) == 0 {
		return "", "", fmt.Errorf("사용 가능한 WebSocket 서버가 없음")
	}

	// WebSocket URL 구성
	server := tokenResp.Data.InstanceServers[0]
	connectId := fmt.Sprintf("%d", time.Now().UnixNano())
	wsURL := fmt.Sprintf("%s?token=%s&connectId=%s",
		server.Endpoint, tokenResp.Data.Token, connectId)

	return tokenResp.Data.Token, wsURL, nil
}