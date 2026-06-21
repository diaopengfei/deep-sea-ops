package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/deepsea-ops/server/internal/agentclient"
)

func main() {
	// 命令行参数: Agent ID 和控制面地址
	agentID := flag.String("id", "agent-1", "Agent 唯一 ID")
	serverAddr := flag.String("server", "127.0.0.1:9090", "控制面 gRPC 地址")
	flag.Parse()

	c, err := agentclient.New(*agentID, *serverAddr)
	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}
	defer c.Close()

	// 监听 Ctrl-C, 优雅退出
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("收到退出信号, 关闭 Agent")
		cancel()
	}()

	log.Printf("Agent %s 启动, 连接 %s", *agentID, *serverAddr)
	if err := c.Run(ctx); err != nil {
		log.Printf("Agent 退出: %v", err)
	}
}