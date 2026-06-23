package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/deepsea-ops/server/internal/agentclient"
	"github.com/deepsea-ops/server/internal/config"
)

func main() {
	// -config 指定配置文件路径, 为空则默认查找 ./config/agent.yaml
	configPath := flag.String("config", "", "配置文件路径 (默认: config/agent.yaml)")
	flag.Parse()

	// 加载配置: 配置文件不存在时用内置默认值(向后兼容)
	cfg, err := config.LoadAgent(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	log.Printf("配置加载完成: agent_id=%s, server=%s", cfg.AgentID, cfg.Server)

	c, err := agentclient.New(cfg.AgentID, cfg.Server)
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

	log.Printf("Agent %s 启动, 连接 %s", cfg.AgentID, cfg.Server)
	if err := c.Run(ctx); err != nil {
		log.Printf("Agent 退出: %v", err)
	}
}
