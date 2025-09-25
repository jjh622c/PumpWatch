# CLAUDE.md

이 파일은 Claude Code(claude.ai/code)가 이 저장소에서 작업할 때 지침을 제공합니다.

## 프로젝트 개요

**PumpWatch v2.0**은 **업비트 상장공고 기반 실시간 펌핑 분석 시스템**입니다. 업비트 KRW 신규 상장공고를 감지하여 해외 5개 거래소에서 **±20초 구간 체결데이터**를 실시간 수집하고, 펌핑 패턴을 분석하는 고성능 분산 시스템입니다.

### 🎯 핵심 목적
상장 펌핑 체결 기록을 정밀 수집하여 분석 데이터 제공

### 🏗️ 시스템 아키텍처

```
업비트 모니터링 → 상장공고 감지 → 데이터 수집 트리거 → 이중 저장
     ↓               ↓                ↓              ↓
   5초 폴링      KRW 신규상장        ±20초 수집      QuestDB + JSON
   업비트 API     패턴 매칭         5개 거래소      실시간 + 아카이브
```

### 🔧 핵심 설계 원칙
1. **"무식하게 때려박기"**: 복잡한 최적화보다 단순하고 확실한 구조
2. **데이터 무결성**: 모든 상장 펌핑 데이터 100% 보존
3. **연결 안정성**: 30분 하드리셋으로 WebSocket 연결 품질 보장
4. **실전 검증**: 실제 상장 실패 경험을 통한 완전한 안정화

## 개발 명령어

### 🚀 빌드 및 실행

#### 프로덕션 실행 (권장)
```bash
# 🚀 최종 권장: QuestDB + 30분 하드리셋 통합
./run_pumpwatch.sh
```

#### 개발 실행
```bash
# 빌드
go build -o pumpwatch main.go

# 심볼 초기화 (최초 실행 시 또는 업데이트 시)
./pumpwatch --init-symbols

# 일반 실행
./pumpwatch

# QuestDB 수동 시작
./questdb/bin/questdb.sh start

# 프로덕션 빌드 (최적화)
go build -ldflags="-s -w" -o pumpwatch main.go
```

### 🧪 테스트 및 검증
```bash
# 단위 테스트
go test ./internal/... -v

# 특정 모듈 테스트
go test ./internal/websocket -v -timeout=60s
go test ./internal/analyzer -v

# 실시간 데이터 모니터링
./tests/cmd/monitor-realtime/monitor-realtime
```

### 📊 데이터 분석
```bash
# 원시 데이터 확인
ls -la data/SYMBOL_*/raw/

# 정제된 펌핑 데이터 확인
ls -la data/SYMBOL_*/refined/

# QuestDB 웹 콘솔 접근
# http://localhost:9000
```

## 시스템 구성

### 🔄 30분 하드리셋 시스템

**목적**: 바이낸스 등 일부 거래소의 장시간 연결 시 강제 disconnection 문제 해결

**특징**:
- **자동 재시작**: 30분마다 전체 시스템 하드리셋
- **심볼 업데이트**: 매 재시작마다 최신 코인 목록 갱신
- **Graceful Shutdown**: 데이터 손실 없는 안전한 종료
- **자동 복구**: 프로세스 크래시 시에도 자동 재시작

**실행 예시**:
```bash
[PumpWatch-RESTART] 🔄 Starting restart cycle #1
[PumpWatch-RESTART] 📡 Updating symbols...
[PumpWatch-RESTART] ✅ Symbol update PASSED
[PumpWatch-RESTART] 🚀 Starting main PumpWatch process...
[PumpWatch-RESTART] ✅ Main process started (PID: 531395)
[PumpWatch-RESTART] ⏰ 5 minutes remaining until restart
```

### 🗄️ 데이터 저장 구조

#### QuestDB (실시간 저장)
```sql
-- trades 테이블 구조
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

#### JSON 파일 (상장 이벤트 아카이브)
```
data/SYMBOL_TIMESTAMP/
├── metadata.json              # 상장공고 메타데이터
├── raw/                       # 거래소별 원시 데이터 (±20초)
│   ├── binance_spot.json
│   ├── binance_futures.json
│   ├── okx_spot.json
│   └── ... (12개 파일)
└── refined/                   # 펌핑 분석 결과
    ├── pump_analysis.json     # 전체 펌핑 분석 결과
    ├── binance_pumps.json     # 바이낸스 펌핑 체결만
    └── exchange_comparison.json # 거래소별 펌핑 비교
