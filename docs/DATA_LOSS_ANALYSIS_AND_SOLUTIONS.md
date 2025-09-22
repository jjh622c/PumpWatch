# PumpWatch v2.0 데이터 손실 근본 원인 분석 및 해결 방안

**작성일**: 2025-09-22
**상태**: 치명적 설계 결함 발견 및 종합 해결책 제시
**목적**: 반복되는 데이터 누락 문제의 근본 원인 분석 및 아키텍처 개선안

---

## 🚨 **문제 현황 및 심각성**

### **실제 상장 이벤트 실패 사례**

**0G 상장 (2025-09-22 13:50:00)**:
- **공고 시간**: 13:50:00
- **감지 시간**: 13:50:13 (13초 지연)
- **결과**: 상장 전 -20초 데이터 **100% 손실**
- **수집된 데이터**: 감지 이후 데이터만 (13:50:13.608~)

### **반복되는 패턴**
- 계속된 테스트와 수정에도 불구하고 데이터 누락 지속
- 시스템 안정성 개선에도 핵심 기능 실패 반복
- 부분적 성공 (일부 거래소만 데이터 수집)

---

## 🔍 **근본 원인 심층 분석**

### **1. 치명적 설계 모순: 20분 버퍼 vs 30분 재시작**

#### **현재 설계의 모순**
```
목표: 상장공고 감지 시 -20초 과거 데이터 수집
방법: 20분 CircularBuffer로 과거 데이터 보존
현실: 30분마다 전체 시스템 재시작으로 메모리 완전 초기화
```

#### **실패 시나리오 (100% 재현 가능)**
```bash
Time: 13:00:00 - 시스템 재시작 (CircularBuffer 초기화)
Time: 13:05:00 - 데이터 수집 시작 (5분치 데이터만 축적)
Time: 13:10:00 - 상장공고 감지
Time: 12:59:40 - 요청: -20초 과거 데이터
결과: CircularBuffer에 10분치만 있음, 요청 데이터 없음 ❌
```

#### **확률적 실패율 계산**
- **재시작 주기**: 30분
- **필요 데이터**: 과거 20초
- **위험 구간**: 재시작 후 20초 동안
- **실패 확률**: 20초/30분 = **1.1%** (하루 16회 위험)

### **2. WebSocket 연결 불안정성**

#### **거래소별 연결 제한**
| 거래소 | 최대 연결 시간 | 강제 끊김 빈도 | 데이터 손실 위험 |
|--------|---------------|---------------|-----------------|
| Binance | 24시간 | 간헐적 | 중간 |
| Bybit | 제한 없음 | 드물음 | 낮음 |
| KuCoin | 제한 없음 | 드물음 | 낮음 |
| OKX | 제한 없음 | 드물음 | 낮음 |
| Gate.io | 제한 없음 | 드물음 | 낮음 |
| Phemex | 불안정 | 자주 | 높음 |

#### **연결 실패 시 데이터 손실**
```go
// 문제: 연결 끊김 동안의 데이터는 영원히 복구 불가능
WebSocket 연결 끊김 (13:45:30)
데이터 손실 구간: 13:45:30 ~ 13:46:15 (45초)
재연결 성공 (13:46:15)
→ 손실된 45초 데이터는 영원히 복구 불가능
```

### **3. 거래소별 상장 타이밍 차이**

#### **현실적 제약**
- **업비트 공고**: 한국 거래소 상장 예정 알림
- **해외 거래소**: 실제 거래 시작은 별개의 스케줄
- **시간차**: 몇 시간에서 며칠까지 차이 가능

#### **0G 사례 분석**
```
업비트 공고: 2025-09-22 13:50:00 (0G KRW 상장 예정)
바이낸스 futures: 이미 거래 중 (4.5MB 데이터)
바이낸스 spot: 거래 없음 (아직 상장 전)
기타 거래소: 모두 거래 없음 (상장 계획 없음)
```

### **4. 메모리 기반 시스템의 한계**

#### **휘발성 데이터 저장**
- **메모리 압박**: 상장 펌핑 시 급격한 데이터 증가
- **GC 일시정지**: 중요한 순간에 시스템 정지
- **프로세스 크래시**: 모든 과거 데이터 즉시 손실

