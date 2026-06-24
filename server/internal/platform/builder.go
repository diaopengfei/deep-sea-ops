package platform

import (
	"io"
	"time"
)

// ProcessSignal 表示发送给进程的信号类型。
type ProcessSignal int

const (
	SignalTerm  ProcessSignal = iota // SIGTERM (15), 优雅退出
	SignalKill                       // SIGKILL (9), 强制终止
	SignalCheck                      // kill -0, 仅检测进程是否存在
)

// Command 表示一条与平台无关的命令(程序名+参数+超时)。
// 由 CommandBuilder 生成, 由 Executor 执行。
type Command struct {
	Name    string        // 程序名: "systemctl" / "tasklist" / "java"
	Args    []string      // 参数: ["status", "nginx"]
	Timeout time.Duration // 超时, 0 表示用默认值(60s)
	Stdin   io.Reader     // 可选输入
}

// DefaultTimeout 是命令执行的默认超时。
const DefaultTimeout = 60 * time.Second

// CommandBuilder 按平台生成命令, 无状态, 不执行。
// 新增平台只需实现此接口, 不改调用方。
type CommandBuilder interface {
	// Platform 返回平台标识("linux"/"windows"/"darwin"), 用于输出解析时区分格式。
	Platform() string

	// 进程
	ListProcesses() Command
	KillProcess(pid int, signal ProcessSignal) Command
	IsProcessAlive(pid int) Command

	// 路径(不走 Executor, 直接 os 调用)
	HostsFilePath() string
	DeployDir(projectName string) string
	DefaultScanDirs() []string
	// 服务文件路径(systemd: /etc/systemd/system/{name}.service)
	ServiceFilePath(name string) string
	// 二进制部署根目录(/opt/deepsea, C:\ProgramData\deepsea)
	BinaryDeployDir() string
	// 配置文件目录(/opt/deepsea/config)
	ConfigDeployDir() string

	// 需要执行的操作(走 Executor)
	CreateDir(path string) Command
	Chmod(path string, mode string) Command
	StartJava(jarPath string, args []string) Command

	// 服务管理(走 Executor)
	StartService(name string) Command
	StopService(name string) Command
	EnableService(name string) Command
	ServiceStatus(name string) Command
	InstallService(name, binaryPath, configPath string) Command
	UninstallService(name string) Command
}

// NewCommandBuilder 根据 PlatformInfo 返回对应的 CommandBuilder。
func NewCommandBuilder(p PlatformInfo) CommandBuilder {
	switch p.OS {
	case "linux":
		if p.InitSystem == "systemd" {
			return &LinuxSystemdBuilder{}
		}
		return &LinuxSysVInitBuilder{}
	case "windows":
		return &WindowsBuilder{}
	case "darwin":
		return &MacOSBuilder{}
	}
	return &LinuxSystemdBuilder{} // 兜底
}
