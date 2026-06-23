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
	"github.com/deepsea-ops/server/internal/config"
	"github.com/deepsea-ops/server/internal/grpcserver"
	pb "github.com/deepsea-ops/server/internal/proto/agent"
	"github.com/deepsea-ops/server/internal/store"
)

func main() {
	// -config 指定配置文件路径, 为空则默认查找 ./config/server.yaml
	configPath := flag.String("config", "", "配置文件路径 (默认: config/server.yaml)")
	flag.Parse()

	// 加载配置: 配置文件不存在时用内置默认值(向后兼容)
	cfg, err := config.LoadServer(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	log.Printf("配置加载完成: node_id=%s, raft=%s, http=%s, grpc=%s",
		cfg.NodeID, cfg.Raft.Addr, cfg.HTTP.Addr, cfg.GRPC.Addr)

	// Raft 存储层: 创建目录、打开 bbolt、配置 Raft、bootstrap 或等待加入
	storeInstance, err := store.NewStore(cfg.Raft.DataDir, cfg.NodeID, cfg.Raft.Addr, cfg.Raft.Join)
	if err != nil {
		log.Fatalf("启动 Raft 失败: %v", err)
	}

	// JWT 密钥: 生产环境必须通过 JWT_SECRET 环境变量设置
	if os.Getenv("JWT_SECRET") == "" {
		log.Println("[警告] JWT_SECRET 未设置, 使用默认密钥(仅限开发环境)")
	}

	// 鉴权服务: 注入 store, 用户数据走 Raft 保证多节点一致
	authSvc := auth.New(storeInstance)

	// 初始管理员账号: 首次启动且无 admin 时自动创建
	if _, ok := storeInstance.GetUser("admin"); !ok {
		password := auth.InitAdminPassword()
		if err := authSvc.CreateAdmin(password); err != nil {
			log.Fatalf("初始化 admin 用户失败: %v", err)
		}
		log.Println("已创建初始管理员账号 admin(请尽快修改密码)")
	}

	// gRPC 服务端: 供 Agent 连接, 建立双向流上报心跳/接收指令
	grpcSrv := grpcserver.NewServer()
	grpcLis, err := net.Listen("tcp", cfg.GRPC.Addr)
	if err != nil {
		log.Fatalf("监听 gRPC %s 失败: %v", cfg.GRPC.Addr, err)
	}
	g := grpc.NewServer()
	pb.RegisterAgentServiceServer(g, grpcSrv)
	go func() {
		log.Printf("gRPC(供 Agent 连接)监听 %s", cfg.GRPC.Addr)
		if err := g.Serve(grpcLis); err != nil {
			log.Fatalf("gRPC 服务退出: %v", err)
		}
	}()

	// HTTP 路由: 所有 /api/ 受 JWT 中间件保护(除 /api/login 等白名单)
	handler := api.New(storeInstance, grpcSrv, authSvc)
	log.Printf("HTTP(供前端访问)监听 %s", cfg.HTTP.Addr)
	log.Fatal(http.ListenAndServe(cfg.HTTP.Addr, handler))
}
