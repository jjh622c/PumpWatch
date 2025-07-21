# NoticePumpCatch - 리팩토링된 시스템

## 📋 개요

NoticePumpCatch는 실시간으로 바이낸스(및 기타 거래소)에서 오더북 데이터와 체결 데이터를 수집하고, 상장공시 신호나 펌핑감지 시그널 발생 시 해당 구간의 데이터를 자동으로 스냅샷으로 저장하는 시스템입니다.

## 🏗️ 시스템 아키텍처

### 디렉토리 구조
```
noticepumpcatch/
├── main.go                    # 메인 애플리케이션 진입점
├── internal/                  # 내부 패키지들
│   ├── config/               # 설정 관리
│   │   └── config.go
│   ├── memory/               # 메모리 관리 (Rolling Buffer)
│   │   └── memory.go
│   ├── websocket/            # WebSocket 클라이언트
│   │   └── binance.go
│   ├── triggers/             # 트리거 관리
│   │   ├── triggers.go
│   │   └── snapshot_handler.go
│   ├── snapshot/             # 스냅샷 저장 (향후 구현)
│   ├── monitor/              # 성능 모니터링
│   │   └── monitor.go
│   ├── notification/         # 알림 시스템
│   │   └── notification.go
│   └── server/               # HTTP 서버
│       └── server.go
├── snapshots/                # 스냅샷 저장 디렉토리
├── logs/                     # 로그 파일
└── config.json              # 설정 파일
```

## 🔧 핵심 컴포넌트

### 1. Memory Manager (`internal/memory/`)
- **역할**: Rolling Buffer 기반 메모리 관리
- **기능**:
  - 오더북 데이터 실시간 저장 (N분 보관)
  - 체결 데이터 실시간 저장 (N분 보관)
  - 시그널 데이터 관리
  - TTL 기반 자동 정리
  - 시간 범위 데이터 조회

### 2. WebSocket Client (`internal/websocket/`)
- **역할**: 실시간 데이터 수집
- **기능**:
  - 바이낸스 WebSocket 연결 관리
  - 오더북 스트림 수신 (`@depth20@100ms`)
  - 체결 스트림 수신 (`@trade`)
  - 멀티 스트림 그룹핑
  - 워커 풀 기반 데이터 처리

### 3. Trigger Manager (`internal/triggers/`)
- **역할**: 트리거 이벤트 관리
- **트리거 유형**:
  - `pump_detection`: 펌핑 감지
  - `listing_announcement`: 상장공시
  - `volume_spike`: 거래량 스파이크
  - `price_spike`: 가격 스파이크
- **기능**:
  - 트리거 발생 감지
  - 핸들러 등록/실행
  - 통계 수집

### 4. Snapshot Handler (`internal/triggers/snapshot_handler.go`)
- **역할**: 트리거 발생 시 데이터 스냅샷 저장
- **기능**:
  - 트리거 발생 시점 ±60초 데이터 수집
  - JSON 형태로 파일 저장
  - 메타데이터 포함 (트리거 유형, 발생시간, 신뢰도 등)
  - 통계 정보 계산

### 5. Config Manager (`internal/config/`)
- **역할**: 시스템 설정 관리
- **설정 항목**:
  - WebSocket 연결 설정
  - 메모리 관리 설정
  - 트리거 설정
  - 스냅샷 설정
  - 알림 설정

## 📊 데이터 구조

### OrderbookSnapshot
```go
type OrderbookSnapshot struct {
    Exchange  string      `json:"exchange"`
    Symbol    string      `json:"symbol"`
    Timestamp time.Time   `json:"timestamp"`
    Bids      [][]string  `json:"bids"`  // [["가격", "수량"], ...]
    Asks      [][]string  `json:"asks"`  // [["가격", "수량"], ...]
}
```

### TradeData
```go
type TradeData struct {
    Exchange  string    `json:"exchange"`
    Symbol    string    `json:"symbol"`
    Timestamp time.Time `json:"timestamp"`
    Price     string    `json:"price"`
    Quantity  string    `json:"quantity"`
    Side      string    `json:"side"` // "BUY" or "SELL"
    TradeID   string    `json:"trade_id"`
}
```

### Trigger
```go
type Trigger struct {
    ID          string      `json:"id"`
    Type        TriggerType `json:"type"`
    Symbol      string      `json:"symbol"`
    Timestamp   time.Time   `json:"timestamp"`
    Confidence  float64     `json:"confidence"`  // 0-100
    Score       float64     `json:"score"`       // 트리거 점수
    Description string      `json:"description"` // 트리거 설명
    Metadata    map[string]interface{} `json:"metadata"`
}
```

## ⚙️ 설정

### 기본 설정 (config.json)
```json
{
  "websocket": {
    "symbols": ["BTCUSDT", "ETHUSDT", "BNBUSDT", "ADAUSDT", "SOLUSDT"],
    "worker_count": 16,
    "buffer_size": 1000,
    "reconnect_interval": "5s",
    "heartbeat_interval": "30s"
  },
  "memory": {
    "orderbook_retention_minutes": 60,
    "trade_retention_minutes": 60,
    "max_orderbooks_per_symbol": 1000,
    "max_trades_per_symbol": 1000,
    "cleanup_interval_minutes": 5
  },
  "triggers": {
    "pump_detection": {
      "enabled": true,
      "min_score": 70.0,
      "volume_threshold": 1000000.0,
      "price_change_threshold": 5.0,
      "time_window_seconds": 300
    },
    "snapshot": {
      "pre_trigger_seconds": 60,
      "post_trigger_seconds": 60,
      "max_snapshots_per_day": 100
    }
  },
  "snapshot": {
    "output_dir": "./snapshots",
    "filename_template": "snapshot_{timestamp}_{symbol}_{trigger_type}.json",
    "compress_data": true,
    "include_metadata": true
  }
}
```

