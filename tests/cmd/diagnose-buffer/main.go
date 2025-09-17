package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"PumpWatch/internal/buffer"
	"PumpWatch/internal/config"
	"PumpWatch/internal/logging"
	"PumpWatch/internal/storage"
	"PumpWatch/internal/symbols"
	"PumpWatch/internal/websocket"
)

func main() {
	fmt.Println("🔍 SOMI 실시간 버퍼 진단 도구")
	fmt.Println("📊 메인 시스템의 CircularTradeBuffer에서 SOMI 데이터 실시간 확인")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	ctx := context.Background()

	// 로깅 초기화 (글로벌)
	if err := logging.InitGlobalLogger("buffer-diagnose", "info", "logs"); err != nil {
		fmt.Printf("❌ 로깅 초기화 실패: %v\n", err)
		os.Exit(1)
	}
	defer logging.CloseGlobalLogger()

	// 설정 로드
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		fmt.Printf("❌ 설정 로드 실패: %v\n", err)
		os.Exit(1)
	}

	// 심볼 설정 로드
	symbolsConfig, err := symbols.LoadConfig("config/symbols/symbols.yaml")
	if err != nil {
		fmt.Printf("❌ 심볼 설정 로드 실패: %v\n", err)
		os.Exit(1)
	}

	// 스토리지 매니저 초기화
	storageManager := storage.NewManager(cfg.Storage, cfg.Analysis)

	// EnhancedTaskManager 초기화 (이미 실행 중인 시스템에 연결)
	taskManager, err := websocket.NewEnhancedTaskManager(ctx, cfg.Exchanges, symbolsConfig, storageManager)
	if err != nil {
		fmt.Printf("❌ TaskManager 초기화 실패: %v\n", err)
		os.Exit(1)
	}

	// CircularTradeBuffer 접근
	circularBuffer := taskManager.GetCircularBuffer()
	if circularBuffer == nil {
		fmt.Println("❌ CircularTradeBuffer에 접근할 수 없습니다")
		os.Exit(1)
	}

	fmt.Println("✅ 메인 시스템 CircularTradeBuffer 접근 성공")

	// ⚡ 핵심: WebSocket 연결 시작 (데이터 수집을 위해 필수)
	fmt.Println("🔄 WebSocket 연결 시작 중...")
	if err := taskManager.Start(); err != nil {
		fmt.Printf("❌ WebSocket 시작 실패: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ WebSocket 연결 완료 - 데이터 수집 시작")
	fmt.Println("⏰ 30초 대기 후 진단 시작 (초기 데이터 축적)")
	time.Sleep(30 * time.Second)

	fmt.Println("⏱️  10초마다 SOMI 데이터 체크 (Ctrl+C로 종료)")
	fmt.Println()

	// 실시간 진단 루프
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	exchanges := []string{"binance_spot", "binance_futures", "bybit_spot", "kucoin_spot", "kucoin_futures", "gate_spot", "gate_futures"}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			diagnoseCurrent(circularBuffer, exchanges)
		}
	}
}

