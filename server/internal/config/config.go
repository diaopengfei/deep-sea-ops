// Package config 提供 YAML 配置文件加载能力。
//
// 设计参考 Kafka / Elasticsearch 的配置方式:
//   - 默认查找 ./config/server.yaml (或 agent.yaml)
//   - 可通过 -config flag 指定任意路径
//   - 配置文件不存在时使用内置默认值(向后兼容)
//   - 环境变量优先级最高(JWT_SECRET / ADMIN_PASSWORD / MASTER_KEY 等)
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ServerConfig 是控制面(deepsea-server)的配置。
type ServerConfig struct {
	NodeID   string         `yaml:"node_id"`  // Raft 节点 ID
	Raft     RaftConfig     `yaml:"raft"`     // Raft 相关配置
	HTTP     ListenConfig   `yaml:"http"`     // HTTP 监听
	GRPC     ListenConfig   `yaml:"grpc"`     // gRPC 监听
	Security SecurityConfig `yaml:"security"` // 安全相关配置 (v0.5.1+)
}

// RaftConfig 是 Raft 相关配置。
type RaftConfig struct {
	Addr    string `yaml:"addr"`     // Raft 通信地址, 如 127.0.0.1:7000
	DataDir string `yaml:"data_dir"` // Raft 数据目录
	Join    string `yaml:"join"`     // 已有集群 Leader 的 Raft 地址; 为空表示首节点
}

// ListenConfig 是监听地址配置。
type ListenConfig struct {
	Addr string `yaml:"addr"` // 监听地址, 如 :8080
}

// SecurityConfig 是安全相关配置。
// 三个字段在多节点 Raft 集群中必须保持一致:
//   - JWT_SECRET:   入口代理转发请求到任意节点, Token 必须被所有节点验证通过
//   - MASTER_KEY:   SSH 凭据加密后存 Raft 复制到所有节点, 解密必须用同一密钥
//   - ADMIN_PASSWORD: 仅首节点首次启动创建 admin 时生效, 不要求一致
//
// 优先级: 环境变量 > YAML 配置 > 内置默认值。
type SecurityConfig struct {
	JWTSecret     string `yaml:"jwt_secret"`     // JWT 签名密钥 (生产必须修改)
	AdminPassword string `yaml:"admin_password"` // 初始管理员密码 (仅首次启动生效)
	MasterKey     string `yaml:"master_key"`     // SSH 凭据加密主密钥 (32字节 base64, 留空则开发模式随机生成)
}

// AgentConfig 是 Agent(deepsea-agent)的配置。
type AgentConfig struct {
	AgentID string `yaml:"agent_id"` // Agent 唯一 ID
	Server  string `yaml:"server"`   // 控制面 gRPC 地址, 如 127.0.0.1:9090
}

// 内置默认值 (开发环境用)
const (
	defaultJWTSecret     = "deepsea-dev-secret-change-me"
	defaultAdminPassword = "admin123"
)

// DefaultServerConfig 返回控制面的默认配置(配置文件不存在时用)。
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		NodeID: "node1",
		Raft: RaftConfig{
			Addr:    "127.0.0.1:7000",
			DataDir: "raft-data",
			Join:    "",
		},
		HTTP:     ListenConfig{Addr: ":8080"},
		GRPC:     ListenConfig{Addr: ":9090"},
		Security: DefaultSecurityConfig(),
	}
}

// DefaultSecurityConfig 返回安全配置的默认值。
func DefaultSecurityConfig() SecurityConfig {
	return SecurityConfig{
		JWTSecret:     defaultJWTSecret,
		AdminPassword: defaultAdminPassword,
		MasterKey:     "", // 留空: 开发模式随机生成, 生产必须设置
	}
}

// DefaultAgentConfig 返回 Agent 的默认配置。
func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		AgentID: "agent-1",
		Server:  "127.0.0.1:9090",
	}
}

// LoadServer 从指定路径加载控制面配置。
// path 为空时尝试 ./config/server.yaml; 文件不存在则返回默认值。
// 加载后合并环境变量优先级: JWT_SECRET / ADMIN_PASSWORD / MASTER_KEY。
func LoadServer(path string) (ServerConfig, error) {
	cfg := DefaultServerConfig()
	if path == "" {
		path = "config/server.yaml"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 配置文件不存在, 用默认值(向后兼容命令行启动)
			applySecurityEnvOverrides(&cfg)
			return cfg, nil
		}
		return cfg, fmt.Errorf("读取配置文件 %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("解析配置文件 %s: %w", path, err)
	}
	// 空值回退到默认值
	if cfg.NodeID == "" {
		cfg.NodeID = "node1"
	}
	if cfg.Raft.Addr == "" {
		cfg.Raft.Addr = "127.0.0.1:7000"
	}
	if cfg.Raft.DataDir == "" {
		cfg.Raft.DataDir = "raft-data"
	}
	if cfg.HTTP.Addr == "" {
		cfg.HTTP.Addr = ":8080"
	}
	if cfg.GRPC.Addr == "" {
		cfg.GRPC.Addr = ":9090"
	}
	if cfg.Security.JWTSecret == "" {
		cfg.Security.JWTSecret = defaultJWTSecret
	}
	if cfg.Security.AdminPassword == "" {
		cfg.Security.AdminPassword = defaultAdminPassword
	}
	// MasterKey 留空表示开发模式随机生成, 不回退默认值
	// 环境变量覆盖 YAML 配置
	applySecurityEnvOverrides(&cfg)
	return cfg, nil
}

// applySecurityEnvOverrides 用环境变量覆盖安全配置。
// 优先级: 环境变量 > YAML 配置 > 内置默认值。
func applySecurityEnvOverrides(cfg *ServerConfig) {
	if v := os.Getenv("JWT_SECRET"); v != "" {
		cfg.Security.JWTSecret = v
	}
	if v := os.Getenv("ADMIN_PASSWORD"); v != "" {
		cfg.Security.AdminPassword = v
	}
	if v := os.Getenv("MASTER_KEY"); v != "" {
		cfg.Security.MasterKey = v
	}
}

// LoadAgent 从指定路径加载 Agent 配置。
// path 为空时尝试 ./config/agent.yaml; 文件不存在则返回默认值。
func LoadAgent(path string) (AgentConfig, error) {
	cfg := DefaultAgentConfig()
	if path == "" {
		path = "config/agent.yaml"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("读取配置文件 %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("解析配置文件 %s: %w", path, err)
	}
	if cfg.AgentID == "" {
		cfg.AgentID = "agent-1"
	}
	if cfg.Server == "" {
		cfg.Server = "127.0.0.1:9090"
	}
	return cfg, nil
}
