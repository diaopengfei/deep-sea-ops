// Package inject 实现 v0.4 自动注入: SSH 推送二进制 + 配置, 远程拉起 systemd。
//
// 两种角色:
//   - raft: 推送 deepsea-server 二进制, 启动后 Leader 调用 AddVoter 纳入集群
//   - agent: 推送 deepsea-agent 二进制, 启动后自动连 Leader gRPC
//
// v0.5.2 起支持两种凭据来源:
//   - CredentialID: 从 Raft 中读取 SSH 凭据(兼容旧 API)
//   - 直接传 SSHConfig: 从 Server 表解密后直接传入(服务器列表触发注入)
//
// 流程:
//  1. 获取 SSH 连接信息(凭据 ID 或直接传入)
//  2. SSH 连接目标服务器
//  3. 上传二进制 + YAML 配置 + systemd 配置
//  4. 远程启动 systemd 服务
//  5. (Raft 节点) 调用 AddVoter 加入集群
package inject

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/deepsea-ops/server/internal/crypto"
	"github.com/deepsea-ops/server/internal/sshclient"
	"github.com/deepsea-ops/server/internal/store"
)

// Role 是注入角色
type Role string

const (
	RoleRaft  Role = "raft"  // Raft 控制面节点
	RoleAgent Role = "agent" // Agent 工作节点
)

// SSHConfig 是直接传入的 SSH 连接信息(v0.5.2+, 从 Server 表解密后传入)。
type SSHConfig struct {
	Host       string // 目标服务器 IP
	Port       int    // SSH 端口
	Username   string // SSH 用户名
	Password   string // SSH 密码(明文, 调用方负责解密)
	PrivateKey string // SSH 私钥(明文, 调用方负责解密)
}

// InjectRequest 是一次注入请求的参数。
type InjectRequest struct {
	// 凭据来源(二选一):
	//   - CredentialID 非空时从 Raft 读取 SSH 凭据
	//   - CredentialID 为空时用 SSHConfig
	CredentialID string     // SSH 凭据 ID(存在 Raft 里, 兼容旧 API)
	SSH          *SSHConfig // 直接传入 SSH 连接信息(v0.5.2+)

	Role   Role   // raft / agent
	NodeID string // 节点 ID(如 node2 / agent-3)
	// Raft 节点参数
	RaftAddr string // Raft 通信地址(如 192.168.1.11:7000), raft 角色必填
	JoinAddr string // 已有集群 Leader 的 Raft 地址, raft 角色必填
	// Agent 节点参数
	LeaderGRPCAddr string // Leader 的 gRPC 地址(如 192.168.1.10:9090), agent 角色必填
	// 二进制路径(本机)
	BinaryPath string // 要推送的二进制文件路径, 留空则用默认值
}

// InjectResult 是注入结果。
type InjectResult struct {
	Success  bool
	Output   string
	Duration time.Duration
}

// Injector 封装注入逻辑, 依赖 Store(读凭据 + 加节点)。
type Injector struct {
	store *store.Store
}

// NewInjector 创建注入器。
func NewInjector(s *store.Store) *Injector {
	return &Injector{store: s}
}

