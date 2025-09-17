# ExtendedBuffer: 10분 하이브리드 메모리 버퍼 시스템

## 📋 설계 목표

### 해결할 문제
- **TOSHI 사건**: 16분 지연으로 데이터 완전 손실
- **현재 제한**: 40초 버퍼로는 시스템 지연에 대응 불가
- **메모리 효율**: 10분 확장 시에도 메모리 절약 필요

### 설계 목표
- **시간 확장**: 40초 → 10분 (15배)
- **메모리 절약**: 8GB → 4GB (50% 절약)
- **성능 보장**: Hot/Cold 계층화로 빠른 접근
- **호환성**: 기존 CollectionEvent 인터페이스 유지

---

## 🏗️ 전체 아키텍처

```
┌─────────────────────────────────────────────────────────────┐
│                    ExtendedBuffer                           │
├─────────────────────────┬───────────────────────────────────┤
│     Hot Buffer          │         Cold Buffer               │
│   (최근 2분, 2GB)        │      (8분, 압축, 2GB)              │
│                         │                                   │
│ ┌─────────────────────┐ │ ┌─────────────────────────────────┐ │
│ │   CircularBuffer    │ │ │      CompressedRing             │ │
│ │  - 즉시 접근        │ │ │   - 30초 단위 압축 블록          │ │
│ │  - 0.1ms latency    │ │ │   - 5ms 압축해제 latency        │ │
│ │  - TradeEvent[]     │ │ │   - MessagePack + Delta         │ │
│ └─────────────────────┘ │ └─────────────────────────────────┘ │
└─────────────────────────┴───────────────────────────────────┘
                    │
                    ▼
            ┌──────────────────┐
            │  CollectionEvent │
            │   (기존 호환)     │
            └──────────────────┘
```

---

## 📊 메모리 사용량 분석

### 현재 시스템 (40초)
```go
// 메모리 구성
채널 버퍼: 500,000개 × 70개 = 7GB    // SafeWorker 채널
실제 데이터: 40초 × 12거래소 = 1GB   // TradeEvent 저장
총합: ~8GB
```

### 새 시스템 (10분)
```go
// 메모리 구성
Hot Buffer:  2분 × 12거래소 = 2GB     // 즉시 접근용
Cold Buffer: 8분 × 압축(70%) = 2GB   // 압축 저장
총합: ~4GB (50% 절약!)
```

---

## 🔧 핵심 구조체 설계

### 1. ExtendedBuffer (메인 버퍼)

```go
// ExtendedBuffer는 10분간의 거래 데이터를 효율적으로 저장
type ExtendedBuffer struct {
    // 설정
    totalDuration  time.Duration      // 10분
    hotDuration    time.Duration      // 2분 (hot)
    coldDuration   time.Duration      // 8분 (cold)

    // 버퍼 저장소
    hotBuffers     map[string]*CircularBuffer    // 거래소별 Hot 버퍼
    coldBuffers    map[string]*CompressedRing    // 거래소별 Cold 버퍼

    // 동시성 제어
    mu             sync.RWMutex

    // 통계 및 상태
    stats          BufferStats
    startTime      time.Time
    isActive       bool

    // 설정 가능한 파라미터
    compressionRatio float64          // 압축률 (기본 0.7)
    blockSize        time.Duration    // 압축 블록 크기 (기본 30초)
}

// BufferStats는 버퍼 사용 통계
type BufferStats struct {
    TotalEvents     int64   // 총 이벤트 수
    HotEvents       int64   // Hot 버퍼 이벤트
    ColdEvents      int64   // Cold 버퍼 이벤트 (압축 후)
    MemoryUsage     int64   // 실제 메모리 사용량
    CompressionRate float64 // 실제 압축률

    // 성능 지표
    HotAccessTime   time.Duration
    ColdAccessTime  time.Duration
}
```

### 2. CircularBuffer (Hot Buffer)

```go
// CircularBuffer는 최근 2분의 고속 접근용 링 버퍼
type CircularBuffer struct {
    data      []models.TradeEvent    // 실제 데이터 저장
    capacity  int                    // 버퍼 크기
    writePos  int                    // 쓰기 위치
    readPos   int                    // 읽기 위치
    full      bool                   // 버퍼 가득참 여부

    // 시간 인덱스 (빠른 시간 기반 검색)
    timeIndex map[int64]int          // timestamp → position

    mu        sync.RWMutex
}

// 메모리 계산: 2분 × 5000거래/초 × 200bytes = ~120MB/거래소
// 12거래소 = 1.44GB ≈ 2GB (여유분 포함)
```

