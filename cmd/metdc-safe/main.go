package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"PumpWatch/internal/analyzer"
	"PumpWatch/internal/config"
	"PumpWatch/internal/logging"
	"PumpWatch/internal/monitor"
	"PumpWatch/internal/storage"
	"PumpWatch/internal/websocket"
)

func main() {
	// 🚀 안전한 METDC 시스템 시작
	fmt.Println("🚀 =====================================")
	fmt.Println("   METDC Safe v3.0.0 - Memory Leak Free")
	fmt.Println("   단일 고루틴 SafeWorker 아키텍처")
	fmt.Println("   \"무식하게 때려박기\" - 안전성 최우선")
	fmt.Println("🚀 =====================================")

	// 설정 로드
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		fmt.Printf("❌ 설정 로드 실패: %v\n", err)
		os.Exit(1)
	}

	// 로거 초기화
	if err := logging.InitGlobalLogger("metdc-safe", "info", "logs"); err != nil {
		fmt.Printf("❌ 로거 초기화 실패: %v\n", err)
		os.Exit(1)
	}
	logger := logging.GetGlobalLogger()

	logger.Info("🚀 METDC Safe v3.0.0 starting up...")
	logger.Info("Configuration loaded from config/config.yaml")

	// Context 설정 (Graceful Shutdown)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 스토리지 매니저 초기화
	storageManager := storage.NewManager(cfg.Storage, cfg.Analysis)
	logger.Info("✅ Storage manager initialized")

	// Initialize and set pump analyzer (if analysis is enabled)
	if cfg.Analysis.Enabled {
		pumpAnalyzer := analyzer.NewPumpAnalyzer()
		storageManager.SetAnalyzer(pumpAnalyzer)
		logger.Info("✅ Pump analyzer initialized and connected to storage manager")
	}

	// SafeTaskManager 초기화 (메모리 누수 없는 아키텍처)
	taskManager := websocket.NewSafeTaskManager(cfg)
	logger.Info("✅ SafeTaskManager initialized")

	// SafeTaskManager 시작
	logger.Info("🚀 Starting SafeTaskManager...")
	if err := taskManager.Start(); err != nil {
		logger.Error("❌ SafeTaskManager 시작 실패: %v", err)
		os.Exit(1)
	}
	logger.Info("✅ SafeTaskManager started successfully")

	// 업비트 모니터 초기화 (SafeTaskManager를 DataCollectionManager로 사용)
	upbitMonitor, err := monitor.NewUpbitMonitor(ctx, cfg.Upbit, taskManager, storageManager)
	if err != nil {
		fmt.Printf("❌ 업비트 모니터 초기화 실패: %v\n", err)
		taskManager.Stop()
		os.Exit(1)
	}
	logger.Info("✅ Upbit monitor initialized")

	// 업비트 모니터 시작
	logger.Info("🚀 Starting Upbit Monitor...")
	if err := upbitMonitor.Start(); err != nil {
		logger.Error("❌ Upbit Monitor 시작 실패: %v", err)
		taskManager.Stop()
		os.Exit(1)
	}
	logger.Info("✅ Upbit Monitor started successfully")

	// 시스템 상태 모니터링
	go func() {
		ticker := time.NewTicker(5 * time.Minute) // 5분마다 상태 출력
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				health := taskManager.GetHealthStatus()
				logger.Info("🩺 시스템 건강 상태 - 가동시간: %s, 건강점수: %.2f, 워커: %s, 거래: %d, 에러: %d",
					health["uptime"], health["health_score"], health["workers"],
					health["trades"], health["errors"])
			}
		}
	}()

	logger.Info("🎯 System ready - monitoring for Upbit KRW listing announcements...")
	logger.Info("📊 메모리 누수 없는 아키텍처로 안전하게 실행 중")

	// 시그널 핸들러 설정
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 메인 루프 (시그널 대기)
	select {
	case sig := <-sigChan:
		logger.Info("🛑 Received signal: %s", sig.String())
	case <-ctx.Done():
		logger.Info("📥 Context cancelled")
	}

	// Graceful Shutdown
	logger.Info("🛑 Initiating graceful shutdown...")

	// 컴포넌트들 중지 (역순)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// 업비트 모니터 중지
	if err := upbitMonitor.Stop(shutdownCtx); err != nil {
		logger.Error("❌ Upbit Monitor 중지 실패: %v", err)
	} else {
		logger.Info("✅ Upbit Monitor 중지 완료")
	}

	// SafeTaskManager 중지
	if err := taskManager.Stop(); err != nil {
		logger.Error("❌ SafeTaskManager 중지 실패: %v", err)
	} else {
		logger.Info("✅ SafeTaskManager 중지 완료")
	}

	// 스토리지 매니저 중지
	if err := storageManager.Close(); err != nil {
		logger.Error("❌ Storage Manager 중지 실패: %v", err)
	} else {
		logger.Info("✅ Storage Manager 중지 완료")
	}

	logger.Info("✅ METDC Safe 완전 종료 완료")
	fmt.Println("👋 METDC Safe shutdown complete - 안전하게 종료되었습니다")
}
