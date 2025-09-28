#!/usr/bin/env bash

# PumpWatch v2.0 긴급 디스크 정리 스크립트
# 디스크 공간이 부족할 때 즉시 실행하는 스크립트

set -e

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() {
    echo -e "${GREEN}[EMERGENCY]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
}

log_warn() {
    echo -e "${YELLOW}[EMERGENCY]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
}

# 스크립트 디렉토리로 이동
cd "$(dirname "$0")/.."

log_info "🚨 긴급 디스크 정리 시작"

# 현재 디스크 사용률 확인
DISK_USAGE=$(df -h . | tail -1 | awk '{print $5}' | sed 's/%//')
log_info "📊 현재 디스크 사용률: ${DISK_USAGE}%"

# 1. 모든 로그 파일 정리 (오늘 것 제외)
log_info "🗑️ 오래된 로그 파일 강제 정리..."
DELETED_LOGS=$(find logs/ -name "*.log" ! -name "*$(date +%Y%m%d)*" 2>/dev/null | wc -l)
find logs/ -name "*.log" ! -name "*$(date +%Y%m%d)*" -delete 2>/dev/null || true
log_info "✅ 로그 파일 ${DELETED_LOGS}개 삭제"

# 2. QuestDB 강제 정리
log_info "🗄️ QuestDB 강제 정리..."
if curl -s "http://localhost:9000/exec?query=SELECT%201;" >/dev/null 2>&1; then
    curl -s "http://localhost:9000/exec?query=DROP%20TABLE%20IF%20EXISTS%20trades;" >/dev/null 2>&1
    CREATE_QUERY="CREATE TABLE trades (timestamp TIMESTAMP, exchange SYMBOL, market_type SYMBOL, symbol SYMBOL, trade_id STRING, price DOUBLE, quantity DOUBLE, side SYMBOL) timestamp(timestamp) PARTITION BY DAY;"
    curl -s "http://localhost:9000/exec" --data-urlencode "query=${CREATE_QUERY}" >/dev/null 2>&1
    log_info "✅ QuestDB trades 테이블 초기화 완료"
else
    log_warn "⚠️ QuestDB 접속 불가 - 건너뜀"
fi

# 3. 임시 파일들 정리
log_info "🧹 임시 파일 정리..."
rm -rf /tmp/pumpwatch* 2>/dev/null || true
rm -rf /tmp/questdb* 2>/dev/null || true
log_info "✅ 임시 파일 정리 완료"

# 4. 최종 디스크 사용률 확인
FINAL_USAGE=$(df -h . | tail -1 | awk '{print $5}' | sed 's/%//')
SAVED_SPACE=$((DISK_USAGE - FINAL_USAGE))

log_info "📊 정리 후 디스크 사용률: ${FINAL_USAGE}%"
log_info "💾 확보된 공간: ${SAVED_SPACE}%"

if [ "$FINAL_USAGE" -lt 90 ]; then
    log_info "✅ 긴급 정리 성공!"
else
    log_warn "⚠️ 여전히 디스크 사용률이 높습니다. 추가 정리가 필요할 수 있습니다."
fi

log_info "🎉 긴급 디스크 정리 완료"