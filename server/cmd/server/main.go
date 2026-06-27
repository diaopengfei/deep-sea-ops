package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/deepsea-ops/server/internal/api"
	"github.com/deepsea-ops/server/internal/audit"
	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/config"
	"github.com/deepsea-ops/server/internal/crypto"
	"github.com/deepsea-ops/server/internal/grpcserver"
	"github.com/deepsea-ops/server/internal/metrics"
	"github.com/deepsea-ops/server/internal/monitor"
	pb "github.com/deepsea-ops/server/internal/proto/agent"
	"github.com/deepsea-ops/server/internal/scheduler"
	"github.com/deepsea-ops/server/internal/store"
)

// 内置默认值 (与 config.DefaultSecurityConfig 保持一致, 用于检测是否仍为默认值)
const (
	defaultJWTSecret = "deepsea-dev-secret-change-me"
)

func main() {
	// -config 指定配置文件路径, 为空则默认查找 ./config/server.yaml
	configPath := flag.String("config", "", "配置文件路径 (默认: config/server.yaml)")
	flag.Parse()

	// 加载配置: 配置文件不存在时用内置默认值(向后兼容)
	// 优先级: 环境变量 > YAML 配置 > 内置默认值
	cfg, err := config.LoadServer(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	log.Printf("配置加载完成: node_id=%s, raft=%s, http=%s, grpc=%s",
		cfg.NodeID, cfg.Raft.Addr, cfg.HTTP.Addr, cfg.GRPC.Addr)

	// 安全配置校验与警告
	validateSecurityConfig(cfg)

	// 初始化 JWT 密钥 (多节点必须一致, 否则入口代理转发请求时鉴权失败)
	auth.InitJWTSecret(cfg.Security.JWTSecret)

	// 初始化 SSH 凭据加密主密钥 (多节点必须一致, 否则 Follower 无法解密 Raft 中的凭据)
	crypto.InitFromSecurityConfig(cfg.Security)

	// Raft 存储层: 创建目录、打开 bbolt、配置 Raft、bootstrap 或等待加入
	storeInstance, err := store.NewStore(cfg.Raft.DataDir, cfg.NodeID, cfg.Raft.Addr, cfg.Raft.Join)
	if err != nil {
		log.Fatalf("启动 Raft 失败: %v", err)
	}
	defer storeInstance.Close()

	// 鉴权服务: 注入 store, 用户数据走 Raft 保证多节点一致
	authSvc := auth.New(storeInstance)

	// v0.6.4: 操作审计日志(独立 bbolt 文件, 不进 Raft, 追加写)
	auditStore, err := audit.New(filepath.Join(cfg.Raft.DataDir, "audit.db"))
	if err != nil {
		log.Fatalf("打开审计日志库失败: %v", err)
	}
	defer auditStore.Close()

	// 初始管理员账号: 首次启动且无 admin 时自动创建
	// ADMIN_PASSWORD 仅在此处生效, admin 创建后密码 hash 存 Raft 复制到所有节点
	if _, ok := storeInstance.GetUser("admin"); !ok {
		if err := authSvc.CreateAdmin(cfg.Security.AdminPassword); err != nil {
			log.Fatalf("初始化 admin 用户失败: %v", err)
		}
		log.Println("已创建初始管理员账号 admin(请尽快修改密码)")
	}

	// gRPC 服务端: 供 Agent 连接, 建立双向流上报心跳/接收指令
	grpcSrv := grpcserver.NewServer()
	// v0.6.3: 指标存储(内存环形缓冲), 心跳的 CPU/内存写入最新值, 完整指标由采集器写入
	metricsStore := metrics.NewStore(metrics.DefaultCapacity)
	grpcSrv.SetMetricsStore(metricsStore)
	grpcLis, err := net.Listen("tcp", cfg.GRPC.Addr)
	if err != nil {
		log.Fatalf("监听 gRPC %s 失败: %v", cfg.GRPC.Addr, err)
	}
	g := grpc.NewServer()
	pb.RegisterAgentServiceServer(g, grpcSrv)
	go func() {
		log.Printf("gRPC(供 Agent 连接)监听 %s", cfg.GRPC.Addr)
		if err := g.Serve(grpcLis); err != nil {
			// 不用 log.Fatalf(会跳过 defer), 用 GracefulStop + 通知 main 退出
			log.Printf("gRPC 服务退出: %v", err)
			g.GracefulStop()
		}
	}()

	// 启动后台自动扫描调度器(每 10 分钟扫描所有在线 Agent)
	scanScheduler := scheduler.NewScheduler(storeInstance, grpcSrv, 10*time.Minute)
	scanScheduler.Start()
	defer scanScheduler.Stop()

	// v0.6.3: 资源监控采集器(每 30s 向在线 Agent 采集完整指标, 存环形缓冲)
	collectInterval := time.Duration(cfg.Monitor.CollectIntervalSec) * time.Second
	metricsCollector := monitor.NewCollector(grpcSrv, metricsStore, collectInterval, 10*time.Second)
	metricsCollector.Start()
	defer metricsCollector.Stop()

	// v0.6.3: 告警引擎(按规则评估指标, 触发走 Webhook; 仅配置 Webhook 时发通知)
	var alertNotifier monitor.Notifier
	if cfg.Monitor.WebhookURL != "" {
		alertNotifier = monitor.NewWebhookNotifier(monitor.WebhookConfig{
			Type: cfg.Monitor.WebhookType,
			URL:  cfg.Monitor.WebhookURL,
		})
	}
	alertRules := make([]monitor.Rule, 0, len(cfg.Monitor.Rules))
	for _, r := range cfg.Monitor.Rules {
		alertRules = append(alertRules, monitor.Rule{
			Name: r.Name, Metric: r.Metric, Threshold: r.Threshold, DurationSec: r.DurationSec,
		})
	}
	alertEngine := monitor.NewAlertEngine(metricsStore, alertRules, alertNotifier, collectInterval)
	alertEngine.Start()
	defer alertEngine.Stop()

	// HTTP 路由: 所有 /api/ 受 JWT 中间件保护(除 /api/login 等白名单)
	// 传入 scanScheduler, 部署成功后自动触发目标 Agent 扫描
	handler := api.New(storeInstance, grpcSrv, authSvc, scanScheduler, metricsStore, auditStore)
	httpSrv := &http.Server{
		Addr:    cfg.HTTP.Addr,
		Handler: handler,
	}

	// HTTP 服务用 goroutine 启动, main goroutine 监听信号做优雅关闭
	go func() {
		log.Printf("HTTP(供前端访问)监听 %s", cfg.HTTP.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP 服务异常退出: %v", err)
			os.Exit(1)
		}
	}()

	// 优雅关闭: 收到 SIGINT/SIGTERM 时停止 HTTP/gRPC/扫描器, defer 链正常执行
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)
	<-stopCh
	log.Println("收到退出信号, 开始优雅关闭...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP 关闭出错: %v", err)
	}
	g.GracefulStop()
	log.Println("已关闭, 退出")
}

