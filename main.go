package main

import (
	"context"
	"flag"
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
	"PumpWatch/internal/symbols"
	"PumpWatch/internal/websocket"
)

// PumpWatch v2.0 - Upbit Listing Pump Analysis System
// 업비트 상장공고 기반 견고한 다중거래소 실시간 데이터 수집 시스템
const (
	Version = "2.0.0"
	AppName = "PumpWatch"
)

var (
	configPath  = flag.String("config", "config/config.yaml", "Configuration file path")
	symbolsPath = flag.String("symbols", "config/symbols/symbols.yaml", "Symbols configuration file path")
	initSymbols = flag.Bool("init-symbols", false, "Initialize symbols configuration")
	logLevel    = flag.String("log", "info", "Log level (debug, info, warn, error)")
)

func main() {
	flag.Parse()

	printBanner()

	// Initialize comprehensive logging system
	if err := logging.InitGlobalLogger("metdc", *logLevel, "logs"); err != nil {
		fmt.Printf("❌ Failed to initialize logging: %v\n", err)
		os.Exit(1)
	}
	defer logging.CloseGlobalLogger()

	logging.Info("🚀 METDC v%s starting up...", Version)

	// Context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load system configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logging.Fatal("Failed to load configuration: %v", err)
	}

	logging.Info("Configuration loaded from %s", *configPath)

	// Initialize or load symbols configuration
	if *initSymbols {
		logging.Info("🔧 Initializing symbols configuration...")
		if err := initializeSymbols(*symbolsPath); err != nil {
			logging.Fatal("❌ Failed to initialize symbols: %v", err)
		}
		logging.Info("✅ Symbols configuration initialized")
		return
	}

	// Load symbols configuration
	symbolsConfig, err := symbols.LoadConfig(*symbolsPath)
	if err != nil {
		logging.Fatal("❌ Failed to load symbols configuration: %v", err)
	}

	logging.Info("✅ Symbols configuration loaded: %d exchanges configured",
		len(symbolsConfig.Exchanges))

	// Initialize storage manager
	storageManager := storage.NewManager(cfg.Storage, cfg.Analysis)
	logging.Info("✅ Storage manager initialized")

	// Initialize and set pump analyzer (if analysis is enabled)
	if cfg.Analysis.Enabled {
		analyzer := analyzer.NewPumpAnalyzer()
		storageManager.SetAnalyzer(analyzer)
		logging.Info("✅ Pump analyzer initialized and connected to storage manager")
	}

	// Initialize EnhancedTaskManager - Complete Data Collection Architecture
	taskManager, err := websocket.NewEnhancedTaskManager(ctx, cfg.Exchanges, symbolsConfig, storageManager)
	if err != nil {
		logging.Fatal("❌ Failed to initialize EnhancedTaskManager: %v", err)
	}

	logging.Info("✅ EnhancedTaskManager initialized - 완전한 20초 타이머 데이터 수집 아키텍처")

	// Start WebSocket Task Manager
	if err := taskManager.Start(); err != nil {
		logging.Fatal("❌ Failed to start WebSocket Task Manager: %v", err)
	}

	logging.Info("🚀 EnhancedTaskManager started - 완전한 데이터 수집 및 20초 타이머 아키텍처")

	// Initialize Upbit Monitor
	upbitMonitor, err := monitor.NewUpbitMonitor(ctx, cfg.Upbit, taskManager, storageManager)
	if err != nil {
		logging.Fatal("❌ Failed to initialize Upbit Monitor: %v", err)
	}

	logging.Info("✅ Upbit Monitor initialized")

	// Start Upbit monitoring (5-second polling)
	if err := upbitMonitor.Start(); err != nil {
		logging.Fatal("❌ Failed to start Upbit Monitor: %v", err)
	}

	logging.Info("📡 Upbit Monitor started - polling every %v", cfg.Upbit.PollInterval)
	logging.Info("🎯 System ready - monitoring for Upbit KRW listing announcements...")

	// System status reporting
	go systemStatusReporter(ctx, upbitMonitor)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigChan:
		logging.Info("📥 Received signal: %v", sig)
	case <-ctx.Done():
		logging.Info("📥 Context cancelled")
	}

	// Graceful shutdown
	logging.Info("🛑 Initiating graceful shutdown...")

	// Stop components in reverse order
	shutdownTimeout := 30 * time.Second
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	// Stop Upbit Monitor first
	if err := upbitMonitor.Stop(shutdownCtx); err != nil {
		logging.Warn(" Error stopping Upbit Monitor: %v", err)
	} else {
		logging.Info("✅ Upbit Monitor stopped")
	}

	// Stop WebSocket Task Manager
	if err := taskManager.Stop(); err != nil {
		logging.Warn(" Error stopping WebSocket Task Manager: %v", err)
	} else {
		logging.Info("✅ WebSocket Task Manager stopped")
	}

	// Cleanup storage if needed
	if err := storageManager.Close(); err != nil {
		logging.Warn(" Error closing storage manager: %v", err)
	} else {
		logging.Info("✅ Storage manager closed")
	}

	logging.Info("👋 METDC v2.0 shutdown complete")
}

// printBanner displays the application banner
func printBanner() {
	banner := `
	🚀 ===================================== 🚀
	   METDC v%s - Multi-Exchange Trade Data Collector
	   업비트 상장공고 기반 견고한 실시간 데이터 수집 시스템
	   
	   "무식하게 때려박기" - 단순함이 최고의 안전장치
	🚀 ===================================== 🚀
	`
	fmt.Printf(banner, Version)
}

// initializeSymbols creates and saves initial symbols configuration
func initializeSymbols(path string) error {
	symbolsManager, err := symbols.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create symbols manager: %w", err)
	}

	// Update symbols from all exchanges
	if err := symbolsManager.UpdateFromExchanges(); err != nil {
		return fmt.Errorf("failed to update symbols from exchanges: %w", err)
	}

	// Save to YAML file
	if err := symbolsManager.SaveToFile(path); err != nil {
		return fmt.Errorf("failed to save symbols config: %w", err)
	}

	return nil
}

// systemStatusReporter periodically reports system status
func systemStatusReporter(ctx context.Context, upbitMonitor *monitor.UpbitMonitor) {
	ticker := time.NewTicker(60 * time.Second) // Every minute
	defer ticker.Stop()

	// 🆕 Status report counter for detailed reporting every 5 minutes
	statusCounter := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			statusCounter++

			// Enhanced health monitoring every minute
			health := upbitMonitor.GetHealthStatus()

			// 🆕 Basic health status every minute
			logging.Info("💗 System Health [Score: %v/100] - Polls: %v, Listings: %v, Failures: %v, Delay: %v",
				health["health_score"], health["total_polls"], health["detected_listings"],
				health["consecutive_failures"], health["average_delay"])

			// 🆕 Detailed status report every 5 minutes
			if statusCounter%5 == 0 {
				logging.Info("📊 === Detailed System Status (5min report) ===")
				upbitMonitor.PrintDetailedStatus()

				// Additional system memory and uptime info
				logging.Info("⏱️ System Uptime: %v | Last Poll: %v ago",
					health["uptime"], health["last_poll_age"])

				if health["health_score"].(int) < 70 {
					logging.Warn("⚠️ SYSTEM HEALTH WARNING - Score below 70!")
				}
			}

			// 🆕 Alert on consecutive failures
			if failures, ok := health["consecutive_failures"].(int); ok && failures > 5 {
				logging.Warn("🚨 HIGH FAILURE RATE - %d consecutive API failures!", failures)
			}
		}
	}
}
