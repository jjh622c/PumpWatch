// Package main - 고급 거래 연동 예제
//
// 이 예제는 NoticePumpCatch를 실제 거래 시스템과 연동하는 방법을 보여줍니다.
// 위험 관리, 포지션 관리, 수익 계산 등의 실전 로직이 포함되어 있습니다.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"noticepumpcatch/internal/interfaces"
	"noticepumpcatch/pkg/detector"
)

// TradingBot 거래 봇 구조체
type TradingBot struct {
	// 설정
	maxPositions      int     // 최대 동시 포지션 수
	riskPerTrade      float64 // 거래당 리스크 비율 (총 자산 대비)
	stopLossPercent   float64 // 손절매 비율
	takeProfitPercent float64 // 익절 비율

	// 상태
	mu           sync.RWMutex
	positions    map[string]*Position // 현재 포지션들
	balance      float64              // 현재 잔고 (USDT)
	totalPnL     float64              // 총 손익
	tradeHistory []TradeRecord        // 거래 기록

	// 통계
	winCount    int
	lossCount   int
	totalTrades int
}

// Position 포지션 정보
type Position struct {
	Symbol     string               // 심볼
	EntryPrice float64              // 진입 가격
	Quantity   float64              // 수량
	EntryTime  time.Time            // 진입 시간
	StopLoss   float64              // 손절가
	TakeProfit float64              // 익절가
	PumpEvent  interfaces.PumpEvent // 원본 펌핑 이벤트
}

// TradeRecord 거래 기록
type TradeRecord struct {
	Symbol    string    // 심볼
	Side      string    // "BUY" or "SELL"
	Price     float64   // 체결 가격
	Quantity  float64   // 수량
	PnL       float64   // 손익
	Timestamp time.Time // 체결 시간
	Reason    string    // 거래 사유
}

// NewTradingBot 새 거래 봇 생성
func NewTradingBot(initialBalance float64) *TradingBot {
	return &TradingBot{
		maxPositions:      5,    // 최대 5개 동시 포지션
		riskPerTrade:      0.02, // 거래당 2% 리스크
		stopLossPercent:   -3.0, // 3% 손절
		takeProfitPercent: 10.0, // 10% 익절
		positions:         make(map[string]*Position),
		balance:           initialBalance,
		tradeHistory:      make([]TradeRecord, 0),
	}
}

func main() {
	fmt.Println("🤖 NoticePumpCatch 고급 거래 봇 예제")
	fmt.Println("=======================================")

	// 거래 봇 생성 (초기 자본 1000 USDT)
	bot := NewTradingBot(1000.0)
	fmt.Printf("💰 초기 자본: %.2f USDT\n", bot.balance)

	// 펌핑 감지기 생성
	detector, err := detector.NewDetector("config.json")
	if err != nil {
		log.Fatal("감지기 생성 실패:", err)
	}

	// 펌핑 감지 콜백 - 거래 로직 연동
	detector.SetPumpCallback(func(event interfaces.PumpEvent) {
		bot.handlePumpSignal(event)
	})

	// 상장공시 콜백 - 우선 매수
	detector.SetListingCallback(func(event interfaces.ListingEvent) {
		bot.handleListingSignal(event)
	})

	// 포지션 모니터링 시작
	go bot.monitorPositions()

	// 주기적 상태 출력
	go bot.printStatus()

	// 시스템 시작
	if err := detector.Start(); err != nil {
		log.Fatal("시스템 시작 실패:", err)
	}

	fmt.Println("🚀 거래 봇 실행 중... (Ctrl+C로 종료)")

	// 종료 신호 대기
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	// 안전한 종료
	fmt.Println("\n🛑 거래 봇 종료 중...")

	// 모든 포지션 청산
	bot.closeAllPositions("시스템 종료")

	// 최종 결과 출력
	bot.printFinalReport()

	detector.Stop()
	fmt.Println("✅ 거래 봇 종료 완료")
}

