# 🚀 펌핑 분석 시스템 운영 가이드

## 📋 목차
1. [시스템 개요](#시스템-개요)
2. [설치 및 배포](#설치-및-배포)
3. [설정](#설정)
4. [운영](#운영)
5. [모니터링](#모니터링)
6. [트러블슈팅](#트러블슈팅)
7. [백업 및 복구](#백업-및-복구)

## 🎯 시스템 개요

### 주요 기능
- **실시간 펌핑 감지**: WebSocket 기반 실시간 데이터 수집 및 분석
- **멀티스트림 최적화**: 16개 워커 풀로 병렬 처리
- **자동매매 연동**: 안전한 리스크 관리와 자동 거래
- **실시간 알림**: 슬랙/텔레그램 연동
- **백테스트**: 과거 데이터 기반 전략 검증
- **웹 대시보드**: 실시간 모니터링 및 제어

### 시스템 아키텍처
```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   WebSocket     │    │   Worker Pool   │    │   Memory        │
│   (Binance)     │───▶│   (16 workers)  │───▶│   Manager       │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                │                        │
                                ▼                        ▼
                       ┌─────────────────┐    ┌─────────────────┐
                       │   Analyzer      │    │   Trading       │
                       │   (Real-time)   │    │   Manager       │
                       └─────────────────┘    └─────────────────┘
                                │                        │
                                ▼                        ▼
                       ┌─────────────────┐    ┌─────────────────┐
                       │   Notification  │    │   Web Dashboard │
                       │   (Slack/TG)    │    │   (Port 8081)   │
                       └─────────────────┘    └─────────────────┘
```

## 🚀 설치 및 배포

### 1. Docker를 이용한 배포 (권장)

```bash
# 1. 저장소 클론
git clone <repository-url>
cd noticepumpcatch

# 2. 환경 변수 설정
cp .env.example .env
# .env 파일 편집하여 API 키 등 설정

# 3. Docker Compose로 실행
docker-compose up -d

# 4. 로그 확인
docker-compose logs -f pump-analyzer
```

### 2. 로컬 설치

```bash
# 1. Go 설치 (1.21 이상)
go version

# 2. 의존성 설치
go mod download

# 3. 빌드
go build -o main .

# 4. 실행
./main
```

### 3. 시스템 요구사항

- **CPU**: 8코어 16스레드 이상 (워커 풀 최적화)
- **메모리**: 8GB 이상
- **네트워크**: 안정적인 인터넷 연결
- **디스크**: 10GB 이상 (로그 및 데이터 저장)

## ⚙️ 설정

### 환경 변수

```bash
# 알림 설정
SLACK_WEBHOOK_URL=https://hooks.slack.com/services/...
TELEGRAM_BOT_TOKEN=your_telegram_bot_token
TELEGRAM_CHAT_ID=your_chat_id

# 거래소 API
BINANCE_API_KEY=your_binance_api_key
BINANCE_SECRET_KEY=your_binance_secret_key

# 시스템 설정
LOG_LEVEL=info
MAX_WORKERS=16
MEMORY_RETENTION_MINUTES=10
```

### 설정 파일

```json
// config/settings.json
{
  "trading": {
    "enabled": false,
    "max_positions": 5,
    "max_position_size": 1000,
    "stop_loss": 5.0,
    "take_profit": 15.0
  },
  "analysis": {
    "min_volume_change": 300,
    "min_price_change": 5.0,
    "signal_threshold": 60.0
  },
  "notifications": {
    "rate_limit_seconds": 30,
    "enable_slack": true,
    "enable_telegram": true
  }
}
```

## 🎮 운영

### 1. 서비스 시작/중지

```bash
# Docker Compose
docker-compose start pump-analyzer
docker-compose stop pump-analyzer
docker-compose restart pump-analyzer

# 로컬 실행
./main
# Ctrl+C로 종료
```

### 2. 웹 대시보드 접속

```
http://localhost:8081
```

### 3. API 엔드포인트

```bash
# 시스템 상태
GET /api/status

# 심볼 리스트
GET /api/symbols

# 자동매매 활성화
POST /api/trading/enable

# 자동매매 비활성화
POST /api/trading/disable

# 백테스트 실행
POST /api/backtest/run

# 알림 테스트
POST /api/notifications/test

# 로그 조회
GET /api/logs?lines=100
```

### 4. 로그 관리

```bash
# 실시간 로그 확인
docker-compose logs -f pump-analyzer

# 로그 파일 위치
./logs/
./signals/  # 중요 시그널 저장
```

## 📊 모니터링

### 1. 시스템 상태 모니터링

```bash
# 헬스체크
curl http://localhost:8081/api/status

# 응답 예시
{
  "system": {
    "health_status": "HEALTHY",
    "uptime": "2h 30m",
    "error_count": 0,
    "warning_count": 2
  },
  "workers": {
    "active_workers": 12,
    "worker_count": 16,
    "data_channel_buffer": 45
  },
  "performance": {
    "peak_throughput": 1500,
    "average_throughput": 1200,
    "overflow_count": 0
  }
}
```

### 2. Prometheus + Grafana 모니터링

```bash
# Prometheus 접속
http://localhost:9090

# Grafana 접속
http://localhost:3000
# ID: admin, PW: admin
```

### 3. 알림 설정

#### 슬랙 설정
1. 슬랙 워크스페이스에서 앱 생성
2. Incoming Webhooks 활성화
3. Webhook URL 복사하여 환경 변수 설정

#### 텔레그램 설정
1. @BotFather에서 봇 생성
2. 봇 토큰 복사
3. 채팅방에 봇 초대
4. 채팅 ID 확인: `https://api.telegram.org/bot<TOKEN>/getUpdates`

## 🔧 트러블슈팅

### 1. 일반적인 문제

#### WebSocket 연결 실패
```bash
# 로그 확인
docker-compose logs pump-analyzer | grep "WebSocket"

# 해결 방법
- 네트워크 연결 확인
- 방화벽 설정 확인
- 바이낸스 API 상태 확인
```

#### 메모리 사용량 높음
```bash
# 메모리 사용량 확인
docker stats pump-analyzer

# 해결 방법
- 워커 수 조정 (MAX_WORKERS 환경 변수)
- 메모리 보관 시간 단축 (MEMORY_RETENTION_MINUTES)
```

#### 알림이 오지 않음
```bash
# 알림 테스트
curl -X POST http://localhost:8081/api/notifications/test

# 해결 방법
- API 키/토큰 확인
- 레이트 리밋 확인 (30초 간격)
- 네트워크 연결 확인
```

### 2. 성능 최적화

#### 처리량 향상
```bash
# 워커 수 증가 (CPU 코어 수에 맞춤)
MAX_WORKERS=32

# 버퍼 크기 증가
DATA_CHANNEL_BUFFER=2000
```

#### 메모리 최적화
```bash
# 보관 시간 단축
MEMORY_RETENTION_MINUTES=5

# 가비지 컬렉션 강제
# 시스템 재시작 필요
```

### 3. 로그 분석

```bash
# 에러 로그만 확인
docker-compose logs pump-analyzer | grep "ERROR"

# 특정 시간대 로그
docker-compose logs pump-analyzer --since="2024-01-01T10:00:00"

# 실시간 로그 필터링
docker-compose logs -f pump-analyzer | grep "펌핑"
```

## 💾 백업 및 복구

### 1. 데이터 백업

```bash
# 중요 데이터 백업
tar -czf backup-$(date +%Y%m%d).tar.gz \
  ./signals/ \
  ./config/ \
  ./data/ \
  .env

# Docker 볼륨 백업
docker run --rm -v pump-analyzer_signals:/data -v $(pwd):/backup alpine tar czf /backup/signals-backup.tar.gz -C /data .
```

### 2. 복구 절차

```bash
# 1. 서비스 중지
docker-compose stop pump-analyzer

# 2. 백업 데이터 복원
tar -xzf backup-20240101.tar.gz

# 3. 서비스 재시작
docker-compose start pump-analyzer

# 4. 상태 확인
curl http://localhost:8081/api/status
```

### 3. 자동 백업 스크립트

```bash
#!/bin/bash
# backup.sh

BACKUP_DIR="/backup"
DATE=$(date +%Y%m%d_%H%M%S)

# 데이터 백업
docker run --rm \
  -v pump-analyzer_signals:/data \
  -v $BACKUP_DIR:/backup \
  alpine tar czf /backup/signals-$DATE.tar.gz -C /data .

# 7일 이전 백업 삭제
find $BACKUP_DIR -name "signals-*.tar.gz" -mtime +7 -delete
```

## 🔒 보안

### 1. API 키 보안

```bash
# 환경 변수 파일 권한 설정
chmod 600 .env

# Docker 시크릿 사용 (프로덕션)
docker secret create binance_api_key ./binance_api_key.txt
```

### 2. 네트워크 보안

```bash
# 방화벽 설정
ufw allow 8081/tcp
ufw deny 22/tcp  # SSH 비활성화 (선택사항)

# Docker 네트워크 격리
docker network create --driver bridge --internal pump-internal
```

### 3. 로그 보안

```bash
# 민감한 정보 마스킹
sed -i 's/API_KEY=.*/API_KEY=***/g' logs/*.log

# 로그 로테이션
logrotate /etc/logrotate.d/pump-analyzer
```

## 📈 성능 튜닝

### 1. 시스템 튜닝

```bash
# 파일 디스크립터 제한 증가
echo "* soft nofile 65536" >> /etc/security/limits.conf
echo "* hard nofile 65536" >> /etc/security/limits.conf

# TCP 튜닝
echo "net.core.somaxconn = 65535" >> /etc/sysctl.conf
echo "net.ipv4.tcp_max_syn_backlog = 65535" >> /etc/sysctl.conf
sysctl -p
```

### 2. Docker 튜닝

```yaml
# docker-compose.yml
services:
  pump-analyzer:
    deploy:
      resources:
        limits:
          cpus: '4.0'
          memory: 4G
        reservations:
          cpus: '2.0'
          memory: 2G
    ulimits:
      nofile:
        soft: 65536
        hard: 65536
```

## 📞 지원

### 연락처
- **이슈 리포트**: GitHub Issues
- **문서**: README_OPERATION.md
- **로그**: `./logs/` 디렉토리

### 유용한 명령어

```bash
# 전체 시스템 상태 확인
docker-compose ps
docker stats

# 로그 실시간 모니터링
docker-compose logs -f --tail=100 pump-analyzer

# 설정 변경 후 재시작
docker-compose restart pump-analyzer

# 완전 초기화
docker-compose down -v
docker-compose up -d
```

---

**⚠️ 주의사항**: 이 시스템은 실시간 거래에 사용되므로, 충분한 테스트 후 운영 환경에 배포하시기 바랍니다. 