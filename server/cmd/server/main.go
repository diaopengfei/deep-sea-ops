package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"

	"google.golang.org/grpc"

	"github.com/deepsea-ops/server/internal/api"
	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/grpcserver"
	pb "github.com/deepsea-ops/server/internal/proto/agent"
	"github.com/deepsea-ops/server/internal/store"
)

func main() {
	// 命令行参数: 支持单节点(首个, join 为空)和加入已有集群(join 指向 Leader)两种模式
	nodeID := flag.String("id", "node1", "本节点 Raft ID")
	raftAddr := flag.String("raft-addr", "127.0.0.1:7000", "本节点 Raft 通信地址")
	raftDir := flag.String("raft-dir", "raft-data", "Raft 数据目录")
	joinAddr := flag.String("join", "", "已有集群 Leader 的 Raft 地址; 为空表示自己是首节点")
	httpAddr := flag.String("http", ":8080", "HTTP 监听地址")
	grpcAddr := flag.String("grpc", ":9090", "gRPC 监听地址")
	flag.Parse()

	// Raft 存储层: 创建目录、打开 bbolt、配置 Raft、bootstrap 或等待加入
	storeInstance, err := store.NewStore(*raftDir, *nodeID, *raftAddr, *joinAddr)
	if err != nil {
		log.Fatalf("启动 Raft 失败: %v", err)
	}

	// JWT 密钥: 生产环境必须通过 JWT_SECRET 环境变量设置
	// auth 包在加载时已从环境变量读取, 这里仅做检查并提醒
	if os.Getenv("JWT_SECRET") == "" {
		log.Println("[警告] JWT_SECRET 未设置, 使用默认密钥(仅限开发环境)")
	}

	// 鉴权服务: 注入 store, 用户数据走 Raft 保证多节点一致
	authSvc := auth.New(storeInstance)

	// 初始管理员账号: 首次启动且无 admin 时自动创建
	// 密码从 ADMIN_PASSWORD 环境变量读, 默认 admin123 并警告
	if _, ok := storeInstance.GetUser("admin"); !ok {
		password := auth.InitAdminPassword()
		if err := authSvc.CreateAdmin(password); err != nil {
			log.Fatalf("初始化 admin 用户失败: %v", err)
		}
		log.Println("已创建初始管理员账号 admin(请尽快修改密码)")
	}

	// gRPC 服务端: 供 Agent 连接, 建立双向流上报心跳/接收指令
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

	// HTTP 路由: 所有 /api/ 受 JWT 中间件保护(除 /api/login 等白名单)
	handler := api.New(storeInstance, grpcSrv, authSvc)
	log.Printf("HTTP(供前端访问)监听 %s", *httpAddr)
	log.Fatal(http.ListenAndServe(*httpAddr, handler))
}