// handlePumpSignal 펌핑 신호 처리
func (bot *TradingBot) handlePumpSignal(event interfaces.PumpEvent) {
	bot.mu.Lock()
	defer bot.mu.Unlock()

	fmt.Printf("\n🚨 [펌핑 신호] %s +%.2f%% (신뢰도: %.1f%%)\n",
		event.Symbol, event.PriceChange, event.Confidence)

	// 이미 포지션이 있는지 확인
	if _, exists := bot.positions[event.Symbol]; exists {
		fmt.Printf("   ⚠️ 이미 %s 포지션 보유 중 - 스킵\n", event.Symbol)
		return
	}

	// 최대 포지션 수 확인
	if len(bot.positions) >= bot.maxPositions {
		fmt.Printf("   ⚠️ 최대 포지션 수 도달 (%d개) - 스킵\n", bot.maxPositions)
		return
	}

	// 신뢰도가 낮으면 스킵
	if event.Confidence < 70.0 {
		fmt.Printf("   ⚠️ 신뢰도 부족 (%.1f%% < 70%%) - 스킵\n", event.Confidence)
		return
	}

	// 포지션 크기 계산 (리스크 기반)
	riskAmount := bot.balance * bot.riskPerTrade
	quantity := riskAmount / event.CurrentPrice

	if riskAmount < 10.0 { // 최소 거래 금액
		fmt.Printf("   ⚠️ 거래 금액 부족 (%.2f USDT < 10 USDT) - 스킵\n", riskAmount)
		return
	}

	// 포지션 생성
	position := &Position{
		Symbol:     event.Symbol,
		EntryPrice: event.CurrentPrice,
		Quantity:   quantity,
		EntryTime:  time.Now(),
		StopLoss:   event.CurrentPrice * (1 + bot.stopLossPercent/100),
		TakeProfit: event.CurrentPrice * (1 + bot.takeProfitPercent/100),
		PumpEvent:  event,
	}

	// 포지션 저장
	bot.positions[event.Symbol] = position

	// 잔고 차감
	bot.balance -= riskAmount

	// 거래 기록
	trade := TradeRecord{
		Symbol:    event.Symbol,
		Side:      "BUY",
		Price:     event.CurrentPrice,
		Quantity:  quantity,
		PnL:       0, // 진입시에는 0
		Timestamp: time.Now(),
		Reason:    fmt.Sprintf("펌핑 감지 (신뢰도: %.1f%%)", event.Confidence),
	}
	bot.tradeHistory = append(bot.tradeHistory, trade)
	bot.totalTrades++

	fmt.Printf("   ✅ 매수 실행: %.4f %s @ %.6f USDT (투자금: %.2f USDT)\n",
		quantity, event.Symbol, event.CurrentPrice, riskAmount)
	fmt.Printf("   🎯 익절가: %.6f USDT (+%.1f%%)\n", position.TakeProfit, bot.takeProfitPercent)
	fmt.Printf("   🛡️ 손절가: %.6f USDT (%.1f%%)\n", position.StopLoss, bot.stopLossPercent)
}

// handleListingSignal 상장공시 신호 처리
func (bot *TradingBot) handleListingSignal(event interfaces.ListingEvent) {
	fmt.Printf("\n📢 [상장공시] %s (신뢰도: %.1f%%)\n", event.Symbol, event.Confidence)

	// 상장공시는 높은 우선순위 - 더 공격적으로 거래
	if event.Confidence > 85.0 {
		fmt.Printf("   🎯 높은 신뢰도 상장공시 - 우선 매수 검토 중...\n")

		// 실제로는 여기서 바이낸스 API를 통해 현재가를 조회하고
		// 가상의 펌핑 이벤트를 생성해서 거래 로직 실행
		// (실제 구현에서는 별도의 가격 조회 로직 필요)
	}
}

// monitorPositions 포지션 모니터링 (손절/익절)
func (bot *TradingBot) monitorPositions() {
	ticker := time.NewTicker(5 * time.Second) // 5초마다 체크
	defer ticker.Stop()

	for range ticker.C {
		bot.checkStopLossAndTakeProfit()
	}
}

// checkStopLossAndTakeProfit 손절/익절 체크
func (bot *TradingBot) checkStopLossAndTakeProfit() {
	bot.mu.Lock()
	defer bot.mu.Unlock()

	// 실제 환경에서는 여기서 현재 가격을 API로 조회해야 함
	// 이 예제에서는 가상의 가격 변동을 시뮬레이션

	for symbol, position := range bot.positions {
		// 시뮬레이션: 랜덤 가격 변동 (-2% ~ +3%)
		priceChange := (float64(time.Now().UnixNano()%1000) - 500) / 500 * 0.05
		currentPrice := position.EntryPrice * (1 + priceChange)

		// 손절 체크
		if currentPrice <= position.StopLoss {
			bot.closePosition(symbol, currentPrice, "손절매")
			continue
		}

		// 익절 체크
		if currentPrice >= position.TakeProfit {
			bot.closePosition(symbol, currentPrice, "익절매")
			continue
		}

		// 시간 기반 청산 (30분 후 자동 청산)
		if time.Since(position.EntryTime) > 30*time.Minute {
			bot.closePosition(symbol, currentPrice, "시간 만료")
		}
	}
}

