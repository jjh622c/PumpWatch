# NoticePumpCatch 모듈화 구조 문서

## 개요

NoticePumpCatch는 실시간 암호화폐 펌핑 감지 및 상장공시 모니터링 시스템입니다. 이 문서는 리팩토링된 모듈화 구조와 각 컴포넌트의 역할을 설명합니다.

## 프로젝트 구조

```
noticepumpcatch/
├── main.go                          # 메인 실행 파일 (조립 및 실행만 담당)
├── internal/                        # 내부 패키지들
│   ├── config/                      # 설정 관리
│   ├── memory/                      # 메모리 관리 (오더북, 체결, 시그널)
│   ├── websocket/                   # WebSocket 연결 관리
│   ├── signals/                     # 시그널 감지 (펌핑, 상장공시)
│   ├── triggers/                    # 트리거 관리
│   ├── storage/                     # 파일 기반 데이터 저장
│   ├── callback/                    # 외부 콜백 관리
│   ├── monitor/                     # 성능 모니터링
│   ├── api/                         # HTTP API 서버
│   ├── notification/                # 알림 시스템
│   ├── analyzer/                    # 데이터 분석
│   ├── trading/                     # 거래 로직
│   └── backtest/                    # 백테스팅
├── signals/                         # 시그널 데이터 저장소
├── orderbooks/                      # 오더북 데이터 저장소
├── trades/                          # 체결 데이터 저장소
└── snapshots/                       # 스냅샷 데이터 저장소
```

## 핵심 기능

### 1. 실시간 데이터 수집
- **WebSocket 연결**: 바이낸스 실시간 오더북(`@depth20@100ms`) 및 체결(`@trade`) 데이터 수집
- **메모리 관리**: Rolling buffer를 통한 최신 데이터 유지
- **워커 풀**: 고성능 데이터 처리

### 2. 펌핑 감지
- **복합 점수 계산**: 가격 변화, 거래량 변화, 오더북 불균형을 종합한 점수
- **실시간 모니터링**: 1초마다 모든 심볼 검사
- **임계값 기반**: 설정 가능한 감지 임계값

### 3. 상장공시 감지
- **외부 콜백 시스템**: 외부 모듈에서 상장공시 신호 전달
- **±60초 데이터 저장**: 트리거 발생 시점 전후 60초 데이터 자동 저장
- **중복 방지**: MD5 해싱을 통한 중복 저장 방지

### 4. 데이터 저장
- **구조화된 저장**: `signals/`, `orderbooks/`, `trades/`, `snapshots/` 폴더로 분류
- **날짜별 구성**: YYYY-MM-DD 형식의 하위 폴더
- **보존 정책**: 설정 가능한 데이터 보존 기간

## 모듈별 상세 설명

### internal/config/
**역할**: 애플리케이션 설정 관리
**주요 파일**:
- `config.go`: 설정 구조체 및 로딩 로직

**설정 항목**:
```json
{
  "websocket": {
    "symbols": ["BTCUSDT", "ETHUSDT"],
    "worker_count": 4,
    "buffer_size": 1000
  },
  "signals": {
    "pump_detection": {
      "enabled": true,
      "min_score": 70.0,
      "volume_threshold": 2.0
    },
    "listing": {
      "enabled": true,
      "auto_trigger": false
    }
  },
  "storage": {
    "base_dir": "./data",
    "retention_days": 30,
    "compress_data": false
  }
}
```

### internal/memory/
**역할**: 실시간 데이터 메모리 관리
**주요 구조체**:
- `Manager`: 메모리 관리자
- `OrderbookSnapshot`: 오더북 스냅샷
- `TradeData`: 체결 데이터
- `AdvancedPumpSignal`: 고급 펌핑 시그널

**주요 메서드**:
- `AddOrderbook()`: 오더북 데이터 추가 (rolling buffer)
- `AddTrade()`: 체결 데이터 추가 (rolling buffer)
- `GetTimeRangeOrderbooks()`: 시간 범위 오더북 조회
- `GetTimeRangeTrades()`: 시간 범위 체결 조회

