#!/usr/bin/env bash
# ============================================================================
# deepsea-ops 开发环境启动脚本 (Linux / macOS / Git Bash)
#
# 用法:
#   ./scripts/start.sh              # 启动全部 (控制面 + Agent + 前端)
#   ./scripts/start.sh dev          # 同上, 显式指定 dev 模式
#   ./scripts/start.sh cluster      # 启动 3 节点 Raft 本地集群 + Agent + 前端
#   ./scripts/start.sh server       # 仅控制面
#   ./scripts/start.sh agent        # 仅 Agent
#   ./scripts/start.sh web          # 仅前端
#
# 停止: ./scripts/stop.sh
# ============================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SERVER_DIR="$ROOT/server"
WEB_DIR="$ROOT/web"
PID_DIR="$ROOT/.run"
mkdir -p "$PID_DIR"

MODE="${1:-dev}"

# ---------- 环境变量 (开发默认值, 生产请覆盖) ----------
export JWT_SECRET="${JWT_SECRET:-dev-secret-change-me}"
export ADMIN_PASSWORD="${ADMIN_PASSWORD:-admin123}"
export MASTER_KEY="${MASTER_KEY:-dev-master-key-please-change-32b!}"

# ---------- 前置检查 ----------
check_cmd() {
    command -v "$1" >/dev/null 2>&1 || { echo "错误: 未找到 $1, 请先安装"; exit 1; }
}
check_cmd go
check_cmd npm

# ---------- 工具函数 ----------
start_bg() {
    # 启动后台进程, 记录 PID
    local name="$1"; shift
    local pidfile="$PID_DIR/$name.pid"
    if [[ -f "$pidfile" ]] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
        echo "[$name] 已在运行 (PID $(cat "$pidfile")), 跳过"
        return
    fi
    "$@" >"$PID_DIR/$name.log" 2>&1 &
    echo $! >"$pidfile"
    echo "[$name] 已启动 (PID $(cat "$pidfile")), 日志: $PID_DIR/$name.log"
}

start_server() {
    local id="${1:-node1}"
    local raft_addr="${2:-127.0.0.1:7000}"
    local raft_dir="${3:-$PID_DIR/raft-$id}"
    local join="${4:-}"
    local http_addr="${5:-:8080}"
    local grpc_addr="${6:-:9090}"
    mkdir -p "$raft_dir"
    local args=(go run ./cmd/server -id "$id" -raft-addr "$raft_addr" -raft-dir "$raft_dir" -http "$http_addr" -grpc "$grpc_addr")
    [[ -n "$join" ]] && args+=(-join "$join")
    start_bg "server-$id" bash -c "cd '$SERVER_DIR' && ${args[*]}"
}

start_agent() {
    local id="${1:-agent-1}"
    local server_addr="${2:-127.0.0.1:9090}"
    start_bg "agent-$id" bash -c "cd '$SERVER_DIR' && go run ./cmd/agent -id '$id' -server '$server_addr'"
}

start_web() {
    start_bg "web" bash -c "cd '$WEB_DIR' && npm run dev"
}

# ---------- 模式分发 ----------
case "$MODE" in
    dev)
        echo "=== 启动开发环境 (单节点) ==="
        start_server "node1" "127.0.0.1:7000" "$PID_DIR/raft-node1" "" ":8080" ":9090"
        sleep 2
        start_agent "agent-1" "127.0.0.1:9090"
        start_web
        ;;
    cluster)
        echo "=== 启动 3 节点 Raft 本地集群 ==="
        # node1: 首节点 (bootstrap), HTTP 8080, gRPC 9090, Raft 7001
        start_server "node1" "127.0.0.1:7001" "$PID_DIR/raft-node1" "" ":8080" ":9090"
        sleep 3
        # node2 / node3: 加入集群, HTTP 8081/8082, gRPC 9091/9092, Raft 7002/7003
        # 注: 加入集群需通过 /api/cluster/join 接口, 此处先启动节点再手动 join
        start_server "node2" "127.0.0.1:7002" "$PID_DIR/raft-node2" "127.0.0.1:7001" ":8081" ":9091"
        start_server "node3" "127.0.0.1:7003" "$PID_DIR/raft-node3" "127.0.0.1:7001" ":8082" ":9092"
        sleep 2
        start_agent "agent-1" "127.0.0.1:9090"
        start_web
        echo ""
        echo "提示: node2/node3 启动后需调用 join 接口加入集群:"
        echo "  curl -X POST http://127.0.0.1:8080/api/cluster/join -H 'Content-Type: application/json' -d '{\"id\":\"node2\",\"addr\":\"127.0.0.1:7002\"}'"
        echo "  curl -X POST http://127.0.0.1:8080/api/cluster/join -H 'Content-Type: application/json' -d '{\"id\":\"node3\",\"addr\":\"127.0.0.1:7003\"}'"
        ;;
    server)
        echo "=== 仅启动控制面 ==="
        start_server "node1" "127.0.0.1:7000" "$PID_DIR/raft-node1" "" ":8080" ":9090"
        ;;
    agent)
        echo "=== 仅启动 Agent ==="
        start_agent "agent-1" "127.0.0.1:9090"
        ;;
    web)
        echo "=== 仅启动前端 ==="
        start_web
        ;;
    *)
        echo "用法: $0 [dev|cluster|server|agent|web]"
        echo "  dev      单节点控制面 + Agent + 前端 (默认)"
        echo "  cluster  3 节点 Raft 本地集群 + Agent + 前端"
        echo "  server   仅控制面"
        echo "  agent    仅 Agent"
        echo "  web      仅前端"
        exit 1
        ;;
esac

echo ""
echo "========================================"
echo " deepsea-ops 开发环境"
echo "========================================"
echo " 前端:     http://localhost:5173"
echo " 控制面:   http://localhost:8080"
echo " 默认账号: admin / ${ADMIN_PASSWORD}"
echo " 停止:     ./scripts/stop.sh"
echo "========================================"
