package main

import (
	"flag"
	"log"
	"net"
	"net/http"

	"google.golang.org/grpc"

	"github.com/deepsea-ops/server/internal/api"
	"github.com/deepsea-ops/server/internal/grpcserver"
	pb "github.com/deepsea-ops/server/internal/proto/agent"
	"github.com/deepsea-ops/server/internal/store"
)

func main() {
	// 命令行参数: 支持单节点(首个)和加入已有集群两种模式
	nodeID := flag.String("id", "node1", "本节点 Raft ID")
	raftAddr := flag.String("raft-addr", "127.0.0.1:7000", "本节点 Raft 通信地址")
	raftDir := flag.String("raft-dir", "raft-data", "Raft 数据目录")
	joinAddr := flag.String("join", "", "已有集群 Leader 的 Raft 地址; 为空表示自己是首节点")
	httpAddr := flag.String("http", ":8080", "HTTP 监听地址")
	grpcAddr := flag.String("grpc", ":9090", "gRPC 监听地址")
	flag.Parse()

	// Raft 存储层
	storeInstance, err := store.NewStore(*raftDir, *nodeID, *raftAddr, *joinAddr)
	if err != nil {
		log.Fatalf("启动 Raft 失败: %v", err)
	}

	// gRPC 服务端: 接收 Agent 连接
	grpcSrv := grpcserver.NewServer()
	grpcLis, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		log.Fatalf("监听 gRPC %s 失败: %v", *grpcAddr, err)
	}
	g := grpc.NewServer()
	pb.RegisterAgentServiceServer(g, grpcSrv)
	go func() {
		log.Printf("gRPC(供 Agent 连接)监听 %s", *grpcAddr)
		if err := g.Serve(grpcLis); err != nil {
			log.Fatalf("gRPC 服务退出: %v", err)
		}
	}()

	// HTTP 路由: 供前端访问
	handler := api.New(storeInstance, grpcSrv)
	log.Printf("HTTP(供前端访问)监听 %s", *httpAddr)
	log.Fatal(http.ListenAndServe(*httpAddr, handler))
}