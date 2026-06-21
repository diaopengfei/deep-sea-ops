package main

import (
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
	// Raft 存储层
	storeInstance, err := store.NewStore("raft-data", "127.0.0.1:7000")
	if err != nil {
		log.Fatalf("启动 Raft 失败: %v", err)
	}

	// gRPC 服务端: 接收 Agent 连接
	grpcSrv := grpcserver.NewServer()
	grpcLis, err := net.Listen("tcp", ":9090")
	if err != nil {
		log.Fatalf("监听 gRPC 9090 失败: %v", err)
	}
	g := grpc.NewServer()
	pb.RegisterAgentServiceServer(g, grpcSrv)
	go func() {
		log.Println("gRPC(供 Agent 连接)监听 :9090")
		if err := g.Serve(grpcLis); err != nil {
			log.Fatalf("gRPC 服务退出: %v", err)
		}
	}()

	// HTTP 路由: 供前端访问
	handler := api.New(storeInstance, grpcSrv)
	log.Println("HTTP(供前端访问)监听 :8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}