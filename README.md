# NoticePumpCatch

실시간 암호화폐 펌핑 감지 및 상장공시 모니터링 시스템

## 🎯 개요

NoticePumpCatch는 바이낸스 실시간 데이터를 기반으로 암호화폐 펌핑 현상을 감지하고, 상장공시 신호를 처리하는 고성능 모니터링 시스템입니다.

## ✨ 주요 기능

### 🔥 실시간 펌핑 감지
- **WebSocket 기반**: 바이낸스 실시간 오더북(`@depth20@100ms`) 및 체결(`@trade`) 데이터 수집
- **복합 점수 계산**: 가격 변화, 거래량 변화, 오더북 불균형을 종합한 펌핑 점수
- **실시간 모니터링**: 1초마다 모든 심볼 검사
- **임계값 기반**: 설정 가능한 감지 임계값

### 📢 상장공시 감지
- **외부 콜백 시스템**: 외부 모듈에서 상장공시 신호 전달
- **±60초 데이터 저장**: 트리거 발생 시점 전후 60초 데이터 자동 저장
- **중복 방지**: MD5 해싱을 통한 중복 저장 방지

### 💾 구조화된 데이터 저장
- **분류 저장**: `signals/`, `orderbooks/`, `trades/`, `snapshots/` 폴더로 분류
- **날짜별 구성**: YYYY-MM-DD 형식의 하위 폴더
- **보존 정책**: 설정 가능한 데이터 보존 기간

### 🚀 고성능 아키텍처
- **워커 풀**: 병렬 데이터 처리
- **Rolling Buffer**: 메모리 효율적 데이터 관리
- **비동기 처리**: 고성능 I/O 처리

## 🏗️ 시스템 아키텍처

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   WebSocket     │    │   Memory        │    │   Storage       │
│   Manager       │───▶│   Manager       │───▶│   Manager       │
│                 │    │                 │    │                 │
│ • Binance WS    │    │ • Orderbooks    │    │ • Files         │
│ • Multi-stream  │    │ • Trades        │    │ • Snapshots     │
│ • Auto-reconnect│    │ • Signals       │    │ • Cleanup       │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                       │                       │
         ▼                       ▼                       ▼
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Signal        │    │   Trigger       │    │   Callback      │
│   Manager       │    │   Manager       │    │   Manager       │
│                 │    │                 │    │                 │
│ • Pump Detection│    │ • Event Handlers│    │ • External APIs │
│ • Listing Alerts│    │ • Snapshots     │    │ • Callbacks     │
│ • Score Calc    │    │ • Notifications │    │ • Validation    │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

## 📁 프로젝트 구조

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
├── data/                            # 데이터 저장소
│   ├── signals/                     # 시그널 데이터
│   ├── orderbooks/                  # 오더북 데이터
│   ├── trades/                      # 체결 데이터
│   └── snapshots/                   # 스냅샷 데이터
├── logs/                            # 로그 파일
├── config.json                      # 설정 파일
├── go.mod                           # Go 모듈 정의
├── go.sum                           # 의존성 체크섬
├── README.md                        # 이 파일
├── README_MODULARIZATION.md         # 모듈화 구조 문서
├── README_OPERATION.md              # 운영 가이드
└── Dockerfile                       # Docker 설정
```

## 🚀 빠른 시작

### 1. 시스템 요구사항

- **OS**: Linux, macOS, Windows
- **Go**: 1.19 이상
- **메모리**: 최소 4GB RAM (권장 8GB)
- **디스크**: 최소 10GB 여유 공간
- **네트워크**: 안정적인 인터넷 연결

### 2. 설치

```bash
# 저장소 클론
git clone https://github.com/your-repo/noticepumpcatch.git
cd noticepumpcatch

# 의존성 설치
go mod download

# 빌드
go build -o noticepumpcatch main.go
```

### 3. 설정 파일 생성

`config.json` 파일을 생성:

```json
{
  "websocket": {
    "symbols": ["BTCUSDT", "ETHUSDT", "ADAUSDT", "DOTUSDT"],
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
  },
  "notification": {
    "slack_webhook": "",
    "telegram_token": "",
    "telegram_chat_id": "",
    "alert_threshold": 80
  },
  "logging": {
    "level": "info",
    "output_file": "./logs/noticepumpcatch.log",
    "max_size": 100,
    "max_backups": 10
  }
}
```

### 4. 실행

```bash
# 기본 실행
./noticepumpcatch

# 설정 파일 지정
./noticepumpcatch -config config.json

# 백그라운드 실행
nohup ./noticepumpcatch > output.log 2>&1 &

# Docker 실행
docker run -d --name noticepumpcatch \
  -v $(pwd)/data:/app/data \
  -v $(pwd)/config.json:/app/config.json \
  noticepumpcatch:latest
```

## 📊 모니터링

### 실시간 통계

시스템이 실행되면 30초마다 다음과 같은 통계가 출력됩니다:

```
📊 메모리: 오더북 1500개, 체결 7500개, 시그널 25개
🔧 WebSocket: 연결=true, 오더북버퍼=45/1000, 체결버퍼=120/1000
⚡ 성능: 오버플로우 0회, 지연 2회
🚨 트리거: 총 15개, 오늘 3개
📈 시그널: 총 25개, 펌핑 18개, 평균점수 75.2
💾 스토리지: 시그널 25개, 오더북 1500개, 체결 7500개, 스냅샷 15개
📞 콜백: 상장공시 2개 등록
```

### 시스템 상태 확인

```bash
# 프로세스 확인
ps aux | grep noticepumpcatch

