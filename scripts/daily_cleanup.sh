#!/usr/bin/env bash

# PumpWatch v2.0 일일 데이터 정리 스크립트
# 매일 자정(KST)에 실행하여 QuestDB 데이터를 정리합니다.
# 상장 이벤트 데이터는 JSON으로 이미 아카이브되므로 QuestDB는 안전하게 정리 가능

set -e

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 로깅 함수
log_info() {
    echo -e "${GREEN}[CLEANUP]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
}

log_warn() {
    echo -e "${YELLOW}[CLEANUP]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
}

log_error() {
    echo -e "${RED}[CLEANUP]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
}

# 스크립트 디렉토리로 이동
cd "$(dirname "$0")/.."

log_info "🧹 PumpWatch 일일 정리 시작"

# 1. QuestDB 데이터 정리
log_info "🗄️ QuestDB 데이터 정리 중..."

# QuestDB 상태 확인
if curl -s "http://localhost:9000/exec?query=SELECT%201;" >/dev/null 2>&1; then
    log_info "✅ QuestDB 연결 확인됨"

    # trades 테이블 데이터 개수 확인
    TRADE_COUNT=$(curl -s "http://localhost:9000/exec?query=SELECT%20COUNT(*)%20FROM%20trades;" 2>/dev/null | grep -o '"count":[0-9]*' | grep -o '[0-9]*' || echo "0")
    log_info "📊 현재 trades 테이블 레코드 수: ${TRADE_COUNT}"

    if [ "$TRADE_COUNT" -gt 0 ]; then
        # 테이블 전체 삭제 (TRUNCATE 대신 DROP & CREATE 사용)
        log_info "🗑️ trades 테이블 삭제 중..."
        curl -s "http://localhost:9000/exec?query=DROP%20TABLE%20IF%20EXISTS%20trades;" >/dev/null 2>&1

        # 테이블 재생성
        log_info "🔧 trades 테이블 재생성 중..."
        CREATE_QUERY="CREATE TABLE trades (timestamp TIMESTAMP, exchange SYMBOL, market_type SYMBOL, symbol SYMBOL, trade_id STRING, price DOUBLE, quantity DOUBLE, side SYMBOL) timestamp(timestamp) PARTITION BY DAY;"
        curl -s "http://localhost:9000/exec" --data-urlencode "query=${CREATE_QUERY}" >/dev/null 2>&1

        log_info "✅ trades 테이블 정리 완료"
    else
        log_info "ℹ️ trades 테이블이 비어있음 - 정리 불필요"
    fi
else
    log_warn "⚠️ QuestDB에 연결할 수 없음 - DB 정리 건너뜀"
fi

# 2. 오래된 로그 파일 정리 (7일 이상)
log_info "📋 오래된 로그 파일 정리 중..."
OLD_LOGS=$(find logs/ -name "*.log" -mtime +7 2>/dev/null | wc -l)
if [ "$OLD_LOGS" -gt 0 ]; then
    find logs/ -name "*.log" -mtime +7 -delete 2>/dev/null || true
    log_info "✅ 오래된 로그 파일 ${OLD_LOGS}개 삭제"
else
    log_info "ℹ️ 삭제할 오래된 로그 파일 없음"
fi

# 3. 임시 파일 정리
log_info "🧽 임시 파일 정리 중..."
if [ -d "/tmp/pumpwatch" ]; then
    rm -rf /tmp/pumpwatch/* 2>/dev/null || true
    log_info "✅ 임시 파일 정리 완료"
fi

# 4. 디스크 사용량 확인
log_info "💾 디스크 사용량 확인..."
DISK_USAGE=$(df -h . | tail -1 | awk '{print $5}' | sed 's/%//')
log_info "📊 현재 디스크 사용률: ${DISK_USAGE}%"

if [ "$DISK_USAGE" -gt 90 ]; then
    log_warn "⚠️ 디스크 사용률이 높습니다 (${DISK_USAGE}%)"
else
    log_info "✅ 디스크 사용률 양호"
fi

# 5. 정리 요약
log_info "📋 정리 요약:"
log_info "   - QuestDB trades 테이블: 정리됨"
log_info "   - 오래된 로그 파일: ${OLD_LOGS}개 삭제"
log_info "   - 현재 디스크 사용률: ${DISK_USAGE}%"

log_info "🎉 일일 정리 완료"

# 6. 다음 정리 시간 안내
NEXT_CLEANUP=$(date -d "tomorrow 00:00" "+%Y-%m-%d %H:%M:%S")
log_info "⏰ 다음 정리 예정: ${NEXT_CLEANUP} KST"