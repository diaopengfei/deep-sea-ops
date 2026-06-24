// Package platform 提供跨平台的命令执行抽象层。
// 三层架构: CommandBuilder(按平台生成命令) → Command(命令表示) → Executor(本地/远程执行)。
// 上层 Domain Ops(ProcessOps/FileOps/ServiceOps 等)组合 Builder+Executor 提供业务语义。
package platform

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// PlatformInfo 描述运行时平台信息, Agent 启动时检测一次。
type PlatformInfo struct {
	OS         string // "linux" / "windows" / "darwin"
	Distro     string // "ubuntu" / "centos" / "debian" / "alpine" / ""(Windows/macOS)
	InitSystem string // "systemd" / "sysvinit" / "openrc" / "windows-service" / "launchd"
	PkgManager string // "apt" / "yum" / "dnf" / "apk" / ""(本次不实现包安装)
}

// DetectPlatform 启动时检测平台信息, 返回缓存的 PlatformInfo。
// 检测逻辑: runtime.GOOS 定 OS; Linux 读 /etc/os-release 定发行版, 检测 init 系统。
func DetectPlatform() PlatformInfo {
	p := PlatformInfo{OS: runtime.GOOS}
	switch p.OS {
	case "linux":
		p.Distro = detectLinuxDistro()
		p.InitSystem = detectInitSystem()
		p.PkgManager = detectPkgManager(p.Distro)
	case "windows":
		p.InitSystem = "windows-service"
	case "darwin":
		p.InitSystem = "launchd"
	}
	return p
}

// detectLinuxDistro 读 /etc/os-release 的 ID= 字段。
func detectLinuxDistro() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") {
			val := strings.Trim(strings.TrimPrefix(line, "ID="), "\"'")
			return strings.ToLower(val)
		}
	}
	return ""
}

// detectInitSystem 检测 init 系统: 优先 systemd, 回退 sysvinit/openrc。
func detectInitSystem() string {
	// systemd: /run/systemd/system 目录存在
	if info, err := os.Stat("/run/systemd/system"); err == nil && info.IsDir() {
		return "systemd"
	}
	// 检测 /sbin/init 是否为 systemd 软链接
	if target, err := os.Readlink("/sbin/init"); err == nil && strings.Contains(strings.ToLower(target), "systemd") {
		return "systemd"
	}
	// openrc: rc-update 命令存在
	if _, err := exec.LookPath("rc-update"); err == nil {
		return "openrc"
	}
	// sysvinit: /etc/init.d 目录存在
	if info, err := os.Stat("/etc/init.d"); err == nil && info.IsDir() {
		return "sysvinit"
	}
	return "sysvinit" // 兜底
}

// detectPkgManager 根据发行版推断包管理器(本次不实现包安装, 仅记录)。
func detectPkgManager(distro string) string {
	switch distro {
	case "ubuntu", "debian":
		return "apt"
	case "centos", "rhel", "fedora":
		return "yum"
	case "alpine":
		return "apk"
	case "arch":
		return "pacman"
	}
	// 兜底: 检测命令是否存在
	if _, err := exec.LookPath("apt-get"); err == nil {
		return "apt"
	}
	if _, err := exec.LookPath("yum"); err == nil {
		return "yum"
	}
	if _, err := exec.LookPath("apk"); err == nil {
		return "apk"
	}
	return ""
}
