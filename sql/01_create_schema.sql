-- PumpWatch QuestDB 스키마 생성
-- 실시간 거래 데이터 수집을 위한 고성능 시계열 테이블

-- ==============================================================================
-- 1. 거래 데이터 메인 테이블 (파티셔닝 및 성능 최적화)
-- ==============================================================================
CREATE TABLE trades (
    timestamp TIMESTAMP,          -- 거래 시각 (나노초 정밀도)
    exchange SYMBOL,             -- 거래소 (인덱스 최적화)
    market_type SYMBOL,          -- spot/futures (인덱스 최적화)
    symbol SYMBOL,               -- 거래 심볼 (인덱스 최적화)
    trade_id STRING,             -- 거래소별 고유 ID
    price DOUBLE,                -- 거래 가격
    quantity DOUBLE,             -- 거래 수량
    side SYMBOL,                 -- buy/sell
    collected_at TIMESTAMP       -- 수집 시각 (지연 분석용)
) timestamp (timestamp) PARTITION BY DAY;

-- ==============================================================================
-- 2. 상장 공고 메타데이터 테이블
-- ==============================================================================
CREATE TABLE listing_events (
    id LONG,                     -- 자동 증가 ID
    symbol SYMBOL,               -- 상장 심볼
    title STRING,                -- 공고 제목
    markets STRING,              -- 마켓 목록 (JSON 배열)
    announced_at TIMESTAMP,      -- 공고 시각
    detected_at TIMESTAMP,       -- 감지 시각
    trigger_time TIMESTAMP,      -- 트리거 시각 (분석 기준점)
    notice_url STRING,           -- 공고 URL
    is_krw_listing BOOLEAN,      -- KRW 상장 여부
    stored_at TIMESTAMP DEFAULT systimestamp()  -- 저장 시각
) timestamp (stored_at) PARTITION BY DAY;

-- ==============================================================================
-- 3. 펌핑 분석 결과 테이블 (고급 분석용)
-- ==============================================================================
CREATE TABLE pump_analysis (
    id LONG,                     -- 자동 증가 ID
    listing_id LONG,             -- 상장 이벤트 FK
    symbol SYMBOL,               -- 분석 심볼
    exchange SYMBOL,             -- 거래소
    market_type SYMBOL,          -- spot/futures

    -- 펌핑 구간 정보
    pump_start_time TIMESTAMP,   -- 펌핑 시작
    pump_end_time TIMESTAMP,     -- 펌핑 종료
    duration_ms INT,             -- 지속 시간 (밀리초)

    -- 가격 분석
    start_price DOUBLE,          -- 시작 가격
    peak_price DOUBLE,           -- 최고 가격
    end_price DOUBLE,            -- 종료 가격
    price_change_percent DOUBLE, -- 가격 변화율

    -- 거래량 분석
    total_volume DOUBLE,         -- 총 거래량
    volume_spike_ratio DOUBLE,   -- 거래량 급증 비율
    trade_count INT,             -- 거래 건수

    -- 통계
    avg_trade_size DOUBLE,       -- 평균 거래 크기
    max_trade_size DOUBLE,       -- 최대 거래 크기
    buy_sell_ratio DOUBLE,       -- 매수/매도 비율

    analyzed_at TIMESTAMP DEFAULT systimestamp()  -- 분석 시각
) timestamp (analyzed_at) PARTITION BY DAY;

-- ==============================================================================
-- 4. 시스템 성능 모니터링 테이블
-- ==============================================================================
CREATE TABLE system_metrics (
    timestamp TIMESTAMP,         -- 측정 시각
    metric_name SYMBOL,          -- 메트릭 이름
    metric_value DOUBLE,         -- 메트릭 값
    tags STRING,                 -- 추가 태그 (JSON)
    node_id STRING DEFAULT 'main' -- 노드 식별자
) timestamp (timestamp) PARTITION BY DAY;

-- ==============================================================================
-- 인덱스 생성 (쿼리 성능 최적화)
-- ==============================================================================

-- 거래 데이터 조회 최적화 (상장 분석용)
-- ALTER TABLE trades ADD INDEX idx_symbol_exchange_time (symbol, exchange, timestamp);
-- ALTER TABLE trades ADD INDEX idx_exchange_time (exchange, timestamp);
-- ALTER TABLE trades ADD INDEX idx_time_symbol (timestamp, symbol);

-- 상장 이벤트 조회 최적화
-- ALTER TABLE listing_events ADD INDEX idx_symbol_trigger (symbol, trigger_time);
-- ALTER TABLE listing_events ADD INDEX idx_trigger_time (trigger_time);

-- 펌핑 분석 조회 최적화
-- ALTER TABLE pump_analysis ADD INDEX idx_symbol_exchange (symbol, exchange);
-- ALTER TABLE pump_analysis ADD INDEX idx_listing_analysis (listing_id, analyzed_at);

-- ==============================================================================
-- TTL 설정 (30일 자동 삭제)
-- ==============================================================================
-- QuestDB는 DROP TABLE 스케줄링으로 TTL 구현
-- 별도 스크립트로 구현 예정: scripts/cleanup_old_data.sql