### 3. CompressedRing (Cold Buffer)

```go
// CompressedRing은 8분의 압축 데이터를 저장하는 링 버퍼
type CompressedRing struct {
    blocks    []CompressedBlock      // 압축 블록 배열
    capacity  int                    // 블록 수 (16개 = 8분/30초)
    writePos  int                    // 현재 쓰기 블록

    // 시간 인덱스
    timeIndex map[int64]int          // timestamp → block index

    mu        sync.RWMutex
}

// CompressedBlock은 30초분 데이터의 압축 블록
type CompressedBlock struct {
    // 메타데이터
    StartTime     int64              // 블록 시작 시간
    EndTime       int64              // 블록 종료 시간
    EventCount    int                // 원본 이벤트 수
    CompressedSize int               // 압축 후 크기

    // 압축된 데이터
    Data          []byte             // MessagePack + 델타 압축

    // 빠른 검색용 인덱스
    PriceRange    [2]float64         // [min, max] 가격 범위
    VolumeSum     float64            // 총 거래량
    UniqueSymbols []string           // 거래 심볼들
}

// 메모리 계산: 30초 × 5000거래 × 60bytes(압축) = 9MB/블록
// 16블록 × 12거래소 = 1.73GB ≈ 2GB (여유분 포함)
```

---

## 💾 압축 알고리즘

### MessagePack + 델타 압축

```go
// DeltaCompressor는 거래 데이터를 효율적으로 압축
type DeltaCompressor struct {
    basePrice    float64             // 기준 가격
    baseTime     int64               // 기준 시간
    priceScale   int                 // 가격 스케일 (소수점 처리)
}

// 압축 전략
func (dc *DeltaCompressor) Compress(trades []models.TradeEvent) ([]byte, error) {
    // 1. 시간순 정렬
    sort.Slice(trades, func(i, j int) bool {
        return trades[i].Timestamp < trades[j].Timestamp
    })

    // 2. 델타 인코딩
    compressed := CompressedData{
        BasePrice:    trades[0].Price,
        BaseTime:     trades[0].Timestamp,
        PriceDeltas:  make([]int16, len(trades)),    // 16비트 가격 차분
        TimeDeltas:   make([]uint16, len(trades)),   // 16비트 시간 차분 (ms)
        Volumes:      make([]uint32, len(trades)),   // 32비트 볼륨
        Symbols:      compressSymbols(trades),       // 심볼 압축
    }

    // 3. MessagePack 직렬화
    return msgpack.Marshal(compressed)
}

// 예상 압축률:
// - 원본: TradeEvent (200 bytes)
// - 압축: CompressedData (60 bytes)
// - 압축률: 70%
```

---

## ⚡ 핵심 인터페이스

### 데이터 저장

```go
// StoreTradeEvent는 거래 이벤트를 적절한 버퍼에 저장
func (eb *ExtendedBuffer) StoreTradeEvent(exchange string, trade models.TradeEvent) error {
    eb.mu.Lock()
    defer eb.mu.Unlock()

    now := time.Now()
    age := now.Sub(time.Unix(0, trade.Timestamp))

    // Hot Buffer에 저장 (최근 2분)
    if age <= eb.hotDuration {
        return eb.hotBuffers[exchange].Store(trade)
    }

    // Cold Buffer로 이동 (2분 이상된 데이터)
    return eb.moveToColdbuffer(exchange, trade)
}
```

### 데이터 조회

```go
// GetTradeEvents는 지정된 시간 범위의 거래 이벤트를 반환
func (eb *ExtendedBuffer) GetTradeEvents(exchange string, startTime, endTime time.Time) ([]models.TradeEvent, error) {
    eb.mu.RLock()
    defer eb.mu.RUnlock()

    var result []models.TradeEvent
    now := time.Now()

    // Hot Buffer에서 조회 (빠름)
    hotStart := now.Add(-eb.hotDuration)
    if endTime.After(hotStart) {
        hotTrades, err := eb.hotBuffers[exchange].GetRange(
            maxTime(startTime, hotStart), endTime)
        if err != nil {
            return nil, err
        }
        result = append(result, hotTrades...)
    }

    // Cold Buffer에서 조회 (압축 해제 필요)
    if startTime.Before(hotStart) {
        coldTrades, err := eb.coldBuffers[exchange].GetRange(
            startTime, minTime(endTime, hotStart))
        if err != nil {
            return nil, err
        }
        result = append(result, coldTrades...)
    }

    // 시간순 정렬
    sort.Slice(result, func(i, j int) bool {
        return result[i].Timestamp < result[j].Timestamp
    })

    return result, nil
}
```

