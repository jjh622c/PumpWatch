package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"PumpWatch/internal/config"
	"PumpWatch/internal/logging"
	"PumpWatch/internal/storage"
	"PumpWatch/internal/symbols"
	"PumpWatch/internal/websocket"
)

func main() {
	fmt.Println("🎯 SOMI 가짜 상장공고 시그널 테스트")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// 로거 초기화
	if err := logging.InitGlobalLogger("fake-listing", "info", "logs"); err != nil {
		log.Fatalf("❌ 로거 초기화 실패: %v", err)
	}
	defer logging.CloseGlobalLogger()

	// 설정 로드
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.Fatalf("❌ 설정 로드 실패: %v", err)
	}

	// 심볼 설정 로드
	symbolsConfig, err := symbols.LoadConfig("config/symbols/symbols.yaml")
	if err != nil {
		log.Fatalf("❌ 심볼 설정 로드 실패: %v", err)
	}

	// 스토리지 매니저 초기화
	storageManager := storage.NewManager(cfg.Storage, cfg.Analysis)

	// Enhanced Task Manager 초기화
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskManager, err := websocket.NewEnhancedTaskManager(ctx, cfg.Exchanges, symbolsConfig, storageManager)
	if err != nil {
		log.Fatalf("❌ Task Manager 초기화 실패: %v", err)
	}

	// WebSocket 연결 시작 (즉시 CircularBuffer 데이터 축적 시작)
	fmt.Println("🔄 WebSocket 연결 시작 중...")
	if err := taskManager.Start(); err != nil {
		log.Fatalf("❌ WebSocket 시작 실패: %v", err)
	}

	fmt.Println("✅ WebSocket 연결 완료")
	fmt.Println("🔄 CircularBuffer 데이터 축적 시작 (백그라운드)")
	fmt.Println("⏳ 3초 후 가짜 상장공고 트리거 예정...")

	// 트리거 시간을 공유하기 위한 채널
	triggerChan := make(chan time.Time, 1)

	// 3초 후 가짜 트리거 발생을 위한 고루틴 (테스트 단축)
	go func() {
		// 3초 대기 (CircularBuffer가 과거 데이터 축적)
		time.Sleep(3 * time.Second)

		fmt.Println()
		fmt.Println("🚀 가짜 SOMI 상장공고 시그널 발생!")
		fmt.Println("📡 데이터 수집 시작: -20초부터 +20초까지 (총 40초)")

		// 가짜 상장공고 시그널 발생
		triggerTime := time.Now()
		if err := taskManager.StartDataCollection("SOMI", triggerTime); err != nil {
			log.Printf("❌ 데이터 수집 시작 실패: %v", err)
			return
		}

		fmt.Printf("⏰ 트리거 시간: %s\n", triggerTime.Format("15:04:05"))
		fmt.Printf("📊 수집 범위: %s ~ %s\n",
			triggerTime.Add(-20*time.Second).Format("15:04:05"),
			triggerTime.Add(20*time.Second).Format("15:04:05"))

		// 트리거 시간을 메인 함수에 전달
		triggerChan <- triggerTime
	}()

	fmt.Println()
	fmt.Println("🔍 CircularBuffer 데이터 축적 및 트리거 대기 중...")

	// 전체 테스트 시간: 25초 (3초 대기 + 22초 수집 완료 대기)
	totalDuration := 25 * time.Second
	startTime := time.Now()

	for {
		elapsed := time.Since(startTime)
		if elapsed >= totalDuration {
			break
		}

		time.Sleep(1 * time.Second)
		if elapsed < 3*time.Second {
			fmt.Printf("📊 [%v] CircularBuffer 작동 중... (트리거까지 %v)\n",
				elapsed.Truncate(time.Second),
				(3*time.Second - elapsed).Truncate(time.Second))
		} else {
			fmt.Printf("📊 [%v] 트리거 발생 후... (완료까지 %v)\n",
				elapsed.Truncate(time.Second),
				(totalDuration - elapsed).Truncate(time.Second))
		}
	}

	fmt.Println()
	fmt.Println("📂 저장된 SOMI 데이터 파일 확인")

	// 트리거 시간 받기 (트리거가 발생했다면)
	var triggerTime time.Time
	select {
	case triggerTime = <-triggerChan:
		fmt.Printf("✅ 트리거 시간 확인: %s\n", triggerTime.Format("15:04:05"))
	default:
		fmt.Println("⚠️ 트리거가 아직 발생하지 않음")
		triggerTime = time.Now() // 기본값 사용
	}

	// 저장된 파일 확인
	dataDir := fmt.Sprintf("data/SOMI_%d", triggerTime.Unix())

	// 디렉토리 존재 확인
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		fmt.Printf("❌ 데이터 디렉토리가 존재하지 않음: %s\n", dataDir)

		// 대안: 시간 범위로 검색
		fmt.Println("🔍 다른 시간대 SOMI 디렉토리 검색 중...")
		baseDir := "data"
		if entries, err := os.ReadDir(baseDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() && len(entry.Name()) > 5 && entry.Name()[:4] == "SOMI" {
					fmt.Printf("📁 발견된 SOMI 디렉토리: %s\n", entry.Name())

					// 이 디렉토리의 파일들 확인
					checkDataFiles(fmt.Sprintf("%s/%s", baseDir, entry.Name()))
				}
			}
		}
	} else {
		fmt.Printf("✅ 데이터 디렉토리 발견: %s\n", dataDir)
		checkDataFiles(dataDir)
	}

	// 정리
	fmt.Println()
	fmt.Println("🛑 Task Manager 정리 중...")
	if err := taskManager.Stop(); err != nil {
		fmt.Printf("⚠️ Task Manager 정리 중 오류: %v\n", err)
	}

	fmt.Println("✅ 가짜 SOMI 상장공고 테스트 완료!")
}

