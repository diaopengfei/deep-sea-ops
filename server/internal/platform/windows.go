package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

// WindowsBuilder 生成 Windows 环境的命令。
// 适用于 Windows Server 2016+。
type WindowsBuilder struct{}

// Platform 返回平台标识, 用于输出解析时区分格式。
func (b *WindowsBuilder) Platform() string { return "windows" }

func (b *WindowsBuilder) ListProcesses() Command {
	return Command{Name: "tasklist", Args: []string{"/FO", "CSV", "/NH"}}
}

func (b *WindowsBuilder) KillProcess(pid int, signal ProcessSignal) Command {
	// Windows 不区分信号, 一律 taskkill /F
	return Command{Name: "taskkill", Args: []string{"/F", "/PID", fmt.Sprintf("%d", pid)}}
}

func (b *WindowsBuilder) IsProcessAlive(pid int) Command {
	return Command{Name: "tasklist", Args: []string{"/FI", fmt.Sprintf("PID eq %d", pid)}}
}

func (b *WindowsBuilder) HostsFilePath() string {
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "drivers", "etc", "hosts")
}

func (b *WindowsBuilder) DeployDir(projectName string) string {
	return filepath.Join(os.Getenv("ProgramData"), "deepsea", projectName)
}

func (b *WindowsBuilder) DefaultScanDirs() []string {
	return []string{`C:\Program Files`, `C:\data`}
}

// ServiceFilePath 返回 Windows 服务文件路径: 空字符串(Windows 用 sc create, 不需要文件)
func (b *WindowsBuilder) ServiceFilePath(name string) string {
	return ""
}

// BinaryDeployDir 返回二进制部署根目录: C:\ProgramData\deepsea
func (b *WindowsBuilder) BinaryDeployDir() string {
	return `C:\ProgramData\deepsea`
}

// ConfigDeployDir 返回配置文件目录: C:\ProgramData\deepsea\config
func (b *WindowsBuilder) ConfigDeployDir() string {
	return `C:\ProgramData\deepsea\config`
}

func (b *WindowsBuilder) CreateDir(path string) Command {
	// Windows 用 powershell New-Item 创建目录
	return Command{Name: "powershell", Args: []string{"-Command", fmt.Sprintf("New-Item -ItemType Directory -Force -Path '%s'", path)}}
}

func (b *WindowsBuilder) Chmod(path string, mode string) Command {
	// Windows 无 chmod, 返回空操作(icacls 可设权限但语义不同)
	// 返回一个 no-op 命令
	return Command{Name: "cmd", Args: []string{"/c", "echo", "n/a"}}
}

func (b *WindowsBuilder) StartJava(jarPath string, args []string) Command {
	allArgs := append([]string{"-jar", jarPath}, args...)
	return Command{Name: "javaw", Args: allArgs}
}

func (b *WindowsBuilder) StartService(name string) Command {
	return Command{Name: "sc", Args: []string{"start", name}}
}

func (b *WindowsBuilder) StopService(name string) Command {
	return Command{Name: "sc", Args: []string{"stop", name}}
}

func (b *WindowsBuilder) EnableService(name string) Command {
	return Command{Name: "sc", Args: []string{"config", name, "start=", "auto"}}
}

func (b *WindowsBuilder) ServiceStatus(name string) Command {
	return Command{Name: "sc", Args: []string{"query", name}}
}

func (b *WindowsBuilder) InstallService(name, binaryPath, configPath string) Command {
	// sc create <name> binPath= "<binaryPath> -config <configPath>"
	binPath := fmt.Sprintf("%s -config %s", binaryPath, configPath)
	return Command{Name: "sc", Args: []string{"create", name, "binPath=", binPath}}
}

func (b *WindowsBuilder) UninstallService(name string) Command {
	return Command{Name: "sc", Args: []string{"delete", name}}
}