func diagnoseCurrent(buffer *buffer.CircularTradeBuffer, exchanges []string) {
	fmt.Printf("🕐 [%s] 실시간 SOMI 데이터 진단\n", time.Now().Format("15:04:05"))

	// 1. 전체 통계
	stats := buffer.GetStats()
	fmt.Printf("📈 전체 버퍼 통계:\n")
	fmt.Printf("   • 총 이벤트 수: %d\n", stats.TotalEvents)
	fmt.Printf("   • 핫 캐시 이벤트: %d\n", stats.HotEvents)
	fmt.Printf("   • 콜드 버퍼 이벤트: %d\n", stats.ColdEvents)
	fmt.Printf("   • 메모리 사용: %.1fMB\n", float64(stats.MemoryUsage)/(1024*1024))

	// 2. SOMI 전용 통계
	somiTotal := 0
	somiFound := false

	fmt.Printf("🎯 SOMI 거래소별 데이터:\n")
	for _, exchange := range exchanges {
		// 최근 5분 데이터 조회
		endTime := time.Now()
		startTime := endTime.Add(-5 * time.Minute)

		allTrades, err := buffer.GetTradeEvents(exchange, startTime, endTime)
		if err != nil {
			fmt.Printf("   ❌ %s: 에러 - %v\n", exchange, err)
			continue
		}

		// SOMI 필터링
		var somiTrades []interface{}
		for _, trade := range allTrades {
			if strings.Contains(strings.ToUpper(trade.Symbol), "SOMI") {
				somiTrades = append(somiTrades, trade)
			}
		}

		somiCount := len(somiTrades)
		somiTotal += somiCount

		if somiCount > 0 {
			somiFound = true
			fmt.Printf("   ✅ %s: %d건 SOMI (전체 %d건 중)\n",
				exchange, somiCount, len(allTrades))
		} else {
			fmt.Printf("   ⭕ %s: 0건 SOMI (전체 %d건)\n", exchange, len(allTrades))
		}
	}

	// 3. 진단 결과
	if somiFound {
		fmt.Printf("🎉 SOMI 데이터 발견: 총 %d건 (최근 5분)\n", somiTotal)

		// 4. 샘플 CollectionEvent 생성 테스트
		testCollectionEvent(buffer)
	} else {
		fmt.Printf("⚠️  SOMI 데이터 없음 (최근 5분)\n")

		// 버퍼에 다른 데이터라도 있는지 확인
		if stats.TotalEvents > 0 {
			fmt.Printf("📊 다른 심볼 데이터는 존재: %d건\n", stats.TotalEvents)
		} else {
			fmt.Printf("🚨 전체 버퍼가 비어있음 - 연결 문제 의심\n")
		}
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

func testCollectionEvent(buffer *buffer.CircularTradeBuffer) {
	fmt.Printf("🧪 CollectionEvent 변환 테스트:\n")

	// 20초 범위 데이터로 CollectionEvent 생성
	triggerTime := time.Now()

	collectionEvent, err := buffer.ToCollectionEvent("SOMI", triggerTime)
	if err != nil {
		fmt.Printf("   ❌ CollectionEvent 생성 실패: %v\n", err)
		return
	}

	// CollectionEvent 통계
	totalTrades := 0
	exchangesWithData := 0

	if len(collectionEvent.BinanceSpot) > 0 {
		totalTrades += len(collectionEvent.BinanceSpot)
		exchangesWithData++
		fmt.Printf("   ✅ Binance Spot: %d건\n", len(collectionEvent.BinanceSpot))
	}
	if len(collectionEvent.BinanceFutures) > 0 {
		totalTrades += len(collectionEvent.BinanceFutures)
		exchangesWithData++
		fmt.Printf("   ✅ Binance Futures: %d건\n", len(collectionEvent.BinanceFutures))
	}
	if len(collectionEvent.BybitSpot) > 0 {
		totalTrades += len(collectionEvent.BybitSpot)
		exchangesWithData++
		fmt.Printf("   ✅ Bybit Spot: %d건\n", len(collectionEvent.BybitSpot))
	}
	if len(collectionEvent.KuCoinSpot) > 0 {
		totalTrades += len(collectionEvent.KuCoinSpot)
		exchangesWithData++
		fmt.Printf("   ✅ KuCoin Spot: %d건\n", len(collectionEvent.KuCoinSpot))
	}
	if len(collectionEvent.KuCoinFutures) > 0 {
		totalTrades += len(collectionEvent.KuCoinFutures)
		exchangesWithData++
		fmt.Printf("   ✅ KuCoin Futures: %d건\n", len(collectionEvent.KuCoinFutures))
	}
	if len(collectionEvent.GateSpot) > 0 {
		totalTrades += len(collectionEvent.GateSpot)
		exchangesWithData++
		fmt.Printf("   ✅ Gate Spot: %d건\n", len(collectionEvent.GateSpot))
	}
	if len(collectionEvent.GateFutures) > 0 {
		totalTrades += len(collectionEvent.GateFutures)
		exchangesWithData++
		fmt.Printf("   ✅ Gate Futures: %d건\n", len(collectionEvent.GateFutures))
	}

	if totalTrades > 0 {
		fmt.Printf("   🎯 총 %d개 거래소에서 %d건의 SOMI 데이터\n", exchangesWithData, totalTrades)
		fmt.Printf("   ✅ CollectionEvent 변환 성공 - 파일 저장 시 null 문제 없음\n")
	} else {
		fmt.Printf("   ⚠️  CollectionEvent 비어있음 - 이 상태로 저장하면 null 파일 생성\n")
	}
}