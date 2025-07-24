# NoticePumpCatch 🚀

**HFT 수준 실시간 암호화폐 펌핑 감지 시스템**

바이낸스 WebSocket을 통해 270개 USDT 페어를 실시간 모니터링하여 급등(펌핑) 현상을 마이크로초 단위로 즉시 감지하고, 펌핑 시점 전후의 거래/오더북 데이터를 자동 수집하는 **HFT 최적화된 Go 기반 고성능 시스템**입니다.

## ✨ 주요 기능

### ⚡ **HFT 수준 실시간 펌핑 감지**
- **270개 바이낸스 USDT 페어** 동시 모니터링
- **1% 이상 급등** 시 **마이크로초 단위**로 즉시 감지
- **WebSocket → HFT 직접 연결**로 레이턴시 최소화
- **링 버퍼** 기반 lock-free 고속 데이터 처리

### 💾 자동 데이터 수집
- 펌핑 감지 시 **±5초 범위** 데이터 자동 저장
- **거래 데이터**: 가격, 거래량, 매수/매도 방향
- **오더북 데이터**: 20단계 호가창 변화
- **1등 진입 데이터** 확보로 **2등 진입** 전략 지원

### 🔧 시스템 최적화
- **메모리 기반** 고속 데이터 처리
- **2GB 메모리 임계치**로 안정적 장시간 운영
- **자동 GC 관리** 및 성능 모니터링
- **패닉 복구** 및 자동 재시작 메커니즘
- **깔끔한 코드베이스** - 불필요한 요소 제거 완료

### 📊 모니터링 & 알림
- **5분마다 시스템 상태** 자동 체크
- **WebSocket 연결 상태** 모니터링
- **메모리/고루틴 사용량** 추적
- **디스크 공간** 및 쓰기 가능 여부 확인

## 🛠️ 기술 스택

- **언어**: Go 1.21+ (Pure Go 구현)
- **WebSocket**: Gorilla WebSocket
- **HFT 최적화**: lock-free 링 버퍼, 캐시 라인 정렬
- **동시성**: Go Routines & Channels
- **캐싱**: 고성능 메모리 캐시
- **로깅**: 구조화된 로그 시스템

## 📦 설치 및 실행

### 1. 프로젝트 클론
```bash
git clone https://github.com/jjh622c/noticepumpcatch.git
cd noticepumpcatch
```

### 2. 의존성 설치
```bash
go mod download
```

### 3. 설정 파일 확인
```bash
# config.json에서 설정 조정 가능
cat config.json
```

### 4. 빌드 및 실행
```bash
# 빌드
go build -o noticepumpcatch.exe

# 실행
./noticepumpcatch.exe
```

## ⚙️ 설정

### config.json 주요 설정

```json
{
  "websocket": {
    "sync_enabled": true,
    "auto_sync_symbols": true,
    "enable_upbit_filter": true
  },
  "signals": {
    "pump_detection": {
      "enabled": true,
      "price_change_threshold": 1.0
    }
  },
  "memory": {
    "orderbook_retention_minutes": 0.5,
    "trade_retention_minutes": 1,
    "max_orderbooks_per_symbol": 50,
    "max_trades_per_symbol": 100
  }
}
```

### 주요 설정값

| 항목 | 설명 | 기본값 |
|------|------|--------|
| `price_change_threshold` | **펌핑 감지 임계치 (%)** | **1.0** |
| `orderbook_retention_minutes` | 오더북 메모리 보존 시간 | 0.5분 |
| `trade_retention_minutes` | 거래 데이터 메모리 보존 시간 | 1분 |
| `max_orderbooks_per_symbol` | 심볼당 최대 오더북 수 | 50개 |
| `max_trades_per_symbol` | 심볼당 최대 거래 수 | 100개 |

## 📁 출력 데이터

### 펌핑 감지 시 저장되는 파일

```
signals/
├── pump_20240722_140456_btcusdt.json
├── pump_20240722_140512_ethusdt.json
└── ...

data/
├── trades/
│   └── binance_BTCUSDT_20240722T140456Z.json
├── orderbooks/
│   └── binance_BTCUSDT_20240722T140456Z.json
└── snapshots/
    └── snapshot_20240722_140456_btcusdt_pump_detection.json
```

### 데이터 구조 예시

**펌핑 시그널 (signals/)**
```json
{
  "symbol": "BTCUSDT",
  "price_change": 1.25,
  "confidence": 85.5,
  "detected_at": "2024-07-22T14:04:56.123Z",
  "first_price": "67500.00",
  "last_price": "68343.75",
  "trade_count": 42
}
```

