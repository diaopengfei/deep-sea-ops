package platform

import (
	"fmt"
	"path/filepath"
)

// MacOSBuilder 生成 macOS 环境的命令(开发环境, 基础支持)。
type MacOSBuilder struct{}

// Platform 返回平台标识, 用于输出解析时区分格式。
func (b *MacOSBuilder) Platform() string { return "darwin" }

func (b *MacOSBuilder) ListProcesses() Command {
	return Command{Name: "ps", Args: []string{"-eo", "pid=,comm=", "--no-headers"}}
}

func (b *MacOSBuilder) KillProcess(pid int, signal ProcessSignal) Command {
	var sig string
	switch signal {
	case SignalTerm:
		sig = "-15"
	case SignalKill:
		sig = "-9"
	case SignalCheck:
		sig = "-0"
	}
	return Command{Name: "kill", Args: []string{sig, fmt.Sprintf("%d", pid)}}
}

func (b *MacOSBuilder) IsProcessAlive(pid int) Command {
	return Command{Name: "kill", Args: []string{"-0", fmt.Sprintf("%d", pid)}}
}

func (b *MacOSBuilder) HostsFilePath() string {
	return "/etc/hosts"
}

func (b *MacOSBuilder) DeployDir(projectName string) string {
	return filepath.Join("/usr/local/var", projectName)
}

func (b *MacOSBuilder) DefaultScanDirs() []string {
	return []string{"/usr/local", "/opt/homebrew"}
}

// ServiceFilePath 返回 launchd plist 文件路径: /Library/LaunchDaemons/{name}.plist
func (b *MacOSBuilder) ServiceFilePath(name string) string {
	return "/Library/LaunchDaemons/" + name + ".plist"
}

// BinaryDeployDir 返回二进制部署根目录: /usr/local/var/deepsea
func (b *MacOSBuilder) BinaryDeployDir() string {
	return "/usr/local/var/deepsea"
}

// ConfigDeployDir 返回配置文件目录: /usr/local/var/deepsea/config
func (b *MacOSBuilder) ConfigDeployDir() string {
	return "/usr/local/var/deepsea/config"
}

func (b *MacOSBuilder) CreateDir(path string) Command {
	return Command{Name: "mkdir", Args: []string{"-p", path}}
}

func (b *MacOSBuilder) Chmod(path string, mode string) Command {
	return Command{Name: "chmod", Args: []string{mode, path}}
}

func (b *MacOSBuilder) StartJava(jarPath string, args []string) Command {
	allArgs := append([]string{"-jar", jarPath}, args...)
	return Command{Name: "java", Args: allArgs}
}

func (b *MacOSBuilder) StartService(name string) Command {
	return Command{Name: "launchctl", Args: []string{"start", name}}
}

func (b *MacOSBuilder) StopService(name string) Command {
	return Command{Name: "launchctl", Args: []string{"stop", name}}
}

func (b *MacOSBuilder) EnableService(name string) Command {
	return Command{Name: "launchctl", Args: []string{"load", "-w", "/Library/LaunchDaemons/" + name + ".plist"}}
}

func (b *MacOSBuilder) ServiceStatus(name string) Command {
	return Command{Name: "launchctl", Args: []string{"list", name}}
}

func (b *MacOSBuilder) InstallService(name, binaryPath, configPath string) Command {
	// launchd: 写 plist 文件, 这里返回 load 命令
	return Command{Name: "launchctl", Args: []string{"load", "/Library/LaunchDaemons/" + name + ".plist"}}
}

func (b *MacOSBuilder) UninstallService(name string) Command {
	return Command{Name: "launchctl", Args: []string{"unload", "/Library/LaunchDaemons/" + name + ".plist"}}
}