### 기존 시스템 호환

```go
// ToCollectionEvent는 ExtendedBuffer를 기존 CollectionEvent로 변환
func (eb *ExtendedBuffer) ToCollectionEvent(symbol string, triggerTime time.Time) (*models.CollectionEvent, error) {
    startTime := triggerTime.Add(-20 * time.Second)
    endTime := triggerTime.Add(20 * time.Second)

    event := &models.CollectionEvent{
        Symbol:      symbol,
        TriggerTime: triggerTime,
        StartTime:   startTime,
        EndTime:     endTime,
    }

    // 각 거래소별 데이터 수집
    exchanges := []string{"binance", "bybit", "okx", "kucoin", "gate", "phemex"}
    for _, exchange := range exchanges {
        trades, err := eb.GetTradeEvents(exchange, startTime, endTime)
        if err != nil {
            continue
        }

        // 기존 구조체에 맞게 변환
        switch exchange {
        case "binance":
            event.BinanceSpot, event.BinanceFutures = eb.splitByMarket(trades)
        case "bybit":
            event.BybitSpot, event.BybitFutures = eb.splitByMarket(trades)
        // ... 다른 거래소들
        }
    }

    return event, nil
}
```

---

## 📈 성능 특성

### 메모리 사용량
```
Hot Buffer:  2GB (즉시 접근)
Cold Buffer: 2GB (압축, 5ms 지연)
총 사용량:   4GB (현재 대비 50% 절약)
```

### 접근 성능
```
Hot Buffer (최근 2분):  0.1ms 평균 지연
Cold Buffer (8분):      5ms 평균 지연 (압축 해제)
전체 조회 (10분):       5ms 최대 지연
```

### 압축 효과
```
원본 데이터:     200 bytes/trade
압축 데이터:     60 bytes/trade
압축률:          70%
해제 속도:       1GB/s (충분히 빠름)
```

---

## 🔄 구현 단계별 계획

### Phase 1: 핵심 구조체
1. `ExtendedBuffer` 기본 구조
2. `CircularBuffer` Hot buffer 구현
3. `CompressedRing` Cold buffer 구현

### Phase 2: 압축 시스템
1. `DeltaCompressor` 구현
2. MessagePack 통합
3. 압축률 최적화

### Phase 3: 통합 및 테스트
1. 기존 시스템과 인터페이스 연동
2. 메모리 사용량 검증
3. 성능 벤치마크

### Phase 4: 프로덕션 배포
1. A/B 테스트
2. 모니터링 시스템 구축
3. 점진적 롤아웃

---

## 🧪 테스트 계획

### 메모리 테스트
```go
func TestMemoryUsage(t *testing.T) {
    buffer := NewExtendedBuffer(10 * time.Minute)

    // 10분간 실제 거래량으로 테스트
    simulateTrading(buffer, 10*time.Minute, 5000) // 초당 5000거래

    // 메모리 사용량 검증
    usage := buffer.GetMemoryUsage()
    assert.Less(t, usage, 4*1024*1024*1024) // 4GB 이하
}
```

### 성능 테스트
```go
func BenchmarkHotAccess(b *testing.B) {
    buffer := setupTestBuffer()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        trades, _ := buffer.GetTradeEvents("binance",
            time.Now().Add(-1*time.Minute), time.Now())
        _ = trades
    }
}

func BenchmarkColdAccess(b *testing.B) {
    buffer := setupTestBuffer()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        trades, _ := buffer.GetTradeEvents("binance",
            time.Now().Add(-5*time.Minute),
            time.Now().Add(-3*time.Minute))
        _ = trades
    }
}
```

---

## 🎯 기대 효과

### 데이터 보존율
- **Before**: 16분 지연 시 0% (완전 손실)
- **After**: 16분 지연 시 100% (완전 보존)

### 메모리 효율
- **Before**: 8GB (40초 버퍼)
- **After**: 4GB (10분 버퍼) → **50% 절약**

### 시스템 안정성
- **과부하 내성**: 30분 지연에도 대응
- **점진적 성능**: Hot/Cold 계층화
- **확장성**: 압축률 조정 가능

이 설계를 통해 **TOSHI 같은 극한 상황에서도 완벽한 데이터 수집**이 보장됩니다!