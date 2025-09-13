#!/bin/bash

# PumpWatch v2.0 Hard Reset Wrapper Script
# 30분마다 프로그램을 완전히 재시작하여 WebSocket 연결 문제 해결

set -e

# 설정
RESTART_INTERVAL=1800  # 30분 = 1800초
BINARY_PATH="./pumpwatch"
SHUTDOWN_TIMEOUT=30    # graceful shutdown 대기 시간 (초)
LOG_PREFIX="[PumpWatch-RESTART]"

# 로그 함수
log() {
    echo "$LOG_PREFIX [$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

# 신호 핸들러 설정 (Ctrl+C 등으로 스크립트 종료시)
cleanup() {
    log "🛑 Restart wrapper received termination signal"
    if [[ -n $MAIN_PID ]] && kill -0 $MAIN_PID 2>/dev/null; then
        log "📤 Sending SIGTERM to main process (PID: $MAIN_PID)"
        kill -TERM $MAIN_PID
        
        # graceful shutdown 대기
        for i in $(seq 1 $SHUTDOWN_TIMEOUT); do
            if ! kill -0 $MAIN_PID 2>/dev/null; then
                log "✅ Main process terminated gracefully"
                break
            fi
            sleep 1
        done
        
        # 강제 종료 fallback
        if kill -0 $MAIN_PID 2>/dev/null; then
            log "⚡ Force killing main process"
            kill -KILL $MAIN_PID
        fi
    fi
    log "👋 Restart wrapper shutdown complete"
    exit 0
}

trap cleanup SIGINT SIGTERM

log "🚀 PumpWatch Hard Reset Wrapper Started"
log "⏰ Restart interval: ${RESTART_INTERVAL}s (30 minutes)"
log "🔧 Binary path: $BINARY_PATH"

# 바이너리 존재 확인
if [[ ! -f "$BINARY_PATH" ]]; then
    log "❌ ERROR: Binary not found at $BINARY_PATH"
    log "💡 Run 'go build -o pumpwatch main.go' first"
    exit 1
fi

RESTART_COUNT=0

while true; do
    RESTART_COUNT=$((RESTART_COUNT + 1))
    log "🔄 Starting restart cycle #$RESTART_COUNT"
    
    # 심볼 업데이트 먼저 실행
    log "📡 Updating symbols..."
    if $BINARY_PATH --init-symbols; then
        log "✅ Symbols updated successfully"
    else
        log "⚠️ Symbol update failed, but continuing with main program"
    fi
    
    # 메인 프로그램 백그라운드 실행
    log "🚀 Starting main PumpWatch process..."
    $BINARY_PATH &
    MAIN_PID=$!
    
    log "✅ Main process started (PID: $MAIN_PID)"
    log "⏳ Waiting ${RESTART_INTERVAL}s before next restart..."
    
    # 30분 대기 (프로세스 상태 모니터링 포함)
    for i in $(seq 1 $RESTART_INTERVAL); do
        # 프로세스가 비정상 종료되었는지 확인
        if ! kill -0 $MAIN_PID 2>/dev/null; then
            log "💥 Main process died unexpectedly! Restarting immediately..."
            break
        fi
        sleep 1
        
        # 진행 상황 표시 (매 5분마다)
        if [[ $((i % 300)) -eq 0 ]]; then
            remaining_minutes=$(( (RESTART_INTERVAL - i) / 60 ))
            log "⏰ ${remaining_minutes} minutes remaining until restart"
        fi
    done
    
    # 프로세스가 아직 살아있으면 graceful shutdown
    if kill -0 $MAIN_PID 2>/dev/null; then
        log "🛑 Initiating graceful shutdown (PID: $MAIN_PID)"
        kill -TERM $MAIN_PID
        
        # graceful shutdown 대기
        for i in $(seq 1 $SHUTDOWN_TIMEOUT); do
            if ! kill -0 $MAIN_PID 2>/dev/null; then
                log "✅ Process terminated gracefully"
                break
            fi
            sleep 1
        done
        
        # 강제 종료 fallback
        if kill -0 $MAIN_PID 2>/dev/null; then
            log "⚡ Graceful shutdown timeout, force killing process"
            kill -KILL $MAIN_PID
            sleep 2
        fi
    fi
    
    log "🔄 Restart cycle #$RESTART_COUNT completed"
    log "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    
    # 다음 재시작까지 잠시 대기 (연속 재시작 방지)
    sleep 5
done