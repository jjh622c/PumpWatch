# CLAUDE.md

이 파일은 Claude Code(claude.ai/code)가 이 저장소에서 작업할 때 지침을 제공합니다.

## 프로젝트 개요

**PumpWatch**는 **업비트 상장공지 기반 펌핑 분석 시스템**입니다. 상장공지 감지 시점 기준 -20초부터 모든 체결데이터를 수집하고, 수집된 데이터에서 펌핑 구간을 탐지하여 분석용으로 정제하는 단순하고 확실한 시스템입니다.

### 최종 단순화 아키텍처

**복잡한 최적화 제거, "무식하게 때려박기" 구조로 확실성 우선:**

```
Upbit Monitor → Event Trigger → Memory Collector → Batch Writer → Pump Analyzer
  (5초 폴링)     (-20초 시작)    (무식하게 축적)     (raw 저장)     (refined 생성)
```

**핵심 목적**: 상장 펌핑 체결 기록 분석
**핵심 전략**: 일단 모든 데이터 저장 → 후처리로 펌핑 구간 탐지 및 정제

### 데이터 수집 흐름

1. **업비트 모니터링**: 5초 간격 API 폴링으로 상장공지 감지
2. **트리거 활성화**: 상장공지 감지 시 -20초부터 데이터 수집 시작  
3. **무식한 수집**: 12개 거래소 스트림에서 20초간 모든 데이터 메모리에 축적
4. **배치 저장**: 20초 후 모든 raw 데이터 일괄 JSON 저장
5. **펌핑 분석**: 저장된 데이터에서 1초에 수% 상승 구간 탐지
6. **데이터 정제**: 펌핑 관련 체결만 추출하여 refined 디렉토리에 별도 저장

## 개발 명령어

### 빌드 및 실행
```bash
# 최종 시스템 빌드 (SafeWorker 아키텍처)
go build -o pumpwatch main.go

# 심볼 초기화 (최초 실행 시 또는 업데이트 시)
./pumpwatch --init-symbols

# 🚀 실전 운영 (30분마다 자동 하드리셋)
./restart_wrapper.sh

# 일반 실행 (하드리셋 없이) - SafeWorker 단일 고루틴 아키텍처
./pumpwatch

# 프로덕션 빌드 (최적화)
go build -ldflags="-s -w" -o pumpwatch main.go

# 대안 빌드들 (테스트용)
go build -o pumpwatch-safe ./cmd/pumpwatch-safe  # SafeWorker 전용 빌드
```

### 테스트 및 검증
```bash
# SafeWorker 아키텍처 테스트
go test ./internal/websocket -v -timeout=60s

# 펌핑 탐지 알고리즘 테스트
go test ./internal/analyzer -v

# 전체 시스템 통합 테스트
go run ./cmd/test-safe  # SafeWorker 테스트 애플리케이션
```

### 데이터 분석
```bash
# 원시 데이터 확인 (자동 생성되는 구조)
ls -la data/SYMBOL_*/raw/

# 정제된 펌핑 데이터 확인
ls -la data/SYMBOL_*/refined/

# 펌핑 분석 (PumpAnalyzer를 통한 자동 분석)
# 데이터는 수집과 동시에 자동으로 분석되어 refined/ 에 저장됨

# 시스템 상태 모니터링 (1분마다 자동 출력)
# SafeTaskManager 상태, 워커풀 상태, 거래 통계 등
```

## 하드리셋 시스템 (WebSocket 안정성)

### 30분 자동 재시작 메커니즘

**문제 해결**: 바이낸스 등 일부 거래소는 장시간 연결 시 WebSocket을 강제로 끊는 문제가 있어, 30분마다 전체 시스템을 하드리셋하여 연결 안정성을 보장합니다.

