# CLAUDE.md

이 파일은 Claude Code(claude.ai/code)가 이 저장소에서 작업할 때 지침을 제공합니다.

## 프로젝트 개요

**PumpWatch v2.0**은 **업비트 KRW 신규 상장공고를 감지하여 해외 5개 거래소에서 ±20초 구간 실시간 체결데이터를 수집하는 고성능 분산 시스템**입니다.

### 현재 시스템 아키텍처

**QuestDB 기반 이중 저장 시스템:**

```
Upbit Monitor → Event Trigger → Data Collection → Dual Storage
  (5초 폴링)     (상장 감지)      (±20초 수집)     (QuestDB + JSON)
```

**핵심 목적**: 업비트 상장 펌핑 체결 데이터 분석
**핵심 전략**: 실시간 QuestDB 저장 + 상장 이벤트별 JSON 아카이브

### 현재 데이터 수집 흐름

1. **업비트 모니터링**: 5초 간격 API 폴링으로 KRW 신규 상장공고 감지
2. **트리거 활성화**: 상장공고 감지 시점 기준 -20초부터 +20초까지 정확한 구간 수집
3. **이중 저장**:
   - **QuestDB**: 모든 실시간 거래 데이터 연속 저장 (포트 9000/9009)
   - **JSON 파일**: 상장 이벤트별 ±20초 구간만 아카이브
4. **심볼 필터링**: 상장된 특정 심볼만 정확히 필터링하여 노이즈 제거
5. **펌핑 분석**: 저장된 데이터에서 급등 구간 탐지 및 분석

## 개발 명령어

### 빌드 및 실행
```bash
# 시스템 빌드
go build -o pumpwatch main.go

# 심볼 초기화 (최초 실행 시 또는 업데이트 시)
./pumpwatch --init-symbols

# QuestDB 시작 (필수!)
./questdb/bin/questdb.sh start

# 🚀 실전 권장: 30분 자동 하드리셋으로 연결 안정성 보장
./restart_wrapper.sh

# 일반 실행
./pumpwatch

# 프로덕션 빌드 (최적화)
go build -ldflags="-s -w" -o pumpwatch main.go
```

### 테스트 및 검증
```bash
# 가짜 상장 테스트 (SOMI 예제)
./tests/cmd/test-fake-listing/test-fake-listing SOMI

# 실시간 데이터 모니터링
./tests/cmd/monitor-realtime/monitor-realtime

# 시스템 통합 테스트
go test ./internal/websocket -v -timeout=60s
```

### 데이터 분석
```bash
# QuestDB 웹 콘솔 접근
http://localhost:9000

# 상장 이벤트 데이터 확인
ls -la data/SYMBOL_*/raw/      # 거래소별 원시 데이터
ls -la data/SYMBOL_*/refined/  # 펌핑 분석 결과

# 실시간 QuestDB 쿼리 예제
SELECT * FROM trades WHERE symbol LIKE '%SOMI%' ORDER BY timestamp DESC LIMIT 10;
```

## 검증된 시스템 성능 (2025-09-25)

### 실제 운영 검증 완료
- **총 워커**: 56개 (5개 거래소 × spot/futures)
- **모니터링 심볼**: 5,712개
- **실시간 처리**: 초당 수백~수천 건 거래 처리
- **데이터 정확도**: 100% (±20초 범위 완벽 준수)
- **메모리 사용**: ~8GB (32GB 환경 권장)

### 거래소별 연결 상태
- ✅ **바이낸스**: spot 3워커 + futures 4워커 = 7워커 정상
- ✅ **바이비트**: spot 8워커 + futures 7워커 = 15워커 정상
- ✅ **OKX**: spot 2워커 + futures 2워커 = 4워커 정상
- ✅ **쿠코인**: spot 10워커 + futures 5워커 = 15워커 정상
- ✅ **게이트**: spot 10워커 + futures 5워커 = 15워커 정상

### 실제 테스트 결과 (SOMI 가짜 상장)
- **QuestDB 저장**: 1,963건 실시간 데이터 저장 확인
- **JSON 아카이브**: 182건 ±20초 구간 데이터 저장
- **심볼 필터링**: 100% 정확한 SOMI 관련 거래만 수집
- **시간 정확도**: 18:21:35 ~ 18:22:15 (40초) 완벽 준수

## 현재 구성 설정

### config/config.yaml
```yaml
upbit:
  poll_interval: 5s

questdb:
  host: "localhost:9000"
  line_host: "localhost:9009"

exchanges:
  binance:
    max_symbols_per_connection: 100
  bybit:
    max_symbols_per_connection: 50
  okx:
    max_symbols_per_connection: 100
  kucoin:
    max_symbols_per_connection: 100
  gate:
    max_symbols_per_connection: 100
```

## 데이터 구조

