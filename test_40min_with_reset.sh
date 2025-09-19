#!/bin/bash

# 40분 하드리셋 테스트 스크립트
# 30분 하드리셋 + 10분 추가 운영으로 하드리셋 효과 검증

TEST_DURATION=2400  # 40분 = 2400초
RESTART_INTERVAL=1800  # 30분 = 1800초
LOG_PREFIX="[40MIN-RESET-TEST]"

echo "$LOG_PREFIX $(date) 🚀 40분 하드리셋 테스트 시작"
echo "$LOG_PREFIX 테스트 계획:"
echo "$LOG_PREFIX   - 0-30분: 첫 번째 사이클 (30분 실행)"
echo "$LOG_PREFIX   - 30분: 하드리셋 실행"
echo "$LOG_PREFIX   - 30-40분: 두 번째 사이클 (10분 실행)"
echo "$LOG_PREFIX   - 40분: 테스트 완료 후 성능 진단"

# 테스트 카운터
cycle_count=0
start_time=$(date +%s)

while true; do
    cycle_count=$((cycle_count + 1))
    current_time=$(date +%s)
    elapsed=$((current_time - start_time))
    
    # 40분 완료 체크
    if [ $elapsed -ge $TEST_DURATION ]; then
        echo "$LOG_PREFIX $(date) ⏰ 40분 테스트 완료!"
        break
    fi
    
    echo "$LOG_PREFIX $(date) 🔄 사이클 #$cycle_count 시작"
    
    # 심볼 업데이트
    echo "$LOG_PREFIX $(date) 📡 심볼 업데이트 중..."
    if ./pumpwatch --init-symbols; then
        echo "$LOG_PREFIX $(date) ✅ 심볼 업데이트 성공"
    else
        echo "$LOG_PREFIX $(date) ❌ 심볼 업데이트 실패"
        exit 1
    fi
    
    # 메인 프로세스 시작
    echo "$LOG_PREFIX $(date) 🚀 PumpWatch 시작 (PID: 감지 예정)"
    
    # 30분 또는 남은 시간 중 짧은 시간 실행
    remaining_time=$((TEST_DURATION - elapsed))
    run_duration=$RESTART_INTERVAL
    if [ $remaining_time -lt $RESTART_INTERVAL ]; then
        run_duration=$remaining_time
    fi
    
    echo "$LOG_PREFIX $(date) ⏱️ $((run_duration / 60))분 실행 예정"
    
    # timeout을 사용하여 정확한 시간 제어
    timeout ${run_duration}s ./pumpwatch &
    pumpwatch_pid=$!
    
    echo "$LOG_PREFIX $(date) ✅ PumpWatch 시작됨 (PID: $pumpwatch_pid)"
    
    # 프로세스가 완료될 때까지 대기
    wait $pumpwatch_pid
    exit_code=$?
    
    current_time=$(date +%s)
    elapsed=$((current_time - start_time))
    
    if [ $exit_code -eq 124 ]; then
        echo "$LOG_PREFIX $(date) ⏰ 타임아웃으로 정상 종료 (${run_duration}초 경과)"
    elif [ $exit_code -eq 0 ]; then
        echo "$LOG_PREFIX $(date) ✅ 정상 종료"
    else
        echo "$LOG_PREFIX $(date) ⚠️ 비정상 종료 (코드: $exit_code)"
    fi
    
    # 40분 완료 재체크
    if [ $elapsed -ge $TEST_DURATION ]; then
        echo "$LOG_PREFIX $(date) ⏰ 40분 테스트 완료!"
        break
    fi
    
    # 하드리셋 사이 잠깐 대기
    echo "$LOG_PREFIX $(date) 💤 3초 대기 후 다음 사이클..."
    sleep 3
done

echo "$LOG_PREFIX $(date) 🎯 40분 테스트 완료 - 성능 진단 시작"
echo "$LOG_PREFIX 총 실행 사이클: $cycle_count"
echo "$LOG_PREFIX 총 실행 시간: $((elapsed / 60))분 $((elapsed % 60))초"

# 성능 진단을 위한 마지막 실행 (5분 진단 모드)
echo "$LOG_PREFIX $(date) 📊 성능 진단 모드 실행 (5분)"
timeout 300s ./pumpwatch

echo "$LOG_PREFIX $(date) ✅ 40분 하드리셋 테스트 완료!"
