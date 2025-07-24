# 🚀 NoticePumpCatch 완전 모듈화 통합 가이드

## 📋 목차
- [🎯 프로젝트 개요](#-프로젝트-개요)
- [✨ 주요 기능](#-주요-기능)
- [📦 패키지 구조](#-패키지-구조)
- [🚀 빠른 시작](#-빠른-시작)
- [🔧 고급 사용법](#-고급-사용법)
- [⚙️ 설정 가이드](#️-설정-가이드)
- [🤝 거래 시스템 통합](#-거래-시스템-통합)
- [📊 모니터링 및 디버깅](#-모니터링-및-디버깅)
- [🛠️ 문제 해결](#️-문제-해결)
- [📈 성능 최적화](#-성능-최적화)

---

## 🎯 프로젝트 개요

**NoticePumpCatch**는 바이낸스 실시간 데이터를 모니터링하여 **펌핑(급등)**과 **상장공시**를 감지하는 고성능 시스템입니다. 완전히 모듈화되어 있어 **어떤 거래 시스템에도 쉽게 통합**할 수 있습니다.

### 🎯 핵심 가치
- ⚡ **실시간 감지**: 1초 이내 펌핑 신호 포착
- 🎯 **높은 정확도**: 거래량, 스프레드, 패턴 분석으로 가짜 신호 필터링
- 🔧 **완전 모듈화**: 플러그인 방식으로 기존 시스템에 통합
- 🛡️ **안정성**: 메모리 누수 방지, 고루틴 관리, 자동 복구
- 📊 **투명성**: 상세한 로깅과 통계로 완전한 추적 가능

---

## ✨ 주요 기능
ㅌ
### 🚨 펌핑 감지
- **실시간 가격 급등 감지**: 설정 가능한 임계값(기본 3%)
- **적응형 시간 윈도우**: 거래량에 따라 자동 조정
- **다중 검증**: 가격 + 거래량 + 신뢰도 종합 판단
- **노이즈 필터링**: 가짜 펌핑 제거 알고리즘

### 📢 상장공시 감지
- **업비트 상장공시 실시간 모니터링**
- **바이낸스 심볼 자동 매칭**
- **신뢰도 기반 필터링**
- **즉시 알림 시스템**

### 🔧 시스템 기능
- **메모리 최적화**: 롤링 버퍼 + 자동 압축
- **고루틴 안전**: 누수 방지 + 타임아웃 정리
- **설정 핫 리로드**: 재시작 없이 설정 변경
- **상세 로깅**: 디버깅부터 운영까지 완벽 지원

---

## 📦 패키지 구조

```
noticepumpcatch/
├── pkg/detector/           # 🔌 외부 통합용 라이브러리
│   └── detector.go        # 메인 인터페이스
├── internal/              # 🔒 내부 구현
│   ├── interfaces/        # 인터페이스 정의
│   ├── config/           # 설정 관리
│   ├── signals/          # 펌핑 감지 알고리즘
│   ├── websocket/        # 바이낸스 WebSocket
│   ├── memory/           # 메모리 관리
│   ├── sync/             # 심볼 동기화
│   └── ...               # 기타 모듈들
├── examples/             # 📖 사용 예제
│   ├── basic_usage.go    # 기본 사용법
│   └── advanced_trading.go  # 고급 거래 봇
├── config.json          # ⚙️ 메인 설정 파일
└── README_INTEGRATION.md # 📚 이 문서
```

---

## 🚀 빠른 시작

### 1️⃣ 기본 설정

**config.json 확인 및 수정:**

```json
{
  "signals": {
    "pump_detection": {
      "enabled": true,
      "price_change_threshold": 3.0,  // 3% 상승시 펌핑 감지
      "time_window_seconds": 1        // 1초 윈도우
    }
  },
  "websocket": {
    "sync_enabled": true,             // 자동 심볼 동기화
    "enable_upbit_filter": true       // 업비트 상장 코인만
  }
}
```

### 2️⃣ 기본 코드

```go
package main

import (
    "log"
    "noticepumpcatch/internal/interfaces"
    "noticepumpcatch/pkg/detector"
)

func main() {
    // 1. 감지기 생성
    detector, err := detector.NewDetector("config.json")
    if err != nil {
        log.Fatal("감지기 생성 실패:", err)
    }
    
    // 2. 펌핑 감지 콜백 설정
    detector.SetPumpCallback(func(event interfaces.PumpEvent) {
        log.Printf("🚀 펌핑 감지: %s +%.2f%%", 
            event.Symbol, event.PriceChange)
        
        // 여기서 거래 로직 실행
        if event.PriceChange > 5.0 {
            // 5% 이상 급등시 매수
            buyToken(event.Symbol, event.CurrentPrice)
        }
    })
    
    // 3. 상장공시 콜백 설정
    detector.SetListingCallback(func(event interfaces.ListingEvent) {
        log.Printf("📢 상장공시: %s", event.Symbol)
        
        // 상장공시는 높은 우선순위
        if event.Confidence > 90.0 {
            priorityBuy(event.Symbol)
        }
    })
    
    // 4. 시스템 시작
    if err := detector.Start(); err != nil {
        log.Fatal("시스템 시작 실패:", err)
    }
    defer detector.Stop()
    
    // 5. 메인 루프
    select {} // 무한 대기
}

func buyToken(symbol string, price float64) {
    // 실제 거래 로직 구현
    log.Printf("💰 매수 실행: %s @ %.6f", symbol, price)
}

func priorityBuy(symbol string) {
    // 우선 매수 로직 구현
    log.Printf("⚡ 우선 매수: %s", symbol)
}
```

### 3️⃣ 실행

```bash
go run main.go
```

---

## 🔧 고급 사용법

### 🤖 거래 봇 통합

전체 거래 봇 예제는 `examples/advanced_trading.go`를 참조하세요.

```go
// 위험 관리가 포함된 거래 봇
type TradingBot struct {
    maxPositions    int     // 최대 동시 포지션
    riskPerTrade    float64 // 거래당 리스크 비율
    stopLossPercent float64 // 손절매 비율
    positions       map[string]*Position
    balance         float64
}

func (bot *TradingBot) handlePumpSignal(event interfaces.PumpEvent) {
    // 리스크 관리
    if len(bot.positions) >= bot.maxPositions {
        return // 최대 포지션 도달
    }
    
    if event.Confidence < 70.0 {
        return // 신뢰도 부족
    }
    
    // 포지션 크기 계산
    riskAmount := bot.balance * bot.riskPerTrade
    quantity := riskAmount / event.CurrentPrice
    
    // 거래 실행
    bot.executeBuy(event.Symbol, event.CurrentPrice, quantity)
}
```

### 📊 실시간 모니터링

```go
func monitorSystem(detector interfaces.PumpDetector) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        status := detector.GetStatus()
        stats := detector.GetStats()
        
        log.Printf("상태: %v, 펌핑: %d회, 메모리: %.1fMB", 
            status.IsRunning, stats.TotalPumpEvents, stats.MemoryUsageMB)
    }
}
```

### ⚙️ 실시간 설정 변경

```go
// 임계값 동적 조정
newConfig := interfaces.DetectorConfig{
    PumpDetection: struct{...}{
        PriceChangeThreshold: 2.0,  // 2%로 낮춤 (더 민감)
        TimeWindowSeconds: 2,       // 2초로 늘림 (더 안정)
    },
}
detector.UpdateConfig(newConfig)
```

---

## ⚙️ 설정 가이드

### 🎯 거래 스타일별 권장 설정

#### 🏃 스캘핑 (초단타)
```json
{
  "signals": {
    "pump_detection": {
      "price_change_threshold": 1.5,  // 민감한 감지
      "time_window_seconds": 1,       // 빠른 반응
      "volume_threshold": 50.0        // 낮은 거래량도 허용
    }
  }
}
```

#### 🌊 스윙 트레이딩
```json
{
  "signals": {
    "pump_detection": {
      "price_change_threshold": 3.0,  // 확실한 신호
      "time_window_seconds": 5,       // 안정적 감지
      "volume_threshold": 150.0       // 확실한 거래량
    }
  }
}
```

#### 🛡️ 보수적 거래
```json
{
  "signals": {
    "pump_detection": {
      "price_change_threshold": 5.0,  // 매우 확실한 신호만
      "time_window_seconds": 10,      // 충분한 검증
      "volume_threshold": 300.0       // 대량 거래량
    }
  }
}
```

### 🔧 성능 튜닝

#### 고성능 서버용
```json
{
  "websocket": {
    "worker_count": 16,        // CPU 코어 수 × 2
    "buffer_size": 10000       // 큰 버퍼
  },
  "memory": {
    "max_trades_per_symbol": 200,
    "cleanup_interval_minutes": 5
  }
}
```

#### 저사양 환경용
```json
{
  "websocket": {
    "worker_count": 4,         // 최소한의 워커
    "buffer_size": 1000        // 작은 버퍼
  },
  "memory": {
    "max_trades_per_symbol": 50,
    "cleanup_interval_minutes": 15
  }
}
```

---

## 🤝 거래 시스템 통합

### 📋 통합 체크리스트

#### ✅ 준비 단계
- [ ] Go 1.19+ 설치 확인
- [ ] config.json 설정 완료
- [ ] 네트워크 연결 확인 (바이낸스, 업비트 API)
- [ ] 로그 디렉토리 쓰기 권한 확인

#### ✅ 통합 단계
- [ ] pkg/detector 패키지 import
- [ ] 펌핑 감지 콜백 구현
- [ ] 상장공시 콜백 구현
- [ ] 에러 처리 로직 추가
- [ ] 안전한 종료 로직 추가

#### ✅ 테스트 단계
- [ ] 기본 연결 테스트
- [ ] 펌핑 감지 테스트 (임계값 낮춤)
- [ ] 메모리 사용량 모니터링
- [ ] 장시간 안정성 테스트

### 🔌 API 인터페이스

#### 메인 인터페이스
```go
type PumpDetector interface {
    Start() error                              // 시스템 시작
    Stop() error                               // 시스템 중지
    SetPumpCallback(callback PumpCallback)     // 펌핑 콜백 설정
    SetListingCallback(callback ListingCallback) // 상장공시 콜백 설정
    UpdateConfig(config DetectorConfig) error  // 실시간 설정 변경
    GetStatus() DetectorStatus                 // 현재 상태 조회
    GetStats() DetectorStats                   // 통계 정보 조회
}
```

#### 이벤트 구조체
```go
type PumpEvent struct {
    Symbol        string    // 심볼 (예: BTCUSDT)
    Exchange      string    // 거래소 (binance)
    Timestamp     time.Time // 감지 시간
    PriceChange   float64   // 가격 변동률 (%)
    CurrentPrice  float64   // 현재 가격
    PreviousPrice float64   // 이전 가격
    Confidence    float64   // 신뢰도 (0-100)
    Action        string    // 권장 액션 (BUY/SELL/HOLD)
    // ... 기타 필드들
}
```

### 🎯 통합 패턴

#### 1. 이벤트 기반 통합
```go
detector.SetPumpCallback(func(event interfaces.PumpEvent) {
    // 비동기로 거래 처리
    go func() {
        if shouldTrade(event) {
            executeTrade(event)
        }
    }()
})
```

#### 2. 큐 기반 통합
```go
var tradeQueue = make(chan interfaces.PumpEvent, 100)

detector.SetPumpCallback(func(event interfaces.PumpEvent) {
    select {
    case tradeQueue <- event:
    default:
        log.Println("거래 큐가 가득참")
    }
})

// 별도 워커에서 처리
go processTrades(tradeQueue)
```

#### 3. 상태 기반 통합
```go
type TradingState struct {
    isEnabled     bool
    riskLevel     float64
    maxPositions  int
}

func (state *TradingState) handlePump(event interfaces.PumpEvent) {
    if !state.isEnabled {
        return
    }
    
    // 상태에 따른 거래 로직
}
```

---

## 📊 모니터링 및 디버깅

### 📈 실시간 대시보드

```go
func createDashboard(detector interfaces.PumpDetector) {
    http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
        status := detector.GetStatus()
        stats := detector.GetStats()
        
        data := map[string]interface{}{
            "running": status.IsRunning,
            "symbols": status.SymbolCount,
            "pumps":   stats.TotalPumpEvents,
            "memory":  stats.MemoryUsageMB,
        }
        
        json.NewEncoder(w).Encode(data)
    })
    
    log.Println("대시보드: http://localhost:8080/status")
    http.ListenAndServe(":8080", nil)
}
```

### 📝 로그 분석

#### 중요 로그 패턴
```bash
# 펌핑 감지
grep "펌핑 감지" logs/noticepumpcatch.log

# 상장공시
grep "상장공시" logs/noticepumpcatch.log

# 에러 확인
grep "ERROR" logs/noticepumpcatch.log

# 성능 이슈
grep "GOROUTINE ALERT\|MEMORY ALERT" logs/noticepumpcatch.log
```

#### 로그 설정 최적화
```json
{
  "logging": {
    "level": "info",              // 운영: info, 개발: debug
    "max_size": 100,              // 100MB로 제한
    "max_backups": 5,             // 5개 백업 파일
    "latency_warn_seconds": 2.0   // 2초 이상 지연시 경고
  }
}
```

### 🔍 성능 메트릭

```go
func printPerformanceMetrics(stats interfaces.DetectorStats) {
    log.Printf("📊 성능 지표:")
    log.Printf("   처리율: 오더북 %.1f/초, 체결 %.1f/초",
        stats.OrderbookRate, stats.TradeRate)
    log.Printf("   감지율: 펌핑 %d회, 상장 %d회",
        stats.TotalPumpEvents, stats.TotalListingEvents)
    log.Printf("   평균 지연: %.1fms, 최대 지연: %.1fms",
        stats.AvgLatencyMs, stats.MaxLatencyMs)
    log.Printf("   에러율: %.2f%%", stats.ErrorRate)
}
```

---

## 🛠️ 문제 해결

### ❌ 자주 발생하는 문제들

#### 1. WebSocket 연결 실패
```
ERROR: WebSocket 연결 실패: dial tcp: i/o timeout
```

**해결책:**
- 네트워크 연결 확인
- 방화벽 설정 확인
- `worker_count`와 `buffer_size` 조정

```json
{
  "websocket": {
    "worker_count": 4,     // 줄이기
    "buffer_size": 2000    // 줄이기
  }
}
```

#### 2. 메모리 사용량 급증
```
WARNING: 힙 메모리 경고: 500.0MB
```

**해결책:**
- retention 시간 줄이기
- cleanup 주기 늘리기

```json
{
  "memory": {
    "trade_retention_minutes": 0.5,     // 1분 → 30초
    "max_trades_per_symbol": 50,        // 100개 → 50개
    "cleanup_interval_minutes": 5       // 10분 → 5분
  }
}
```

#### 3. 너무 많은 가짜 신호
```
INFO: 펌핑 감지: TESTUSDT +2.1%
INFO: 펌핑 감지: TESTUSDT +1.8%
```

**해결책:**
- 임계값과 신뢰도 상향 조정

```json
{
  "signals": {
    "pump_detection": {
      "price_change_threshold": 5.0,  // 3.0 → 5.0
      "min_score": 80.0,              // 60.0 → 80.0
      "volume_threshold": 200.0       // 100.0 → 200.0
    }
  }
}
```

#### 4. 신호를 놓침
```
# 로그에 펌핑 신호가 없음
```

**해결책:**
- 임계값과 시간 윈도우 하향 조정

```json
{
  "signals": {
    "pump_detection": {
      "price_change_threshold": 2.0,  // 3.0 → 2.0
      "time_window_seconds": 5,       // 1 → 5
      "min_score": 50.0               // 60.0 → 50.0
    }
  }
}
```

### 🔧 디버깅 도구

#### 로그 레벨 변경
```json
{
  "logging": {
    "level": "debug"  // 상세한 디버그 정보 출력
  }
}
```

#### 테스트 모드
```go
// 임계값을 낮춰서 테스트
testConfig := interfaces.DetectorConfig{
    PumpDetection: struct{...}{
        PriceChangeThreshold: 0.5,  // 0.5%로 매우 민감하게
    },
}
detector.UpdateConfig(testConfig)
```

---

## 📈 성능 최적화

### 🚀 성능 튜닝 가이드

#### CPU 최적화
```json
{
  "websocket": {
    "worker_count": 12,        // CPU 코어 수에 맞춤
    "buffer_size": 8000        // 적절한 버퍼 크기
  }
}
```

#### 메모리 최적화
```json
{
  "memory": {
    "compression_interval_seconds": 20,  // 더 자주 압축
    "heap_warning_mb": 200.0,           // 임계값 상향
    "gc_threshold_orderbooks": 30,      // 더 자주 GC
    "gc_threshold_trades": 100
  }
}
```

#### 네트워크 최적화
```json
{
  "websocket": {
    "max_symbols_per_group": 80,        // 그룹 크기 조정
    "report_interval_seconds": 120,     // 보고 주기 늘림
    "message_timeout_seconds": 60       // 타임아웃 늘림
  }
}
```

### 📊 벤치마킹

```go
func benchmark(detector interfaces.PumpDetector) {
    start := time.Now()
    
    // 1시간 실행
    time.Sleep(1 * time.Hour)
    
    stats := detector.GetStats()
    duration := time.Since(start)
    
    log.Printf("📊 1시간 벤치마크 결과:")
    log.Printf("   처리된 이벤트: %d개", stats.TotalOrderbooks + stats.TotalTrades)
    log.Printf("   감지된 펌핑: %d회", stats.TotalPumpEvents)
    log.Printf("   평균 메모리: %.1fMB", stats.MemoryUsageMB)
    log.Printf("   최대 지연: %.1fms", stats.MaxLatencyMs)
    log.Printf("   에러율: %.2f%%", stats.ErrorRate)
}
```

---

## 📞 지원 및 커뮤니티

### 🆘 도움이 필요하면

1. **로그 확인**: `logs/noticepumpcatch.log`와 `logs/critical.log`
2. **설정 검토**: config.json의 설정값들
3. **예제 참조**: `examples/` 디렉토리의 샘플 코드
4. **성능 확인**: `GetStats()`로 현재 상태 파악

### 📝 이슈 리포트

문제 발생시 다음 정보를 포함해주세요:

```
운영체제: Windows/Linux/macOS
Go 버전: 1.19+
설정 파일: config.json (민감 정보 제거)
로그 파일: 최근 에러 로그
재현 방법: 문제가 발생하는 구체적 상황
```

---

## 📜 라이센스 및 면책조항

⚠️ **투자 위험 고지**: 
- 이 시스템은 정보 제공 목적이며, 투자 권유가 아닙니다
- 암호화폐 거래는 높은 리스크를 수반합니다
- 모든 투자 결정에 대한 책임은 사용자에게 있습니다

📄 **라이센스**: MIT License  
🔧 **기술 지원**: 시스템 연동 및 기술적 문제만 지원

---

## 🎉 성공적인 통합을 위한 마지막 팁

### ✅ 체크리스트
- [ ] 충분한 테스트 기간 확보 (최소 1주일)
- [ ] 백테스트로 설정값 검증
- [ ] 점진적 자본 투입 (소액 → 대액)
- [ ] 실시간 모니터링 시스템 구축
- [ ] 비상 정지 메커니즘 준비

### 🎯 성공 요인
1. **보수적 시작**: 높은 임계값으로 시작해서 점진적 조정
2. **지속적 모니터링**: 성능 지표와 수익률 추적
3. **리스크 관리**: 절대 100% 자동화하지 말고 사람의 판단 포함
4. **기술적 분석 병행**: 펌핑 신호만으로는 부족, 차트 분석 추가

---

**🚀 이제 여러분의 거래 시스템에 NoticePumpCatch를 통합할 준비가 완료되었습니다!**

**시작은 작게, 꿈은 크게! 📈✨** 