### internal/websocket/
**역할**: WebSocket 연결 및 데이터 수집
**주요 파일**:
- `binance.go`: 바이낸스 WebSocket 클라이언트

**기능**:
- 멀티 스트림 구독 (`@depth20@100ms`, `@trade`)
- 자동 재연결 및 하트비트
- 워커 풀을 통한 데이터 처리
- 메모리 관리자로 데이터 전달

### internal/signals/
**역할**: 시그널 감지 및 처리
**주요 구조체**:
- `SignalManager`: 시그널 관리자
- `ListingSignal`: 상장공시 신호
- `ListingCallback`: 상장공시 콜백 인터페이스

**펌핑 감지 로직**:
```go
// 복합 점수 계산
score := calculatePumpScore(symbol, orderbooks, trades)
if score >= minScore {
    // 펌핑 신호 생성 및 저장
    signal := createPumpSignal(symbol, score, orderbooks, trades)
    memManager.AddSignal(signal)
    storageManager.SaveSignal(signal)
    triggerManager.TriggerPumpDetection(symbol, score, confidence, metadata)
}
```

### internal/triggers/
**역할**: 트리거 관리 및 핸들러 실행
**주요 구조체**:
- `Manager`: 트리거 관리자
- `Trigger`: 트리거 정보
- `TriggerHandler`: 트리거 핸들러 인터페이스

**트리거 타입**:
- `pump_detection`: 펌핑 감지
- `listing_announcement`: 상장공시
- `volume_spike`: 거래량 급증
- `price_spike`: 가격 급등

### internal/storage/
**역할**: 파일 기반 데이터 저장
**주요 구조체**:
- `StorageManager`: 스토리지 관리자
- `SnapshotData`: 스냅샷 데이터
- `OrderbookData`: 오더북 데이터
- `TradeData`: 체결 데이터

**저장 구조**:
```
data/
├── signals/
│   └── 2024-01-15/
│       ├── pump_BTCUSDT_20240115_143022.json
│       └── listing_ETHUSDT_20240115_143045.json
├── orderbooks/
│   └── 2024-01-15/
│       ├── BTCUSDT_orderbooks.json
│       └── ETHUSDT_orderbooks.json
├── trades/
│   └── 2024-01-15/
│       ├── BTCUSDT_trades.json
│       └── ETHUSDT_trades.json
└── snapshots/
    └── 2024-01-15/
        ├── pump_BTCUSDT_143022_snapshot.json
        └── listing_ETHUSDT_143045_snapshot.json
```

**중복 방지 메커니즘**:
```go
// MD5 해시 기반 중복 방지
hash := generateSnapshotHash(trigger)
if hashCache[hash] {
    return nil // 중복 무시
}
hashCache[hash] = true
```

### internal/callback/
**역할**: 외부 콜백 관리
**주요 구조체**:
- `CallbackManager`: 콜백 관리자

**사용 예시**:
```go
// 콜백 등록
callbackManager.RegisterListingCallback(myListingHandler)

// 상장공시 신호 트리거
callbackManager.TriggerListingAnnouncement("NEWUSDT", "binance", "manual", 95.0)
```

### internal/monitor/
**역할**: 성능 모니터링
**주요 기능**:
- 메모리 사용량 모니터링
- WebSocket 연결 상태 모니터링
- 데이터 처리 성능 측정
- 시스템 통계 수집

## 데이터 흐름

### 1. 실시간 데이터 수집
```
WebSocket → Worker Pool → Memory Manager → Storage Manager
```

### 2. 펌핑 감지
```
Memory Manager → Signal Manager → Trigger Manager → Storage Manager
```

### 3. 상장공시 처리
```
External Callback → Callback Manager → Signal Manager → Trigger Manager → Storage Manager
```

### 4. 스냅샷 저장
```
Trigger → Storage Manager → File System (JSON)
```

## 설정 예시

