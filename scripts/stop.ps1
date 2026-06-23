# 停止 deepsea-ops 开发环境所有后台进程
$ErrorActionPreference = "SilentlyContinue"

$Root = (Resolve-Path "$PSScriptRoot\..").Path
$RunDir = Join-Path $Root ".run"

if (-not (Test-Path $RunDir)) {
    Write-Host "无运行记录 ($RunDir 不存在)"
    exit 0
}

$stopped = 0
Get-ChildItem -Path $RunDir -Filter "*.pid" | ForEach-Object {
    $name = $_.BaseName
    $pid = [int](Get-Content $_.FullName | Select-Object -First 1)
    $proc = Get-Process -Id $pid -ErrorAction SilentlyContinue
    if ($proc) {
        # 先尝试优雅终止, 再强杀
        $proc | Stop-Process -Force
        Write-Host "[$name] 已停止 (PID $pid)" -ForegroundColor Green
        $stopped++
    } else {
        Write-Host "[$name] 进程已不存在 (PID $pid)" -ForegroundColor Gray
    }
    Remove-Item $_.FullName -Force
}

# 清理可能残留的进程 (仅开发环境)
Get-Process -Name "deepsea-server", "deepsea-agent" -ErrorAction SilentlyContinue | Stop-Process -Force
Get-Process | Where-Object { $_.ProcessName -eq "go" -or $_.ProcessName -eq "vite" } | Stop-Process -Force

Write-Host "已停止 $stopped 个进程" -ForegroundColor Cyan
