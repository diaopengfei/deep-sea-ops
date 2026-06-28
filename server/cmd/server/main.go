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
	"github.com/deepsea-ops/server/internal/eventbus"
	"github.com/deepsea-ops/server/internal/grpcserver"
	"github.com/deepsea-ops/server/internal/metrics"
	"github.com/deepsea-ops/server/internal/model"
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

	// v0.7.0: 事件总线(异步, 缓冲 256)。部署完成/扫描发现新项目/告警触发等事件经总线推送到 Webhook。
	// 启动顺序要点: 先创建总线 + 注册所有订阅者(WebhookDispatcher), 再 Start 总线,
	// 避免 Start 后到 Subscribe 前的窗口期内发布的事件被静默丢弃。
	eventBus := eventbus.New(256)

	// v0.7.0: Webhook 分发器(订阅事件总线, 按订阅过滤后异步推送, 带指数退避重试)
	// 必须在 eventBus.Start() 之前 Subscribe, 否则启动窗口的事件无订阅者会丢失。
	webhookDispatcher := api.NewWebhookDispatcher(storeInstance, eventBus)
	webhookDispatcher.Start() // 仅 Subscribe, 不消费(eventBus 未 Start)
	defer webhookDispatcher.Stop() // LIFO: 先于 eventBus.Stop 执行, 等在途推送完成

	eventBus.Start()
	defer eventBus.Stop() // LIFO: 后执行, 排空剩余事件

	// v0.7.0: 装配 Agent 离线回调, 连接断开时发布 node.offline 事件供 Webhook 推送。
	// removeAgent 仅在确实从注册表删除(非重连替换)时调用回调。
	grpcSrv.SetOnAgentOffline(func(agentID, hostname, ip string) {
		eventBus.Publish(model.Event{
			Type:      model.EventNodeOffline,
			Timestamp: time.Now(),
			Payload: map[string]interface{}{
				"agentId":  agentID,
				"hostname": hostname,
				"ip":       ip,
			},
		})
	})

	// 启动后台自动扫描调度器(每 10 分钟扫描所有在线 Agent)
	scanScheduler := scheduler.NewScheduler(storeInstance, grpcSrv, 10*time.Minute)
	// v0.7.0: 装配新项目发现回调, 扫描到此前未持久化的项目时发布事件供 Webhook 推送
	scanScheduler.SetOnNewProjects(func(agentID string, newPaths []string) {
		if len(newPaths) == 0 {
			return
		}
		eventBus.Publish(model.Event{
			Type:      model.EventScanNewProject,
			Timestamp: time.Now(),
			Payload: map[string]interface{}{
				"agentId":  agentID,
				"newPaths": newPaths,
				"count":    len(newPaths),
			},
		})
	})
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
	// v0.7.0: 用事件总线适配器包裹原通知器, 告警触发/恢复时同时发布事件供 Webhook 推送
	alertNotifier = newEventBusNotifier(alertNotifier, eventBus)
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
	// 传入 eventBus, 部署/扫描/告警等事件经总线推送到 Webhook(v0.7.0)
	handler := api.New(storeInstance, grpcSrv, authSvc, scanScheduler, metricsStore, auditStore, alertEngine, eventBus)
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

// eventBusNotifier 将告警事件桥接到事件总线(v0.7.0)。
// 实现 monitor.Notifier 接口, 告警触发/恢复时发布事件供 Webhook 推送;
// 同时委托给底层 notifier(可能为 nil), 保留原有的钉钉/飞书等通知渠道。
type eventBusNotifier struct {
	inner monitor.Notifier
	bus   *eventbus.EventBus
}

// newEventBusNotifier 用事件总线适配器包裹原通知器。
// inner 可为 nil(未配置告警 Webhook 时), 此时只发布事件不发送原通知。
func newEventBusNotifier(inner monitor.Notifier, bus *eventbus.EventBus) *eventBusNotifier {
	return &eventBusNotifier{inner: inner, bus: bus}
}

// Notify 实现 monitor.Notifier 接口。
// 先委托给原通知器(钉钉/飞书/Webhook 等), 保留 v0.6.3 行为; 再发布到事件总线供 Webhook 订阅推送。
func (n *eventBusNotifier) Notify(event monitor.AlertEvent) error {
	var err error
	if n.inner != nil {
		err = n.inner.Notify(event)
	}
	if n.bus != nil {
		eventType := model.EventAlertFiring
		if event.Status == "resolved" {
			eventType = model.EventAlertResolved
		}
		n.bus.Publish(model.Event{
			Type:      eventType,
			Timestamp: time.Now(),
			Payload: map[string]interface{}{
				"agentId":    event.AgentID,
				"ruleName":   event.Rule.Name,
				"metric":     event.Rule.Metric,
				"threshold":  event.Rule.Threshold,
				"value":      event.Value,
				"status":     event.Status,
				"firedAt":    event.FiredAt,
				"resolvedAt": event.ResolvedAt,
			},
		})
	}
	return err
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