#### 실전 운영 방법
```bash
# 🚀 실전 권장 방법 - 자동 하드리셋
./restart_wrapper.sh

# 📊 30분마다 다음 과정이 자동 실행:
# 1. 심볼 업데이트 (--init-symbols)
# 2. PumpWatch 실행 (30분)
# 3. Graceful Shutdown (SIGTERM)
# 4. 강제 종료 fallback (필요시)
# 5. 3초 대기 후 재시작
```

#### 시스템 특징
- **30분 주기**: WebSocket 연결 문제 예방을 위한 최적 간격
- **심볼 자동 업데이트**: 매 재시작시 최신 코인 목록 갱신
- **Graceful Shutdown**: 데이터 손실 없는 안전한 종료
- **자동 복구**: 프로세스 크래시 시에도 자동 재시작
- **로그 추적**: 모든 재시작 과정 상세 기록

#### 테스트 방법
```bash
# 30초 간격 테스트 (3회 사이클)
./test_restart.sh

# 수동 중지 (Ctrl+C로 언제든 안전하게 종료)
# → Graceful shutdown 자동 실행
```

#### 로그 예시
```
[PumpWatch-RESTART] [2025-09-06 20:12:55] 🔄 Starting restart cycle #1
[PumpWatch-RESTART] [2025-09-06 20:12:55] 📡 Updating symbols...
[PumpWatch-RESTART] [2025-09-06 20:12:55] ✅ Symbol update PASSED
[PumpWatch-RESTART] [2025-09-06 20:12:55] 🚀 Starting main PumpWatch process...
[PumpWatch-RESTART] [2025-09-06 20:12:55] ✅ Main process started (PID: 531395)
[PumpWatch-RESTART] [2025-09-06 20:13:25] ⏰ 5 minutes remaining until restart
[PumpWatch-RESTART] [2025-09-06 20:13:25] 🛑 Initiating graceful shutdown
[PumpWatch-RESTART] [2025-09-06 20:13:26] ✅ Graceful shutdown PASSED
```

### API 이슈 해결 완료

**업비트 API 400 에러 수정**: API 엔드포인트에 필수 파라미터 `?os=web&page=1&per_page=20&category=trade` 추가 및 JSON 응답 구조 업데이트로 정상 작동 확인

```bash
# API 수정 전: HTTP 400 Bad Request
# API 수정 후: 정상 공지사항 데이터 수신 ✅
📡 Upbit Monitor started - polling every 5s
🎯 System ready - monitoring for Upbit KRW listing announcements...
```

## SafeWorker 아키텍처 v2.0 (메모리 누수 방지)

### 고루틴 최적화 설계 (93% 감소: 136 → 10개)

**기존 문제점 해결:**
- **136개 고루틴** (34 워커 × 4 고루틴) → **10개 고루틴** (SafeWorker 단일 고루틴)
- **메모리 누수 위험** → **Context 기반 완전 생명주기 관리**
- **Mutex 데드락 위험** → **RWMutex 안전 패턴 적용**
- **파싱 실패 문제** → **견고한 메시지 처리 구조**

```go
// SafeWorker: 단일 고루틴으로 모든 WebSocket 이벤트 처리
type SafeWorker struct {
    ctx         context.Context    // 통합 생명주기 관리
    cancel      context.CancelFunc // 안전한 종료 제어
    conn        *websocket.Conn    // 단일 연결 관리
    messageChan chan []byte       // 버퍼링된 메시지 채널 (백프레셔 처리)
    tradeChan   chan models.TradeEvent // 처리된 거래 이벤트
}

// eventLoop: 모든 이벤트를 단일 고루틴에서 처리 (동시성 문제 해결)
func (sw *SafeWorker) eventLoop() {
    for {
        select {
        case <-sw.ctx.Done():        // Context 기반 안전한 종료
            return
        case msg := <-sw.messageChan: // 메시지 처리
            sw.handleMessage(msg)
        case <-sw.pingTicker.C:      // Ping/Pong 관리
            sw.sendPing()
        }
    }
}
```

### SafeTaskManager: 워커풀 통합 관리