// closePosition 포지션 청산
func (bot *TradingBot) closePosition(symbol string, exitPrice float64, reason string) {
	position, exists := bot.positions[symbol]
	if !exists {
		return
	}

	// 손익 계산
	pnl := (exitPrice - position.EntryPrice) * position.Quantity
	pnlPercent := (exitPrice/position.EntryPrice - 1) * 100

	// 잔고 업데이트
	bot.balance += (position.EntryPrice * position.Quantity) + pnl
	bot.totalPnL += pnl

	// 거래 기록
	trade := TradeRecord{
		Symbol:    symbol,
		Side:      "SELL",
		Price:     exitPrice,
		Quantity:  position.Quantity,
		PnL:       pnl,
		Timestamp: time.Now(),
		Reason:    reason,
	}
	bot.tradeHistory = append(bot.tradeHistory, trade)

	// 승/패 기록
	if pnl > 0 {
		bot.winCount++
	} else {
		bot.lossCount++
	}

	fmt.Printf("   🔄 [%s] %s 청산: %.6f USDT (%.2f%% / %.2f USDT)\n",
		reason, symbol, exitPrice, pnlPercent, pnl)

	// 포지션 삭제
	delete(bot.positions, symbol)
}

// closeAllPositions 모든 포지션 청산
func (bot *TradingBot) closeAllPositions(reason string) {
	bot.mu.Lock()
	defer bot.mu.Unlock()

	for symbol, position := range bot.positions {
		// 진입가로 청산 (시뮬레이션)
		bot.closePosition(symbol, position.EntryPrice, reason)
	}
}

// printStatus 주기적 상태 출력
func (bot *TradingBot) printStatus() {
	ticker := time.NewTicker(60 * time.Second) // 1분마다
	defer ticker.Stop()

	for range ticker.C {
		bot.mu.RLock()

		fmt.Printf("\n📊 [거래 봇 상태] %s\n", time.Now().Format("15:04:05"))
		fmt.Printf("   💰 현재 잔고: %.2f USDT\n", bot.balance)
		fmt.Printf("   📈 총 손익: %.2f USDT\n", bot.totalPnL)
		fmt.Printf("   📊 포지션: %d개 (최대 %d개)\n", len(bot.positions), bot.maxPositions)

		if bot.totalTrades > 0 {
			winRate := float64(bot.winCount) / float64(bot.totalTrades) * 100
			fmt.Printf("   🎯 승률: %.1f%% (%d승 %d패)\n", winRate, bot.winCount, bot.lossCount)
		}

		// 활성 포지션 상세
		if len(bot.positions) > 0 {
			fmt.Printf("   📋 활성 포지션:\n")
			for symbol, pos := range bot.positions {
				duration := time.Since(pos.EntryTime)
				fmt.Printf("      - %s: %.6f USDT (보유: %v)\n",
					symbol, pos.EntryPrice, duration.Round(time.Second))
			}
		}

		bot.mu.RUnlock()
	}
}

// printFinalReport 최종 결과 출력
func (bot *TradingBot) printFinalReport() {
	bot.mu.RLock()
	defer bot.mu.RUnlock()

	fmt.Printf("\n📊 ========== 최종 거래 결과 ==========\n")
	fmt.Printf("💰 최종 잔고: %.2f USDT\n", bot.balance)
	fmt.Printf("📈 총 손익: %.2f USDT\n", bot.totalPnL)
	fmt.Printf("📊 총 거래: %d회\n", bot.totalTrades)

	if bot.totalTrades > 0 {
		winRate := float64(bot.winCount) / float64(bot.totalTrades) * 100
		fmt.Printf("🎯 승률: %.1f%% (%d승 %d패)\n", winRate, bot.winCount, bot.lossCount)

		avgPnL := bot.totalPnL / float64(bot.totalTrades)
		fmt.Printf("📊 평균 거래당 손익: %.2f USDT\n", avgPnL)
	}

	fmt.Printf("=====================================\n")
}
