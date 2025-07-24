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

**NoticePumpCatch**는 바이낸스 실시간 데이터를 모니터링하여 **펌핑(급등)**과 **상장공시**를 **HFT 수준**으로 감지하는 고성능 시스템입니다. **대대적인 정리를 통해 깔끔해진 코드베이스**로 **어떤 거래 시스템에도 쉽게 통합**할 수 있습니다.

### 🎯 핵심 가치
- ⚡ **HFT 수준 감지**: **마이크로초 단위** 펌핑 신호 포착
- 🎯 **높은 정확도**: 거래량, 스프레드, 패턴 분석으로 가짜 신호 필터링
- 🔧 **완전 모듈화**: 플러그인 방식으로 기존 시스템에 통합
- 🛡️ **안정성**: 메모리 누수 방지, 고루틴 관리, 자동 복구
- 📊 **투명성**: 상세한 로깅과 통계로 완전한 추적 가능
- 🧹 **깔끔한 코드베이스**: 불필요한 요소 제거로 통합 최적화

---

## ✨ 주요 기능

### ⚡ HFT 펌핑 감지
- **실시간 가격 급등 감지**: **1% 임계값**으로 민감한 감지
- **마이크로초 단위 처리**: lock-free 링 버퍼 기반
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
├── internal/              # 🔒 핵심 구현 (정리 완료)
│   ├── hft/              # ⚡ HFT 수준 펌핑 감지 (핵심)
│   ├── websocket/        # 바이낸스 WebSocket
│   ├── memory/           # 메모리 관리
│   ├── signals/          # 시그널 처리
│   ├── sync/             # 심볼 동기화
│   ├── config/           # 설정 관리
│   ├── cache/            # 고성능 캐싱
│   ├── storage/          # 데이터 저장
│   ├── triggers/         # 트리거 관리
│   ├── callback/         # 콜백 시스템
│   ├── latency/          # 지연 모니터링
│   ├── logger/           # 로깅 시스템
│   └── monitor/          # 성능 모니터링
├── config.json          # ⚙️ 메인 설정 파일
├── main.go              # 🚀 시스템 진입점
├── signals/             # 📁 감지된 시그널 저장소
├── data/                # 📁 거래/오더북 데이터
├── logs/                # 📁 시스템 로그
└── README_INTEGRATION.md # 📚 이 문서
```

**🧹 정리 완료**: 불필요한 `examples/`, `pkg/`, 파이썬 파일들 모두 제거

---

## 🚀 빠른 시작

### 1️⃣ 기본 설정

**config.json 확인 및 수정:**

```json
{
  "signals": {
    "pump_detection": {
      "enabled": true,
      "price_change_threshold": 1.0,  // 1% 상승시 펌핑 감지
      "time_window_seconds": 2        // 2초 윈도우
    }
  },
  "websocket": {
    "sync_enabled": true,             // 자동 심볼 동기화
    "enable_upbit_filter": true       // 업비트 상장 코인만
  },
  "memory": {
    "trade_retention_minutes": 1,     // 거래 데이터 1분 보존
    "cleanup_interval_minutes": 10    // 10분마다 정리
  }
}
```

### 2️⃣ 직접 통합 방법

**방법 1: HFT 감지기 직접 연결**

```go
package main

import (
    "log"
    "noticepumpcatch/internal/hft"
    "noticepumpcatch/internal/memory"
)

func main() {
    // HFT 펌핑 감지기 생성 (1% 임계값, 2초 윈도우)
    detector := hft.NewHFTPumpDetector(1.0, 2)
    
    // 감지기 시작
    if err := detector.Start(); err != nil {
        log.Fatal("HFT 감지기 시작 실패:", err)
    }
    
    // 펌핑 감지 시 콜백
    go func() {
        for alert := range detector.GetResultQueue() {
            symbol := string(alert.Symbol[:])
            priceChange := float64(alert.PriceChange) / 10000.0
            
            log.Printf("🚨 펌핑 감지: %s +%.2f%% (신뢰도: %d, 거래: %d건)",
                symbol, priceChange, alert.Confidence, alert.TradeCount)
            
            // 여기에 실제 거래 로직 추가
            executeTradeStrategy(symbol, priceChange)
        }
    }()
    
    select {} // 무한 대기
}

