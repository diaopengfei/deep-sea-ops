# deepsea-ops Makefile
# 閺嬪嫬缂撻妴浣哥磻閸欐垯鈧焦顥呴弻銉ф畱缂佺喍绔撮崗銉ュ經閵嗗倷楠囬悧鈺冪埠娑撯偓鏉堟挸鍤崚?dist/, 娑撳秵钖勯弻鎾寸爱閻胶娲拌ぐ鏇樷偓?
GO          ?= go
GOFMT       ?= $(GO) fmt
GOOS        ?= $(shell go env GOOS)
GOARCH      ?= $(shell go env GOARCH)
DIST_DIR    := dist
SERVER_BIN  := $(DIST_DIR)/deepsea-server
AGENT_BIN   := $(DIST_DIR)/deepsea-agent

.PHONY: all build build-linux server agent web clean dev check fmt vet help

all: build

## build: 閺嬪嫬缂撻崥搴ｎ伂(server + agent)閸滃苯澧犵粩?build: server agent web

build-linux: cross-linux web

## server: 閺嬪嫬缂撻幒褍鍩楅棃銏犲煂 dist/deepsea-server
server:
	@mkdir -p $(DIST_DIR)
	cd server && CGO_ENABLED=0 $(GO) build -buildvcs=false -ldflags="-s -w" -o ../$(SERVER_BIN) ./cmd/server

## agent: 閺嬪嫬缂?Agent 閸?dist/deepsea-agent
agent:
	@mkdir -p $(DIST_DIR)
	cd server && CGO_ENABLED=0 $(GO) build -buildvcs=false -ldflags="-s -w" -o ../$(AGENT_BIN) ./cmd/agent

## web: 閺嬪嫬缂撻崜宥囶伂閸?web/dist/
web:
	cd web && npm install && npm run build

## dev: 閸氼垰濮╅崥搴ｎ伂閸滃苯澧犵粩顖氱磻閸欐垶婀囬崝?闂団偓娑撱倓閲滅紒鍫㈩伂, 閹存牕鎮囬懛顏勬倵閸?
dev:
	@echo "閸︺劋琚辨稉顏嗙矒缁旑垰鍨庨崚顐ョ箥鐞?"
	@echo "  cd server && $(GO) run ./cmd/server"
	@echo "  cd web && npm run dev"

## check: 閺嶇厧绱￠崠?+ 闂堟瑦鈧焦顥呴弻?check: fmt vet

## fmt: 閺嶇厧绱￠崠?Go 娴狅絿鐖?fmt:
	cd server && $(GOFMT) ./...

## vet: 闂堟瑦鈧焦顥呴弻?vet:
	cd server && $(GO) vet ./...

## clean: 濞撳懐鎮婇弸鍕紦娴溠呭⒖
clean:
	rm -rf $(DIST_DIR) web/dist

## cross-linux: 娴溿倕寮剁紓鏍槯 Linux amd64(娴?Windows/Mac 瀵偓閸欐垶婧€)
cross-linux:
	@mkdir -p $(DIST_DIR)
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -buildvcs=false -ldflags="-s -w" -o ../$(SERVER_BIN) ./cmd/server
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -buildvcs=false -ldflags="-s -w" -o ../$(AGENT_BIN) ./cmd/agent

help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'