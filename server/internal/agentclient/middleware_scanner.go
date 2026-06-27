package agentclient

import (
	"strconv"
	"strings"
)

// v0.6.7 中间件类型常量(扩展 ProjectType)
// 复用 ProjectInfo / ProjectRecord 持久化管线, 中间件作为特殊"项目"展示,
// Path 字段为二进制路径, ConfigFiles 为常见配置文件路径(可能不存在)。
const (
	MiddlewareRedis         ProjectType = "redis"
	MiddlewarePostgreSQL    ProjectType = "postgresql"
	MiddlewareMySQL         ProjectType = "mysql"
	MiddlewareKafka         ProjectType = "kafka"
	MiddlewareZookeeper     ProjectType = "zookeeper"
	MiddlewareElasticsearch ProjectType = "elasticsearch"
	MiddlewareClickHouse    ProjectType = "clickhouse"
)

// middlewareSignature 描述一种中间件的进程识别特征。
// 识别策略: 进程命令行(CmdLine)或进程名(Name)小写包含任一关键字即视为该中间件。
type middlewareSignature struct {
	Type            ProjectType
	ProcessKeywords []string // 进程名/cmdline 关键字(小写匹配)
	DefaultConfigs  []string // 常见配置文件路径(仅作展示, 不强制存在)
	DefaultPort     int      // 默认端口(仅作展示, 实际端口需读配置)
}

// middlewareSignatures 内置常见中间件识别特征。
// 新增中间件只需在此列表追加一项, 无需改其他代码。
var middlewareSignatures = []middlewareSignature{
	{
		Type:            MiddlewareRedis,
		ProcessKeywords: []string{"redis-server"},
		DefaultConfigs:  []string{"/etc/redis/redis.conf", "/usr/local/etc/redis/redis.conf"},
		DefaultPort:     6379,
	},
	{
		Type:            MiddlewarePostgreSQL,
		ProcessKeywords: []string{"postgres", "postmaster"},
		DefaultConfigs:  []string{"/etc/postgresql/postgresql.conf", "/var/lib/pgsql/data/postgresql.conf"},
		DefaultPort:     5432,
	},
	{
		Type:            MiddlewareMySQL,
		ProcessKeywords: []string{"mysqld", "mariadbd"},
		DefaultConfigs:  []string{"/etc/my.cnf", "/etc/mysql/my.cnf"},
		DefaultPort:     3306,
	},
	{
		Type:            MiddlewareKafka,
		ProcessKeywords: []string{"kafka.kafka"}, // Kafka 进程主类名
		DefaultConfigs:  []string{"/opt/kafka/config/server.properties", "/etc/kafka/server.properties"},
		DefaultPort:     9092,
	},
	{
		Type:            MiddlewareZookeeper,
		ProcessKeywords: []string{"quorumpeermain"}, // Zookeeper 进程主类名
		DefaultConfigs:  []string{"/opt/zookeeper/conf/zoo.cfg", "/etc/zookeeper/zoo.cfg"},
		DefaultPort:     2181,
	},
	{
		Type:            MiddlewareElasticsearch,
		ProcessKeywords: []string{"org.elasticsearch.bootstrap.elasticsearch"},
		DefaultConfigs:  []string{"/etc/elasticsearch/elasticsearch.yml"},
		DefaultPort:     9200,
	},
	{
		Type:            MiddlewareClickHouse,
		ProcessKeywords: []string{"clickhouse-server", "clickhouse"},
		DefaultConfigs:  []string{"/etc/clickhouse-server/config.xml", "/etc/clickhouse-server/users.xml"},
		DefaultPort:     8123,
	},
}

// IsMiddlewareType 判断项目类型是否为中间件(供持久化层/前端判断使用)。
func IsMiddlewareType(t ProjectType) bool {
	switch t {
	case MiddlewareRedis, MiddlewarePostgreSQL, MiddlewareMySQL,
		MiddlewareKafka, MiddlewareZookeeper, MiddlewareElasticsearch, MiddlewareClickHouse:
		return true
	}
	return false
}