// Inject 执行一次注入。
func (inj *Injector) Inject(req InjectRequest) InjectResult {
	start := time.Now()
	result := InjectResult{}

	// 1. 获取 SSH 连接信息
	sshCfg, err := inj.resolveSSHConfig(req)
	if err != nil {
		result.Output = err.Error()
		return result
	}

	// 2. 确定 binary 路径
	binaryPath := req.BinaryPath
	if binaryPath == "" {
		if req.Role == RoleRaft {
			binaryPath = "./deepsea-server"
		} else {
			binaryPath = "./deepsea-agent"
		}
	}

	// 3. SSH 连接
	client, err := sshclient.NewClient(*sshCfg)
	if err != nil {
		result.Output = "SSH 连接失败: " + err.Error()
		return result
	}
	defer client.Close()

	var steps []string

	// 4. 上传二进制
	remoteBinPath := "/opt/deepsea/" + filepath.Base(binaryPath)
	if err := client.UploadFile(binaryPath, remoteBinPath); err != nil {
		result.Output = "上传二进制失败: " + err.Error()
		return result
	}
	steps = append(steps, "已上传二进制到 "+remoteBinPath)

	// chmod +x
	if _, err := client.RunCommand("chmod +x " + remoteBinPath); err != nil {
		result.Output = "chmod 失败: " + err.Error()
		return result
	}

	// 4.5 上传 YAML 配置文件 (v0.5: 改为配置文件启动)
	remoteCfgPath := "/opt/deepsea/config/" + string(req.Role) + ".yaml"
	cfgContent := genConfigContent(req)
	if _, err := client.RunCommand("mkdir -p /opt/deepsea/config"); err != nil {
		result.Output = "创建配置目录失败: " + err.Error()
		return result
	}
	if err := client.UploadContent([]byte(cfgContent), remoteCfgPath); err != nil {
		result.Output = "上传配置文件失败: " + err.Error()
		return result
	}
	steps = append(steps, "已写入配置文件 "+remoteCfgPath)

	// 5. 生成 systemd 配置并上传
	serviceName := "deepsea-" + string(req.Role)
	serviceContent := genSystemdService(serviceName, remoteBinPath, remoteCfgPath)
	remoteServicePath := "/etc/systemd/system/" + serviceName + ".service"
	if err := client.UploadContent([]byte(serviceContent), remoteServicePath); err != nil {
		result.Output = "上传 systemd 配置失败: " + err.Error()
		return result
	}
	steps = append(steps, "已写入 systemd 配置 "+remoteServicePath)

	// 6. 启动 systemd 服务
	startCmd := fmt.Sprintf("systemctl daemon-reload && systemctl enable %s && systemctl restart %s", serviceName, serviceName)
	if out, err := client.RunCommand(startCmd); err != nil {
		result.Output = "启动服务失败: " + err.Error() + "\n" + out
		return result
	}
	steps = append(steps, "已启动 systemd 服务 "+serviceName)

	// 7. Raft 节点: 调用 AddVoter 加入集群
	if req.Role == RoleRaft && req.JoinAddr != "" {
		// 等待新节点启动
		time.Sleep(2 * time.Second)
		if err := inj.store.AddVoter(req.NodeID, req.RaftAddr); err != nil {
			steps = append(steps, "警告: AddVoter 失败(节点可能已启动但未加入集群): "+err.Error())
		} else {
			steps = append(steps, "已调用 AddVoter 加入集群: "+req.NodeID)
		}
	}

	result.Success = true
	result.Output = strings.Join(steps, "\n")
	result.Duration = time.Since(start)
	return result
}

// resolveSSHConfig 从 CredentialID 或 SSHConfig 获取 SSH 连接信息。
func (inj *Injector) resolveSSHConfig(req InjectRequest) (*sshclient.Config, error) {
	// v0.5.2+: 直接传入 SSH 配置(从 Server 表解密后传入)
	if req.SSH != nil {
		return &sshclient.Config{
			Host:       req.SSH.Host,
			Port:       req.SSH.Port,
			Username:   req.SSH.Username,
			Password:   req.SSH.Password,
			PrivateKey: req.SSH.PrivateKey,
		}, nil
	}

	// 兼容旧 API: 从 Raft 读取 SSH 凭据
	if req.CredentialID == "" {
		return nil, fmt.Errorf("必须提供 CredentialID 或 SSH 配置")
	}
	cred, ok := inj.store.GetCredential(req.CredentialID)
	if !ok {
		return nil, fmt.Errorf("凭据不存在: %s", req.CredentialID)
	}
	password, err := crypto.Decrypt(cred.Password)
	if err != nil {
		return nil, fmt.Errorf("解密密码失败: %w", err)
	}
	privateKey, err := crypto.Decrypt(cred.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("解密私钥失败: %w", err)
	}
	return &sshclient.Config{
		Host:       cred.IP,
		Port:       cred.Port,
		Username:   cred.Username,
		Password:   password,
		PrivateKey: privateKey,
	}, nil
}

// genConfigContent 生成远程节点的 YAML 配置文件内容。
func genConfigContent(req InjectRequest) string {
	if req.Role == RoleRaft {
		// Raft 节点: 监听用 0.0.0.0(绑所有网卡), AddVoter 用实际 IP
		listenAddr := "0.0.0.0:" + portFromAddr(req.RaftAddr)
		return fmt.Sprintf(`# deepsea-server 配置 (自动注入生成)
node_id: %s
raft:
  addr: %s
  data_dir: /opt/deepsea/data
  join: %q
http:
  addr: :8080
grpc:
  addr: :9090
`, req.NodeID, listenAddr, req.JoinAddr)
	}
	// Agent 节点
	return fmt.Sprintf(`# deepsea-agent 配置 (自动注入生成)
agent_id: %s
server: %s
`, req.NodeID, req.LeaderGRPCAddr)
}

// genSystemdService 生成 systemd service 文件内容。
func genSystemdService(name, binPath, cfgPath string) string {
	execStart := fmt.Sprintf("%s -config %s", binPath, cfgPath)

	return fmt.Sprintf(`[Unit]
Description=DeepSea Ops %s
After=network.target

[Service]
Type=simple
ExecStart=%s
Restart=always
RestartSec=5
User=root

[Install]
WantedBy=multi-user.target
`, name, execStart)
}

// portFromAddr 从 "host:port" 中提取端口部分。
func portFromAddr(addr string) string {
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return "7000"
	}
	return addr[idx+1:]
}