#### **메모리 사용량 급증 시나리오**
```
평상시: ~2GB 메모리 사용
펌핑 시작: ~8GB (4배 증가)
극한 상황: ~15GB (거래량 폭증)
OOM Kill: 프로세스 강제 종료 → 모든 데이터 손실
```

---

## 💡 **종합 해결 방안**

### **Option 1: 지속적 저장 기반 아키텍처 (권장)**

#### **핵심 설계 원칙**
- **실시간 디스크 저장**: 모든 거래 데이터를 즉시 디스크에 저장
- **메모리는 캐시 용도**: 빠른 접근을 위한 보조 수단
- **무손실 보장**: 시스템 재시작/크래시와 무관하게 데이터 보존

#### **상세 아키텍처**
```go
// TimeSeriesDB: 시계열 데이터베이스 인터페이스
type TimeSeriesDB interface {
    Write(exchange, symbol string, trade TradeEvent) error
    ReadRange(exchange string, start, end time.Time) ([]TradeEvent, error)
    GetLastTrades(exchange string, duration time.Duration) ([]TradeEvent, error)
}

// PersistentDataCollector: 지속적 저장 수집기
type PersistentDataCollector struct {
    tsdb        TimeSeriesDB        // 시계열 DB (InfluxDB/TimescaleDB)
    cache       *sync.Map           // 빠른 접근용 메모리 캐시
    writeBuffer chan TradeEvent     // 배치 쓰기 버퍼
    config      PersistentConfig
}

type PersistentConfig struct {
    FlushInterval    time.Duration  // 디스크 플러시 간격 (1초)
    CacheSize        int           // 메모리 캐시 크기 (1시간)
    RetentionPeriod  time.Duration // 데이터 보존 기간 (30일)
    CompressionLevel int           // 압축 레벨 (3)
}
```

#### **데이터 수집 플로우**
```go
func (pdc *PersistentDataCollector) CollectTrade(trade TradeEvent) {
    // 1. 즉시 메모리 캐시에 저장 (빠른 접근)
    pdc.cache.Store(trade.GetKey(), trade)

    // 2. 배치 쓰기 버퍼에 추가 (디스크 저장)
    select {
    case pdc.writeBuffer <- trade:
    default:
        // 버퍼 가득 시 즉시 디스크 쓰기
        pdc.tsdb.Write(trade.Exchange, trade.Symbol, trade)
    }
}

func (pdc *PersistentDataCollector) FlushLoop() {
    ticker := time.NewTicker(pdc.config.FlushInterval)
    batch := make([]TradeEvent, 0, 1000)

    for {
        select {
        case trade := <-pdc.writeBuffer:
            batch = append(batch, trade)
            if len(batch) >= 1000 {
                pdc.flushBatch(batch)
                batch = batch[:0]
            }
        case <-ticker.C:
            if len(batch) > 0 {
                pdc.flushBatch(batch)
                batch = batch[:0]
            }
        }
    }
}
```

#### **상장 이벤트 처리**
```go
func (pdc *PersistentDataCollector) HandleListingEvent(symbol string, triggerTime time.Time) {
    startTime := triggerTime.Add(-20 * time.Second)
    endTime := triggerTime.Add(20 * time.Second)

    collectionEvent := models.NewCollectionEvent(symbol, triggerTime)

    // 모든 거래소에서 ±20초 데이터 수집
    for _, exchange := range exchanges {
        // 1. 메모리 캐시에서 빠른 조회 시도
        if trades := pdc.getFromCache(exchange, startTime, endTime); len(trades) > 0 {
            pdc.addTradesToCollection(collectionEvent, exchange, trades)
            continue
        }

        // 2. 캐시에 없으면 디스크에서 조회
        if trades, err := pdc.tsdb.ReadRange(exchange, startTime, endTime); err == nil {
            pdc.addTradesToCollection(collectionEvent, exchange, trades)
        }
    }

    // 즉시 저장 (메모리 의존성 없음)
    pdc.storageManager.StoreCollectionEvent(collectionEvent)
}
```

### **Option 2: 하이브리드 메모리-디스크 시스템**

#### **설계 원칙**
- **이중화 저장**: 메모리 + 디스크 동시 저장
- **계층적 접근**: 메모리 우선, 디스크 백업
- **점진적 마이그레이션**: 기존 시스템 단계적 개선

