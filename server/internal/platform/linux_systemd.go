package platform

import (
	"fmt"
	"path/filepath"
)

// LinuxSystemdBuilder 生成 Linux systemd 环境的命令。
// 适用于 Ubuntu 16+/CentOS 7+/Debian 8+ 等主流发行版。
type LinuxSystemdBuilder struct{}

// Platform 返回平台标识, 用于输出解析时区分格式。
func (b *LinuxSystemdBuilder) Platform() string { return "linux" }

// ListProcesses 读 /proc/*/cmdline, 不通过 Executor 执行命令。
// 返回一个特殊标记的 Command(Name=":read_proc"), 由 ProcessOps 特殊处理。
// 实际进程列表读取由 processOps 直接调用 os.ReadDir 完成。
func (b *LinuxSystemdBuilder) ListProcesses() Command {
	return Command{Name: ":read_proc"} // 特殊标记, ProcessOps 直接读 /proc
}

func (b *LinuxSystemdBuilder) KillProcess(pid int, signal ProcessSignal) Command {
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

func (b *LinuxSystemdBuilder) IsProcessAlive(pid int) Command {
	return Command{Name: "kill", Args: []string{"-0", fmt.Sprintf("%d", pid)}}
}

func (b *LinuxSystemdBuilder) HostsFilePath() string {
	return "/etc/hosts"
}

func (b *LinuxSystemdBuilder) DeployDir(projectName string) string {
	return filepath.Join("/opt", projectName)
}

func (b *LinuxSystemdBuilder) DefaultScanDirs() []string {
	return []string{"/home", "/data"}
}

// ServiceFilePath 返回 systemd 服务文件路径: /etc/systemd/system/{name}.service
func (b *LinuxSystemdBuilder) ServiceFilePath(name string) string {
	return "/etc/systemd/system/" + name + ".service"
}

// BinaryDeployDir 返回二进制部署根目录: /opt/deepsea
func (b *LinuxSystemdBuilder) BinaryDeployDir() string {
	return "/opt/deepsea"
}

// ConfigDeployDir 返回配置文件目录: /opt/deepsea/config
func (b *LinuxSystemdBuilder) ConfigDeployDir() string {
	return "/opt/deepsea/config"
}

func (b *LinuxSystemdBuilder) CreateDir(path string) Command {
	return Command{Name: "mkdir", Args: []string{"-p", path}}
}

func (b *LinuxSystemdBuilder) Chmod(path string, mode string) Command {
	return Command{Name: "chmod", Args: []string{mode, path}}
}

func (b *LinuxSystemdBuilder) StartJava(jarPath string, args []string) Command {
	allArgs := append([]string{"-jar", jarPath}, args...)
	return Command{Name: "java", Args: allArgs}
}

func (b *LinuxSystemdBuilder) StartService(name string) Command {
	return Command{Name: "systemctl", Args: []string{"start", name}}
}

func (b *LinuxSystemdBuilder) StopService(name string) Command {
	return Command{Name: "systemctl", Args: []string{"stop", name}}
}

func (b *LinuxSystemdBuilder) EnableService(name string) Command {
	return Command{Name: "systemctl", Args: []string{"enable", name}}
}

func (b *LinuxSystemdBuilder) ServiceStatus(name string) Command {
	return Command{Name: "systemctl", Args: []string{"status", name}}
}

func (b *LinuxSystemdBuilder) InstallService(name, binaryPath, configPath string) Command {
	// systemd: 写 service 文件 + daemon-reload + enable
	// 实际 service 文件内容由 ServiceOps 写入, 这里返回 daemon-reload 命令
	return Command{Name: "systemctl", Args: []string{"daemon-reload"}}
}

func (b *LinuxSystemdBuilder) UninstallService(name string) Command {
	// 返回 disable 命令, service 文件删除由 ServiceOps 处理
	return Command{Name: "systemctl", Args: []string{"disable", name}}
}
