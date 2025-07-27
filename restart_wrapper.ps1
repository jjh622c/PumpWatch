# NoticePumpCatch 자동 재시작 래퍼 스크립트
# WebSocket 오류시 자동으로 프로그램을 재시작합니다

$programName = "noticepumpcatch.exe"
$maxRestarts = 10 # 최대 재시작 횟수 (무한 루프 방지)
$restartCount = 0
$startTime = Get-Date

Write-Host "🚀 NoticePumpCatch 자동 재시작 래퍼 시작" -ForegroundColor Green
Write-Host "📂 경로: $(Get-Location)" -ForegroundColor Cyan
Write-Host "🔄 최대 재시작: $maxRestarts 회" -ForegroundColor Yellow
Write-Host "=" * 50

while ($restartCount -lt $maxRestarts) {
    $currentTime = Get-Date
    Write-Host "🕐 [$($currentTime.ToString('HH:mm:ss'))] 프로그램 시작 (재시작 #$restartCount)" -ForegroundColor Green
    
    # 프로그램 실행
    $process = Start-Process -FilePath ".\$programName" -NoNewWindow -Wait -PassThru
    $exitCode = $process.ExitCode
    
    $endTime = Get-Date
    $runTime = $endTime - $currentTime
    
    Write-Host "⏹️  프로그램 종료: exit code $exitCode, 실행시간: $($runTime.ToString('hh\:mm\:ss'))" -ForegroundColor Yellow
    
    if ($exitCode -eq 0) {
        Write-Host "✅ 정상 종료 - 래퍼 스크립트 종료" -ForegroundColor Green
        break
    }
    elseif ($exitCode -eq 1) {
        $restartCount++
        Write-Host "🔄 WebSocket 오류로 인한 재시작 필요 (${restartCount}/${maxRestarts})" -ForegroundColor Magenta
        
        # 너무 빠른 재시작 방지 (최소 5초 대기)
        if ($runTime.TotalSeconds -lt 5) {
            Write-Host "⏳ 너무 빠른 재시작 - 10초 대기..." -ForegroundColor Red
            Start-Sleep -Seconds 10
        }
        else {
            Write-Host "⏳ 3초 후 재시작..." -ForegroundColor Cyan
            Start-Sleep -Seconds 3
        }
    }
    else {
        Write-Host "❌ 예상치 못한 종료 코드: $exitCode" -ForegroundColor Red
        $restartCount++
        Write-Host "⏳ 5초 후 재시작..." -ForegroundColor Cyan
        Start-Sleep -Seconds 5
    }
}

if ($restartCount -ge $maxRestarts) {
    Write-Host "🚨 최대 재시작 횟수 도달 - 수동 확인 필요" -ForegroundColor Red
}

$totalTime = (Get-Date) - $startTime
Write-Host "📊 총 실행시간: $($totalTime.ToString('hh\:mm\:ss'))" -ForegroundColor Cyan
Write-Host "🏁 래퍼 스크립트 종료" -ForegroundColor Green

# 사용자 입력 대기 (터미널이 바로 닫히지 않도록)
Read-Host "아무 키나 누르세요..." 