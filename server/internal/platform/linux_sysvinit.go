package platform

import (
	"fmt"
	"path/filepath"
)

// LinuxSysVInitBuilder 生成 Linux SysVInit/openrc 环境的命令。
// 适用于 CentOS 6/Alpine 等非 systemd 系统。
type LinuxSysVInitBuilder struct{}

// Platform 返回平台标识, 用于输出解析时区分格式。
func (b *LinuxSysVInitBuilder) Platform() string { return "linux" }

func (b *LinuxSysVInitBuilder) ListProcesses() Command {
	return Command{Name: ":read_proc"} // 同样读 /proc
}

func (b *LinuxSysVInitBuilder) KillProcess(pid int, signal ProcessSignal) Command {
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

func (b *LinuxSysVInitBuilder) IsProcessAlive(pid int) Command {
	return Command{Name: "kill", Args: []string{"-0", fmt.Sprintf("%d", pid)}}
}

func (b *LinuxSysVInitBuilder) HostsFilePath() string {
	return "/etc/hosts"
}

func (b *LinuxSysVInitBuilder) DeployDir(projectName string) string {
	return filepath.Join("/opt", projectName)
}

func (b *LinuxSysVInitBuilder) DefaultScanDirs() []string {
	return []string{"/home", "/data"}
}

// ServiceFilePath 返回 SysVInit 脚本路径: /etc/init.d/{name}
func (b *LinuxSysVInitBuilder) ServiceFilePath(name string) string {
	return "/etc/init.d/" + name
}

// BinaryDeployDir 返回二进制部署根目录: /opt/deepsea
func (b *LinuxSysVInitBuilder) BinaryDeployDir() string {
	return "/opt/deepsea"
}

// ConfigDeployDir 返回配置文件目录: /opt/deepsea/config
func (b *LinuxSysVInitBuilder) ConfigDeployDir() string {
	return "/opt/deepsea/config"
}

func (b *LinuxSysVInitBuilder) CreateDir(path string) Command {
	return Command{Name: "mkdir", Args: []string{"-p", path}}
}

func (b *LinuxSysVInitBuilder) Chmod(path string, mode string) Command {
	return Command{Name: "chmod", Args: []string{mode, path}}
}

func (b *LinuxSysVInitBuilder) StartJava(jarPath string, args []string) Command {
	allArgs := append([]string{"-jar", jarPath}, args...)
	return Command{Name: "java", Args: allArgs}
}

func (b *LinuxSysVInitBuilder) StartService(name string) Command {
	return Command{Name: "service", Args: []string{name, "start"}}
}

func (b *LinuxSysVInitBuilder) StopService(name string) Command {
	return Command{Name: "service", Args: []string{name, "stop"}}
}

func (b *LinuxSysVInitBuilder) EnableService(name string) Command {
	// Debian: update-rc.d, RHEL: chkconfig
	// 优先用 update-rc.d, 失败由调用方降级
	return Command{Name: "update-rc.d", Args: []string{name, "defaults"}}
}

func (b *LinuxSysVInitBuilder) ServiceStatus(name string) Command {
	return Command{Name: "service", Args: []string{name, "status"}}
}

func (b *LinuxSysVInitBuilder) InstallService(name, binaryPath, configPath string) Command {
	// SysVInit: 写 /etc/init.d/<name> 脚本 + chmod +x
	// 实际脚本由 ServiceOps 写入, 这里返回 chmod 命令
	return Command{Name: "chmod", Args: []string{"+x", "/etc/init.d/" + name}}
}

func (b *LinuxSysVInitBuilder) UninstallService(name string) Command {
	// 返回 update-rc.d remove 命令
	return Command{Name: "update-rc.d", Args: []string{name, "remove"}}
}
