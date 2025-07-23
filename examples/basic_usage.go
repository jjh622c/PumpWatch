// Package main - 기본 사용법 예제
//
// 이 예제는 NoticePumpCatch 펌핑 감지 시스템을 다른 거래 프로그램에
// 통합하는 가장 기본적인 방법을 보여줍니다.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"noticepumpcatch/internal/interfaces"
	"noticepumpcatch/pkg/detector"
)

func main() {
	fmt.Println("🚀 NoticePumpCatch 기본 사용법 예제")
	fmt.Println("===================================")

	// 1단계: 펌핑 감지기 생성
	fmt.Println("📡 펌핑 감지기 생성 중...")
	pumpDetector, err := detector.NewDetector("config.json")
	if err != nil {
		log.Fatal("감지기 생성 실패:", err)
	}
	fmt.Println("✅ 감지기 생성 완료")

	// 2단계: 펌핑 감지 콜백 설정
	fmt.Println("🔔 펌핑 감지 콜백 설정 중...")
	pumpDetector.SetPumpCallback(func(event interfaces.PumpEvent) {
		fmt.Printf("\n🚨 [펌핑 감지] %s\n", event.Symbol)
		fmt.Printf("   💹 가격 변동: +%.2f%% (%.4f → %.4f)\n",
			event.PriceChange, event.PreviousPrice, event.CurrentPrice)
		fmt.Printf("   📊 신뢰도: %.1f%%\n", event.Confidence)
		fmt.Printf("   ⏰ 시간: %s\n", event.Timestamp.Format("15:04:05"))
		fmt.Printf("   💡 권장 액션: %s\n", event.Action)

		// 여기서 실제 거래 로직을 실행하세요
		// 예: trading.Buy(event.Symbol, event.CurrentPrice)

		// 5% 이상 급등시 특별 처리
		if event.PriceChange >= 5.0 {
			fmt.Printf("   🔥 5%% 이상 급등! 즉시 매수 고려\n")
		}

		fmt.Println("   " + strings.Repeat("-", 40))
	})

	// 3단계: 상장공시 감지 콜백 설정
	fmt.Println("📢 상장공시 감지 콜백 설정 중...")
	pumpDetector.SetListingCallback(func(event interfaces.ListingEvent) {
		fmt.Printf("\n📢 [상장공시] %s\n", event.Symbol)
		fmt.Printf("   🏢 거래소: %s\n", event.Exchange)
		fmt.Printf("   📝 소스: %s\n", event.Source)
		fmt.Printf("   📊 신뢰도: %.1f%%\n", event.Confidence)
		fmt.Printf("   ⏰ 시간: %s\n", event.Timestamp.Format("15:04:05"))

		// 상장공시는 매우 중요한 신호입니다!
		if event.Confidence > 90.0 {
			fmt.Printf("   🎯 높은 신뢰도! 즉시 매수 권장\n")
			// 여기서 즉시 매수 로직 실행
			// 예: trading.BuyAtMarket(event.Symbol)
		}

		fmt.Println("   " + strings.Repeat("-", 40))
	})

	// 4단계: 시스템 시작
	fmt.Println("🚀 펌핑 감지 시스템 시작 중...")
	if err := pumpDetector.Start(); err != nil {
		log.Fatal("시스템 시작 실패:", err)
	}
	fmt.Println("✅ 시스템 시작 완료")

	// 5단계: 실시간 상태 모니터링
	go monitorSystem(pumpDetector)

	// 6단계: 종료 신호 대기
	fmt.Println("📊 실시간 모니터링 중... (Ctrl+C로 종료)")

	// 안전한 종료를 위한 시그널 핸들링
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	fmt.Println("\n🛑 종료 신호 수신 - 시스템 정리 중...")

	// 7단계: 안전한 종료
	if err := pumpDetector.Stop(); err != nil {
		log.Printf("종료 중 오류: %v", err)
	}

	fmt.Println("✅ 시스템 정리 완료 - 안전하게 종료됨")
}

// monitorSystem 시스템 상태를 주기적으로 모니터링
func monitorSystem(detector interfaces.PumpDetector) {
	ticker := time.NewTicker(30 * time.Second) // 30초마다 상태 확인
	defer ticker.Stop()

	for range ticker.C {
		// 현재 상태 조회
		status := detector.GetStatus()
		stats := detector.GetStats()

		fmt.Printf("\n📊 [시스템 상태] %s\n", time.Now().Format("15:04:05"))
		fmt.Printf("   🟢 실행 중: %v\n", status.IsRunning)
		fmt.Printf("   📡 모니터링 심볼: %d개\n", status.SymbolCount)
		fmt.Printf("   🔗 WebSocket: %s\n", status.WebSocketStatus)

		if stats.TotalPumpEvents > 0 {
			fmt.Printf("   🚨 감지된 펌핑: %d회 (평균 +%.2f%%)\n",
				stats.TotalPumpEvents, stats.AvgPumpChange)
			fmt.Printf("   🔥 최대 급등률: +%.2f%%\n", stats.MaxPumpChange)
		}

		if stats.TotalListingEvents > 0 {
			fmt.Printf("   📢 상장공시: %d회\n", stats.TotalListingEvents)
		}

		fmt.Printf("   💾 메모리: %.1fMB\n", stats.MemoryUsageMB)
		fmt.Printf("   🔄 고루틴: %d개\n", stats.GoroutineCount)

		uptime := time.Duration(stats.UptimeSeconds) * time.Second
		fmt.Printf("   ⏱️ 가동시간: %v\n", uptime.Round(time.Second))

		fmt.Println("   " + strings.Repeat("-", 30))
	}
}
