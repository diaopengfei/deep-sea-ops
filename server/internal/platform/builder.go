package platform

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