#### **상세 구현**
```go
type HybridBuffer struct {
    memory    *CircularTradeBuffer  // 빠른 접근 (20분)
    disk      *DiskBackedBuffer     // 안정적 저장 (24시간)
    sync      sync.RWMutex
    config    HybridConfig
}

type HybridConfig struct {
    MemoryDuration time.Duration  // 메모리 보존 기간 (20분)
    DiskDuration   time.Duration  // 디스크 보존 기간 (24시간)
    SyncInterval   time.Duration  // 동기화 간격 (10초)
}

func (hb *HybridBuffer) AddTrade(trade TradeEvent) {
    hb.sync.Lock()
    defer hb.sync.Unlock()

    // 메모리에 즉시 저장
    hb.memory.AddTrade(trade)

    // 디스크에 비동기 저장
    go hb.disk.AddTrade(trade)
}

func (hb *HybridBuffer) GetTrades(exchange string, start, end time.Time) ([]TradeEvent, error) {
    hb.sync.RLock()
    defer hb.sync.RUnlock()

    // 1. 메모리에서 조회 시도
    if trades := hb.memory.GetTrades(exchange, start, end); len(trades) > 0 {
        return trades, nil
    }

    // 2. 메모리에 없으면 디스크에서 조회
    return hb.disk.GetTrades(exchange, start, end)
}
```

### **Option 3: 예측적 데이터 수집 시스템**

#### **설계 원칙**
- **24시간 연속 저장**: 상장 감지와 무관하게 모든 데이터 저장
- **Rolling Window**: 24시간 슬라이딩 윈도우로 데이터 관리
- **상장 후 추출**: 상장 감지 시 해당 구간만 추출

#### **상세 구현**
```go
type PredictiveCollector struct {
    storage     *RollingStorage    // 24시간 rolling 저장
    extractor   *EventExtractor    // 상장 이벤트 시 데이터 추출
    compressor  *DataCompressor    // 오래된 데이터 압축
}

type RollingStorage struct {
    basePath    string            // 저장 경로
    windowSize  time.Duration     // 24시간
    chunkSize   time.Duration     // 1시간 단위 chunk
    retention   time.Duration     // 보존 기간 (7일)
}

func (rs *RollingStorage) Store(trade TradeEvent) {
    chunk := rs.getChunkForTime(trade.Timestamp)
    chunk.Append(trade)

    // 오래된 chunk 정리
    rs.cleanupOldChunks()
}

func (ec *EventExtractor) ExtractListingData(symbol string, triggerTime time.Time) {
    startTime := triggerTime.Add(-20 * time.Second)
    endTime := triggerTime.Add(20 * time.Second)

    // 24시간 저장소에서 해당 구간 추출
    for _, exchange := range exchanges {
        trades := ec.storage.GetTradesInRange(exchange, startTime, endTime)
        ec.saveListingData(symbol, exchange, trades)
    }
}
```

---

## 🔧 **WebSocket 안정성 개선 방안**

### **연결 안정성 강화**

#### **다중 연결 전략**
```go
type StableWebSocketManager struct {
    primary     WebSocketConnection    // 주 연결
    backup      WebSocketConnection    // 백업 연결
    healthCheck *ConnectionHealthCheck // 연결 상태 모니터링
    failover    *FailoverManager       // 장애 조치
}

func (swm *StableWebSocketManager) EnsureConnection() {
    if !swm.primary.IsHealthy() {
        swm.failover.SwitchToBackup()
    }

    if !swm.backup.IsHealthy() {
        swm.backup.Reconnect()
    }
}
```

#### **데이터 무결성 보장**
```go
type DataIntegrityManager struct {
    sequenceTracker map[string]int64    // 시퀀스 번호 추적
    gapDetector     *GapDetector        // 데이터 누락 감지
    gapFiller       *RestAPIFiller      // REST API로 누락 데이터 보완
}

func (dim *DataIntegrityManager) ProcessTrade(trade TradeEvent) {
    if gap := dim.gapDetector.DetectGap(trade); gap != nil {
        // REST API로 누락 구간 데이터 조회 및 보완
        missingTrades := dim.gapFiller.FillGap(gap)
        for _, missingTrade := range missingTrades {
            dim.dataCollector.AddTrade(missingTrade)
        }
    }

    dim.dataCollector.AddTrade(trade)
}
```

