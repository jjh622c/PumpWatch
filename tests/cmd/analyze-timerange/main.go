package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"time"
)

type TradeEvent struct {
	Exchange   string `json:"exchange"`
	MarketType string `json:"market_type"`
	Symbol     string `json:"symbol"`
	TradeID    string `json:"trade_id"`
	Price      string `json:"price"`
	Quantity   string `json:"quantity"`
	Side       string `json:"side"`
	Timestamp  int64  `json:"timestamp"`
}

type TimeAnalysis struct {
	Exchange      string
	MarketType    string
	TradeCount    int
	FirstTrade    int64
	LastTrade     int64
	TimeRange     string
	TriggerOffset string
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("Usage: go run main.go <data_directory>")
	}

	dataDir := os.Args[1]
	fmt.Printf("🔍 시간 범위 분석: %s\n", dataDir)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// 트리거 시간: 00:04:15 (2025-09-18 실제 테스트 시그널 발생 시점)
	triggerTime := int64(1758121455000) // 2025-09-18 00:04:15
	fmt.Printf("🎯 트리거 시간: %s (Unix: %d)\n",
		time.Unix(triggerTime/1000, 0).Format("15:04:05"), triggerTime)

	rawDir := dataDir + "/raw"
	exchanges := []string{"binance", "bybit", "kucoin", "okx", "gate", "phemex"}
	markets := []string{"spot", "futures"}

	var allAnalyses []TimeAnalysis

	for _, exchange := range exchanges {
		for _, market := range markets {
			filePath := fmt.Sprintf("%s/%s/%s.json", rawDir, exchange, market)

			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				continue
			}

			analysis := analyzeFile(filePath, exchange, market, triggerTime)
			if analysis.TradeCount > 0 {
				allAnalyses = append(allAnalyses, analysis)
			}
		}
	}

	if len(allAnalyses) == 0 {
		fmt.Println("❌ 분석할 데이터가 없습니다")
		return
	}

	// 시간순 정렬
	sort.Slice(allAnalyses, func(i, j int) bool {
		return allAnalyses[i].FirstTrade < allAnalyses[j].FirstTrade
	})

	fmt.Println("\n📊 거래소별 시간 범위 분석:")
	fmt.Printf("%-15s %-8s %8s %12s %12s %15s %15s\n",
		"Exchange", "Market", "Trades", "First", "Last", "Range", "Trigger Offset")
	fmt.Println("────────────────────────────────────────────────────────────────────────────────────────────")

	for _, analysis := range allAnalyses {
		fmt.Printf("%-15s %-8s %8d %12s %12s %15s %15s\n",
			analysis.Exchange, analysis.MarketType, analysis.TradeCount,
			formatTime(analysis.FirstTrade), formatTime(analysis.LastTrade),
			analysis.TimeRange, analysis.TriggerOffset)
	}

	// 전체 범위 분석
	if len(allAnalyses) > 0 {
		earliestTrade := allAnalyses[0].FirstTrade
		latestTrade := allAnalyses[0].LastTrade
		totalTrades := 0

		for _, analysis := range allAnalyses {
			if analysis.FirstTrade < earliestTrade {
				earliestTrade = analysis.FirstTrade
			}
			if analysis.LastTrade > latestTrade {
				latestTrade = analysis.LastTrade
			}
			totalTrades += analysis.TradeCount
		}

		fmt.Println("\n🎯 전체 데이터 요약:")
		fmt.Printf("📈 총 거래 수: %d개\n", totalTrades)
		fmt.Printf("⏰ 최초 거래: %s (Unix: %d)\n", formatTime(earliestTrade), earliestTrade)
		fmt.Printf("⏰ 최종 거래: %s (Unix: %d)\n", formatTime(latestTrade), latestTrade)
		fmt.Printf("📊 전체 범위: %s\n", formatDuration(latestTrade-earliestTrade))

		if triggerTime > 0 {
			fmt.Printf("🎯 트리거 기준:\n")
			fmt.Printf("   📍 트리거 시간: %s\n", formatTime(triggerTime))
			fmt.Printf("   ⬅️  최초 거래까지: %s\n", formatDuration(triggerTime-earliestTrade))
			fmt.Printf("   ➡️  최종 거래까지: %s\n", formatDuration(latestTrade-triggerTime))

			// -20초 ~ +20초 범위 확인
			targetStart := triggerTime - 20*1000  // -20초
			targetEnd := triggerTime + 20*1000    // +20초

			fmt.Printf("\n🎯 목표 수집 범위 (-20초 ~ +20초):\n")
			fmt.Printf("   📅 목표 시작: %s (Unix: %d)\n", formatTime(targetStart), targetStart)
			fmt.Printf("   📅 목표 종료: %s (Unix: %d)\n", formatTime(targetEnd), targetEnd)

			if earliestTrade <= targetStart && latestTrade >= targetEnd {
				fmt.Println("   ✅ 완벽한 범위 커버리지!")
			} else {
				if earliestTrade > targetStart {
					fmt.Printf("   ⚠️  시작 시간 부족: %s 늦음\n", formatDuration(earliestTrade-targetStart))
				}
				if latestTrade < targetEnd {
					fmt.Printf("   ⚠️  종료 시간 부족: %s 일찍 끝남\n", formatDuration(targetEnd-latestTrade))
				}
			}
		}
	}
}

func analyzeFile(filePath, exchange, market string, triggerTime int64) TimeAnalysis {
	analysis := TimeAnalysis{
		Exchange:   exchange,
		MarketType: market,
	}

	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return analysis
	}

	var trades []TradeEvent
	if err := json.Unmarshal(data, &trades); err != nil {
		return analysis
	}

	if len(trades) == 0 {
		return analysis
	}

	analysis.TradeCount = len(trades)
	analysis.FirstTrade = trades[0].Timestamp
	analysis.LastTrade = trades[len(trades)-1].Timestamp

	// 시간순으로 정렬되어 있지 않을 수 있으므로 확인
	for _, trade := range trades {
		if trade.Timestamp < analysis.FirstTrade {
			analysis.FirstTrade = trade.Timestamp
		}
		if trade.Timestamp > analysis.LastTrade {
			analysis.LastTrade = trade.Timestamp
		}
	}

	analysis.TimeRange = formatDuration(analysis.LastTrade - analysis.FirstTrade)

	if triggerTime > 0 {
		startOffset := triggerTime - analysis.FirstTrade
		endOffset := analysis.LastTrade - triggerTime
		analysis.TriggerOffset = fmt.Sprintf("-%ss~+%ss",
			formatSeconds(startOffset), formatSeconds(endOffset))
	}

	return analysis
}

func formatTime(timestamp int64) string {
	return time.Unix(timestamp/1000, 0).Format("15:04:05")
}

func formatDuration(milliseconds int64) string {
	duration := time.Duration(milliseconds) * time.Millisecond
	seconds := duration.Seconds()
	return fmt.Sprintf("%.1fs", seconds)
}

func formatSeconds(milliseconds int64) string {
	seconds := float64(milliseconds) / 1000.0
	return fmt.Sprintf("%.1f", seconds)
}