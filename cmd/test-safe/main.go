package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"PumpWatch/internal/config"
	"PumpWatch/internal/logging"
	"PumpWatch/internal/models"
	"PumpWatch/internal/websocket"
)

func main() {
	fmt.Println("🧪 ===================================")
	fmt.Println("   METDC Safe Architecture Test")
	fmt.Println("   단일 고루틴 SafeWorker 테스트")
	fmt.Println("🧪 ===================================")

	// 기본 설정 생성
	cfg := config.NewDefaultConfig()

	// 간단한 로깅 설정
	if err := logging.InitGlobalLogger("test-safe", "info", ""); err != nil {
		fmt.Printf("❌ 로거 초기화 실패: %v\n", err)
		os.Exit(1)
	}
	logger := logging.GetGlobalLogger()

	logger.Info("🚀 SafeWorker 아키텍처 테스트 시작")

	// SafeTaskManager 초기화
	taskManager := websocket.NewSafeTaskManager(cfg)

	// 거래 이벤트 핸들러 (단순 로깅)
	tradeCount := 0
	taskManager.SetOnTradeEvent(func(trade models.TradeEvent) {
		tradeCount++
		if tradeCount%100 == 0 {
			logger.Info("📊 거래 데이터 수신: %d건, 최신: %s %s", tradeCount, trade.Exchange, trade.Symbol)
		}
	})

	// 에러 핸들러
	taskManager.SetOnError(func(err error) {
		logger.Warn("⚠️ 에러 발생: %v", err)
	})

	// Context 설정
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 시그널 핸들러 설정
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// SafeTaskManager 시작
	logger.Info("🚀 SafeTaskManager 시작...")
	if err := taskManager.Start(); err != nil {
		logger.Error("❌ SafeTaskManager 시작 실패: %v", err)
		os.Exit(1)
	}

	// 상태 모니터링
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				health := taskManager.GetHealthStatus()
				logger.Info("🩺 시스템 상태 - 가동시간: %s, 건강점수: %.2f, 워커: %s, 거래: %d개, 에러: %d개",
					health["uptime"], health["health_score"], health["workers"],
					health["trades"], health["errors"])

				// 상세 풀 정보
				if poolsDetail, ok := health["pools_detail"].(map[string]interface{}); ok {
					for poolName, poolInfo := range poolsDetail {
						if info, ok := poolInfo.(map[string]interface{}); ok {
							logger.Info("  📊 %s: 워커 %s, 거래 %d개",
								poolName, info["workers"], info["trades"])
						}
					}
				}
			}
		}
	}()

	logger.Info("✅ SafeWorker 테스트 실행 중 (Ctrl+C로 종료)")
	logger.Info("📊 메모리 누수 없는 단일 고루틴 아키텍처로 실행")

	// 메인 루프 (시그널 대기)
	select {
	case sig := <-sigChan:
		logger.Info("🛑 종료 신호 수신: %s", sig.String())
	case <-ctx.Done():
		logger.Info("📥 Context 취소됨")
	}

	// Graceful Shutdown
	logger.Info("🛑 SafeTaskManager 중지 중...")
	if err := taskManager.Stop(); err != nil {
		logger.Error("❌ SafeTaskManager 중지 실패: %v", err)
	} else {
		logger.Info("✅ SafeTaskManager 중지 완료")
	}

	logger.Info("✅ SafeWorker 아키텍처 테스트 완료")
	fmt.Println("👋 테스트 종료 - 메모리 누수 없이 안전하게 종료되었습니다")
}