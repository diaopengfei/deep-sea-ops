package ops

import (
	"fmt"
	"time"

	"github.com/deepsea-ops/server/internal/platform"
)

// MiddlewareOps 中间件状态查询接口(v0.6.7 方向 6)。
//
// 设计目的: 为每种中间件提供平台无关的状态查询命令, 复用 Executor 执行。
// 当前作为扩展点提供(已注入到 Ops 聚合体), 后续可接入新的 gRPC 指令/API/前端。
//
// 命令构造原则: 中间件 CLI(redis-cli/psql/curl 等)本身跨平台一致,
// 因此不进 CommandBuilder 接口(避免每个平台 Builder 都要实现一遍),
// 而是直接在此处构造 platform.Command, 由 Executor 执行。
//
// 扩展新中间件: 在此接口追加方法 + 实现, 不影响现有调用方。
type MiddlewareOps interface {
	// RedisInfo 生成 redis-cli INFO 命令(返回服务端信息: 版本/内存/客户端/主从等)
	RedisInfo(host string, port int, password string) platform.Command
	// PostgresStatus 生成 psql 版本查询命令
	PostgresStatus(host string, port int, user, password, database string) platform.Command
	// MySQLStatus 生成 mysqladmin status 命令(返回运行时长/线程数/QPS 等)
	MySQLStatus(host string, port int, user, password string) platform.Command
	// KafkaTopics 生成 kafka-topics.sh --list 命令(列出所有 topic)
	KafkaTopics(bootstrapServer string) platform.Command
	// ESHealth 生成 curl _cluster/health 命令(集群健康状态)
	ESHealth(host string, port int) platform.Command
	// ClickHouseStatus 生成 clickhouse-client SELECT version() 命令
	ClickHouseStatus(host string, port int) platform.Command
}

type middlewareOps struct {
	builder  platform.CommandBuilder
	executor platform.Executor
}

func newMiddlewareOps(b platform.CommandBuilder, e platform.Executor) *middlewareOps {
	return &middlewareOps{builder: b, executor: e}
}

// 短超时: 状态查询不应长时间阻塞, 默认 10 秒
const middlewareCmdTimeout = 10 * time.Second

// RedisInfo 生成 redis-cli INFO 命令。
// 输出示例: redis_version:7.0.0 / used_memory_human:1.5M / connected_clients:5
func (o *middlewareOps) RedisInfo(host string, port int, password string) platform.Command {
	if host == "" {
		host = "127.0.0.1"
	}
	if port <= 0 {
		port = 6379
	}
	args := []string{"-h", host, "-p", fmt.Sprintf("%d", port)}
	if password != "" {
		// 注意: -a 会在日志中产生警告(明文密码), 生产建议用 ACL FILE 或 --pass-stdin
		args = append(args, "-a", password, "--no-auth-warning")
	}
	args = append(args, "INFO")
	return platform.Command{Name: "redis-cli", Args: args, Timeout: middlewareCmdTimeout}
}

// PostgresStatus 生成 psql 版本查询命令。
// 通过 PGPASSWORD 环境变量传密码(避免命令行明文), 但此处简化为命令行参数,
// 生产环境建议改为 .pgpass 或连接池配置。
func (o *middlewareOps) PostgresStatus(host string, port int, user, password, database string) platform.Command {
	if host == "" {
		host = "127.0.0.1"
	}
	if port <= 0 {
		port = 5432
	}
	if user == "" {
		user = "postgres"
	}
	if database == "" {
		database = "postgres"
	}
	connStr := fmt.Sprintf("host=%s port=%d user=%s dbname=%s", host, port, user, database)
	if password != "" {
		connStr += " password=" + password
	}
	return platform.Command{
		Name:    "psql",
		Args:    []string{connStr, "-c", "SELECT version();"},
		Timeout: middlewareCmdTimeout,
	}
}

// MySQLStatus 生成 mysqladmin status 命令。
// 输出示例: Uptime: 3600  Threads: 5  Questions: 1234  Slow queries: 0
func (o *middlewareOps) MySQLStatus(host string, port int, user, password string) platform.Command {
	if host == "" {
		host = "127.0.0.1"
	}
	if port <= 0 {
		port = 3306
	}
	if user == "" {
		user = "root"
	}
	args := []string{
		"-h", host,
		"-P", fmt.Sprintf("%d", port),
		"-u", user,
	}
	if password != "" {
		args = append(args, "-p"+password)
	}
	args = append(args, "status")
	return platform.Command{Name: "mysqladmin", Args: args, Timeout: middlewareCmdTimeout}
}

// KafkaTopics 生成 kafka-topics.sh --list 命令。
// bootstrapServer 格式: host:port(默认 127.0.0.1:9092)
func (o *middlewareOps) KafkaTopics(bootstrapServer string) platform.Command {
	if bootstrapServer == "" {
		bootstrapServer = "127.0.0.1:9092"
	}
	return platform.Command{
		Name:    "kafka-topics.sh",
		Args:    []string{"--list", "--bootstrap-server", bootstrapServer},
		Timeout: middlewareCmdTimeout,
	}
}

// ESHealth 生成 curl _cluster/health 命令。
// 输出 JSON: {"status":"green","number_of_nodes":3,...}
func (o *middlewareOps) ESHealth(host string, port int) platform.Command {
	if host == "" {
		host = "127.0.0.1"
	}
	if port <= 0 {
		port = 9200
	}
	url := fmt.Sprintf("http://%s:%d/_cluster/health?pretty", host, port)
	return platform.Command{
		Name:    "curl",
		Args:    []string{"-s", "--max-time", "8", url},
		Timeout: middlewareCmdTimeout,
	}
}

// ClickHouseStatus 生成 clickhouse-client SELECT version() 命令。
func (o *middlewareOps) ClickHouseStatus(host string, port int) platform.Command {
	if host == "" {
		host = "127.0.0.1"
	}
	if port <= 0 {
		port = 8123
	}
	return platform.Command{
		Name:    "clickhouse-client",
		Args:    []string{"--host", host, "--port", fmt.Sprintf("%d", port), "--query", "SELECT version()"},
		Timeout: middlewareCmdTimeout,
	}
}