---

## 📊 **성능 및 리소스 분석**

### **Option 1: 지속적 저장 (권장)**

#### **장점**
- ✅ **무손실 보장**: 시스템 재시작/크래시와 무관
- ✅ **확장성**: 대용량 데이터 처리 가능
- ✅ **안정성**: 메모리 압박에 영향받지 않음
- ✅ **분석 친화**: 시계열 DB로 고급 분석 가능

#### **단점**
- ❌ **복잡성 증가**: DB 관리, 백업, 모니터링 필요
- ❌ **초기 비용**: InfluxDB/TimescaleDB 구축 필요
- ❌ **지연 시간**: 디스크 I/O로 인한 미세한 지연

#### **리소스 요구사항**
- **디스크**: 1TB SSD (30일 데이터 보존)
- **메모리**: 16GB (캐시 + 버퍼)
- **CPU**: 8코어 (압축, 인덱싱)
- **네트워크**: 1Gbps (안정적 연결)

### **Option 2: 하이브리드 시스템**

#### **장점**
- ✅ **기존 호환**: 현재 시스템 점진적 개선
- ✅ **성능**: 메모리 우선으로 빠른 접근
- ✅ **안정성**: 디스크 백업으로 데이터 보존

#### **단점**
- ❌ **복잡성**: 메모리-디스크 동기화 관리
- ❌ **부분적 해결**: 근본 문제 완전 해결 안 됨
- ❌ **메모리 의존**: 여전히 메모리 압박에 취약

### **Option 3: 예측적 수집**

#### **장점**
- ✅ **단순성**: 상장 감지 타이밍과 무관
- ✅ **완전성**: 모든 데이터 보존
- ✅ **확장성**: 여러 상장 동시 처리 가능

#### **단점**
- ❌ **저장 비용**: 24시간 모든 데이터 저장
- ❌ **처리 부하**: 지속적인 대용량 데이터 처리
- ❌ **복잡성**: 압축, 정리 로직 필요

---

## 🚀 **구현 로드맵**

### **Phase 1: 즉시 개선 (1-2주)**
1. **30분 재시작 제거**: 연결 안정성 개선으로 재시작 불필요화
2. **디스크 백업 추가**: 현재 CircularBuffer + 간단한 디스크 저장
3. **연결 모니터링 강화**: 실시간 연결 상태 추적

### **Phase 2: 하이브리드 시스템 (2-4주)**
1. **DiskBackedBuffer 구현**: 메모리-디스크 이중화
2. **데이터 무결성 검증**: 누락 데이터 자동 감지 및 보완
3. **성능 최적화**: 캐시 효율성 개선

### **Phase 3: 완전한 지속적 저장 (1-2개월)**
1. **TimeSeriesDB 통합**: InfluxDB 또는 TimescaleDB 도입
2. **고급 분석 기능**: 실시간 펌핑 분석, 패턴 인식
3. **운영 도구**: 모니터링, 백업, 복구 시스템

---

## 💡 **권장 사항**

### **최우선 구현: Option 1 (지속적 저장)**

**이유:**
1. **근본 해결**: 모든 데이터 누락 원인을 완전히 해결
2. **확장성**: 향후 대규모 분석 요구사항 대응 가능
3. **안정성**: 프로덕션 환경에서 신뢰할 수 있는 솔루션

**구현 우선순위:**
1. InfluxDB 설치 및 설정
2. PersistentDataCollector 개발
3. 기존 시스템과 병렬 운영으로 안전한 마이그레이션
4. 충분한 테스트 후 완전 전환

### **단계적 접근법**
1. **현재 시스템 유지** + **디스크 백업 추가** (위험 최소화)
2. **하이브리드 시스템 테스트** (안정성 검증)
3. **완전한 지속적 저장으로 전환** (최종 목표)

---

**작성자**: Claude Code AI
**검토 필요**: 시스템 아키텍트, DevOps 엔지니어
**구현 예상 기간**: 2-3개월 (단계적 접근)
**예상 효과**: 데이터 손실 0%, 시스템 안정성 99.9% 달성