```go
type SafeTaskManager struct {
    pools  map[string]*SafeWorkerPool  // 거래소별 워커풀
    ctx    context.Context             // 전체 생명주기 관리
    cancel context.CancelFunc
}

// 메모리 누수 없는 아키텍처
// - 각 워커는 독립적 Context 관리
// - Graceful Shutdown으로 모든 자원 정리
// - 원자적 통계 연산으로 데이터 경합 방지
```

## 핵심 시스템 구조

### 단순한 메모리 관리 - "무식하게 때려박기"
```go
// 최대한 단순한 구조
type CollectionEvent struct {
    Symbol    string
    StartTime time.Time
    
    // 거래소별 완전 독립 메모리 (동시성 문제 제거)
    BinanceSpot    []TradeEvent
    BinanceFutures []TradeEvent
    OKXSpot        []TradeEvent
    OKXFutures     []TradeEvent
    // ... 12개 독립 슬라이스
}

// -20초부터 수집 시작
func (ce *CollectionEvent) StartCollection(triggerTime time.Time) {
    ce.StartTime = triggerTime.Add(-20 * time.Second)  // 20초 전부터
    // 각 슬라이스에 20초간 데이터 축적
}

// 20초 후 모든 데이터 저장
func (ce *CollectionEvent) SaveRawData() {
    // raw/ 디렉토리에 모든 체결 데이터 저장
    saveToFile("raw/binance_spot.json", ce.BinanceSpot)
    saveToFile("raw/binance_futures.json", ce.BinanceFutures)
    // ... 12개 파일 저장
}
```

### 펌핑 탐지 시스템
```go
type PumpDetector struct {
    Threshold    float64  // 임계치 (예: 3% = 0.03)
    TimeWindow   time.Duration // 1초 단위 분석
}

// 1초에 수% 상승 구간 탐지
func (pd *PumpDetector) DetectPumps(trades []TradeEvent) []PumpEvent {
    var pumps []PumpEvent
    
    for _, second := range groupBySecond(trades) {
        priceChange := calculatePriceChange(second)
        if priceChange >= pd.Threshold {
            pump := PumpEvent{
                StartTime:    second.Start,
                EndTime:      second.End,
                PriceChange:  priceChange,
                VolumeSpike:  calculateVolumeSpike(second),
                Trades:       second.Trades,
            }
            pumps = append(pumps, pump)
        }
    }
    
    return pumps
}
```

### 데이터 정제 시스템
```go
// 펌핑 구간만 추출하여 분석 친화적으로 저장
func (ce *CollectionEvent) RefinePumpData() error {
    detector := NewPumpDetector(0.03) // 3% 임계치
    
    for exchange, trades := range ce.getAllTrades() {
        pumps := detector.DetectPumps(trades)
        if len(pumps) > 0 {
            refined := RefinedData{
                Exchange:     exchange,
                Symbol:       ce.Symbol,
                TriggerTime:  ce.StartTime.Add(20 * time.Second),
                PumpEvents:   pumps,
                TotalTrades:  len(trades),
                PumpTrades:   countPumpTrades(pumps),
            }
            
            saveToFile(fmt.Sprintf("refined/%s_pumps.json", exchange), refined)
        }
    }
    
    return nil
}
```

## 저장 구조

최종 데이터 저장 구조:

```
data/listings/SYMBOL_TIMESTAMP/
├── metadata.json              # 상장공지 메타데이터
├── raw/                       # 원시 데이터 (20초 전체)
│   ├── binance_spot.json      
│   ├── binance_futures.json   
│   ├── okx_spot.json          
│   ├── okx_futures.json       
│   └── ... (12개 파일)
└── refined/                   # 정제된 데이터 (펌핑 구간만)
    ├── pump_analysis.json     # 전체 펌핑 분석 결과
    ├── binance_pumps.json     # 바이낸스 펌핑 체결만
    ├── okx_pumps.json         # OKX 펌핑 체결만
    └── exchange_comparison.json # 거래소별 펌핑 비교
```

