# ============================================================================
# deepsea-ops 开发环境启动脚本 (Windows PowerShell)
#
# 用法:
#   .\scripts\start.ps1              # 启动全部 (控制面 + Agent + 前端)
#   .\scripts\start.ps1 -Mode dev    # 同上, 显式指定 dev 模式
#   .\scripts\start.ps1 -Mode cluster # 启动 3 节点 Raft 本地集群 + Agent + 前端
#   .\scripts\start.ps1 -Mode server # 仅控制面
#   .\scripts\start.ps1 -Mode agent  # 仅 Agent
#   .\scripts\start.ps1 -Mode web    # 仅前端
#
# 启动方式: 为每个节点生成 YAML 配置文件到 .run\config\, 通过 -config 启动
# (参考 Kafka / Elasticsearch 的配置文件启动方式)
#
# 停止: .\scripts\stop.ps1
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

# ---------- 环境变量 (开发默认值, 生产请覆盖) ----------
if (-not $env:JWT_SECRET)     { $env:JWT_SECRET = "dev-secret-change-me" }
if (-not $env:ADMIN_PASSWORD) { $env:ADMIN_PASSWORD = "admin123" }
if (-not $env:MASTER_KEY)     { $env:MASTER_KEY = "dev-master-key-please-change-32b!" }

# ---------- 前置检查 ----------
function Check-Cmd {
    param([string]$Name)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        Write-Host "错误: 未找到 $Name, 请先安装" -ForegroundColor Red
        exit 1
    }
}
Check-Cmd "go"
Check-Cmd "npm"

# ---------- 生成 YAML 配置文件 ----------
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
# 自动生成: 开发环境控制面配置 (节点 $Id)
node_id: $Id
raft:
  addr: $RaftAddr
  data_dir: $RaftDir
  join: "$Join"
http:
  addr: $HttpAddr
grpc:
  addr: $GrpcAddr
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
# 自动生成: 开发环境 Agent 配置 ($Id)
agent_id: $Id
server: $ServerAddr
"@
    Set-Content -Path $cfgFile -Value $content -Encoding UTF8
    return $cfgFile
}

# ---------- 工具函数 ----------
function Start-Bg {
    param(
        [string]$Name,
        [scriptblock]$Command,
        [string]$WorkDir
    )
    $pidFile = Join-Path $RunDir "$Name.pid"
    if ((Test-Path $pidFile) -and (Get-Process -Id (Get-Content $pidFile) -ErrorAction SilentlyContinue)) {
        $existingPid = Get-Content $pidFile
        Write-Host "[$Name] 已在运行 (PID $existingPid), 跳过" -ForegroundColor Yellow
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
    Write-Host "[$Name] 已启动 (PID $($proc.Id)), 日志: $logFile" -ForegroundColor Green
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

# ---------- 模式分发 ----------
switch ($Mode) {
    "dev" {
        Write-Host "=== 启动开发环境 (单节点) ===" -ForegroundColor Cyan
        Start-Server -Id "node1" -RaftAddr "127.0.0.1:7000" -RaftDir (Join-Path $RunDir "raft-node1") -HttpAddr ":8080" -GrpcAddr ":9090"
        Start-Sleep -Seconds 2
        Start-Agent -Id "agent-1" -ServerAddr "127.0.0.1:9090"
        Start-Web
    }
    "cluster" {
        Write-Host "=== 启动 3 节点 Raft 本地集群 ===" -ForegroundColor Cyan
        Start-Server -Id "node1" -RaftAddr "127.0.0.1:7001" -RaftDir (Join-Path $RunDir "raft-node1") -HttpAddr ":8080" -GrpcAddr ":9090"
        Start-Sleep -Seconds 3
        Start-Server -Id "node2" -RaftAddr "127.0.0.1:7002" -RaftDir (Join-Path $RunDir "raft-node2") -Join "127.0.0.1:7001" -HttpAddr ":8081" -GrpcAddr ":9091"
        Start-Server -Id "node3" -RaftAddr "127.0.0.1:7003" -RaftDir (Join-Path $RunDir "raft-node3") -Join "127.0.0.1:7001" -HttpAddr ":8082" -GrpcAddr ":9092"
        Start-Sleep -Seconds 2
        Start-Agent -Id "agent-1" -ServerAddr "127.0.0.1:9090"
        Start-Web
        Write-Host ""
        Write-Host "提示: node2/node3 启动后需调用 join 接口加入集群:" -ForegroundColor Yellow
        Write-Host "  curl -X POST http://127.0.0.1:8080/api/cluster/join -H 'Content-Type: application/json' -d '{`"id`":`"node2`",`"addr`":`"127.0.0.1:7002`"}'"
        Write-Host "  curl -X POST http://127.0.0.1:8080/api/cluster/join -H 'Content-Type: application/json' -d '{`"id`":`"node3`",`"addr`":`"127.0.0.1:7003`"}'"
    }
    "server" {
        Write-Host "=== 仅启动控制面 ===" -ForegroundColor Cyan
        Start-Server -Id "node1" -RaftAddr "127.0.0.1:7000" -RaftDir (Join-Path $RunDir "raft-node1") -HttpAddr ":8080" -GrpcAddr ":9090"
    }
    "agent" {
        Write-Host "=== 仅启动 Agent ===" -ForegroundColor Cyan
        Start-Agent -Id "agent-1" -ServerAddr "127.0.0.1:9090"
    }
    "web" {
        Write-Host "=== 仅启动前端 ===" -ForegroundColor Cyan
        Start-Web
    }
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host " deepsea-ops 开发环境"
Write-Host "========================================" -ForegroundColor Cyan
Write-Host " 前端:     http://localhost:5173"
Write-Host " 控制面:   http://localhost:8080"
Write-Host " 默认账号: admin / $env:ADMIN_PASSWORD"
Write-Host " 配置文件: $CfgDir\"
Write-Host " 停止:     .\scripts\stop.ps1"
Write-Host "========================================" -ForegroundColor Cyan
