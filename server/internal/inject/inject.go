// Package inject 实现 v0.4 自动注入: SSH 推送二进制 + 配置, 远程拉起服务。
//
// 两种角色:
//   - raft: 推送 deepsea-server 二进制, 启动后 Leader 调用 AddVoter 纳入集群
//   - agent: 推送 deepsea-agent 二进制, 启动后自动连 Leader gRPC
//
// v0.6.0 起使用 platform 抽象层:
//   - SSHExecutor 包装 sshclient.Client, 实现 Executor 接口
//   - CommandBuilder 按目标平台生成命令(systemd/SysVInit/Windows Service)
//   - ServiceOps 管理服务安装/启动/停止
package inject

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/deepsea-ops/server/internal/crypto"
	"github.com/deepsea-ops/server/internal/platform"
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

	// 2. 确定 binary 路径, 校验文件名安全(防 shell 注入)
	binaryPath := req.BinaryPath
	if binaryPath == "" {
		if req.Role == RoleRaft {
			binaryPath = "./deepsea-server"
		} else {
			binaryPath = "./deepsea-agent"
		}
	}
	binBase := filepath.Base(binaryPath)
	if err := validateBinName(binBase); err != nil {
		result.Output = "二进制文件名不安全: " + err.Error()
		return result
	}

	// 3. SSH 连接
	client, err := sshclient.NewClient(*sshCfg)
	if err != nil {
		result.Output = "SSH 连接失败: " + err.Error()
		return result
	}
	defer client.Close()

	// 4. 检测目标服务器平台(通过 SSH 执行命令)
	targetPlatform, err := detectRemotePlatform(client)
	if err != nil {
		// 检测失败, 兜底用 Linux systemd
		targetPlatform = platform.PlatformInfo{OS: "linux", InitSystem: "systemd"}
	}

	// 5. 创建 SSHExecutor + Builder
	sshExec := platform.NewSSHExecutor(client)
	builder := platform.NewCommandBuilder(targetPlatform)

	var steps []string

	// 6. 上传二进制
	remoteBinDir := builder.BinaryDeployDir()
	remoteBinPath := filepath.Join(remoteBinDir, binBase)
	// 创建目录
	if _, _, _, err := sshExec.Run(builder.CreateDir(remoteBinDir)); err != nil {
		result.Output = "创建远程目录失败: " + err.Error()
		return result
	}
	if err := client.UploadFile(binaryPath, remoteBinPath); err != nil {
		result.Output = "上传二进制失败: " + err.Error()
		return result
	}
	steps = append(steps, "已上传二进制到 "+remoteBinPath)

	// chmod +x (Linux/macOS, Windows 跳过)
	if targetPlatform.OS != "windows" {
		if _, _, _, err := sshExec.Run(builder.Chmod(remoteBinPath, "+x")); err != nil {
			result.Output = "chmod 失败: " + err.Error()
			return result
		}
	}

	// 7. 上传 YAML 配置文件
	remoteCfgDir := builder.ConfigDeployDir()
	if _, _, _, err := sshExec.Run(builder.CreateDir(remoteCfgDir)); err != nil {
		result.Output = "创建配置目录失败: " + err.Error()
		return result
	}
	remoteCfgPath := filepath.Join(remoteCfgDir, string(req.Role)+".yaml")
	cfgContent := genConfigContent(req)
	if err := client.UploadContent([]byte(cfgContent), remoteCfgPath); err != nil {
		result.Output = "上传配置文件失败: " + err.Error()
		return result
	}
	steps = append(steps, "已写入配置文件 "+remoteCfgPath)

	// 8. 安装并启动服务
	serviceName := "deepsea-" + string(req.Role)
	serviceSteps, err := installAndStartService(sshExec, builder, client, serviceName, remoteBinPath, remoteCfgPath, targetPlatform)
	if err != nil {
		result.Output = err.Error() + "\n" + strings.Join(serviceSteps, "\n")
		return result
	}
	steps = append(steps, serviceSteps...)

	// 9. Raft 节点: 调用 AddVoter 加入集群
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

// detectRemotePlatform 通过 SSH 检测目标服务器的平台信息。
func detectRemotePlatform(client *sshclient.Client) (platform.PlatformInfo, error) {
	// 检测 OS: uname -s (Linux 返回 Linux, Windows 无此命令)
	out, err := client.RunCommand("uname -s 2>/dev/null || echo windows")
	if err != nil {
		return platform.PlatformInfo{}, err
	}
	out = strings.TrimSpace(strings.ToLower(out))
	if out == "windows" || out == "" {
		return platform.PlatformInfo{OS: "windows", InitSystem: "windows-service"}, nil
	}
	// Linux: 检测 init 系统
	p := platform.PlatformInfo{OS: "linux"}
	// 检测 systemd
	if _, err := client.RunCommand("test -d /run/systemd/system"); err == nil {
		p.InitSystem = "systemd"
	} else if _, err := client.RunCommand("test -d /etc/init.d"); err == nil {
		p.InitSystem = "sysvinit"
	} else {
		p.InitSystem = "systemd" // 兜底
	}
	return p, nil
}

