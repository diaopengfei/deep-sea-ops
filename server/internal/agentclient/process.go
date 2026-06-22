package agentclient

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// RunningProcess 记录一个运行中的进程, 用于判断项目是否在运行。
type RunningProcess struct {
	PID     int    `json:"pid"`
	Name    string `json:"name"`    // 进程名(java/python3 等)
	CmdLine string `json:"cmdLine"` // 完整命令行, 用于匹配项目路径或 jar 名
}

// ListProcesses 获取当前运行的进程列表。
// Linux: 读 /proc/*/cmdline; Windows: 调 tasklist; macOS: 读 /proc 不存在, 用 ps。
func ListProcesses() []RunningProcess {
	switch runtime.GOOS {
	case "linux":
		return listLinuxProcesses()
	case "windows":
		return listWindowsProcesses()
	default:
		return listUnixProcesses()
	}
}

// listLinuxProcesses 读 /proc/*/cmdline 获取进程列表。
func listLinuxProcesses() []RunningProcess {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return []RunningProcess{}
	}
	var out []RunningProcess
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid := 0
		for _, c := range entry.Name() {
			if c < '0' || c > '9' {
				pid = -1
				break
			}
		}
		if pid < 0 {
			continue
		}
		cmdlineBytes, err := os.ReadFile("/proc/" + entry.Name() + "/cmdline")
		if err != nil {
			continue
		}
		if len(cmdlineBytes) == 0 {
			continue
		}
		// /proc/*/cmdline 用 \0 分隔参数, 替换成空格
		cmd := strings.ReplaceAll(strings.TrimSpace(string(cmdlineBytes)), "\x00", " ")
		if cmd == "" {
			continue
		}
		parts := strings.Fields(cmd)
		name := parts[0]
		var pidInt int
		for _, c := range entry.Name() {
			pidInt = pidInt*10 + int(c-'0')
		}
		out = append(out, RunningProcess{PID: pidInt, Name: name, CmdLine: cmd})
	}
	return out
}

// listWindowsProcesses 用 tasklist 命令获取进程列表。
func listWindowsProcesses() []RunningProcess {
	cmd := exec.Command("tasklist", "/FO", "CSV", "/NH")
	output, err := cmd.Output()
	if err != nil {
		return []RunningProcess{}
	}
	var out []RunningProcess
	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// tasklist CSV 格式: "名称","PID","会话名","会话#","内存"
		fields := strings.Split(line, ",")
		if len(fields) < 2 {
			continue
		}
		name := strings.Trim(fields[0], "\"")
		pidStr := strings.Trim(fields[1], "\"")
		pid := 0
		for _, c := range pidStr {
			if c >= '0' && c <= '9' {
				pid = pid*10 + int(c-'0')
			}
		}
		if pid == 0 {
			continue
		}
		_ = i
		out = append(out, RunningProcess{PID: pid, Name: name, CmdLine: name})
	}
	return out
}

// listUnixProcesses 用 ps 命令获取进程列表(macOS 等)。
func listUnixProcesses() []RunningProcess {
	cmd := exec.Command("ps", "-eo", "pid=,comm=", "--no-headers")
	output, err := cmd.Output()
	if err != nil {
		return []RunningProcess{}
	}
	var out []RunningProcess
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		pid := 0
		for _, c := range parts[0] {
			if c >= '0' && c <= '9' {
				pid = pid*10 + int(c-'0')
			}
		}
		out = append(out, RunningProcess{PID: pid, Name: parts[1], CmdLine: strings.Join(parts[1:], " ")})
	}
	return out
}

// IsProjectRunning 判断扫描到的项目是否在运行。
// 匹配策略: 进程命令行包含项目路径 或 jar 文件名。
func IsProjectRunning(projectPath string, processes []RunningProcess) bool {
	for _, p := range processes {
		if strings.Contains(p.CmdLine, projectPath) {
			return true
		}
		// jar 项目: 检查 jar 文件名是否出现在命令行里
		// (java -jar /path/to/app.jar 的命令行包含 app.jar)
	}
	return false
}