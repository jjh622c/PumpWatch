# 🏗️ 프로젝트 모듈화 문서

## 📋 개요

`main.go`의 모든 구조체와 핵심 기능을 기능별로 분리하여 모듈화된 패키지 구조로 리팩토링했습니다.

## 📁 디렉토리 구조

```
noticepumpcatch/
├── main.go                           # 메인 실행 파일 (조립/실행 로직만)
├── internal/                         # 내부 패키지들
│   ├── memory/                       # 메모리 관리
│   │   └── memory.go                 # 메모리 관리자, 오더북/시그널 저장
│   ├── websocket/                    # WebSocket 연결 관리
│   │   └── binance.go                # 바이낸스 WebSocket 클라이언트
│   ├── analyzer/                     # 분석 엔진
│   │   └── analyzer.go               # 펌핑 시그널 분석기
│   ├── notification/                 # 알림 시스템
│   │   └── notification.go           # 슬랙/텔레그램 알림 관리
│   ├── monitor/                      # 모니터링 시스템
│   │   └── monitor.go                # 성능/시스템 모니터링
│   ├── config/                       # 설정 관리
│   │   └── config.go                 # 설정 로드/검증
│   └── server/                       # HTTP 서버
│       └── server.go                 # 대시보드 및 API 서버
├── go.mod                           # Go 모듈 정의
├── go.sum                           # 의존성 체크섬
└── README_MODULARIZATION.md         # 이 문서
```

## 🎯 패키지별 역할

### 1. `internal/memory` - 메모리 관리
**파일**: `memory.go`  
**패키지**: `memory`

**주요 구조체**:
- `OrderbookSnapshot`: 오더북 스냅샷 데이터
- `PumpSignal`: 기본 펌핑 시그널
- `AdvancedPumpSignal`: 고도화된 펌핑 시그널
- `Manager`: 메모리 관리자

**주요 기능**:
- 실시간 오더북 데이터 저장/관리
- 펌핑 시그널 저장 및 검색
- 메모리 정리 (TTL 기반)
- 중요 시그널 디스크 저장
- 메모리 상태 통계 제공

**Public API**:
```go
func NewManager() *Manager
func (mm *Manager) AddOrderbook(snapshot *OrderbookSnapshot)
func (mm *Manager) GetRecentOrderbooks(exchange, symbol string, duration time.Duration) []*OrderbookSnapshot
func (mm *Manager) AddSignal(signal *PumpSignal)
func (mm *Manager) GetMemoryStats() map[string]interface{}
func (mm *Manager) GetRecentSignals(limit int) []*AdvancedPumpSignal
```

### 2. `internal/websocket` - WebSocket 연결 관리
**파일**: `binance.go`  
**패키지**: `websocket`

**주요 구조체**:
- `OrderbookData`: WebSocket 데이터 구조
- `BinanceWebSocket`: 바이낸스 WebSocket 클라이언트

**주요 기능**:
- 멀티스트림 WebSocket 연결
- 워커 풀 기반 데이터 처리
- 자동 재연결 및 에러 처리
- 스트림 그룹 관리 (20개씩 묶음)
- 실시간 오더북 데이터 수신

**Public API**:
```go
func NewBinanceWebSocket(symbols []string, mm *memory.Manager) *BinanceWebSocket
func (bws *BinanceWebSocket) Connect(ctx context.Context) error
func (bws *BinanceWebSocket) Close() error
func (bws *BinanceWebSocket) GetWorkerPoolStats() map[string]interface{}
func (bws *BinanceWebSocket) GetSymbols() []string
```

### 3. `internal/analyzer` - 분석 엔진
**파일**: `analyzer.go`  
**패키지**: `analyzer`

**주요 구조체**:
- `UltraFastAnalyzer`: 초고속 분석기

**주요 기능**:
- 실시간 오더북 분석
- 펌핑 시그널 감지
- 멀티지표 점수 계산
- 액션 권장 (매수/대기)