### 펌핑 분석 데이터 형식
```json
{
  "symbol": "TIA",
  "trigger_time": 1693876830000,
  "collection_period": {
    "start": 1693876810000,
    "end": 1693876830000
  },
  "pump_events": [
    {
      "exchange": "binance",
      "market_type": "spot",
      "start_time": 1693876825000,
      "end_time": 1693876826000,
      "price_change_percent": 4.2,
      "volume_spike_ratio": 15.6,
      "trades_count": 234,
      "key_trades": [
        {
          "price": "5.45",
          "quantity": "10000.0",
          "side": "buy",
          "timestamp": 1693876825123
        }
      ]
    }
  ],
  "summary": {
    "total_exchanges_with_pumps": 4,
    "max_price_change": 7.8,
    "first_pump_exchange": "binance",
    "pump_duration_ms": 1500
  }
}
```

## 구성 설정

`config.yaml` 파일의 단순화된 설정:

```yaml
# 단순하고 확실한 구성
monitor:
  poll_interval: 5s          # 업비트 API 폴링 간격
  proxy_enabled: false       # 프록시 사용 안함 (단순화)

collection:
  pre_trigger_duration: 20s  # 상장공지 전 20초부터 수집
  buffer_size: 50000        # 슬라이스 미리 할당 크기

memory:
  simple_mode: true         # "무식한" 메모리 관리
  cleanup_after_save: true  # 저장 후 즉시 메모리 정리
  trade_channel_size: 500000 # 거래 채널 크기 (상장 펌핑 대응)

pump_detection:
  threshold_percent: 3.0    # 3% 이상 상승을 펌핑으로 간주
  time_window: 1s          # 1초 단위 분석
  min_volume_ratio: 2.0    # 최소 거래량 증가 비율

storage:
  raw_data: true           # 원시 데이터 저장
  refined_data: true       # 정제 데이터 저장
  compression: false       # 압축 사용 안함 (단순성 우선)
```

## 시스템 특성

### SafeWorker 안정성 설계
- **고루틴 최적화**: 136개 → 10개 (93% 감소)로 시스템 부담 최소화
- **메모리 누수 방지**: Context 기반 생명주기 관리로 완전한 메모리 안전성
- **동시성 안전**: 단일 고루틴 이벤트 루프로 데이터 경합 문제 해결
- **연결 안정성**: 파싱 실패 및 ping/pong 문제 해결로 견고한 WebSocket 관리

### 단순성 우선 설계
- **복잡한 최적화 제거**: 실시간 최적화보다 확실한 데이터 수집
- **"무식한" 메모리 관리**: 32GB 환경에서 안전하게 모든 데이터 저장
- **2단계 처리**: 수집(실시간) → 분석(후처리) 분리로 안정성 확보
- **목적 특화**: 펌핑 분석이라는 명확한 목적에 최적화

### 메모리 사용량
- **20초 수집**: 일반적 48MB, 최악의 경우 480MB (32GB의 1.5%)
- **채널 버퍼**: SafeWorker당 500,000개 × 70개 = 7GB (상장 펌핑 대응)
- **총 메모리**: ~8GB (32GB의 25%, 안전한 범위)
- **안전한 운영**: 메모리 부족 걱정 없이 확실한 데이터 수집
- **즉시 정리**: 저장 완료 후 명시적 메모리 해제
- **고루틴 경량화**: SafeWorker 아키텍처로 메모리 오버헤드 최소화

### 데이터 품질
- **100% 수집**: 20초 전체 구간 모든 체결 데이터 보존
- **펌핑 특화**: 1초에 수% 상승 구간 정밀 탐지
- **분석 친화**: raw + refined 2단계 구조로 다양한 분석 지원

## 통합 포인트

### 분석 도구 연동
- **raw 데이터**: 전체 시장 상황 분석용
- **refined 데이터**: 펌핑 패턴 분석용
- **비교 분석**: 거래소별 펌핑 차이 분석

