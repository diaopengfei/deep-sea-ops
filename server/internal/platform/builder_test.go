// platform 包单元测试: 验证各 CommandBuilder 生成的命令正确性。
// 覆盖 Linux systemd / Linux SysVInit / Windows / macOS 四个实现,
// 以及 NewCommandBuilder 的分发逻辑。这是矛盾7 的测试覆盖。
package platform

import (
	"path/filepath"
	"reflect"
	"testing"
)

// assertCommand 比较命令的 Name 和 Args 是否符合预期(忽略 Timeout/Stdin)。
func assertCommand(t *testing.T, label string, got, want Command) {
	t.Helper()
	if got.Name != want.Name {
		t.Errorf("%s: Name = %q, want %q", label, got.Name, want.Name)
	}
	if !reflect.DeepEqual(got.Args, want.Args) {
		t.Errorf("%s: Args = %v, want %v", label, got.Args, want.Args)
	}
}

// --- 工厂分发 ---

func TestNewCommandBuilderDispatch(t *testing.T) {
	cases := []struct {
		name     string
		p        PlatformInfo
		wantPlat string
		wantType string
	}{
		{"linux systemd", PlatformInfo{OS: "linux", InitSystem: "systemd"}, "linux", "*platform.LinuxSystemdBuilder"},
		{"linux sysvinit", PlatformInfo{OS: "linux", InitSystem: "sysvinit"}, "linux", "*platform.LinuxSysVInitBuilder"},
		{"linux openrc 回退 sysvinit", PlatformInfo{OS: "linux", InitSystem: "openrc"}, "linux", "*platform.LinuxSysVInitBuilder"},
		{"linux 空 init 回退 sysvinit", PlatformInfo{OS: "linux"}, "linux", "*platform.LinuxSysVInitBuilder"},
		{"windows", PlatformInfo{OS: "windows"}, "windows", "*platform.WindowsBuilder"},
		{"darwin", PlatformInfo{OS: "darwin"}, "darwin", "*platform.MacOSBuilder"},
		{"未知 OS 兜底 linux systemd", PlatformInfo{OS: "solaris"}, "linux", "*platform.LinuxSystemdBuilder"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := NewCommandBuilder(c.p)
			if b.Platform() != c.wantPlat {
				t.Errorf("Platform() = %q, want %q", b.Platform(), c.wantPlat)
			}
			gotType := reflect.TypeOf(b).String()
			if gotType != c.wantType {
				t.Errorf("builder type = %s, want %s", gotType, c.wantType)
			}
		})
	}
}

// --- Linux systemd ---

