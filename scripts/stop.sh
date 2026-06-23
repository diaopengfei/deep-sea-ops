#!/usr/bin/env bash
# 停止 deepsea-ops 开发环境所有后台进程
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PID_DIR="$ROOT/.run"

if [[ ! -d "$PID_DIR" ]]; then
    echo "无运行记录 ($PID_DIR 不存在)"
    exit 0
fi

stopped=0
for pidfile in "$PID_DIR"/*.pid; do
    [[ -f "$pidfile" ]] || continue
    name="$(basename "$pidfile" .pid)"
    pid="$(cat "$pidfile")"
    if kill -0 "$pid" 2>/dev/null; then
        # 先尝试优雅终止, 再强杀
        kill "$pid" 2>/dev/null || true
        sleep 1
        kill -9 "$pid" 2>/dev/null || true
        echo "[$name] 已停止 (PID $pid)"
        ((stopped++))
    else
        echo "[$name] 进程已不存在 (PID $pid)"
    fi
    rm -f "$pidfile"
done

# 清理可能残留的 go run / deepsea 进程 (仅开发环境)
pkill -f "go run ./cmd/server" 2>/dev/null || true
pkill -f "go run ./cmd/agent" 2>/dev/null || true
pkill -f "deepsea-server" 2>/dev/null || true
pkill -f "deepsea-agent" 2>/dev/null || true
pkill -f "vite" 2>/dev/null || true

echo "已停止 $stopped 个进程"