**Public API**:
```go
func NewUltraFastAnalyzer(mm *memory.Manager) *UltraFastAnalyzer
func (ufa *UltraFastAnalyzer) AnalyzeOrderbook(snapshot *memory.OrderbookSnapshot) *memory.PumpSignal
```

### 4. `internal/notification` - 알림 시스템
**파일**: `notification.go`  
**패키지**: `notification`

**주요 구조체**:
- `Manager`: 알림 관리자
- `Notification`: 알림 메시지

**주요 기능**:
- 슬랙 Webhook 알림
- 텔레그램 Bot 알림
- 레이트 리밋 관리
- 알림 레벨별 색상 구분
- 펌핑 시그널 자동 알림

**Public API**:
```go
func NewManager(slackWebhook, telegramToken, telegramChatID string) *Manager
func (nm *Manager) SendNotification(notification *Notification) error
func (nm *Manager) SendPumpSignal(signal *memory.AdvancedPumpSignal) error
func (nm *Manager) SendSystemAlert(level, title, message string, data map[string]interface{}) error
func (nm *Manager) SendErrorAlert(err error, context string) error
func (nm *Manager) SendPerformanceAlert(stats map[string]interface{}) error
```

### 5. `internal/monitor` - 모니터링 시스템
**파일**: `monitor.go`  
**패키지**: `monitor`

**주요 구조체**:
- `PerformanceMonitor`: 성능 모니터링
- `SystemMonitor`: 시스템 모니터링

**주요 기능**:
- 실시간 성능 지표 수집
- 시스템 건강 상태 모니터링
- 에러/경고 추적
- 자동 재시작 로직
- 처리량 및 지연 모니터링

**Public API**:
```go
func NewPerformanceMonitor() *PerformanceMonitor
func (pm *PerformanceMonitor) RecordProcessingTime(duration time.Duration)
func (pm *PerformanceMonitor) RecordOverflow()
func (pm *PerformanceMonitor) GetStats() map[string]interface{}

func NewSystemMonitor() *SystemMonitor
func (sm *SystemMonitor) RecordError(err error)
func (sm *SystemMonitor) RecordWarning(msg string)
func (sm *SystemMonitor) RecordRestart()
func (sm *SystemMonitor) GetHealthStatus() map[string]interface{}
func (sm *SystemMonitor) AutoRestart() bool
```

### 6. `internal/config` - 설정 관리
**파일**: `config.go`  
**패키지**: `config`

**주요 구조체**:
- `Config`: 전체 설정 구조체

**주요 기능**:
- JSON 설정 파일 로드
- 환경변수 기반 설정
- 설정 유효성 검사
- 기본값 제공
- 설정 저장/로드

**Public API**:
```go
func LoadConfig(configPath string) (*Config, error)
func (c *Config) SaveConfig(configPath string) error
func (c *Config) GetSymbols() []string
func (c *Config) IsNotificationEnabled() bool
func (c *Config) IsTradingEnabled() bool
func (c *Config) IsBacktestEnabled() bool
```

### 7. `internal/server` - HTTP 서버
**파일**: `server.go`  
**패키지**: `server`

**주요 구조체**:
- `Server`: HTTP 서버

**주요 기능**:
- 실시간 대시보드 제공
- REST API 엔드포인트
- 시스템 상태 조회
- 시그널 조회 API
- 오더북 데이터 API
- 시스템 제어 API (재시작/정지)

**Public API**:
```go
func NewServer(port int, mm *memory.Manager, ws *websocket.BinanceWebSocket, 
    nm *notification.Manager, pm *monitor.PerformanceMonitor, sm *monitor.SystemMonitor) *Server
func (s *Server) Start() error
```

## 🔄 모듈화 이점

### 1. **코드 구조 개선**
- 단일 책임 원칙 적용
- 기능별 명확한 분리
- 코드 가독성 향상
- 유지보수성 개선