**거래 데이터 (data/trades/)**
```json
{
  "metadata": {
    "exchange": "binance",
    "symbol": "BTCUSDT",
    "timestamp": "20240722T140456Z",
    "trade_count": 8
  },
  "trades": [
    {
      "timestamp": "2024-07-22T14:04:51.123+09:00",
      "price": "67500.00",
      "quantity": "0.1500",
      "side": "BUY"
    }
  ]
}
```

## 📊 시스템 모니터링

### 실시간 로그 모니터링
```bash
# 실시간 로그 확인
tail -f logs/noticepumpcatch.log

# 펌핑 감지 로그만 필터링
grep "HFT PUMP" logs/noticepumpcatch.log
```

### HFT 성능 메트릭
```
📊 [HFT STATS] 체결: 12,350건, 펌핑: 8건, 평균지연: 245μs, 심볼: 270개
⚡ [HFT PUMP] BTCUSDT: +1.25% (지연: 234μs, 체결: 42건)
```

### 시스템 상태 확인
- **WebSocket 연결**: 270개 심볼 연결 상태
- **HFT 감지기**: 마이크로초 단위 레이턴시 추적
- **메모리 사용량**: 힙 메모리 및 고루틴 수
- **데이터 수집**: 초당 오더북/거래 처리 건수

## 🚨 경고 및 알림

### 자동 감지되는 이상 상황
- **메모리 사용량 과다**: 2GB 초과 시 강제 GC
- **WebSocket 연결 끊김**: 자동 재연결 권장
- **HFT 지연 증가**: 1ms 초과 시 성능 경고
- **데이터 수집 중단**: 5분간 데이터 없음 시 알림
- **디스크 공간 부족**: 파일 쓰기 실패 시 알림

### 로그 레벨별 메시지
- `⚡ [HFT PUMP]`: **펌핑 감지** (핵심 시그널)
- `📊 [HFT STATS]`: HFT 성능 통계
- `🔍 [HEALTH]`: 시스템 상태 체크
- `⚠️ [ALERT]`: 경고 상황
- `❌ [ERROR]`: 오류 발생

## 🔧 트러블슈팅

### 자주 발생하는 문제

**1. 메모리 사용량 과다**
```bash
# 메모리 설정 조정
# config.json에서 retention_minutes 값 감소
```

**2. WebSocket 연결 불안정**
```bash
# 네트워크 상태 확인
ping api.binance.com

# 프로그램 재시작
./noticepumpcatch.exe
```

**3. HFT 지연시간 증가**
```bash
# 시스템 리소스 확인
top -p $(pgrep noticepumpcatch)

# CPU 친화성 설정 (Linux)
taskset -c 0-3 ./noticepumpcatch.exe
```

## 📈 성능 지표

### HFT 처리 성능
- **동시 WebSocket 연결**: 270개 심볼
- **초당 데이터 처리**: ~1,000건 오더북 + ~500건 거래
- **펌핑 감지 지연시간**: **평균 200-500μs**
- **메모리 사용량**: 평균 100-200MB (최대 2GB)

### 안정성
- **99.9% 업타임**: 자동 복구 메커니즘
- **패닉 복구**: 시스템 오류 시 자동 재시작
- **메모리 누수 방지**: 주기적 정리 및 GC
- **깔끔한 코드베이스**: 불필요한 복잡성 제거

## 🎯 거래 시스템 통합

이 시스템은 **다른 거래 프로그램과의 통합**을 위해 최적화되어 있습니다:

### 통합 방법
1. **HFT 감지기 직접 연결**: `internal/hft` 패키지 활용
2. **시그널 파일 모니터링**: `signals/` 디렉토리 실시간 감시
3. **WebSocket 데이터 공유**: 메모리 기반 데이터 접근
4. **REST API**: HTTP 엔드포인트로 상태 조회

### 핵심 장점
- ✅ **깔끔한 코드베이스**: 불필요한 요소 제거 완료
- ✅ **모듈화된 구조**: 각 컴포넌트 독립적 사용 가능
- ✅ **고성능 최적화**: HFT 수준 레이턴시 보장
- ✅ **안정적 운영**: 장시간 무인 운영 가능

## 🤝 기여하기

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📄 라이선스

이 프로젝트는 MIT 라이선스 하에 배포됩니다. 자세한 내용은 [LICENSE](LICENSE) 파일을 참조하세요.

## ⚠️ 면책 조항

이 소프트웨어는 교육 및 연구 목적으로만 제공됩니다. 실제 거래에 사용할 때는 충분한 테스트를 거쳐야 하며, 사용자의 책임 하에 이용해야 합니다. 개발자는 이 소프트웨어 사용으로 인한 어떠한 손실이나 피해에 대해서도 책임지지 않습니다.

---

**⚡ HFT 수준 실시간 펌핑 감지로 1등의 진입 데이터를 확보하고 2등으로 진입하세요!** 