### 외부 시스템 연동
- **데이터 접근**: JSON 파일로 쉬운 외부 도구 연동
- **실시간 알림**: 펌핑 탐지 시 외부 시스템 알림 가능
- **백업 시스템**: 클라우드 스토리지 자동 업로드 지원

## 시스템 요구사항

### 하드웨어 사양
- **CPU**: 4코어 이상 (12개 동시 스트림 처리)
- **메모리**: 32GB 권장 ("무식한" 메모리 관리 활용)
- **저장소**: SSD 권장 (대용량 JSON 파일 처리)
- **네트워크**: 안정적 연결 (프록시 없는 단순 구조)

### 운영 환경
- **Linux**: 프로덕션 권장
- **Go 1.21+**: 최신 성능 활용
- **Docker**: 메모리 제한 없는 컨테이너 설정 (최소 10GB 권장)

## 개발 참고사항

### 설계 원칙
- **단순성**: 복잡한 최적화보다 단순하고 확실한 구조
- **확실성**: 모든 데이터 수집 후 안전하게 분석
- **목적성**: 펌핑 분석이라는 명확한 목적에 집중
- **실용성**: 디버깅하기 쉽고 유지보수가 간단한 구조
- **안전성**: SafeWorker 아키텍처로 메모리 누수 및 동시성 문제 완전 해결

### 핵심 특징
- **고루틴 최적화**: SafeWorker로 136개 → 10개 (93% 감소)
- **메모리 안전**: Context 기반 생명주기 관리 + "무식한" 구조로 완전한 안전성
- **연결 안정성**: 파싱 실패 해결, ping/pong 정상화로 견고한 WebSocket 관리
- **2단계 저장**: raw → refined 흐름으로 데이터 보존 + 분석 편의
- **펌핑 특화**: 1초 단위 급등 탐지로 정확한 분석 데이터 제공

## 미래 확장

### 분석 기능 강화
- **패턴 학습**: 펌핑 패턴 자동 분류 및 예측
- **실시간 대시보드**: 펌핑 탐지 결과 실시간 시각화
- **알림 시스템**: 특정 패턴 감지 시 즉시 알림

### 데이터 활용
- **백테스팅**: 과거 펌핑 데이터로 전략 검증
- **통계 분석**: 거래소별/시간대별 펌핑 특성 분석
- **머신러닝**: 펌핑 예측 모델 훈련 데이터 제공

---

**💡 핵심 메시지**: PumpWatch v2.0은 SafeWorker 아키텍처로 메모리 누수 및 동시성 문제를 완전 해결하면서, "무식하게 때려박기" 전략으로 상장 펌핑 분석에 특화된 견고한 데이터 수집 시스템입니다. 고루틴 93% 감소(136→10개)와 Context 기반 생명주기 관리로 안정성과 확실성을 최우선으로 합니다.

## 최신 업데이트 (2025-09-13) - 채널 최적화

### SafeWorker 채널 크기 대폭 확대
```go
// 기존: 1,000개 → 신규: 500,000개
tradeChan: make(chan models.TradeEvent, 500000)
```

**배경**: 로그 분석 결과 상장 펌핑 시점에 bybit futures SafeWorker에서 백프레셔 발생으로 일부 데이터 손실 확인

**해결책**: "무식하게 때려박기" 철학에 맞게 채널 크기를 극대화하여 어떤 극한 상황에서도 데이터 손실 방지

**메모리 영향**:
- 채널당 메모리: 500,000 × 200 bytes = 100MB
- 전체 70개 워커: 7GB (32GB의 22%)
- 30분 하드리셋으로 메모리 누수 완전 방지

**예상 효과**:
- ✅ 백프레셔 경고 완전 제거
- ✅ 상장 펌핑 시 100% 데이터 보존
- ✅ 시스템 안정성 극대화

**구현 위치**: `internal/websocket/safe_worker.go:52`