### 2. **재사용성 증대**
- 각 패키지를 독립적으로 사용 가능
- 다른 프로젝트에서 패키지 재사용
- 테스트 용이성 향상

### 3. **확장성 향상**
- 새로운 기능 추가 시 해당 패키지만 수정
- 다른 거래소 지원 시 websocket 패키지 확장
- 새로운 분석 알고리즘 추가 시 analyzer 패키지 확장

### 4. **의존성 관리**
- 명확한 import 경로
- 순환 의존성 방지
- 패키지 간 결합도 감소

## 🚀 사용 방법

### 1. **기본 실행**
```bash
go run main.go
```

### 2. **개별 패키지 테스트**
```bash
# 메모리 패키지 테스트
go test ./internal/memory

# WebSocket 패키지 테스트
go test ./internal/websocket

# 분석기 패키지 테스트
go test ./internal/analyzer
```

### 3. **설정 파일 사용**
```bash
# 설정 파일과 함께 실행
go run main.go -config config.json
```

## 📊 모듈화 전후 비교

### **모듈화 전**
- `main.go`: 2,907줄 (모든 기능 포함)
- 단일 파일에 모든 로직 집중
- 유지보수 어려움
- 테스트 작성 어려움

### **모듈화 후**
- `main.go`: 136줄 (조립/실행 로직만)
- 7개 패키지로 기능 분리
- 각 패키지별 독립적 개발/테스트
- 명확한 책임 분리

## 🔧 개발 가이드

### 1. **새로운 기능 추가**
1. 해당 기능에 맞는 패키지 선택
2. 패키지 내 새로운 구조체/함수 추가
3. 필요한 경우 다른 패키지와의 인터페이스 정의
4. `main.go`에서 새로운 기능 조립

### 2. **새로운 패키지 추가**
1. `internal/` 디렉토리 내 새 패키지 생성
2. 패키지별 `package` 선언
3. Public API 정의 (대문자로 시작하는 함수/구조체)
4. `main.go`에서 import 및 사용

### 3. **의존성 관리**
- 패키지 간 의존성은 최소화
- 인터페이스를 통한 느슨한 결합
- 순환 의존성 방지

## 🧪 테스트 전략

### 1. **단위 테스트**
```bash
# 모든 패키지 테스트
go test ./internal/...

# 특정 패키지 테스트
go test ./internal/memory
```

### 2. **통합 테스트**
```bash
# 전체 시스템 테스트
go test -tags=integration
```

### 3. **성능 테스트**
```bash
# 벤치마크 테스트
go test -bench=. ./internal/...
```

## 📈 성능 최적화

### 1. **메모리 최적화**
- TTL 기반 자동 정리
- 오브젝트 풀 사용
- 불필요한 데이터 즉시 해제

### 2. **CPU 최적화**
- 워커 풀 패턴
- 고루틴 효율적 사용
- 비동기 처리 최적화

### 3. **네트워크 최적화**
- WebSocket 연결 풀
- 재연결 로직 최적화
- 데이터 압축 사용

## 🔒 보안 고려사항

### 1. **API 키 관리**
- 환경변수 사용
- 설정 파일 암호화
- 키 로테이션 지원

### 2. **데이터 보호**
- 민감한 데이터 암호화
- 로그에서 민감 정보 제거
- 접근 권한 제한

### 3. **네트워크 보안**
- HTTPS 사용
- CORS 설정
- Rate Limiting 적용

## 📝 향후 개선 계획

### 1. **추가 패키지**
- `internal/trading`: 자동매매 기능
- `internal/backtest`: 백테스팅 엔진
- `internal/database`: 데이터베이스 관리

### 2. **기능 확장**
- 다중 거래소 지원
- 고급 분석 알고리즘
- 머신러닝 통합

### 3. **운영 도구**
- 모니터링 대시보드
- 로그 분석 도구
- 성능 프로파일링

---

**모듈화 완료일**: 2024년 1월  
**담당자**: AI Assistant  
**버전**: 1.0.0 