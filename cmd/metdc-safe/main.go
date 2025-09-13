package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"PumpWatch/internal/config"
	"PumpWatch/internal/logging"
	"PumpWatch/internal/models"
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
	cfg, err := config.LoadConfig("config/config.yaml")
	if err != nil {
		fmt.Printf("❌ 설정 로드 실패: %v\n", err)
		os.Exit(1)
	}

	// 로거 초기화
	if err := logging.InitializeGlobalLogger(cfg.Logging); err != nil {
		fmt.Printf("❌ 로거 초기화 실패: %v\n", err)
		os.Exit(1)
	}
	logger := logging.GetGlobalLogger().SystemLogger()

	logger.Info("🚀 METDC Safe v3.0.0 starting up...")
	logger.Info("Configuration loaded from config/config.yaml")

	// 스토리지 매니저 초기화
	storageManager := storage.NewManager(cfg.Storage.BasePath)
	logger.Info("✅ Storage manager initialized")

	// 업비트 모니터 초기화
	upbitMonitor := monitor.NewUpbitMonitor(cfg.Monitor)
	logger.Info("✅ Upbit monitor initialized")

	// SafeTaskManager 초기화 (메모리 누수 없는 아키텍처)
	taskManager := websocket.NewSafeTaskManager(cfg)

	// 거래 이벤트 핸들러 설정 (메모리에 축적)
	var currentCollection *storage.CollectionEvent
	var collectionMutex sync.RWMutex

	taskManager.SetOnTradeEvent(func(trade models.TradeEvent) {
		collectionMutex.RLock()
		if currentCollection != nil {
			currentCollection.AddTrade(trade) // 메모리에 안전하게 축적
		}
		collectionMutex.RUnlock()
	})

	// 에러 핸들러 설정
	taskManager.SetOnError(func(err error) {
		logger.Error("🚨 WebSocket 에러: %v", err)
	})

	// 상장공지 핸들러 설정
	upbitMonitor.SetOnNewListing(func(listing models.ListingAnnouncement) {
		logger.Info("🎯 새로운 상장공지 감지: %s", listing.Symbol)

		collectionMutex.Lock()

		// 기존 수집이 진행 중이면 먼저 저장
		if currentCollection != nil {
			logger.Info("💾 기존 수집 데이터 저장 중...")
			if err := storageManager.SaveCollectionEvent(currentCollection); err != nil {
				logger.Error("❌ 기존 데이터 저장 실패: %v", err)
			}
		}

		// 새로운 수집 이벤트 시작 (-20초부터)
		startTime := listing.Timestamp.Add(-20 * time.Second)
		currentCollection = storage.NewCollectionEvent(listing.Symbol, startTime)
		logger.Info("🔥 데이터 수집 시작: %s (20초 전부터)", listing.Symbol)

		collectionMutex.Unlock()

		// 20초 후 자동 저장
		go func() {
			time.Sleep(20 * time.Second)

			collectionMutex.Lock()
			if currentCollection != nil && currentCollection.Symbol == listing.Symbol {
				logger.Info("💾 수집 데이터 저장: %s", listing.Symbol)
				if err := storageManager.SaveCollectionEvent(currentCollection); err != nil {
					logger.Error("❌ 데이터 저장 실패: %v", err)
				} else {
					logger.Info("✅ 데이터 저장 완료: %s", listing.Symbol)
				}
				currentCollection = nil // 메모리 정리
			}
			collectionMutex.Unlock()
		}()
	})

	// Context 설정 (Graceful Shutdown)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 시그널 핸들러 설정
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// SafeTaskManager 시작
	logger.Info("🚀 Starting SafeTaskManager...")
	if err := taskManager.Start(); err != nil {
		logger.Error("❌ SafeTaskManager 시작 실패: %v", err)
		os.Exit(1)
	}
	logger.Info("✅ SafeTaskManager started successfully")

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

	// 메인 루프 (시그널 대기)
	select {
	case sig := <-sigChan:
		logger.Info("🛑 Received signal: %s", sig.String())
	case <-ctx.Done():
		logger.Info("📥 Context cancelled")
	}

	// Graceful Shutdown
	logger.Info("🛑 Initiating graceful shutdown...")

	// 현재 수집 중인 데이터 저장
	collectionMutex.Lock()
	if currentCollection != nil {
		logger.Info("💾 마지막 수집 데이터 저장 중...")
		if err := storageManager.SaveCollectionEvent(currentCollection); err != nil {
			logger.Error("❌ 마지막 데이터 저장 실패: %v", err)
		} else {
			logger.Info("✅ 마지막 데이터 저장 완료")
		}
	}
	collectionMutex.Unlock()

	// 컴포넌트들 중지
	if err := upbitMonitor.Stop(); err != nil {
		logger.Error("❌ Upbit Monitor 중지 실패: %v", err)
	} else {
		logger.Info("✅ Upbit Monitor 중지 완료")
	}

	if err := taskManager.Stop(); err != nil {
		logger.Error("❌ SafeTaskManager 중지 실패: %v", err)
	} else {
		logger.Info("✅ SafeTaskManager 중지 완료")
	}

	logger.Info("✅ METDC Safe 완전 종료 완료")
	fmt.Println("👋 METDC Safe shutdown complete - 안전하게 종료되었습니다")
}