### 기본 설정 파일 (config.json)
```json
{
  "websocket": {
    "symbols": ["BTCUSDT", "ETHUSDT", "ADAUSDT"],
    "reconnect_interval": "5s",
    "heartbeat_interval": "30s",
    "worker_count": 4,
    "buffer_size": 1000
  },
  "memory": {
    "orderbook_retention_minutes": 60,
    "trade_retention_minutes": 60,
    "max_orderbooks_per_symbol": 1000,
    "max_trades_per_symbol": 5000,
    "cleanup_interval_minutes": 10
  },
  "signals": {
    "pump_detection": {
      "enabled": true,
      "min_score": 70.0,
      "volume_threshold": 2.0,
      "price_change_threshold": 5.0,
      "time_window_seconds": 60
    },
    "listing": {
      "enabled": true,
      "auto_trigger": false
    }
  },
  "storage": {
    "base_dir": "./data",
    "retention_days": 30,
    "compress_data": false
  },
  "triggers": {
    "pump_detection": {
      "enabled": true,
      "min_score": 70.0,
      "volume_threshold": 2.0,
      "price_change_threshold": 5.0,
      "time_window_seconds": 60
    },
    "snapshot": {
      "pre_trigger_seconds": 60,
      "post_trigger_seconds": 60,
      "max_snapshots_per_day": 100
    }
  }
}
```

## API 인터페이스

### 상장공시 콜백 인터페이스
```go
type ListingCallback interface {
    OnListingAnnouncement(signal ListingSignal)
}

type ListingSignal struct {
    Symbol     string                 `json:"symbol"`
    Exchange   string                 `json:"exchange"`
    Timestamp  time.Time              `json:"timestamp"`
    Confidence float64                `json:"confidence"`
    Source     string                 `json:"source"`
    Metadata   map[string]interface{} `json:"metadata"`
}
```

### 외부에서 상장공시 신호 트리거
```go
// Application 인스턴스를 통해
app.TriggerListingSignal("NEWUSDT", "binance", "external_api", 95.0)

// 또는 CallbackManager 직접 사용
callbackManager.TriggerListingAnnouncement("NEWUSDT", "binance", "manual", 95.0)
```

## 성능 최적화

### 1. 메모리 관리
- Rolling buffer를 통한 메모리 사용량 제한
- TTL 기반 자동 정리
- 고루틴 안전한 동시성 처리

### 2. 데이터 처리
- 워커 풀을 통한 병렬 처리
- 버퍼링을 통한 처리량 최적화
- 비동기 파일 I/O

### 3. 중복 방지
- MD5 해시 기반 중복 감지
- 메모리 캐시를 통한 빠른 검색
- 주기적 캐시 정리

## 모니터링 및 로깅

### 시스템 통계
- 메모리 사용량 (오더북, 체결, 시그널 개수)
- WebSocket 연결 상태 및 버퍼 사용량
- 트리거 발생 횟수 및 통계
- 스토리지 사용량 및 파일 개수

### 로그 레벨
- `INFO`: 일반적인 시스템 동작
- `WARN`: 경고 상황 (버퍼 가득참, 중복 데이터 등)
- `ERROR`: 오류 상황 (연결 실패, 파일 저장 실패 등)

## 확장성

### 새로운 트리거 타입 추가
1. `internal/triggers/`에 새로운 핸들러 구현
2. `TriggerType` 상수 추가
3. 설정 파일에 관련 설정 추가

### 새로운 데이터 소스 추가
1. `internal/websocket/`에 새로운 클라이언트 구현
2. `internal/memory/`에 데이터 구조 추가
3. `internal/storage/`에 저장 로직 추가

### 새로운 시그널 타입 추가
1. `internal/signals/`에 새로운 감지 로직 구현
2. 관련 설정 및 구조체 추가
3. 트리거 및 저장 로직 연결

## 결론

이 모듈화된 구조는 다음과 같은 이점을 제공합니다:

1. **유지보수성**: 각 기능이 독립적인 패키지로 분리
2. **확장성**: 새로운 기능 추가가 용이
3. **테스트 가능성**: 각 모듈을 독립적으로 테스트 가능
4. **성능**: 최적화된 데이터 처리 및 저장
5. **안정성**: 중복 방지 및 오류 처리
6. **모니터링**: 포괄적인 시스템 상태 모니터링

이 구조를 통해 실시간 암호화폐 펌핑 감지 및 상장공시 모니터링 시스템을 효율적으로 운영할 수 있습니다. 