func TestLinuxSystemdBuilder(t *testing.T) {
	b := &LinuxSystemdBuilder{}
	if b.Platform() != "linux" {
		t.Fatalf("Platform() = %q, want linux", b.Platform())
	}

	t.Run("进程", func(t *testing.T) {
		assertCommand(t, "ListProcesses", b.ListProcesses(), Command{Name: ":read_proc"})
		assertCommand(t, "KillProcess TERM", b.KillProcess(1234, SignalTerm), Command{Name: "kill", Args: []string{"-15", "1234"}})
		assertCommand(t, "KillProcess KILL", b.KillProcess(1234, SignalKill), Command{Name: "kill", Args: []string{"-9", "1234"}})
		assertCommand(t, "KillProcess CHECK", b.KillProcess(1234, SignalCheck), Command{Name: "kill", Args: []string{"-0", "1234"}})
		assertCommand(t, "IsProcessAlive", b.IsProcessAlive(5678), Command{Name: "kill", Args: []string{"-0", "5678"}})
	})

	t.Run("路径", func(t *testing.T) {
		if got := b.HostsFilePath(); got != "/etc/hosts" {
			t.Errorf("HostsFilePath() = %q, want /etc/hosts", got)
		}
		// DeployDir 用 filepath.Join, 跨平台测试时归一化为正斜杠比较逻辑路径
		if got := filepath.ToSlash(b.DeployDir("myapp")); got != "/opt/myapp" {
			t.Errorf("DeployDir() = %q, want /opt/myapp", got)
		}
		if got := b.ServiceFilePath("myapp"); got != "/etc/systemd/system/myapp.service" {
			t.Errorf("ServiceFilePath() = %q, want /etc/systemd/system/myapp.service", got)
		}
		if got := b.BinaryDeployDir(); got != "/opt/deepsea" {
			t.Errorf("BinaryDeployDir() = %q, want /opt/deepsea", got)
		}
		if got := b.ConfigDeployDir(); got != "/opt/deepsea/config" {
			t.Errorf("ConfigDeployDir() = %q, want /opt/deepsea/config", got)
		}
		scan := b.DefaultScanDirs()
		if len(scan) != 2 || scan[0] != "/home" || scan[1] != "/data" {
			t.Errorf("DefaultScanDirs() = %v, want [/home /data]", scan)
		}
	})

	t.Run("文件操作", func(t *testing.T) {
		assertCommand(t, "CreateDir", b.CreateDir("/opt/app"), Command{Name: "mkdir", Args: []string{"-p", "/opt/app"}})
		assertCommand(t, "Chmod", b.Chmod("/opt/app", "755"), Command{Name: "chmod", Args: []string{"755", "/opt/app"}})
		assertCommand(t, "StartJava", b.StartJava("/opt/app/app.jar", []string{"--port=8080"}),
			Command{Name: "java", Args: []string{"-jar", "/opt/app/app.jar", "--port=8080"}})
	})

	t.Run("服务管理", func(t *testing.T) {
		assertCommand(t, "StartService", b.StartService("myapp"), Command{Name: "systemctl", Args: []string{"start", "myapp"}})
		assertCommand(t, "StopService", b.StopService("myapp"), Command{Name: "systemctl", Args: []string{"stop", "myapp"}})
		assertCommand(t, "EnableService", b.EnableService("myapp"), Command{Name: "systemctl", Args: []string{"enable", "myapp"}})
		assertCommand(t, "ServiceStatus", b.ServiceStatus("myapp"), Command{Name: "systemctl", Args: []string{"status", "myapp"}})
		assertCommand(t, "InstallService", b.InstallService("myapp", "/opt/app/app", "/opt/app/config.yaml"),
			Command{Name: "systemctl", Args: []string{"daemon-reload"}})
		assertCommand(t, "UninstallService", b.UninstallService("myapp"), Command{Name: "systemctl", Args: []string{"disable", "myapp"}})
	})
}

// --- Linux SysVInit ---

func TestLinuxSysVInitBuilder(t *testing.T) {
	b := &LinuxSysVInitBuilder{}
	if b.Platform() != "linux" {
		t.Fatalf("Platform() = %q, want linux", b.Platform())
	}

	t.Run("进程", func(t *testing.T) {
		assertCommand(t, "ListProcesses", b.ListProcesses(), Command{Name: ":read_proc"})
		assertCommand(t, "KillProcess KILL", b.KillProcess(99, SignalKill), Command{Name: "kill", Args: []string{"-9", "99"}})
		assertCommand(t, "IsProcessAlive", b.IsProcessAlive(99), Command{Name: "kill", Args: []string{"-0", "99"}})
	})

	t.Run("路径", func(t *testing.T) {
		if got := b.ServiceFilePath("myapp"); got != "/etc/init.d/myapp" {
			t.Errorf("ServiceFilePath() = %q, want /etc/init.d/myapp", got)
		}
		if got := b.BinaryDeployDir(); got != "/opt/deepsea" {
			t.Errorf("BinaryDeployDir() = %q, want /opt/deepsea", got)
		}
		if got := b.ConfigDeployDir(); got != "/opt/deepsea/config" {
			t.Errorf("ConfigDeployDir() = %q, want /opt/deepsea/config", got)
		}
	})

	t.Run("服务管理(用 service 命令)", func(t *testing.T) {
		assertCommand(t, "StartService", b.StartService("myapp"), Command{Name: "service", Args: []string{"myapp", "start"}})
		assertCommand(t, "StopService", b.StopService("myapp"), Command{Name: "service", Args: []string{"myapp", "stop"}})
		assertCommand(t, "EnableService", b.EnableService("myapp"), Command{Name: "update-rc.d", Args: []string{"myapp", "defaults"}})
		assertCommand(t, "ServiceStatus", b.ServiceStatus("myapp"), Command{Name: "service", Args: []string{"myapp", "status"}})
		assertCommand(t, "InstallService", b.InstallService("myapp", "/opt/app/app", "/opt/app/config.yaml"),
			Command{Name: "chmod", Args: []string{"+x", "/etc/init.d/myapp"}})
		assertCommand(t, "UninstallService", b.UninstallService("myapp"), Command{Name: "update-rc.d", Args: []string{"myapp", "remove"}})
	})
}