# 로그 확인
tail -f logs/noticepumpcatch.log

# 메모리 사용량 확인
top -p $(pgrep noticepumpcatch)
```

## 🔌 API 사용법

### 상장공시 콜백 등록

```go
package main

import (
    "noticepumpcatch/internal/signals"
    "log"
)

type MyListingHandler struct{}

func (h *MyListingHandler) OnListingAnnouncement(signal signals.ListingSignal) {
    log.Printf("상장공시 감지: %s (신뢰도: %.2f%%)", signal.Symbol, signal.Confidence)
    // 여기에 상장공시 처리 로직 추가
}

func main() {
    // 콜백 등록
    callbackManager.RegisterListingCallback(&MyListingHandler{})
}
```

### 상장공시 신호 트리거

```go
// Application 인스턴스를 통해
app.TriggerListingSignal("NEWUSDT", "binance", "external_api", 95.0)

// 또는 CallbackManager 직접 사용
callbackManager.TriggerListingAnnouncement("NEWUSDT", "binance", "manual", 95.0)
```

## 📁 데이터 구조

### 저장소 구조

```
data/
├── signals/                    # 시그널 데이터
│   └── 2024-01-15/
│       ├── pump_BTCUSDT_20240115_143022.json
│       └── listing_ETHUSDT_20240115_143045.json
├── orderbooks/                 # 오더북 데이터
│   └── 2024-01-15/
│       ├── BTCUSDT_orderbooks.json
│       └── ETHUSDT_orderbooks.json
├── trades/                     # 체결 데이터
│   └── 2024-01-15/
│       ├── BTCUSDT_trades.json
│       └── ETHUSDT_trades.json
└── snapshots/                  # 스냅샷 데이터
    └── 2024-01-15/
        ├── pump_BTCUSDT_143022_snapshot.json
        └── listing_ETHUSDT_143045_snapshot.json
```

### 데이터 정리

```bash
# 수동 정리 (30일 이상 된 데이터)
find data/ -name "*.json" -mtime +30 -delete

# 디스크 사용량 확인
du -sh data/

# 파일 개수 확인
find data/ -name "*.json" | wc -l
```

## 🔧 설정 옵션

### 주요 설정 항목

| 설정 | 설명 | 기본값 |
|------|------|--------|
| `websocket.symbols` | 모니터링할 심볼 목록 | `["BTCUSDT", "ETHUSDT"]` |
| `websocket.worker_count` | 워커 풀 크기 | `4` |
| `signals.pump_detection.min_score` | 펌핑 감지 최소 점수 | `70.0` |
| `storage.retention_days` | 데이터 보존 기간 | `30` |
| `memory.max_orderbooks_per_symbol` | 심볼당 최대 오더북 수 | `1000` |

### 성능 튜닝

```json
{
  "websocket": {
    "worker_count": 8,        // 워커 수 증가
    "buffer_size": 2000       // 버퍼 크기 증가
  },
  "memory": {
    "max_orderbooks_per_symbol": 2000,  // 오더북 저장량 증가
    "max_trades_per_symbol": 10000      // 체결 저장량 증가
  }
}
```

## 🐛 문제 해결

### 일반적인 문제

#### WebSocket 연결 실패
```
❌ WebSocket 연결 실패: dial tcp: lookup stream.binance.com: no such host
```

**해결 방법**:
- 네트워크 연결 확인
- DNS 설정 확인
- 방화벽 설정 확인

#### 메모리 부족
```
❌ 메모리 할당 실패: cannot allocate memory
```

**해결 방법**:
- 메모리 사용량 모니터링
- 설정에서 `max_orderbooks_per_symbol`, `max_trades_per_symbol` 값 조정
- 시스템 메모리 증설

#### 디스크 공간 부족
```
❌ 파일 저장 실패: no space left on device
```

**해결 방법**:
- 디스크 사용량 확인: `df -h`
- 오래된 데이터 정리
- `retention_days` 설정 조정

### 로그 분석

```bash
# 펌핑 감지 로그
grep "🚨 펌핑 감지" logs/noticepumpcatch.log

# 상장공시 로그
grep "📢 상장공시" logs/noticepumpcatch.log

# 오류 로그
grep "❌" logs/noticepumpcatch.log

# 성능 로그
grep "⚡" logs/noticepumpcatch.log
```

## 📚 추가 문서

- **[모듈화 구조 문서](README_MODULARIZATION.md)**: 상세한 모듈 구조 및 API 설명
- **[운영 가이드](README_OPERATION.md)**: 시스템 운영 및 모니터링 가이드

## 🤝 기여하기

1. Fork the Project
2. Create your Feature Branch (`git checkout -b feature/AmazingFeature`)
3. Commit your Changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the Branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

## 📄 라이선스

이 프로젝트는 MIT 라이선스 하에 배포됩니다. 자세한 내용은 `LICENSE` 파일을 참조하세요.

## ⚠️ 면책 조항

이 소프트웨어는 교육 및 연구 목적으로만 제공됩니다. 실제 거래에 사용할 경우 발생하는 손실에 대해 개발자는 책임지지 않습니다. 투자는 항상 본인의 판단과 책임 하에 이루어져야 합니다.

## 📞 지원

- **GitHub Issues**: 버그 리포트 및 기능 요청
- **Documentation**: 상세한 API 문서
- **Community**: 개발자 커뮤니티

---

**버전**: 1.0.0  
**Go 버전**: 1.19+  
**최종 업데이트**: 2024년 1월 