## 🚀 실행 방법

### 1. 기본 실행
```bash
go run .
```

### 2. 설정 파일 지정
```bash
go run . -config=my_config.json
```

### 3. 빌드 후 실행
```bash
go build -o noticepumpcatch
./noticepumpcatch
```

## 📈 모니터링

### 실시간 통계
- **메모리 상태**: 오더북/체결/시그널 개수
- **WebSocket 상태**: 연결 상태, 버퍼 사용량
- **성능 지표**: 처리량, 오버플로우, 지연
- **트리거 통계**: 발생 횟수, 유형별 분포

### 로그 레벨
- `INFO`: 일반적인 시스템 동작
- `WARNING`: 주의가 필요한 상황
- `ERROR`: 오류 상황
- `DEBUG`: 디버깅 정보

## 🔄 트리거 시스템

### 트리거 발생 조건

#### 1. 펌핑 감지 (Pump Detection)
- **조건**: 복합 점수 ≥ 70점
- **지표**:
  - 거래량 급증 (100% 이상)
  - 가격 변동 (5% 이상)
  - 오더북 불균형
  - 시간 윈도우: 5분

#### 2. 상장공시 (Listing Announcement)
- **조건**: 외부 신호 수신
- **신뢰도**: 0-100%
- **우선순위**: 최고 (점수 100점)

#### 3. 거래량 스파이크 (Volume Spike)
- **조건**: 거래량 변화율 ≥ 100%
- **점수 계산**: (변화율 - 100) / 9

#### 4. 가격 스파이크 (Price Spike)
- **조건**: 가격 변화율 ≥ 5%
- **점수 계산**: (변화율 - 5) / 0.45

### 스냅샷 저장

#### 파일명 형식
```
snapshot_20250721_143022_btcusdt_pump_detection.json
```

#### 저장 내용
```json
{
  "trigger": {
    "id": "trigger_1732105822123456789",
    "type": "pump_detection",
    "symbol": "BTCUSDT",
    "timestamp": "2025-07-21T14:30:22Z",
    "confidence": 85.5,
    "score": 92.3,
    "description": "펌핑 감지: BTCUSDT (점수: 92.30, 신뢰도: 85.50%)"
  },
  "metadata": {
    "snapshot_id": "snapshot_BTCUSDT_1732105822123456789",
    "created_at": "2025-07-21T14:30:25Z",
    "symbol": "BTCUSDT",
    "trigger_type": "pump_detection",
    "trigger_time": "2025-07-21T14:30:22Z",
    "pre_trigger_seconds": 60,
    "post_trigger_seconds": 60,
    "data_points": 245,
    "file_size": 156789,
    "compressed": false
  },
  "orderbooks": [...],
  "trades": [...],
  "statistics": {
    "orderbook_count": 120,
    "trade_count": 125,
    "time_span_seconds": 120.5,
    "price_range": {"min": 43250.0, "max": 43800.0},
    "volume_range": {"min": 0.001, "max": 2.5}
  }
}
```

## 🔧 개발 가이드

### 새로운 트리거 추가
1. `internal/triggers/triggers.go`에 트리거 유형 정의
2. 트리거 발생 함수 구현
3. 핸들러 등록

### 새로운 거래소 추가
1. `internal/websocket/`에 새로운 클라이언트 구현
2. WebSocket 인터페이스 준수
3. 메모리 관리자와 연동

### 성능 최적화
- **메모리**: Rolling Buffer 크기 조정
- **CPU**: 워커 풀 크기 최적화
- **네트워크**: 스트림 그룹핑 최적화

## 🐛 문제 해결

### 일반적인 문제들

#### 1. WebSocket 연결 실패
- **원인**: 네트워크 문제, 바이낸스 서버 장애
- **해결**: 자동 재연결, 로그 확인

#### 2. 메모리 사용량 과다
- **원인**: 보관 시간 설정 문제
- **해결**: `retention_minutes` 조정

#### 3. 데이터 파싱 실패
- **원인**: 바이낸스 API 변경
- **해결**: 파싱 로직 업데이트

#### 4. 스냅샷 저장 실패
- **원인**: 디스크 공간 부족, 권한 문제
- **해결**: 디렉토리 권한 확인, 공간 확보

## 📝 커밋 메시지 예시

```
feat: Add rolling buffer memory management

- Implement rolling buffer for orderbook and trade data
- Add TTL-based cleanup mechanism
- Support time-range data queries
- Add memory statistics monitoring

Closes #123
```

```
fix: Resolve WebSocket JSON parsing issue

- Fix bids/asks array parsing from []interface{} to [][]interface{}
- Add debugging logs for data structure inspection
- Improve error handling for malformed messages

Fixes #456
```

```
refactor: Modularize trigger system

- Extract trigger management to separate package
- Implement handler interface for extensibility
- Add snapshot handler for data preservation
- Support multiple trigger types

Part of #789
```

## 🔮 향후 계획

### 단기 계획
- [ ] 스냅샷 압축 기능 구현
- [ ] HTTP 대시보드 개선
- [ ] 알림 시스템 통합
- [ ] 백테스팅 모듈 추가

### 중기 계획
- [ ] 다중 거래소 지원
- [ ] 머신러닝 기반 시그널 개선
- [ ] 실시간 거래 실행
- [ ] 리스크 관리 시스템

### 장기 계획
- [ ] 분산 처리 지원
- [ ] 클라우드 배포
- [ ] API 서버 제공
- [ ] 모바일 앱 연동

## 📞 지원

문제가 발생하거나 개선 사항이 있으면 이슈를 등록해 주세요.

---

**NoticePumpCatch** - 실시간 암호화폐 시장 모니터링 시스템 