// --- Windows ---

func TestWindowsBuilder(t *testing.T) {
	b := &WindowsBuilder{}
	if b.Platform() != "windows" {
		t.Fatalf("Platform() = %q, want windows", b.Platform())
	}

	// Windows 路径依赖环境变量, 显式设置以保证测试可重现
	t.Setenv("SystemRoot", `C:\Windows`)
	t.Setenv("ProgramData", `C:\ProgramData`)

	t.Run("进程", func(t *testing.T) {
		assertCommand(t, "ListProcesses", b.ListProcesses(),
			Command{Name: "tasklist", Args: []string{"/FO", "CSV", "/NH"}})
		// Windows 不分信号, 一律 taskkill /F
		assertCommand(t, "KillProcess TERM", b.KillProcess(4321, SignalTerm),
			Command{Name: "taskkill", Args: []string{"/F", "/PID", "4321"}})
		assertCommand(t, "KillProcess KILL", b.KillProcess(4321, SignalKill),
			Command{Name: "taskkill", Args: []string{"/F", "/PID", "4321"}})
		assertCommand(t, "IsProcessAlive", b.IsProcessAlive(4321),
			Command{Name: "tasklist", Args: []string{"/FI", "PID eq 4321"}})
	})

	t.Run("路径", func(t *testing.T) {
		if got := b.HostsFilePath(); got != `C:\Windows\System32\drivers\etc\hosts` {
			t.Errorf("HostsFilePath() = %q", got)
		}
		if got := b.DeployDir("myapp"); got != `C:\ProgramData\deepsea\myapp` {
			t.Errorf("DeployDir() = %q", got)
		}
		// Windows 用 sc create, 不需要服务文件
		if got := b.ServiceFilePath("myapp"); got != "" {
			t.Errorf("ServiceFilePath() = %q, want empty", got)
		}
		if got := b.BinaryDeployDir(); got != `C:\ProgramData\deepsea` {
			t.Errorf("BinaryDeployDir() = %q", got)
		}
		if got := b.ConfigDeployDir(); got != `C:\ProgramData\deepsea\config` {
			t.Errorf("ConfigDeployDir() = %q", got)
		}
		scan := b.DefaultScanDirs()
		if len(scan) != 2 || scan[0] != `C:\Program Files` || scan[1] != `C:\data` {
			t.Errorf("DefaultScanDirs() = %v", scan)
		}
	})

	t.Run("文件操作", func(t *testing.T) {
		assertCommand(t, "CreateDir", b.CreateDir(`C:\app`),
			Command{Name: "powershell", Args: []string{"-Command", "New-Item -ItemType Directory -Force -Path 'C:\\app'"}})
		// Windows 无 chmod, 返回 no-op
		assertCommand(t, "Chmod", b.Chmod(`C:\app`, "755"),
			Command{Name: "cmd", Args: []string{"/c", "echo", "n/a"}})
		// Windows 用 javaw 而非 java
		assertCommand(t, "StartJava", b.StartJava(`C:\app\app.jar`, []string{"--port=8080"}),
			Command{Name: "javaw", Args: []string{"-jar", `C:\app\app.jar`, "--port=8080"}})
	})

	t.Run("服务管理(用 sc 命令)", func(t *testing.T) {
		assertCommand(t, "StartService", b.StartService("myapp"), Command{Name: "sc", Args: []string{"start", "myapp"}})
		assertCommand(t, "StopService", b.StopService("myapp"), Command{Name: "sc", Args: []string{"stop", "myapp"}})
		assertCommand(t, "EnableService", b.EnableService("myapp"),
			Command{Name: "sc", Args: []string{"config", "myapp", "start=", "auto"}})
		assertCommand(t, "ServiceStatus", b.ServiceStatus("myapp"), Command{Name: "sc", Args: []string{"query", "myapp"}})
		assertCommand(t, "InstallService", b.InstallService("myapp", `C:\app\app.exe`, `C:\app\config.yaml`),
			Command{Name: "sc", Args: []string{"create", "myapp", "binPath=", `C:\app\app.exe -config C:\app\config.yaml`}})
		assertCommand(t, "UninstallService", b.UninstallService("myapp"), Command{Name: "sc", Args: []string{"delete", "myapp"}})
	})
}

// --- macOS ---

