# NoticePumpCatch 운영 가이드

## 개요

NoticePumpCatch는 실시간 암호화폐 펌핑 감지 및 상장공시 모니터링 시스템입니다. 이 문서는 시스템 운영에 필요한 모든 정보를 제공합니다.

## 시스템 아키텍처

### 핵심 컴포넌트

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

### 데이터 흐름

1. **실시간 데이터 수집**
   - WebSocket → Worker Pool → Memory Manager
   - 오더북: `@depth20@100ms` 스트림
   - 체결: `@trade` 스트림

2. **시그널 감지**
   - Memory Manager → Signal Manager → Trigger Manager
   - 펌핑 감지: 1초마다 모든 심볼 검사
   - 상장공시: 외부 콜백을 통한 신호 수신

3. **데이터 저장**
   - Trigger → Storage Manager → File System
   - ±60초 스냅샷 자동 저장
   - 중복 방지 (MD5 해싱)

## 설치 및 설정

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

`config.json` 파일을 생성하고 다음 내용을 추가:

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

## 운영 가이드

### 1. 시스템 시작

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

### 2. 모니터링

#### 시스템 상태 확인

```bash
# 프로세스 확인
ps aux | grep noticepumpcatch

# 로그 확인
tail -f logs/noticepumpcatch.log

# 메모리 사용량 확인
top -p $(pgrep noticepumpcatch)
```

#### 실시간 통계

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

### 3. 데이터 관리

#### 저장소 구조

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

#### 데이터 정리

```bash
# 수동 정리 (30일 이상 된 데이터)
find data/ -name "*.json" -mtime +30 -delete

# 디스크 사용량 확인
du -sh data/

# 파일 개수 확인
find data/ -name "*.json" | wc -l
```

### 4. 외부 콜백 설정

#### 상장공시 콜백 등록

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

#### 상장공시 신호 트리거

```go
// Application 인스턴스를 통해
app.TriggerListingSignal("NEWUSDT", "binance", "external_api", 95.0)

// 또는 CallbackManager 직접 사용
callbackManager.TriggerListingAnnouncement("NEWUSDT", "binance", "manual", 95.0)
```

## 문제 해결

### 1. 일반적인 문제

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

### 2. 성능 최적화

#### 처리량 향상
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

#### 메모리 사용량 최적화
```json
{
  "memory": {
    "orderbook_retention_minutes": 30,  // 보존 시간 단축
    "trade_retention_minutes": 30,
    "cleanup_interval_minutes": 5       // 정리 주기 단축
  }
}
```

### 3. 로그 분석

#### 로그 레벨 설정
```json
{
  "logging": {
    "level": "debug",    // 상세 로그
    "level": "info",     // 일반 로그 (기본값)
    "level": "warn",     // 경고만
    "level": "error"     // 오류만
  }
}
```

#### 로그 패턴 분석
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

## 백업 및 복구

### 1. 데이터 백업

```bash
# 전체 데이터 백업
tar -czf backup_$(date +%Y%m%d_%H%M%S).tar.gz data/

# 설정 파일 백업
cp config.json backup_config_$(date +%Y%m%d_%H%M%S).json

# 로그 백업
tar -czf logs_backup_$(date +%Y%m%d_%H%M%S).tar.gz logs/
```

### 2. 데이터 복구

```bash
# 데이터 복구
tar -xzf backup_20240115_143022.tar.gz

# 설정 파일 복구
cp backup_config_20240115_143022.json config.json
```

### 3. 자동 백업 스크립트

```bash
#!/bin/bash
# backup.sh

BACKUP_DIR="/backup/noticepumpcatch"
DATE=$(date +%Y%m%d_%H%M%S)

# 백업 디렉토리 생성
mkdir -p $BACKUP_DIR

# 데이터 백업
tar -czf $BACKUP_DIR/data_$DATE.tar.gz data/

# 설정 백업
cp config.json $BACKUP_DIR/config_$DATE.json

# 30일 이상 된 백업 삭제
find $BACKUP_DIR -name "*.tar.gz" -mtime +30 -delete
find $BACKUP_DIR -name "config_*.json" -mtime +30 -delete

echo "백업 완료: $DATE"
```

## 보안 고려사항

### 1. 네트워크 보안
- 방화벽 설정으로 불필요한 포트 차단
- VPN 사용 권장
- API 키 보안 관리

### 2. 데이터 보안
- 민감한 데이터 암호화
- 접근 권한 제한
- 정기적인 보안 감사

### 3. 시스템 보안
- 정기적인 시스템 업데이트
- 로그 모니터링
- 백업 데이터 보안

## 모니터링 대시보드

### 1. 시스템 상태 대시보드

```bash
# 실시간 모니터링 스크립트
#!/bin/bash
# monitor.sh

while true; do
    clear
    echo "=== NoticePumpCatch 모니터링 ==="
    echo "시간: $(date)"
    echo ""
    
    # 프로세스 상태
    if pgrep -x "noticepumpcatch" > /dev/null; then
        echo "✅ 프로세스 상태: 실행 중"
    else
        echo "❌ 프로세스 상태: 중지됨"
    fi
    
    # 메모리 사용량
    MEMORY=$(ps -o rss= -p $(pgrep noticepumpcatch) 2>/dev/null)
    if [ ! -z "$MEMORY" ]; then
        echo "💾 메모리 사용량: $((MEMORY/1024)) MB"
    fi
    
    # 디스크 사용량
    DISK_USAGE=$(du -sh data/ 2>/dev/null | cut -f1)
    echo "💿 디스크 사용량: $DISK_USAGE"
    
    # 최근 로그
    echo ""
    echo "📋 최근 로그 (마지막 5줄):"
    tail -5 logs/noticepumpcatch.log
    
    sleep 10
done
```

### 2. 알림 설정

#### Slack 알림
```json
{
  "notification": {
    "slack_webhook": "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"
  }
}
```

#### Telegram 알림
```json
{
  "notification": {
    "telegram_token": "YOUR_BOT_TOKEN",
    "telegram_chat_id": "YOUR_CHAT_ID"
  }
}
```

## 성능 튜닝

### 1. 시스템 리소스 최적화

#### CPU 최적화
- 워커 풀 크기 조정
- 고루틴 수 제한
- CPU 친화성 설정

#### 메모리 최적화
- 객체 풀 사용
- 가비지 컬렉션 튜닝
- 메모리 매핑 파일 사용

#### 디스크 I/O 최적화
- SSD 사용 권장
- 파일 시스템 최적화
- 비동기 I/O 사용

### 2. 네트워크 최적화

#### WebSocket 최적화
- 연결 풀 사용
- 재연결 로직 최적화
- 데이터 압축 사용

#### 대역폭 최적화
- 불필요한 데이터 필터링
- 데이터 압축
- 배치 처리

## 결론

이 운영 가이드를 통해 NoticePumpCatch 시스템을 안정적으로 운영할 수 있습니다. 정기적인 모니터링과 백업을 통해 시스템의 안정성과 성능을 유지하시기 바랍니다.

### 지원 및 문의

- **GitHub Issues**: 버그 리포트 및 기능 요청
- **Documentation**: 상세한 API 문서
- **Community**: 개발자 커뮤니티

### 버전 정보

- **현재 버전**: 1.0.0
- **Go 버전**: 1.19+
- **최종 업데이트**: 2024년 1월 