// validateSecurityConfig 校验安全配置, 对不安全的情况打印警告。
// 多节点 Raft 集群中, JWT_SECRET 和 MASTER_KEY 必须在所有节点保持一致。
func validateSecurityConfig(cfg config.ServerConfig) {
	// JWT_SECRET 检查
	if cfg.Security.JWTSecret == defaultJWTSecret {
		log.Println("[警告] JWT_SECRET 仍为默认值, 生产环境必须修改 (多节点集群必须保持一致)")
	}

	// MASTER_KEY 检查
	if cfg.Security.MasterKey == "" {
		log.Println("[警告] MASTER_KEY 未设置, 使用随机密钥 (仅限开发, 重启后已加密凭据无法解密)")
		log.Println("[警告] 多节点 Raft 集群必须设置同一 MASTER_KEY, 否则 Follower 无法解密凭据")
	}

	// 多节点场景下的强警告: join 模式表示加入已有集群
	if cfg.Raft.Join != "" {
		if cfg.Security.JWTSecret == defaultJWTSecret {
			log.Println("[严重警告] 加入已有集群但 JWT_SECRET 为默认值, 鉴权将失败!")
		}
		if cfg.Security.MasterKey == "" {
			log.Println("[严重警告] 加入已有集群但 MASTER_KEY 未设置, 无法解密 Raft 中的 SSH 凭据!")
		}
	}
}