### QuestDB 테이블 구조
```sql
CREATE TABLE trades (
    timestamp    TIMESTAMP,
    exchange     SYMBOL,
    market_type  SYMBOL,
    symbol       SYMBOL,
    trade_id     STRING,
    price        DOUBLE,
    quantity     DOUBLE,
    side         SYMBOL
);
```

### 상장 이벤트 JSON 구조
```
data/SYMBOL_TIMESTAMP/
├── raw/                    # 거래소별 원시 데이터
│   ├── binance/
│   │   ├── spot.json       # ±20초 spot 거래
│   │   └── futures.json    # ±20초 futures 거래
│   ├── bybit/
│   ├── okx/
│   ├── kucoin/
│   └── gate/
└── refined/                # 펌핑 분석 결과
    └── pump_analysis.json  # 펌핑 구간 탐지 결과
```

## 시스템 특성

### 핵심 설계 원칙
1. **검증된 안정성**: 모든 거래소 실시간 데이터 수집 완전 검증 완료
2. **이중 저장 시스템**: QuestDB(실시간) + JSON(아카이브) 동시 운영
3. **정밀한 타이밍**: 상장 감지 시점 기준 정확한 ±20초 데이터 수집
4. **병렬 처리**: 56개 워커로 5,712개 심볼 동시 모니터링
5. **연결 안정성**: 30분 자동 하드리셋으로 WebSocket 연결 품질 보장

### 심볼 필터링 시스템
거래소별 심볼 형식을 완벽 지원:
- **바이낸스**: SOMI → SOMIUSDT
- **바이비트**: SOMI → SOMIUSDT
- **OKX**: SOMI → SOMI-USDT
- **쿠코인**: SOMI → SOMI-USDT
- **게이트**: SOMI → SOMI_USDT

### WebSocket 연결 관리
- **자동 재연결**: 연결 실패 시 지수 백오프로 재연결
- **에러 복구**: 3분 쿨다운, 3회 재시도 지능형 복구
- **연결 상태 감시**: ping/pong 기반 헬스체크
- **Policy Violation 회피**: 심볼 분할로 구독 메시지 크기 제한

## 30분 하드리셋 시스템

### 목적
바이낸스 등 일부 거래소의 장시간 연결 시 WebSocket 강제 종료 문제 해결

### 실전 운영 방법
```bash
# 🚀 실전 권장 방법
./restart_wrapper.sh

# 30분마다 자동 실행되는 과정:
# 1. 심볼 업데이트 (--init-symbols)
# 2. PumpWatch 실행 (30분)
# 3. Graceful Shutdown (SIGTERM)
# 4. 강제 종료 fallback (필요시)
# 5. 3초 대기 후 재시작
```

## 모니터링 및 디버깅

### 실시간 헬스체크
```
💗 Health: BN(3+4✅), BY(8+7✅), OKX(2+2✅), KC(10+5✅), PH(0+0❌), GT(10+5✅) | 56/56 workers, msgs
```
- **거래소별 상태**: BN(바이낸스), BY(바이비트), OKX, KC(쿠코인), GT(게이트)
- **워커 수 표시**: (spot_workers+futures_workers)
- **연결 상태**: ✅(정상), ❌(비활성)

### 로그 확인
```bash
# 메인 로그 확인
tail -f logs/pumpwatch_main_$(date +%Y%m%d).log

# QuestDB 웹 콘솔
http://localhost:9000
```

## 시스템 요구사항

### 하드웨어 사양
- **CPU**: 4코어 이상
- **메모리**: 32GB 권장 (최소 16GB)
- **저장소**: SSD 권장
- **네트워크**: 안정적 연결

### 소프트웨어 요구사항
- **Go**: 1.21+
- **QuestDB**: 7.4.2+ (자동 설치됨)
- **OS**: Linux/macOS 권장

## 주요 업데이트

### v2.0.0 (2025-09-25) - 최종 검증 완료
- ✅ QuestDB 기반 고성능 시계열 저장
- ✅ 56개 워커 병렬 처리 아키텍처
- ✅ ±20초 정밀 데이터 수집 완전 검증
- ✅ 이중 저장 시스템 (실시간 + 아카이브)
- ✅ 심볼 필터링 100% 정확도
- ✅ 30분 자동 하드리셋으로 연결 안정성 보장

## 문제 해결

### 일반적인 문제들

**1. QuestDB 연결 실패**
```bash
# QuestDB가 실행되지 않은 경우
./questdb/bin/questdb.sh start
```

**2. WebSocket 연결 실패**
```bash
# 30분 하드리셋이 해결책
./restart_wrapper.sh
```

**3. 심볼 업데이트**
```bash
# 새로운 코인 상장 시 심볼 목록 갱신
./pumpwatch --init-symbols
```

---

**💡 핵심 메시지**: PumpWatch v2.0은 실전 검증을 거친 안정적인 상장 펌핑 분석 시스템으로, 모든 주요 거래소에서 완벽한 데이터 수집과 정확한 ±20초 타이밍을 보장합니다.