// installAndStartService 根据平台安装并启动服务。
// 参数 exec 用 platform.Executor 接口, 不绑定具体 SSHExecutor 实现。
func installAndStartService(exec platform.Executor, builder platform.CommandBuilder, client *sshclient.Client, serviceName, binPath, cfgPath string, p platform.PlatformInfo) ([]string, error) {
	var steps []string

	switch p.OS {
	case "linux":
		if p.InitSystem == "systemd" {
			// systemd: 写 service 文件 + daemon-reload + enable + restart
			serviceContent := genSystemdService(serviceName, binPath, cfgPath)
			servicePath := builder.ServiceFilePath(serviceName)
			if err := client.UploadContent([]byte(serviceContent), servicePath); err != nil {
				return steps, fmt.Errorf("上传 systemd 配置失败: %w", err)
			}
			steps = append(steps, "已写入 systemd 配置 "+servicePath)
			// daemon-reload
			if _, _, _, err := exec.Run(builder.InstallService(serviceName, binPath, cfgPath)); err != nil {
				return steps, fmt.Errorf("daemon-reload 失败: %w", err)
			}
			// enable + start
			if _, _, _, err := exec.Run(builder.EnableService(serviceName)); err != nil {
				return steps, fmt.Errorf("enable 失败: %w", err)
			}
			startCmd := builder.StartService(serviceName)
			startCmd.Timeout = 120 * time.Second
			if _, _, _, err := exec.Run(startCmd); err != nil {
				return steps, fmt.Errorf("启动服务失败: %w", err)
			}
			steps = append(steps, "已启动 systemd 服务 "+serviceName)
		} else {
			// SysVInit: 写 init.d 脚本 + chmod +x + service start
			scriptContent := genSysVInitScript(serviceName, binPath, cfgPath)
			scriptPath := builder.ServiceFilePath(serviceName)
			if err := client.UploadContent([]byte(scriptContent), scriptPath); err != nil {
				return steps, fmt.Errorf("上传 init.d 脚本失败: %w", err)
			}
			steps = append(steps, "已写入 init.d 脚本 "+scriptPath)
			// chmod +x
			if _, _, _, err := exec.Run(builder.Chmod(scriptPath, "+x")); err != nil {
				return steps, fmt.Errorf("chmod 失败: %w", err)
			}
			// service start
			startCmd := builder.StartService(serviceName)
			startCmd.Timeout = 120 * time.Second
			if _, _, _, err := exec.Run(startCmd); err != nil {
				return steps, fmt.Errorf("启动服务失败: %w", err)
			}
			steps = append(steps, "已启动 init.d 服务 "+serviceName)
		}
	case "windows":
		// Windows: sc create + sc config + sc start
		installCmd := builder.InstallService(serviceName, binPath, cfgPath)
		if _, _, _, err := exec.Run(installCmd); err != nil {
			return steps, fmt.Errorf("sc create 失败: %w", err)
		}
		steps = append(steps, "已创建 Windows 服务 "+serviceName)
		// sc config start= auto
		if _, _, _, err := exec.Run(builder.EnableService(serviceName)); err != nil {
			return steps, fmt.Errorf("sc config 失败: %w", err)
		}
		// sc start
		startCmd := builder.StartService(serviceName)
		startCmd.Timeout = 120 * time.Second
		if _, _, _, err := exec.Run(startCmd); err != nil {
			return steps, fmt.Errorf("启动服务失败: %w", err)
		}
		steps = append(steps, "已启动 Windows 服务 "+serviceName)
	default:
		return steps, fmt.Errorf("不支持的平台: %s", p.OS)
	}

	return steps, nil
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

// genSysVInitScript 生成 SysVInit /etc/init.d/ 脚本内容。
func genSysVInitScript(name, binPath, cfgPath string) string {
	return fmt.Sprintf(`#!/bin/sh
### BEGIN INIT INFO
# Provides:          %s
# Required-Start:    $network
# Required-Stop:     $network
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Description:       DeepSea Ops %s
### END INIT INFO

DAEMON=%s
CONFIG=%s
PIDFILE=/var/run/%s.pid

case "$1" in
  start)
    start-stop-daemon --start --background --make-pidfile --pidfile $PIDFILE --exec $DAEMON -- -config $CONFIG
    ;;
  stop)
    start-stop-daemon --stop --pidfile $PIDFILE
    ;;
  restart)
    $0 stop
    $0 start
    ;;
  status)
    if [ -f $PIDFILE ]; then
      echo "Running"
    else
      echo "Stopped"
    fi
    ;;
  *)
    echo "Usage: $0 {start|stop|restart|status}"
    exit 1
    ;;
esac
exit 0
`, name, name, binPath, cfgPath, name)
}

// portFromAddr 从 "host:port" 中提取端口部分。
func portFromAddr(addr string) string {
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return "7000"
	}
	return addr[idx+1:]
}

// validateBinName 校验二进制文件名是否安全(仅允许字母/数字/._-)。
func validateBinName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("文件名无效: %q", name)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return fmt.Errorf("文件名含不安全字符 %q: %s", r, name)
		}
	}
	return nil
}
