package analyzer

import (
	"log"
	"strconv"
	"time"

	"noticepumpcatch/internal/memory"
)

// UltraFastAnalyzer 초고속 분석기
type UltraFastAnalyzer struct {
	memManager *memory.Manager
}

// NewUltraFastAnalyzer 초고속 분석기 생성
func NewUltraFastAnalyzer(mm *memory.Manager) *UltraFastAnalyzer {
	return &UltraFastAnalyzer{
		memManager: mm,
	}
}

// AnalyzeOrderbook 오더북 분석
func (ufa *UltraFastAnalyzer) AnalyzeOrderbook(snapshot *memory.OrderbookSnapshot) *memory.PumpSignal {
	// 오더북 깊이 분석
	depth := ufa.analyzeOrderbookDepth(snapshot)

	// 스프레드 분석
	spread := ufa.analyzeSpread(snapshot)

	// 거래량 분석
	volume := ufa.analyzeVolume(snapshot)

	// 종합 점수 계산
	score := ufa.calculateScore(depth, spread, volume)

	// 펌핑 시그널 생성
	if score >= 60 {
		signal := &memory.PumpSignal{
			Symbol:     snapshot.Symbol,
			Exchange:   snapshot.Exchange,
			Timestamp:  snapshot.Timestamp,
			Score:      score,
			MaxPumpPct: ufa.calculateMaxPumpPercentage(snapshot),
			Action:     ufa.determineAction(score),
			Reasons:    ufa.generateReasons(depth, spread, volume),
		}

		log.Printf("🚀 펌핑 시그널: %s (점수: %.2f, 액션: %s)",
			signal.Symbol, signal.Score, signal.Action)

		return signal
	}

	return nil
}

// analyzeOrderbookDepth 오더북 깊이 분석
func (ufa *UltraFastAnalyzer) analyzeOrderbookDepth(snapshot *memory.OrderbookSnapshot) float64 {
	if len(snapshot.Bids) == 0 || len(snapshot.Asks) == 0 {
		return 0
	}

	// 매수/매도 호가 총량 계산
	totalBidVolume := 0.0
	totalAskVolume := 0.0

	for _, bid := range snapshot.Bids {
		if len(bid) >= 2 {
			if qty, err := strconv.ParseFloat(bid[1], 64); err == nil {
				totalBidVolume += qty
			}
		}
	}

	for _, ask := range snapshot.Asks {
		if len(ask) >= 2 {
			if qty, err := strconv.ParseFloat(ask[1], 64); err == nil {
				totalAskVolume += qty
			}
		}
	}

	// 매수/매도 불균형 계산
	if totalAskVolume == 0 {
		return 0
	}

	imbalance := (totalBidVolume - totalAskVolume) / totalAskVolume
	return imbalance * 100 // 백분율로 변환
}

// analyzeSpread 스프레드 분석
func (ufa *UltraFastAnalyzer) analyzeSpread(snapshot *memory.OrderbookSnapshot) float64 {
	if len(snapshot.Bids) == 0 || len(snapshot.Asks) == 0 {
		return 0
	}

	// 최고 매수가와 최저 매도가
	bestBid, err1 := strconv.ParseFloat(snapshot.Bids[0][0], 64)
	bestAsk, err2 := strconv.ParseFloat(snapshot.Asks[0][0], 64)

	if err1 != nil || err2 != nil {
		return 0
	}

	// 스프레드 계산
	spread := (bestAsk - bestBid) / bestBid * 100
	return spread
}

// analyzeVolume 거래량 분석
func (ufa *UltraFastAnalyzer) analyzeVolume(snapshot *memory.OrderbookSnapshot) float64 {
	// 최근 거래량 데이터 조회 (5분)
	recentOrderbooks := ufa.memManager.GetRecentOrderbooks(snapshot.Exchange, snapshot.Symbol, 5*time.Minute)

	if len(recentOrderbooks) < 2 {
		return 0
	}

	// 거래량 변화율 계산
	oldVolume := ufa.calculateTotalVolume(recentOrderbooks[0])
	newVolume := ufa.calculateTotalVolume(recentOrderbooks[len(recentOrderbooks)-1])

	if oldVolume == 0 {
		return 0
	}

	volumeChange := (newVolume - oldVolume) / oldVolume * 100
	return volumeChange
}

// calculateTotalVolume 총 거래량 계산
func (ufa *UltraFastAnalyzer) calculateTotalVolume(snapshot *memory.OrderbookSnapshot) float64 {
	total := 0.0

	for _, bid := range snapshot.Bids {
		if len(bid) >= 2 {
			if qty, err := strconv.ParseFloat(bid[1], 64); err == nil {
				total += qty
			}
		}
	}

	for _, ask := range snapshot.Asks {
		if len(ask) >= 2 {
			if qty, err := strconv.ParseFloat(ask[1], 64); err == nil {
				total += qty
			}
		}
	}

	return total
}

// calculateScore 종합 점수 계산
func (ufa *UltraFastAnalyzer) calculateScore(depth, spread, volume float64) float64 {
	// 가중 평균 계산
	depthWeight := 0.4  // 오더북 깊이 40%
	spreadWeight := 0.3 // 스프레드 30%
	volumeWeight := 0.3 // 거래량 30%

	score := depth*depthWeight + spread*spreadWeight + volume*volumeWeight

	// 점수 범위 조정 (0-100)
	if score < 0 {
		score = 0
	} else if score > 100 {
		score = 100
	}

	return score
}

// calculateMaxPumpPercentage 최대 펌핑 퍼센티지 계산
func (ufa *UltraFastAnalyzer) calculateMaxPumpPercentage(snapshot *memory.OrderbookSnapshot) float64 {
	if len(snapshot.Bids) == 0 || len(snapshot.Asks) == 0 {
		return 0
	}

	// 최고 매수가와 최저 매도가
	bestBid, err1 := strconv.ParseFloat(snapshot.Bids[0][0], 64)
	bestAsk, err2 := strconv.ParseFloat(snapshot.Asks[0][0], 64)

	if err1 != nil || err2 != nil {
		return 0
	}

	// 펌핑 퍼센티지 계산
	pumpPct := (bestAsk - bestBid) / bestBid * 100
	return pumpPct
}

// determineAction 액션 결정
func (ufa *UltraFastAnalyzer) determineAction(score float64) string {
	if score >= 90 {
		return "즉시매수"
	} else if score >= 80 {
		return "빠른매수"
	} else if score >= 70 {
		return "신중매수"
	} else if score >= 60 {
		return "대기"
	}
	return "관망"
}

// generateReasons 이유 생성
func (ufa *UltraFastAnalyzer) generateReasons(depth, spread, volume float64) []string {
	reasons := make([]string, 0)

	if depth > 10 {
		reasons = append(reasons, "매수세 강함")
	} else if depth < -10 {
		reasons = append(reasons, "매도세 강함")
	}

	if spread < 0.1 {
		reasons = append(reasons, "스프레드 좁음")
	} else if spread > 1.0 {
		reasons = append(reasons, "스프레드 넓음")
	}

	if volume > 50 {
		reasons = append(reasons, "거래량 급증")
	} else if volume < -30 {
		reasons = append(reasons, "거래량 감소")
	}

	return reasons
}
