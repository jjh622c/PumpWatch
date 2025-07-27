#!/bin/bash

# NoticePumpCatch 자동 재시작 래퍼 스크립트
# WebSocket 오류시 자동으로 프로그램을 재시작합니다

PROGRAM_NAME="./noticepumpcatch"
MAX_RESTARTS=10  # 최대 재시작 횟수 (무한 루프 방지)
restart_count=0
start_time=$(date +%s)

echo "🚀 NoticePumpCatch 자동 재시작 래퍼 시작"
echo "📂 경로: $(pwd)"
echo "🔄 최대 재시작: $MAX_RESTARTS 회"
echo "=================================================="

while [ $restart_count -lt $MAX_RESTARTS ]; do
    current_time=$(date '+%H:%M:%S')
    echo "🕐 [$current_time] 프로그램 시작 (재시작 #$restart_count)"
    
    # 프로그램 실행 시간 측정
    run_start=$(date +%s)
    
    # 프로그램 실행
    $PROGRAM_NAME
    exit_code=$?
    
    run_end=$(date +%s)
    run_time=$((run_end - run_start))
    
    echo "⏹️  프로그램 종료: exit code $exit_code, 실행시간: ${run_time}초"
    
    if [ $exit_code -eq 0 ]; then
        echo "✅ 정상 종료 - 래퍼 스크립트 종료"
        break
    elif [ $exit_code -eq 1 ]; then
        restart_count=$((restart_count + 1))
        echo "🔄 WebSocket 오류로 인한 재시작 필요 (${restart_count}/${MAX_RESTARTS})"
        
        # 너무 빠른 재시작 방지 (최소 5초 대기)
        if [ $run_time -lt 5 ]; then
            echo "⏳ 너무 빠른 재시작 - 10초 대기..."
            sleep 10
        else
            echo "⏳ 3초 후 재시작..."
            sleep 3
        fi
    else
        echo "❌ 예상치 못한 종료 코드: $exit_code"
        restart_count=$((restart_count + 1))
        echo "⏳ 5초 후 재시작..."
        sleep 5
    fi
done

if [ $restart_count -ge $MAX_RESTARTS ]; then
    echo "🚨 최대 재시작 횟수 도달 - 수동 확인 필요"
fi

end_time=$(date +%s)
total_time=$((end_time - start_time))
echo "📊 총 실행시간: ${total_time}초"
echo "🏁 래퍼 스크립트 종료" 