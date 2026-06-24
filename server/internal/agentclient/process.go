package agentclient

import (
	"strings"

	"github.com/deepsea-ops/server/internal/platform"
	"github.com/deepsea-ops/server/internal/platform/ops"
)

// RunningProcess 记录一个运行中的进程, 用于判断项目是否在运行。
// 保留原类型以兼容现有调用方, 内部转换为 ops.ProcessInfo。
type RunningProcess struct {
	PID     int    `json:"pid"`
	Name    string `json:"name"`
	CmdLine string `json:"cmdLine"`
}

// 全局 PlatformInfo 和 Ops, Agent 启动时初始化一次。
var (
	globalPlatform platform.PlatformInfo
	globalOps      *ops.Ops
)

// InitPlatform 初始化平台抽象层, Agent 启动时调用一次。
func InitPlatform() {
	globalPlatform = platform.DetectPlatform()
	globalOps = ops.NewOps(globalPlatform, platform.NewLocalExecutor())
}

// ListProcesses 获取当前运行的进程列表。
// 委托给 platform.ops, 保持原签名以兼容调用方。
func ListProcesses() []RunningProcess {
	if globalOps == nil {
		return []RunningProcess{}
	}
	infos, err := globalOps.Process.List()
	if err != nil {
		return []RunningProcess{}
	}
	out := make([]RunningProcess, 0, len(infos))
	for _, info := range infos {
		out = append(out, RunningProcess{
			PID:     info.PID,
			Name:    info.Name,
			CmdLine: info.CmdLine,
		})
	}
	return out
}

// IsProjectRunning 判断扫描到的项目是否在运行, 返回 (是否运行, PID)。
func IsProjectRunning(projectPath string, processes []RunningProcess) (bool, int) {
	for _, p := range processes {
		if strings.Contains(p.CmdLine, projectPath) {
			return true, p.PID
		}
	}
	return false, 0
}