func executeTradeStrategy(symbol string, priceChange float64) {
    // 실제 거래 전략 구현
    log.Printf("🎯 거래 실행: %s (변화율: %.2f%%)", symbol, priceChange)
}
```

**방법 2: 시그널 파일 모니터링**

```go
package main

import (
    "encoding/json"
    "io/ioutil"
    "log"
    "path/filepath"
    "time"
    
    "github.com/fsnotify/fsnotify"
)

type PumpSignal struct {
    Symbol      string    `json:"symbol"`
    PriceChange float64   `json:"price_change"`
    Confidence  float64   `json:"confidence"`
    DetectedAt  time.Time `json:"detected_at"`
    TradeCount  int32     `json:"trade_count"`
}

func main() {
    watcher, err := fsnotify.NewWatcher()
    if err != nil {
        log.Fatal(err)
    }
    defer watcher.Close()

    // signals 디렉토리 모니터링
    err = watcher.Add("./signals")
    if err != nil {
        log.Fatal(err)
    }

    for {
        select {
        case event := <-watcher.Events:
            if event.Op&fsnotify.Create == fsnotify.Create && 
               filepath.Ext(event.Name) == ".json" {
                handleNewSignal(event.Name)
            }
        case err := <-watcher.Errors:
            log.Println("감시 오류:", err)
        }
    }
}

func handleNewSignal(filename string) {
    data, err := ioutil.ReadFile(filename)
    if err != nil {
        log.Printf("파일 읽기 실패: %v", err)
        return
    }

    var signal PumpSignal
    if err := json.Unmarshal(data, &signal); err != nil {
        log.Printf("JSON 파싱 실패: %v", err)
        return
    }

    // 신뢰도 기반 필터링
    if signal.Confidence >= 70.0 && signal.PriceChange >= 1.0 {
        log.Printf("🎯 고신뢰도 펌핑: %s +%.2f%% (신뢰도: %.1f%%)",
            signal.Symbol, signal.PriceChange, signal.Confidence)
        
        // 거래 전략 실행
        executeTradeStrategy(signal.Symbol, signal.PriceChange)
    }
}
```

### 3️⃣ 메모리 기반 데이터 접근

```go
package main

import (
    "log"
    "time"
    "noticepumpcatch/internal/memory"
)

