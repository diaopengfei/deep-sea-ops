# ============================================================================
# deepsea-ops development startup script (Windows PowerShell)
#
# Usage:
#   .\scripts\start.ps1              # Start all (server + agent + frontend)
#   .\scripts\start.ps1 -Mode dev    # Dev mode (default)
#   .\scripts\start.ps1 -Mode cluster # 3-node Raft cluster
#   .\scripts\start.ps1 -Mode server # Server only
#   .\scripts\start.ps1 -Mode agent  # Agent only
#   .\scripts\start.ps1 -Mode web    # Frontend only
#
# Stop: .\scripts\stop.ps1
# ============================================================================
param(
    [ValidateSet("dev", "cluster", "server", "agent", "web")]
    [string]$Mode = "dev"
)

$ErrorActionPreference = "Stop"

$Root = (Resolve-Path "$PSScriptRoot\..").Path
$ServerDir = Join-Path $Root "server"
$WebDir = Join-Path $Root "web"
$RunDir = Join-Path $Root ".run"
$CfgDir = Join-Path $RunDir "config"
if (-not (Test-Path $RunDir)) { New-Item -ItemType Directory -Path $RunDir | Out-Null }
if (-not (Test-Path $CfgDir)) { New-Item -ItemType Directory -Path $CfgDir | Out-Null }

# Environment variables (dev defaults, override in production)
# These are also written into YAML config files (security section) for v0.5.1+
# Priority: env var > YAML > built-in default
if (-not $env:JWT_SECRET)     { $env:JWT_SECRET = "dev-secret-change-me" }
if (-not $env:ADMIN_PASSWORD) { $env:ADMIN_PASSWORD = "admin123" }
if (-not $env:MASTER_KEY)     { $env:MASTER_KEY = "dev-master-key-please-change-32b!" }

# Pre-checks
function Check-Cmd {
    param([string]$Name)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        Write-Host "Error: $Name not found, please install it first" -ForegroundColor Red
        exit 1
    }
}
Check-Cmd "go"
Check-Cmd "npm"

# Generate YAML config files
function Write-ServerConfig {
    param(
        [string]$Id,
        [string]$RaftAddr,
        [string]$RaftDir,
        [string]$Join,
        [string]$HttpAddr,
        [string]$GrpcAddr
    )
    $cfgFile = Join-Path $CfgDir "server-$Id.yaml"
    $content = @"
# Auto-generated: dev server config (node $Id)
node_id: $Id
raft:
  addr: $RaftAddr
  data_dir: $RaftDir
  join: "$Join"
http:
  addr: $HttpAddr
grpc:
  addr: $GrpcAddr
security:
  jwt_secret: "$env:JWT_SECRET"
  admin_password: "$env:ADMIN_PASSWORD"
  master_key: "$env:MASTER_KEY"
"@
    Set-Content -Path $cfgFile -Value $content -Encoding UTF8
    return $cfgFile
}

function Write-AgentConfig {
    param(
        [string]$Id,
        [string]$ServerAddr
    )
    $cfgFile = Join-Path $CfgDir "agent-$Id.yaml"
    $content = @"
# Auto-generated: dev agent config ($Id)
agent_id: $Id
server: $ServerAddr
"@
    Set-Content -Path $cfgFile -Value $content -Encoding UTF8
    return $cfgFile
}

# Background process helper
function Start-Bg {
    param(
        [string]$Name,
        [scriptblock]$Command,
        [string]$WorkDir
    )
    $pidFile = Join-Path $RunDir "$Name.pid"
    if ((Test-Path $pidFile) -and (Get-Process -Id (Get-Content $pidFile) -ErrorAction SilentlyContinue)) {
        $existingPid = Get-Content $pidFile
        Write-Host "[$Name] already running (PID $existingPid), skipping" -ForegroundColor Yellow
        return
    }
    $logFile = Join-Path $RunDir "$Name.log"
    $proc = Start-Process -FilePath "powershell" `
        -ArgumentList "-NoProfile", "-Command", $Command.ToString() `
        -WorkingDirectory $WorkDir `
        -WindowStyle Hidden `
        -RedirectStandardOutput $logFile `
        -RedirectStandardError "$logFile.err" `
        -PassThru
    $proc.Id | Out-File -FilePath $pidFile -Encoding ascii
    Write-Host "[$Name] started (PID $($proc.Id)), log: $logFile" -ForegroundColor Green
}