func checkDataFiles(dataDir string) {
	fmt.Printf("📂 디렉토리 내용 확인: %s\n", dataDir)

	// raw 디렉토리 확인
	rawDir := fmt.Sprintf("%s/raw", dataDir)
	if entries, err := os.ReadDir(rawDir); err == nil {
		fmt.Printf("📁 raw 디렉토리: %d개 파일\n", len(entries))

		for _, entry := range entries {
			if !entry.IsDir() {
				filePath := fmt.Sprintf("%s/%s", rawDir, entry.Name())
				if info, err := os.Stat(filePath); err == nil {
					fmt.Printf("   📄 %s: %d bytes", entry.Name(), info.Size())

					// 파일 내용 간단히 확인
					if content, err := os.ReadFile(filePath); err == nil {
						if len(content) > 100 {
							fmt.Printf(" (데이터 있음)\n")
						} else if len(content) == 0 {
							fmt.Printf(" (빈 파일)\n")
						} else {
							fmt.Printf(" (소량 데이터: %s)\n", string(content)[:min(50, len(content))])
						}
					} else {
						fmt.Printf(" (읽기 실패)\n")
					}
				}
			}
		}
	} else {
		fmt.Printf("❌ raw 디렉토리 읽기 실패: %v\n", err)
	}

	// refined 디렉토리 확인
	refinedDir := fmt.Sprintf("%s/refined", dataDir)
	if entries, err := os.ReadDir(refinedDir); err == nil {
		fmt.Printf("📁 refined 디렉토리: %d개 파일\n", len(entries))

		for _, entry := range entries {
			if !entry.IsDir() {
				filePath := fmt.Sprintf("%s/%s", refinedDir, entry.Name())
				if info, err := os.Stat(filePath); err == nil {
					fmt.Printf("   📄 %s: %d bytes\n", entry.Name(), info.Size())
				}
			}
		}
	} else {
		fmt.Printf("❌ refined 디렉토리 읽기 실패: %v\n", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}