func main() {
    // 메모리 매니저 생성
    memManager := memory.NewManager(50, 100, 1000, 1, 0.5, 
        60, 500.0, 1000, 2000, 50, 300)
    
    // 실시간 거래 데이터 조회
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        // 최근 1분간 BTCUSDT 거래 데이터
        trades := memManager.GetRecentTrades("binance", "BTCUSDT", 
            time.Minute)
        
        if len(trades) > 0 {
            latest := trades[len(trades)-1]
            log.Printf("📊 BTCUSDT 최신: %s %s @ %s", 
                latest.Quantity, latest.Side, latest.Price)
        }
        
        // 메모리 사용량 모니터링
        stats := memManager.GetMemoryStats()
        log.Printf("💾 메모리: 거래 %v건, 오더북 %v건", 
            stats["total_trades"], stats["total_orderbooks"])
    }
}
```

---

## ⚙️ 설정 가이드

### HFT 최적화 설정

```json
{
  "signals": {
    "pump_detection": {
      "enabled": true,
      "price_change_threshold": 1.0,     // HFT용 1% 임계값
      "time_window_seconds": 2,          // 2초 윈도우
      "min_score": 70.0                  // 신뢰도 70% 이상
    }
  },
  "websocket": {
    "worker_count": 8,                   // 워커 수 (CPU 코어 수)
    "buffer_size": 10000,                // 버퍼 크기
    "max_symbols_per_group": 100,        // 그룹당 심볼 수
    "sync_enabled": true
  },
  "memory": {
    "max_trades_per_symbol": 100,        // 심볼당 최대 거래 수
    "max_orderbooks_per_symbol": 50,     // 심볼당 최대 오더북 수
    "trade_retention_minutes": 1,        // 거래 데이터 보존 시간
    "orderbook_retention_minutes": 0.5,  // 오더북 보존 시간
    "cleanup_interval_minutes": 10,      // 정리 주기
    "heap_warning_mb": 500.0,            // 메모리 경고 임계값
    "compression_interval_seconds": 60,  // 압축 주기
    "gc_threshold_trades": 2000,         // GC 트리거 거래 수
    "gc_threshold_orderbooks": 1000      // GC 트리거 오더북 수
  },
  "storage": {
    "base_dir": "./data",                // 데이터 저장 경로
    "retention_days": 7,                 // 데이터 보존 일수
    "compress_data": true                // 데이터 압축 여부
  }
}
```

### 임계값 튜닝 가이드

| 시장 상황 | 임계값 | 윈도우 | 신뢰도 | 설명 |
|---------|--------|--------|--------|------|
| **고변동성** | 2.0% | 1초 | 80% | 큰 움직임만 감지 |
| **중변동성** | 1.0% | 2초 | 70% | **기본 권장 설정** |
| **저변동성** | 0.5% | 3초 | 60% | 민감한 감지 |
| **스캘핑** | 0.3% | 1초 | 50% | 매우 민감 (노이즈 주의) |

---

## 🤝 거래 시스템 통합

### 통합 패턴

**1. 이벤트 기반 통합**
```go
// 펌핑 감지 → 즉시 거래 실행
detector.SetCallback(func(signal PumpSignal) {
    if signal.Confidence >= 80 {
        market.Buy(signal.Symbol, calculateQuantity(signal))
    }
})
```

**2. 폴링 기반 통합**
```go
// 주기적으로 시그널 확인
for {
    signals := getLatestSignals()
    for _, signal := range signals {
        processSignal(signal)
    }
    time.Sleep(100 * time.Millisecond)
}
```

**3. 스트림 기반 통합**
```go
// WebSocket 데이터 스트림 공유
websocket.SetTradeCallback(func(trade TradeData) {
    // 실시간 거래 데이터 처리
    updateIndicators(trade)
    checkArbitrage(trade)
})
```

### 성능 최적화 팁

**1. 메모리 튜닝**
```json
{
  "memory": {
    "trade_retention_minutes": 0.5,      // 거래: 30초만 보존
    "orderbook_retention_minutes": 0.1,  // 오더북: 6초만 보존
    "cleanup_interval_minutes": 5        // 5분마다 정리
  }
}
```

**2. CPU 최적화**
```bash
# CPU 친화성 설정 (Linux)
taskset -c 0-3 ./noticepumpcatch.exe

# 우선순위 설정
nice -n -10 ./noticepumpcatch.exe
```

**3. 네트워크 최적화**
```json
{
  "websocket": {
    "worker_count": 16,        // 더 많은 워커
    "buffer_size": 50000,      // 큰 버퍼
    "max_symbols_per_group": 50 // 작은 그룹
  }
}
```

---

## 📊 모니터링 및 디버깅

### HFT 성능 메트릭

```bash
# 실시간 HFT 통계 모니터링
tail -f logs/noticepumpcatch.log | grep "HFT STATS"

# 출력 예시:
# 📊 [HFT STATS] 체결: 12,350건, 펌핑: 8건, 평균지연: 245μs, 심볼: 270개
# ⚡ [HFT PUMP] BTCUSDT: +1.25% (지연: 234μs, 체결: 42건)
```

### 중요 로그 패턴
```bash
# 펌핑 감지
grep "HFT PUMP" logs/noticepumpcatch.log

# 성능 이슈
grep "MEMORY ALERT\|GOROUTINE ALERT" logs/noticepumpcatch.log

# 연결 문제
grep "WebSocket.*실패\|연결.*끊김" logs/noticepumpcatch.log