func TestMacOSBuilder(t *testing.T) {
	b := &MacOSBuilder{}
	if b.Platform() != "darwin" {
		t.Fatalf("Platform() = %q, want darwin", b.Platform())
	}

	t.Run("进程", func(t *testing.T) {
		assertCommand(t, "ListProcesses", b.ListProcesses(),
			Command{Name: "ps", Args: []string{"-eo", "pid=,comm=", "--no-headers"}})
		assertCommand(t, "KillProcess TERM", b.KillProcess(777, SignalTerm),
			Command{Name: "kill", Args: []string{"-15", "777"}})
		assertCommand(t, "IsProcessAlive", b.IsProcessAlive(777),
			Command{Name: "kill", Args: []string{"-0", "777"}})
	})

	t.Run("路径", func(t *testing.T) {
		if got := b.HostsFilePath(); got != "/etc/hosts" {
			t.Errorf("HostsFilePath() = %q", got)
		}
		// DeployDir 用 filepath.Join, 跨平台测试时归一化为正斜杠比较逻辑路径
		if got := filepath.ToSlash(b.DeployDir("myapp")); got != "/usr/local/var/myapp" {
			t.Errorf("DeployDir() = %q", got)
		}
		if got := b.ServiceFilePath("myapp"); got != "/Library/LaunchDaemons/myapp.plist" {
			t.Errorf("ServiceFilePath() = %q", got)
		}
		if got := b.BinaryDeployDir(); got != "/usr/local/var/deepsea" {
			t.Errorf("BinaryDeployDir() = %q", got)
		}
		if got := b.ConfigDeployDir(); got != "/usr/local/var/deepsea/config" {
			t.Errorf("ConfigDeployDir() = %q", got)
		}
		scan := b.DefaultScanDirs()
		if len(scan) != 2 || scan[0] != "/usr/local" || scan[1] != "/opt/homebrew" {
			t.Errorf("DefaultScanDirs() = %v", scan)
		}
	})

	t.Run("文件操作", func(t *testing.T) {
		assertCommand(t, "CreateDir", b.CreateDir("/usr/local/var/app"),
			Command{Name: "mkdir", Args: []string{"-p", "/usr/local/var/app"}})
		assertCommand(t, "StartJava", b.StartJava("/usr/local/var/app/app.jar", nil),
			Command{Name: "java", Args: []string{"-jar", "/usr/local/var/app/app.jar"}})
	})

	t.Run("服务管理(用 launchctl)", func(t *testing.T) {
		assertCommand(t, "StartService", b.StartService("com.deepsea.myapp"),
			Command{Name: "launchctl", Args: []string{"start", "com.deepsea.myapp"}})
		assertCommand(t, "StopService", b.StopService("com.deepsea.myapp"),
			Command{Name: "launchctl", Args: []string{"stop", "com.deepsea.myapp"}})
		assertCommand(t, "EnableService", b.EnableService("com.deepsea.myapp"),
			Command{Name: "launchctl", Args: []string{"load", "-w", "/Library/LaunchDaemons/com.deepsea.myapp.plist"}})
		assertCommand(t, "ServiceStatus", b.ServiceStatus("com.deepsea.myapp"),
			Command{Name: "launchctl", Args: []string{"list", "com.deepsea.myapp"}})
		assertCommand(t, "InstallService", b.InstallService("com.deepsea.myapp", "/usr/local/var/app/app", "/usr/local/var/app/config.yaml"),
			Command{Name: "launchctl", Args: []string{"load", "/Library/LaunchDaemons/com.deepsea.myapp.plist"}})
		assertCommand(t, "UninstallService", b.UninstallService("com.deepsea.myapp"),
			Command{Name: "launchctl", Args: []string{"unload", "/Library/LaunchDaemons/com.deepsea.myapp.plist"}})
	})
}

// TestWindowsCreateDirPathEscape 验证 Windows CreateDir 路径用单引号包裹,
// 含空格时命令拼接正确(避免 PowerShell 解析错误)。
func TestWindowsCreateDirPathEscape(t *testing.T) {
	b := &WindowsBuilder{}
	cmd := b.CreateDir(`C:\my app`)
	// Args = ["-Command", "New-Item -ItemType Directory -Force -Path 'C:\my app'"]
	want := `New-Item -ItemType Directory -Force -Path 'C:\my app'`
	if len(cmd.Args) < 2 || cmd.Args[1] != want {
		t.Errorf("CreateDir path not quoted correctly: got args=%v, want args[1]=%q", cmd.Args, want)
	}
}