// ScanMiddlewares 从进程列表识别中间件, 返回 ProjectInfo 列表。
//
// 识别逻辑: 遍历进程, 按 middlewareSignatures 匹配 cmdline/name,
// 命中即生成一条 ProjectInfo(Type=中间件类型, Running=true, PID=进程 PID)。
// 同 type+binaryPath 的多个实例只保留一条(防止同二进制多进程重复展示)。
//
// 与 ScanProjects 的关系: ScanProjects 基于文件系统识别 Java/Python 项目,
// 本函数基于进程列表识别中间件, 二者结果在 client.go SCAN_PROJECTS 处合并。
func ScanMiddlewares(processes []RunningProcess) []ProjectInfo {
	seen := make(map[string]bool) // key = type + "|" + binPath, 去重
	out := make([]ProjectInfo, 0, len(processes))

	for _, p := range processes {
		sig := matchMiddleware(p)
		if sig == nil {
			continue
		}
		binPath := extractBinaryPath(p.CmdLine)
		key := string(sig.Type) + "|" + binPath
		if seen[key] {
			continue
		}
		seen[key] = true

		// 名称: 类型名 + 端口(端口优先从 cmdline 解析, 失败用默认端口)
		port := extractPortFromCmdline(p.CmdLine, sig.DefaultPort)
		name := string(sig.Type)
		if port > 0 {
			name = name + "-" + strconv.Itoa(port)
		}

		out = append(out, ProjectInfo{
			Path:        binPath,
			Type:        sig.Type,
			Name:        name,
			ConfigFiles: sig.DefaultConfigs,
			Running:     true, // 进程存在即视为运行中
			PID:         p.PID,
		})
	}
	return out
}

// matchMiddleware 在签名表中查找首个匹配的中间件, 无匹配返回 nil。
func matchMiddleware(p RunningProcess) *middlewareSignature {
	cmd := strings.ToLower(p.CmdLine)
	name := strings.ToLower(p.Name)
	for i := range middlewareSignatures {
		sig := &middlewareSignatures[i]
		for _, kw := range sig.ProcessKeywords {
			k := strings.ToLower(kw)
			if strings.Contains(cmd, k) || strings.Contains(name, k) {
				return sig
			}
		}
	}
	return nil
}

// extractBinaryPath 从 cmdline 提取二进制路径(第一个 token)。
// 例: "/usr/bin/redis-server /etc/redis/redis.conf" → "/usr/bin/redis-server"
// 失败(无空格)返回原 cmdline。
func extractBinaryPath(cmdline string) string {
	cmdline = strings.TrimSpace(cmdline)
	if cmdline == "" {
		return cmdline
	}
	// 处理引号包裹的路径
	if cmdline[0] == '"' {
		if end := strings.IndexByte(cmdline[1:], '"'); end >= 0 {
			return cmdline[1 : 1+end]
		}
	}
	if sp := strings.IndexByte(cmdline, ' '); sp > 0 {
		return cmdline[:sp]
	}
	return cmdline
}

// extractPortFromCmdline 从 cmdline 中尝试提取端口号。
// 支持: --port=6379 / --port 6379 / -p 6379 / --server.port=9092 / --http.port=9200
// 解析失败返回 defaultPort。
func extractPortFromCmdline(cmdline string, defaultPort int) int {
	tokens := strings.Fields(cmdline)
	for i, tok := range tokens {
		// --port=6379 / --server.port=9092 形式
		if eq := strings.IndexByte(tok, '='); eq > 0 {
			flag := strings.ToLower(tok[:eq])
			val := tok[eq+1:]
			if isPortFlag(flag) {
				if port, err := strconv.Atoi(val); err == nil && port > 0 {
					return port
				}
			}
			continue
		}
		// --port 6379 / -p 6379 形式(下一 token 为值)
		flag := strings.ToLower(tok)
		if isPortFlag(flag) && i+1 < len(tokens) {
			if port, err := strconv.Atoi(tokens[i+1]); err == nil && port > 0 {
				return port
			}
		}
	}
	return defaultPort
}

// isPortFlag 判断 flag 名是否表示端口。
func isPortFlag(flag string) bool {
	switch flag {
	case "--port", "-p", "--server.port", "--http.port", "--transport.tcp.port",
		"--clickhouse.http_port", "--postgresql.port":
		return true
	}
	return false
}
