package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/deepsea-ops/server/internal/platform"
)

// ProcessInfo 记录一个运行中的进程。
type ProcessInfo struct {
	PID     int    `json:"pid"`
	Name    string `json:"name"`    // 进程名(java/python3 等)
	CmdLine string `json:"cmdLine"` // 完整命令行, 用于匹配项目路径或 jar 名
}

// ProcessOps 进程相关操作接口。
type ProcessOps interface {
	List() ([]ProcessInfo, error)
	Kill(pid int, signal platform.ProcessSignal) error
	IsAlive(pid int) (bool, error)
}

// processOps 组合 CommandBuilder + Executor 实现 ProcessOps。
type processOps struct {
	builder  platform.CommandBuilder
	executor platform.Executor
}

func newProcessOps(b platform.CommandBuilder, e platform.Executor) *processOps {
	return &processOps{builder: b, executor: e}
}

func (o *processOps) List() ([]ProcessInfo, error) {
	cmd := o.builder.ListProcesses()
	// 特殊标记: 直接读 /proc(Linux)
	if cmd.Name == ":read_proc" {
		return listProcProcesses()
	}
	// 通用: 执行命令并解析输出
	out, _, _, err := o.executor.Run(cmd)
	if err != nil {
		return nil, fmt.Errorf("获取进程列表失败: %w", err)
	}
	return parseProcessOutput(out, o.builder), nil
}

func (o *processOps) Kill(pid int, signal platform.ProcessSignal) error {
	cmd := o.builder.KillProcess(pid, signal)
	_, _, _, err := o.executor.Run(cmd)
	return err
}

func (o *processOps) IsAlive(pid int) (bool, error) {
	cmd := o.builder.IsProcessAlive(pid)
	_, _, code, err := o.executor.Run(cmd)
	// kill -0 / tasklist: 退出码 0 表示存在, 非 0 表示不存在
	if err != nil && code != 0 {
		return false, nil
	}
	return code == 0, nil
}

// listProcProcesses 读 /proc/*/cmdline 获取进程列表(Linux)。
// 从原 agentclient/process.go 迁移。
func listProcProcesses() ([]ProcessInfo, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("读取 /proc 失败: %w", err)
	}
	var out []ProcessInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid := 0
		valid := true
		for _, c := range entry.Name() {
			if c < '0' || c > '9' {
				valid = false
				break
			}
			pid = pid*10 + int(c-'0')
		}
		if !valid || pid == 0 {
			continue
		}
		cmdlineBytes, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil || len(cmdlineBytes) == 0 {
			continue
		}
		// /proc/*/cmdline 用 \0 分隔参数, 替换成空格
		cmd := strings.ReplaceAll(strings.TrimSpace(string(cmdlineBytes)), "\x00", " ")
		if cmd == "" {
			continue
		}
		parts := strings.Fields(cmd)
		name := parts[0]
		out = append(out, ProcessInfo{PID: pid, Name: name, CmdLine: cmd})
	}
	return out, nil
}

// parseProcessOutput 解析 tasklist/ps 命令输出。
// 用 builder.Platform() 区分输出格式, 避免跨包类型断言。
func parseProcessOutput(output string, builder platform.CommandBuilder) []ProcessInfo {
	var out []ProcessInfo
	lines := strings.Split(output, "\n")
	// 判断是 Windows tasklist 还是 Unix ps
	// Windows: "名称","PID","会话名","会话#","内存"
	// Unix ps: "pid comm"
	if builder.Platform() == "windows" {
		// WindowsBuilder
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
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
			out = append(out, ProcessInfo{PID: pid, Name: name, CmdLine: name})
		}
	} else {
		// Unix ps 输出: "pid comm"
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
			out = append(out, ProcessInfo{PID: pid, Name: parts[1], CmdLine: strings.Join(parts[1:], " ")})
		}
	}
	return out
}

// IsProjectRunning 判断扫描到的项目是否在运行, 返回 (是否运行, PID)。
// 匹配策略: 进程命令行包含项目路径 或 jar 文件名。
// 保留为包级函数, 兼容现有调用方。
func IsProjectRunning(projectPath string, processes []ProcessInfo) (bool, int) {
	for _, p := range processes {
		if strings.Contains(p.CmdLine, projectPath) {
			return true, p.PID
		}
	}
	return false, 0
}

// killProcessWithWait 先发 SIGTERM, 等待最多 5 秒, 仍不退出则 SIGKILL。
// 从原 agentclient/deploy.go 迁移, 供 DeployOps 使用。
func killProcessWithWait(po ProcessOps, pid int) error {
	if runtime.GOOS == "windows" {
		// Windows taskkill /F 直接强制, 无需等待
		return po.Kill(pid, platform.SignalKill)
	}
	// 先发 SIGTERM
	if err := po.Kill(pid, platform.SignalTerm); err != nil {
		return err
	}
	// 等待最多 5 秒
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		alive, _ := po.IsAlive(pid)
		if !alive {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	// 仍在运行, 强制 SIGKILL
	return po.Kill(pid, platform.SignalKill)
}
