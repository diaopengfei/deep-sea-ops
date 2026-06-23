# Stop all deepsea-ops development processes
$ErrorActionPreference = "SilentlyContinue"

$Root = (Resolve-Path "$PSScriptRoot\..").Path
$RunDir = Join-Path $Root ".run"

if (-not (Test-Path $RunDir)) {
    Write-Host "No running processes found ($RunDir does not exist)"
    exit 0
}

$stopped = 0
Get-ChildItem -Path $RunDir -Filter "*.pid" | ForEach-Object {
    $name = $_.BaseName
    $procId = [int](Get-Content $_.FullName | Select-Object -First 1)
    $proc = Get-Process -Id $procId -ErrorAction SilentlyContinue
    if ($proc) {
        $proc | Stop-Process -Force
        Write-Host "[$name] stopped (PID $procId)" -ForegroundColor Green
        $stopped++
    } else {
        Write-Host "[$name] process not found (PID $procId)" -ForegroundColor Gray
    }
    Remove-Item $_.FullName -Force
}

# Clean up any remaining processes
Get-Process -Name "deepsea-server","deepsea-agent" -ErrorAction SilentlyContinue | Stop-Process -Force
Get-Process | Where-Object { $_.ProcessName -eq "go" -or $_.ProcessName -eq "vite" } | Stop-Process -Force

Write-Host "Stopped $stopped processes" -ForegroundColor Cyan