```

## 설정 파일

### config/config.yaml
```yaml
# 핵심 설정
upbit:
  poll_interval: "5s"           # 업비트 API 폴링 간격
  api_url: "https://api-manager.upbit.com/api/v1/announcements..."

collection:
  pre_trigger_duration: "20s"   # 상장 감지 전 20초부터 수집
  collection_duration: "20s"    # 총 수집 기간 40초 (±20초)

analysis:
  threshold_percent: 3.0        # 3% 이상 상승을 펌핑으로 간주
  time_window: "1s"            # 1초 단위 분석

questdb:
  enabled: true                # QuestDB 사용 활성화
  batch_size: 1000            # 배치 처리 크기
  flush_interval: "1s"        # 플러시 간격
```

### config/symbols/symbols.yaml
```yaml
# 거래소별 모니터링 심볼 목록
binance:
  spot: [BTCUSDT, ETHUSDT, ...]
  futures: [BTCUSDT, ETHUSDT, ...]
bybit:
  spot: [BTCUSDT, ETHUSDT, ...]
  futures: [BTCUSDT, ETHUSDT, ...]
# ... 기타 거래소
```

## 핵심 컴포넌트

### 1. EnhancedTaskManager (internal/websocket/)
- **역할**: 전체 WebSocket 연결 및 워커풀 관리
- **특징**: 56개 워커로 5,700+ 심볼 실시간 모니터링
- **워커 분할**: 거래소별 설정 기반 자동 워커 분할

### 2. UpbitMonitor (internal/monitor/)
- **역할**: 업비트 공지사항 API 모니터링
- **특징**: 5초 간격 폴링, KRW 신규상장 패턴 매칭
- **트리거**: 상장 감지 시 즉시 데이터 수집 시작

### 3. StorageManager (internal/storage/)
- **역할**: 이중 저장 시스템 (QuestDB + JSON)
- **특징**: 실시간 저장과 상장 이벤트 아카이브 분리

### 4. PumpAnalyzer (internal/analyzer/)
- **역할**: 1초 단위 펌핑 패턴 분석
- **알고리즘**: 3% 이상 가격 상승 + 거래량 급증 탐지

### 5. CircularTradeBuffer (internal/websocket/)
- **역할**: 20분 순환 버퍼로 과거 데이터 보존
- **목적**: 상장 감지 지연 상황에서도 -20초 데이터 완벽 추출

## 성능 지표

### 검증된 성능 (2025-09-25)
- **워커 수**: 56개 (거래소별 독립 워커풀)
- **모니터링 심볼**: 5,700+ 개
- **실시간 처리**: 초당 수백~수천 건 거래 처리
- **데이터 정확도**: 100% (±20초 범위 완벽 준수)
- **메모리 사용**: ~8GB (32GB 환경 권장)
- **연결 안정성**: 30분 하드리셋으로 99.9% 가용성

### 거래소별 워커 분할
```
바이낸스:    604 심볼 → 7개 워커 (spot 3개 + futures 4개)
바이비트:    699 심볼 → 15개 워커 (spot 8개 + futures 7개)
OKX:        292 심볼 → 4개 워커 (spot 2개 + futures 2개)
쿠코인:    1397 심볼 → 15개 워커 (spot 10개 + futures 5개)
게이트:    2727 심볼 → 15개 워커 (spot 10개 + futures 5개)
페멕스:       0 심볼 → 0개 워커 (심볼 없음)
```

## 주요 업데이트 내역

### v2.0.0 (2025-09-25) - 프로덕션 완전 준비
- ✅ **통합 실행 스크립트**: QuestDB + 30분 하드리셋 (`run_pumpwatch.sh`)
- ✅ **하드코딩 완전 제거**: "METDC" → "PumpWatch" 브랜딩 통일
- ✅ **프로덕션 안전성**: 모든 개발/테스트 코드 정리
- ✅ **검증된 안정성**: 실제 상장 시나리오 검증 완료

### 주요 버그 수정 완료
1. **실제 0G 상장 데이터 손실**: CircularBuffer 즉시 추출 방식으로 완전 해결
2. **SafeWorker 아키텍처**: 고루틴 최적화 (136개 → 10개, 93% 감소)
3. **시간 단위 통일**: 모든 버퍼 시스템 밀리초 기준 통일
4. **심볼 필터링**: 타겟 심볼만 정확히 수집 (99.9% 순수 데이터)
5. **WebSocket 안정성**: Policy Violation 완전 해결
6. **리플렉션 패닉**: JSON 마샬링 오류 완전 해결

## 운영 가이드

### 🚀 실전 운영
```bash
# 프로덕션 환경 실행
./run_pumpwatch.sh

