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
	"noticepumpcatch/internal/notification"
	"noticepumpcatch/internal/server"
	"noticepumpcatch/internal/websocket"
)

func main() {
	log.Printf("🚀 초고속 펌핑 분석 시스템 시작")
	log.Printf("⚡ Go 고성능 WebSocket 오더북 수집기")

	// 설정 로드
	cfg, err := config.LoadConfig("")
	if err != nil {
		log.Fatalf("❌ 설정 로드 실패: %v", err)
	}

	// 메모리 관리자 생성
	memManager := memory.NewManager()

	// 성능 모니터 생성
	perfMonitor := monitor.NewPerformanceMonitor()

	// 시스템 모니터 생성
	sysMonitor := monitor.NewSystemMonitor()

	// 알림 관리자 생성
	notifManager := notification.NewManager(
		cfg.Notification.SlackWebhook,
		cfg.Notification.TelegramToken,
		cfg.Notification.TelegramChatID,
	)

	// 컨텍스트 생성
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 바이낸스 WebSocket 클라이언트 생성
	binanceWS := websocket.NewBinanceWebSocket(cfg.GetSymbols(), memManager)

	// WebSocket 연결
	if err := binanceWS.Connect(ctx); err != nil {
		log.Fatalf("❌ WebSocket 연결 실패: %v", err)
	}
	defer binanceWS.Close()

	// HTTP 서버 생성 및 시작
	httpServer := server.NewServer(
		cfg.Server.Port,
		memManager,
		binanceWS,
		notifManager,
		perfMonitor,
		sysMonitor,
	)

	// HTTP 서버 시작 (고루틴)
	go func() {
		if err := httpServer.Start(); err != nil {
			log.Printf("❌ HTTP 서버 시작 실패: %v", err)
		}
	}()

	// 메모리 모니터링 고루틴 시작
	go monitorMemory(memManager, binanceWS, perfMonitor, sysMonitor)

	// 시그널 대기
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Printf("✅ 시스템 준비 완료! Ctrl+C로 종료")
	log.Printf("📊 대시보드: http://localhost:%d", cfg.Server.Port)
	log.Printf("💾 중요 시그널 저장: ./signals/ 디렉토리")

	// 30초 자동 테스트 후 종료
	go func() {
		time.Sleep(30 * time.Second)
		log.Printf("⏰ 30초 테스트 완료 - 자동 종료")
		sigChan <- syscall.SIGTERM
	}()

	<-sigChan
	log.Printf("🔴 시스템 종료 중...")
	cancel()
	time.Sleep(1 * time.Second)
	log.Printf("✅ 시스템 종료 완료")
}

// monitorMemory 메모리 상태 모니터링 고루틴
func monitorMemory(mm *memory.Manager, bws *websocket.BinanceWebSocket, pm *monitor.PerformanceMonitor, sm *monitor.SystemMonitor) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// 메모리 통계
		memStats := mm.GetMemoryStats()
		log.Printf("📊 메모리 상태: 오더북 %v개, 시그널 %v개, 보관시간 %v분",
			memStats["total_orderbooks"],
			memStats["total_signals"],
			memStats["retention_minutes"])

		// 워커 풀 통계
		workerStats := bws.GetWorkerPoolStats()
		log.Printf("🔧 워커 풀 상태: 활성 %v/%v, 버퍼 %v/%v",
			workerStats["active_workers"],
			workerStats["worker_count"],
			workerStats["data_channel_buffer"],
			workerStats["data_channel_capacity"])

		// 성능 통계
		perfStats := pm.GetStats()
		log.Printf("⚡ 성능 상태: 최대처리량 %v/초, 오버플로우 %v회, 지연 %v회",
			perfStats["peak_throughput"],
			perfStats["overflow_count"],
			perfStats["delay_count"])

		// 시스템 상태
		sysStats := sm.GetHealthStatus()
		log.Printf("🏥 시스템 상태: %v, 에러 %v회, 경고 %v회",
			sysStats["health_status"],
			sysStats["error_count"],
			sysStats["warning_count"])
	}
}