function Start-Server {
    param(
        [string]$Id = "node1",
        [string]$RaftAddr = "127.0.0.1:7000",
        [string]$RaftDir,
        [string]$Join = "",
        [string]$HttpAddr = ":8080",
        [string]$GrpcAddr = ":9090"
    )
    if (-not $RaftDir) { $RaftDir = Join-Path $RunDir "raft-$Id" }
    if (-not (Test-Path $RaftDir)) { New-Item -ItemType Directory -Path $RaftDir -Force | Out-Null }
    $cfgFile = Write-ServerConfig -Id $Id -RaftAddr $RaftAddr -RaftDir $RaftDir -Join $Join -HttpAddr $HttpAddr -GrpcAddr $GrpcAddr
    $cmd = { Set-Location $using:ServerDir; go run ./cmd/server -config $using:cfgFile }
    Start-Bg -Name "server-$Id" -Command $cmd -WorkDir $ServerDir
}

function Start-Agent {
    param(
        [string]$Id = "agent-1",
        [string]$ServerAddr = "127.0.0.1:9090"
    )
    $cfgFile = Write-AgentConfig -Id $Id -ServerAddr $ServerAddr
    $cmd = { Set-Location $using:ServerDir; go run ./cmd/agent -config $using:cfgFile }
    Start-Bg -Name "agent-$Id" -Command $cmd -WorkDir $ServerDir
}

function Start-Web {
    $cmd = { Set-Location $using:WebDir; npm run dev }
    Start-Bg -Name "web" -Command $cmd -WorkDir $WebDir
}

# Mode dispatch
switch ($Mode) {
    "dev" {
        Write-Host "=== Starting dev environment (single node) ===" -ForegroundColor Cyan
        Start-Server -Id "node1" -RaftAddr "127.0.0.1:7000" -RaftDir (Join-Path $RunDir "raft-node1") -HttpAddr ":8080" -GrpcAddr ":9090"
        Start-Sleep -Seconds 2
        Start-Agent -Id "agent-1" -ServerAddr "127.0.0.1:9090"
        Start-Web
    }
    "cluster" {
        Write-Host "=== Starting 3-node Raft cluster ===" -ForegroundColor Cyan
        Start-Server -Id "node1" -RaftAddr "127.0.0.1:7001" -RaftDir (Join-Path $RunDir "raft-node1") -HttpAddr ":8080" -GrpcAddr ":9090"
        Start-Sleep -Seconds 3
        Start-Server -Id "node2" -RaftAddr "127.0.0.1:7002" -RaftDir (Join-Path $RunDir "raft-node2") -Join "127.0.0.1:7001" -HttpAddr ":8081" -GrpcAddr ":9091"
        Start-Server -Id "node3" -RaftAddr "127.0.0.1:7003" -RaftDir (Join-Path $RunDir "raft-node3") -Join "127.0.0.1:7001" -HttpAddr ":8082" -GrpcAddr ":9092"
        Start-Sleep -Seconds 2
        Start-Agent -Id "agent-1" -ServerAddr "127.0.0.1:9090"
        Start-Web
        Write-Host ""
        Write-Host "Note: After node2/node3 start, call join API to add to cluster:" -ForegroundColor Yellow
        Write-Host "  curl -X POST http://127.0.0.1:8080/api/cluster/join -H 'Content-Type: application/json' -d '{`"id`":`"node2`",`"addr`":`"127.0.0.1:7002`"}'"
        Write-Host "  curl -X POST http://127.0.0.1:8080/api/cluster/join -H 'Content-Type: application/json' -d '{`"id`":`"node3`",`"addr`":`"127.0.0.1:7003`"}'"
    }
    "server" {
        Write-Host "=== Starting server only ===" -ForegroundColor Cyan
        Start-Server -Id "node1" -RaftAddr "127.0.0.1:7000" -RaftDir (Join-Path $RunDir "raft-node1") -HttpAddr ":8080" -GrpcAddr ":9090"
    }
    "agent" {
        Write-Host "=== Starting agent only ===" -ForegroundColor Cyan
        Start-Agent -Id "agent-1" -ServerAddr "127.0.0.1:9090"
    }
    "web" {
        Write-Host "=== Starting frontend only ===" -ForegroundColor Cyan
        Start-Web
    }
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host " deepsea-ops dev environment"
Write-Host "========================================" -ForegroundColor Cyan
Write-Host " Frontend:  http://localhost:5173"
Write-Host " Server:    http://localhost:8080"
Write-Host " Account:   admin / $env:ADMIN_PASSWORD"
Write-Host " Configs:   $CfgDir\"
Write-Host " Stop:      .\scripts\stop.ps1"
Write-Host "========================================" -ForegroundColor Cyan