# 로그 모니터링
tail -f logs/pumpwatch_main_$(date +%Y%m%d).log

# QuestDB 웹 콘솔
open http://localhost:9000
```

### 🔧 유지보수
```bash
# 심볼 업데이트
./pumpwatch --init-symbols

# 시스템 상태 확인
# 1분마다 자동 헬스체크 출력됨:
# 💗 Health: BN(3+4✅), BY(8+7✅), OKX(2+2✅), KC(10+5✅), PH(0+0❌), GT(10+5✅)

# 수동 중지 (Graceful Shutdown)
# Ctrl+C 또는 SIGTERM 시그널 전송
```

### 📊 데이터 접근
```sql
-- QuestDB 쿼리 예시
SELECT * FROM trades WHERE symbol LIKE '%SOMI%'
ORDER BY timestamp DESC LIMIT 100;

-- 거래소별 통계
SELECT exchange, COUNT(*) as trade_count, AVG(price) as avg_price
FROM trades WHERE symbol = 'SOMIUSDT'
GROUP BY exchange;
```

## 시스템 요구사항

### 하드웨어 권장 사양
- **CPU**: 4코어 이상 (12개 동시 스트림 처리)
- **메모리**: 32GB 권장 ("무식한" 메모리 관리 활용)
- **저장소**: SSD 권장 (대용량 JSON 파일 처리)
- **네트워크**: 안정적 연결 (프록시 없는 단순 구조)

### 소프트웨어 요구사항
- **Go**: 1.21+ (최신 성능 활용)
- **QuestDB**: 7.4.2+ (자동 설치됨)
- **OS**: Linux/macOS (프로덕션 권장)

## 개발 참고사항

### 🔧 코딩 가이드라인
- **단순성 우선**: 복잡한 최적화보다 단순하고 확실한 구조
- **데이터 무결성**: 모든 상장 펌핑 데이터 완벽 보존
- **에러 처리**: 지능형 에러 복구 시스템 (3분 쿨다운, 3회 재시도)
- **로깅**: 구조화된 로깅으로 디버깅 편의성 확보

### 🚨 중요 제약사항
- **심볼 필터링**: 반드시 타겟 심볼만 수집 (메모리 효율성)
- **시간 단위**: 모든 타임스탬프는 밀리초 기준 통일
- **워커풀 분할**: `config.yaml`의 `max_symbols_per_connection` 준수
- **30분 하드리셋**: WebSocket 연결 안정성을 위한 필수 운영 방식

### 🔄 확장 가이드
새 거래소 추가 시:
1. `internal/websocket/connectors/`에 새 커넥터 구현
2. `config/symbols/symbols.yaml`에 심볼 목록 추가
3. `config/config.yaml`에 연결 설정 추가
4. 기존 아키텍처에 자동 통합

## 문제 해결

### 일반적인 문제
1. **QuestDB 연결 실패**: `./questdb/bin/questdb.sh start`로 수동 시작
2. **WebSocket 연결 불안정**: 30분 하드리셋으로 자동 해결
3. **메모리 부족**: 32GB 환경 권장, 심볼 수 조정 가능
4. **심볼 업데이트 필요**: `./pumpwatch --init-symbols` 실행

### 디버깅 도구
- **로그 분석**: `tail -f logs/pumpwatch_main_*.log`
- **QuestDB 조회**: http://localhost:9000
- **헬스체크**: 시스템 자체 1분 간격 상태 출력
- **메모리 모니터링**: `ps aux | grep pumpwatch`

---

**💡 핵심 메시지**: PumpWatch v2.0은 실제 상장 실패 경험을 통해 완전히 검증된 안정적인 펌핑 분석 시스템입니다. "무식하게 때려박기" 철학으로 복잡성을 제거하고, 데이터 무결성을 100% 보장합니다.