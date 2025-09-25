#!/usr/bin/env bash

# PumpWatch v2.0 통합 실행 스크립트
# QuestDB 자동 시작 + 30분 하드리셋 기능 포함

set -e

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 로그 함수
log_info() {
    echo -e "${GREEN}[PumpWatch]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
}

log_warn() {
    echo -e "${YELLOW}[PumpWatch]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
}

log_error() {
    echo -e "${RED}[PumpWatch]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
}

log_cycle() {
    echo -e "${BLUE}[PumpWatch-RESTART]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
}

# QuestDB 상태 확인 함수
check_questdb() {
    if curl -s http://localhost:9000 > /dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

# QuestDB 시작 함수
start_questdb() {
    log_info "🗄️ QuestDB 상태 확인 중..."

    if check_questdb; then
        log_info "✅ QuestDB가 이미 실행 중입니다"
        return 0
    fi

    log_info "🚀 QuestDB 시작 중..."

    if [ ! -f "./questdb/bin/questdb.sh" ]; then
        log_error "❌ QuestDB 바이너리를 찾을 수 없습니다: ./questdb/bin/questdb.sh"
        log_info "💡 다음 명령으로 QuestDB를 먼저 설치하세요:"
        log_info "   wget https://github.com/questdb/questdb/releases/download/7.4.2/questdb-7.4.2-rt-linux-amd64.tar.gz"
        log_info "   tar -xzf questdb-7.4.2-rt-linux-amd64.tar.gz"
        log_info "   mv questdb-7.4.2-rt-linux-amd64 questdb"
        exit 1
    fi

    # QuestDB 백그라운드 시작
    ./questdb/bin/questdb.sh start > /dev/null 2>&1 &

    # QuestDB 시작 대기 (최대 30초)
    for i in {1..30}; do
        if check_questdb; then
            log_info "✅ QuestDB 시작 완료 (${i}초)"
            return 0
        fi
        sleep 1
    done

    log_error "❌ QuestDB 시작 실패 (30초 타임아웃)"
    exit 1
}

# PumpWatch 프로세스 종료 함수
cleanup_pumpwatch() {
    if [ ! -z "$PUMPWATCH_PID" ] && kill -0 $PUMPWATCH_PID 2>/dev/null; then
        log_cycle "🛑 Graceful shutdown 시작 (PID: $PUMPWATCH_PID)"
        kill -TERM $PUMPWATCH_PID

        # Graceful shutdown 대기 (최대 10초)
        for i in {1..10}; do
            if ! kill -0 $PUMPWATCH_PID 2>/dev/null; then
                log_cycle "✅ Graceful shutdown 완료"
                return 0
            fi
            sleep 1
        done

        # 강제 종료
        log_cycle "⚠️ 강제 종료 실행"
        kill -KILL $PUMPWATCH_PID 2>/dev/null || true
        sleep 1
    fi
}

# 시그널 핸들러
cleanup() {
    log_cycle "🔄 종료 시그널 수신"
    cleanup_pumpwatch
    exit 0
}

# 시그널 등록
trap cleanup SIGINT SIGTERM

# 메인 실행 함수
main() {
    log_cycle "🚀 PumpWatch v2.0 통합 실행 시작"

    # 1. QuestDB 시작
    start_questdb

    # 2. 초기 빌드 확인
    if [ ! -f "./pumpwatch" ]; then
        log_info "🔨 PumpWatch 바이너리 빌드 중..."
        go build -o pumpwatch main.go
    fi

    # 3. 30분 하드리셋 루프
    cycle=1
    while true; do
        log_cycle "🔄 재시작 사이클 #${cycle} 시작"

        # 심볼 업데이트
        log_cycle "📡 심볼 목록 업데이트 중..."
        if ./pumpwatch --init-symbols; then
            log_cycle "✅ 심볼 업데이트 완료"
        else
            log_cycle "⚠️ 심볼 업데이트 실패, 계속 진행"
        fi

        # PumpWatch 시작
        log_cycle "🚀 PumpWatch 메인 프로세스 시작..."
        ./pumpwatch &
        PUMPWATCH_PID=$!
        log_cycle "✅ 메인 프로세스 시작 완료 (PID: $PUMPWATCH_PID)"

        # 30분 대기 (5분마다 상태 확인)
        for i in {1..6}; do
            sleep 300  # 5분
            remaining=$((30 - i * 5))

            # 프로세스 상태 확인
            if ! kill -0 $PUMPWATCH_PID 2>/dev/null; then
                log_cycle "❌ 메인 프로세스 예상치 못한 종료 감지"
                log_cycle "🔄 즉시 재시작 진행"
                break
            fi

            if [ $remaining -gt 0 ]; then
                log_cycle "⏰ ${remaining}분 후 재시작 예정"
            fi
        done

        # 30분 후 또는 프로세스 종료 시 정리
        cleanup_pumpwatch

        cycle=$((cycle + 1))
        sleep 3
    done
}

# 사용법 표시
show_usage() {
    echo "사용법: $0 [옵션]"
    echo ""
    echo "PumpWatch v2.0 통합 실행 스크립트"
    echo "- QuestDB 자동 시작"
    echo "- 30분마다 자동 하드리셋"
    echo "- WebSocket 연결 안정성 보장"
    echo ""
    echo "옵션:"
    echo "  --help    이 도움말 표시"
    echo ""
    echo "예제:"
    echo "  $0              # 일반 실행"
    echo "  nohup $0 &      # 백그라운드 실행"
    echo ""
}

# 파라미터 처리
case "${1:-}" in
    --help|-h)
        show_usage
        exit 0
        ;;
    *)
        main "$@"
        ;;
esac