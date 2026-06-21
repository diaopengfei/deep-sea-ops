# deepsea-ops Makefile
# 构建、开发、检查的统一入口。产物统一输出到 dist/, 不污染源码目录。

GO          ?= go
GOFMT       ?= $(GO) fmt
GOOS        ?= $(shell go env GOOS)
GOARCH      ?= $(shell go env GOARCH)
DIST_DIR    := dist
SERVER_BIN  := $(DIST_DIR)/deepsea-server
AGENT_BIN   := $(DIST_DIR)/deepsea-agent

.PHONY: all build server agent web clean dev check fmt vet help

all: build

## build: 构建后端(server + agent)和前端
build: server agent web

## server: 构建控制面到 dist/deepsea-server
server:
	@mkdir -p $(DIST_DIR)
	cd server && CGO_ENABLED=0 $(GO) build -ldflags="-s -w" -o ../$(SERVER_BIN) ./cmd/server

## agent: 构建 Agent 到 dist/deepsea-agent
agent:
	@mkdir -p $(DIST_DIR)
	cd server && CGO_ENABLED=0 $(GO) build -ldflags="-s -w" -o ../$(AGENT_BIN) ./cmd/agent

## web: 构建前端到 web/dist/
web:
	cd web && npm install && npm run build

## dev: 启动后端和前端开发服务(需两个终端, 或各自后台)
dev:
	@echo "在两个终端分别运行:"
	@echo "  cd server && $(GO) run ./cmd/server"
	@echo "  cd web && npm run dev"

## check: 格式化 + 静态检查
check: fmt vet

## fmt: 格式化 Go 代码
fmt:
	cd server && $(GOFMT) ./...

## vet: 静态检查
vet:
	cd server && $(GO) vet ./...

## clean: 清理构建产物
clean:
	rm -rf $(DIST_DIR) web/dist

## cross-linux: 交叉编译 Linux amd64(从 Windows/Mac 开发机)
cross-linux:
	@mkdir -p $(DIST_DIR)
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -ldflags="-s -w" -o ../$(SERVER_BIN) ./cmd/server
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -ldflags="-s -w" -o ../$(AGENT_BIN) ./cmd/agent

help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'