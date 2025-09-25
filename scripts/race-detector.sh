#!/bin/bash

echo "🏁 Go Race Detector를 사용한 동시성 문제 검사 시작"
echo "📊 30초간 race condition 감지 실행..."

# 빌드 시 race detector 활성화
echo "🔧 Race detector와 함께 스트레스 테스트 빌드 중..."
go build -race -o stress-test-questdb-race ./cmd/stress-test-questdb

if [ $? -ne 0 ]; then
    echo "❌ Race detector 빌드 실패"
    exit 1
fi

echo "✅ Race detector 빌드 완료"
echo "🚀 30초간 race detector로 실행 중..."

# 30초간 실행
timeout 30s ./stress-test-questdb-race 2>&1 | tee race-detector-output.log

RACE_DETECTED=$?

echo ""
echo "🔍 Race Detector 결과 분석:"

# Race condition 검출 확인
if grep -q "WARNING: DATA RACE" race-detector-output.log; then
    echo "🚨 데이터 경합 감지됨!"
    echo "📋 발견된 race condition들:"
    grep -A 10 "WARNING: DATA RACE" race-detector-output.log
    echo ""
    echo "❌ 동시성 문제가 발견되었습니다. 수정이 필요합니다."
else
    echo "✅ 데이터 경합이 감지되지 않았습니다."
fi

# 고루틴 누수 검사
echo ""
echo "🔍 고루틴 수 분석:"
GOROUTINE_COUNT=$(tail -20 race-detector-output.log | grep "고루틴:" | tail -1 | sed 's/.*고루틴: \([0-9]*\).*/\1/')
if [ ! -z "$GOROUTINE_COUNT" ]; then
    if [ "$GOROUTINE_COUNT" -gt 100 ]; then
        echo "⚠️ 고루틴 수가 높습니다: ${GOROUTINE_COUNT}개"
        echo "💡 고루틴 누수 가능성을 확인해주세요"
    else
        echo "✅ 고루틴 수가 정상 범위입니다: ${GOROUTINE_COUNT}개"
    fi
fi

# 메모리 사용량 추이 분석
echo ""
echo "🔍 메모리 사용량 추이 분석:"
echo "📈 초기 → 최종 메모리 사용량:"
INITIAL_MEMORY=$(head -20 race-detector-output.log | grep "메모리:" | head -1 | sed 's/.*메모리: \([0-9]*\)MB.*/\1/')
FINAL_MEMORY=$(tail -20 race-detector-output.log | grep "메모리:" | tail -1 | sed 's/.*메모리: \([0-9]*\)MB.*/\1/')

if [ ! -z "$INITIAL_MEMORY" ] && [ ! -z "$FINAL_MEMORY" ]; then
    MEMORY_DIFF=$((FINAL_MEMORY - INITIAL_MEMORY))
    echo "초기: ${INITIAL_MEMORY}MB → 최종: ${FINAL_MEMORY}MB (차이: ${MEMORY_DIFF}MB)"

    if [ "$MEMORY_DIFF" -gt 10 ]; then
        echo "⚠️ 메모리 사용량이 크게 증가했습니다 (+${MEMORY_DIFF}MB)"
        echo "💡 메모리 누수 가능성을 확인해주세요"
    elif [ "$MEMORY_DIFF" -lt -5 ]; then
        echo "✅ 메모리 사용량이 감소했습니다 (${MEMORY_DIFF}MB)"
        echo "💡 GC가 효과적으로 작동하고 있습니다"
    else
        echo "✅ 메모리 사용량이 안정적입니다"
    fi
fi

# 성능 분석
echo ""
echo "🔍 성능 분석:"
FINAL_TPS=$(tail -20 race-detector-output.log | grep "TPS:" | tail -1 | sed 's/.*TPS: \([0-9.]*\).*/\1/')
if [ ! -z "$FINAL_TPS" ]; then
    TPS_RATIO=$(echo "scale=2; $FINAL_TPS / 12000" | bc -l 2>/dev/null || echo "0")
    echo "최종 TPS: ${FINAL_TPS} (목표 대비 ${TPS_RATIO}x)"

    if (( $(echo "$TPS_RATIO < 0.8" | bc -l) )); then
        echo "⚠️ 성능이 목표의 80% 미만입니다"
        echo "💡 병목점을 찾아 최적화가 필요합니다"
    else
        echo "✅ 성능이 목표를 달성했습니다"
    fi
fi

# 에러율 확인
ERROR_RATE=$(tail -20 race-detector-output.log | grep "에러:" | tail -1 | sed 's/.*에러: [0-9]* (\([0-9.]*\)%).*/\1/')
if [ ! -z "$ERROR_RATE" ]; then
    echo "에러율: ${ERROR_RATE}%"

    if (( $(echo "$ERROR_RATE > 0.1" | bc -l 2>/dev/null || echo 0) )); then
        echo "⚠️ 에러율이 높습니다: ${ERROR_RATE}%"
        echo "💡 에러 원인을 분석해주세요"
    else
        echo "✅ 에러율이 낮습니다: ${ERROR_RATE}%"
    fi
fi

# 최종 종합 평가
echo ""
echo "🎯 Race Detector 종합 평가:"
RACE_ISSUES=$(grep -c "WARNING: DATA RACE" race-detector-output.log 2>/dev/null || echo 0)

if [ "$RACE_ISSUES" -eq 0 ]; then
    echo "✅ 동시성 안전성: 문제 없음"
else
    echo "❌ 동시성 안전성: ${RACE_ISSUES}개 문제 발견"
fi

if [ ! -z "$GOROUTINE_COUNT" ] && [ "$GOROUTINE_COUNT" -le 100 ]; then
    echo "✅ 고루틴 관리: 정상"
else
    echo "⚠️ 고루틴 관리: 확인 필요"
fi

if [ ! -z "$MEMORY_DIFF" ] && [ "$MEMORY_DIFF" -le 10 ]; then
    echo "✅ 메모리 관리: 정상"
else
    echo "⚠️ 메모리 관리: 확인 필요"
fi

echo ""
echo "📝 상세 로그는 race-detector-output.log 파일을 확인하세요"
echo "🏁 Race Detector 검사 완료"

# 정리
rm -f ./stress-test-questdb-race