# 시스템 상태
grep "HEALTH" logs/noticepumpcatch.log
```

### 대시보드 활용

내장 HTTP 서버로 실시간 모니터링:
```bash
# 브라우저에서 접속
http://localhost:8080

# API로 통계 조회
curl http://localhost:8080/api/stats | jq
```

---

## 🛠️ 문제 해결

### 자주 발생하는 문제들

**1. HFT 지연시간 증가**
```bash
# 시스템 리소스 확인
top -p $(pgrep noticepumpcatch)

# 해결책: CPU 친화성 설정
taskset -c 0-3 ./noticepumpcatch.exe
```

**2. 메모리 사용량 급증**
```json
{
  "memory": {
    "trade_retention_minutes": 0.5,     // 더 짧게
    "cleanup_interval_minutes": 5       // 더 자주 정리
  }
}
```

**3. WebSocket 연결 불안정**
```bash
# 네트워크 확인
ping api.binance.com

# DNS 캐시 초기화
sudo systemctl flush-dns
```

**4. 가짜 시그널 과다**
```json
{
  "signals": {
    "pump_detection": {
      "price_change_threshold": 1.5,   // 임계값 상향
      "min_score": 80.0,               // 신뢰도 상향
      "volume_threshold": 200.0        // 거래량 필터 강화
    }
  }
}
```

---

## 📈 성능 최적화

### 운영 환경 최적화

**시스템 설정**
```bash
# 파일 디스크립터 한계 증가
ulimit -n 65536

# 메모리 오버커밋 설정
echo 1 > /proc/sys/vm/overcommit_memory

# TCP 버퍼 크기 증가
echo 'net.core.rmem_max = 16777216' >> /etc/sysctl.conf
echo 'net.core.wmem_max = 16777216' >> /etc/sysctl.conf
```

**Go 런타임 튜닝**
```bash
# 가비지 컬렉터 최적화
export GOGC=100
export GOMEMLIMIT=2GB

# CPU 사용률 제한
export GOMAXPROCS=8
```

### 벤치마크 결과

| 메트릭 | 값 | 설명 |
|--------|-----|------|
| **감지 지연시간** | 200-500μs | 평균 HFT 처리 시간 |
| **처리량** | 1,500건/초 | 거래+오더북 합계 |
| **메모리 사용량** | 100-200MB | 정상 운영 시 |
| **CPU 사용률** | 10-30% | 8코어 기준 |
| **정확도** | 85-95% | 신뢰도 기반 |

### 확장성 가이드

**수평 확장**: 여러 인스턴스로 심볼 분산
```bash
# 인스턴스 1: A-M 심볼
./noticepumpcatch.exe --symbol-filter="^[A-M]"

# 인스턴스 2: N-Z 심볼  
./noticepumpcatch.exe --symbol-filter="^[N-Z]"
```

**수직 확장**: 리소스 증대
```json
{
  "websocket": {
    "worker_count": 32,        // 더 많은 워커
    "buffer_size": 100000      // 더 큰 버퍼
  },
  "memory": {
    "max_trades_per_symbol": 500,     // 더 많은 데이터
    "max_orderbooks_per_symbol": 200
  }
}
```

---

## 🔗 추가 리소스

### 공식 문서
- [메인 README](README.md) - 기본 설치 및 사용법
- [설정 가이드](config.json) - 상세 설정 옵션

### 커뮤니티 및 지원
- GitHub Issues: 버그 리포트 및 기능 요청
- Discussions: 사용법 문의 및 경험 공유

### 관련 도구
- **fsnotify**: 파일 시스템 모니터링
- **gorilla/websocket**: WebSocket 클라이언트
- **golang/glog**: 구조화된 로깅

---

**🚀 이제 여러분의 거래 시스템에 NoticePumpCatch를 통합해서 시장의 기회를 놓치지 마세요!**

**💡 핵심**: 정리된 깔끔한 코드베이스로 통합이 훨씬 쉬워졌습니다. HFT 수준의 성능으로 1등의 진입 데이터를 확보하고 2등으로 진입하는 전략을 구현하세요! 