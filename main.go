package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"noticepumpcatch/internal/config"
	"noticepumpcatch/internal/memory"
	"noticepumpcatch/internal/monitor"
	"noticepumpcatch/internal/triggers"
	"noticepumpcatch/internal/websocket"
)

func main() {
	log.Printf("🚀 NoticePumpCatch 시스템 시작")

	// 설정 로드
	cfg, err := config.LoadConfig("")
	if err != nil {
		log.Fatalf("❌ 설정 로드 실패: %v", err)
	}

	// 설정 유효성 검사
	if err := cfg.Validate(); err != nil {
		log.Fatalf("❌ 설정 유효성 검사 실패: %v", err)
	}

	log.Printf("✅ 설정 로드 완료")

	// 메모리 관리자 생성
	memManager := memory.NewManager(
		cfg.Memory.MaxOrderbooksPerSymbol,
		cfg.Memory.MaxTradesPerSymbol,
		1000, // 최대 시그널 수
		cfg.Memory.OrderbookRetentionMinutes,
	)
	log.Printf("✅ 메모리 관리자 생성 완료")

	// 성능 모니터 생성
	perfMonitor := monitor.NewPerformanceMonitor()
	log.Printf("✅ 성능 모니터 생성 완료")

	// 트리거 관리자 생성
	triggerConfig := &triggers.TriggerConfig{
		PumpDetection: triggers.PumpDetectionConfig{
			Enabled:              cfg.Triggers.PumpDetection.Enabled,
			MinScore:             cfg.Triggers.PumpDetection.MinScore,
			VolumeThreshold:      cfg.Triggers.PumpDetection.VolumeThreshold,
			PriceChangeThreshold: cfg.Triggers.PumpDetection.PriceChangeThreshold,
			TimeWindowSeconds:    cfg.Triggers.PumpDetection.TimeWindowSeconds,
		},
		Snapshot: triggers.SnapshotConfig{
			PreTriggerSeconds:  cfg.Triggers.Snapshot.PreTriggerSeconds,
			PostTriggerSeconds: cfg.Triggers.Snapshot.PostTriggerSeconds,
			MaxSnapshotsPerDay: cfg.Triggers.Snapshot.MaxSnapshotsPerDay,
		},
	}

	triggerManager := triggers.NewManager(triggerConfig, memManager)
	log.Printf("✅ 트리거 관리자 생성 완료")

	// WebSocket 클라이언트 생성
	binanceWS := websocket.NewBinanceWebSocket(
		cfg.GetSymbols(),
		memManager,
		cfg.WebSocket.WorkerCount,
		cfg.WebSocket.BufferSize,
	)
	log.Printf("✅ WebSocket 클라이언트 생성 완료")

	// 컨텍스트 생성
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// WebSocket 연결
	log.Printf("🔗 WebSocket 연결 시도 중...")
	if err := binanceWS.Connect(ctx); err != nil {
		log.Fatalf("❌ WebSocket 연결 실패: %v", err)
	}
	log.Printf("✅ WebSocket 연결 성공")

	// 모니터링 고루틴 시작
	go monitorSystem(memManager, binanceWS, perfMonitor, triggerManager)

	// 시그널 핸들링 (종료 처리)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 메인 루프
	log.Printf("🎯 시스템 실행 중... (Ctrl+C로 종료)")
	for {
		select {
		case <-sigChan:
			log.Printf("🛑 종료 신호 수신")
			cancel()
			time.Sleep(2 * time.Second)
			log.Printf("👋 시스템 종료")
			return
		case <-ctx.Done():
			log.Printf("🔴 컨텍스트 종료")
			return
		}
	}
}

// monitorSystem 시스템 모니터링
func monitorSystem(
	memManager *memory.Manager,
	binanceWS *websocket.BinanceWebSocket,
	perfMonitor *monitor.PerformanceMonitor,
	triggerManager *triggers.Manager,
) {
	ticker := time.NewTicker(30 * time.Second) // 30초마다 체크
	defer ticker.Stop()

	for range ticker.C {
		// 메모리 통계
		memStats := memManager.GetMemoryStats()
		log.Printf("📊 메모리 상태: 오더북 %v개, 체결 %v개, 시그널 %v개",
			memStats["total_orderbooks"], memStats["total_trades"], memStats["total_signals"])

		// WebSocket 통계
		wsStats := binanceWS.GetWorkerPoolStats()
		log.Printf("🔧 WebSocket: 연결=%v, 오더북버퍼=%v/%v, 체결버퍼=%v/%v",
			wsStats["is_connected"],
			wsStats["data_channel_buffer"], wsStats["data_channel_capacity"],
			wsStats["trade_channel_buffer"], wsStats["trade_channel_capacity"])

		// 성능 통계
		perfStats := perfMonitor.GetStats()
		log.Printf("⚡ 성능: 오버플로우 %v회, 지연 %v회",
			perfStats["overflow_count"], perfStats["delay_count"])

		// 트리거 통계
		triggerStats := triggerManager.GetStats()
		log.Printf("🚨 트리거: 총 %v개, 오늘 %v개",
			triggerStats.TotalTriggers, triggerStats.DailyTriggerCount)

		// 최근 시그널 확인
		recentSignals := memManager.GetRecentSignals(3)
		if len(recentSignals) > 0 {
			log.Printf("🚨 최근 시그널 %d개:", len(recentSignals))
			for _, signal := range recentSignals {
				log.Printf("   - %s: 점수 %.2f, 액션: %s",
					signal.Symbol, signal.CompositeScore, signal.Action)
			}
		}

		log.Printf("---")
	}
}
