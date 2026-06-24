package platform

import (
	"io"
	"time"
)

// ProcessSignal 表示发送给进程的信号类型。
type ProcessSignal int

const (
	SignalTerm  ProcessSignal = iota // SIGTERM (15), 优雅退出
	SignalKill                       // SIGKILL (9), 强制终止
	SignalCheck                      // kill -0, 仅检测进程是否存在
)

// Command 表示一条与平台无关的命令(程序名+参数+超时)。
// 由 CommandBuilder 生成, 由 Executor 执行。
type Command struct {
	Name    string        // 程序名: "systemctl" / "tasklist" / "java"
	Args    []string      // 参数: ["status", "nginx"]
	Timeout time.Duration // 超时, 0 表示用默认值(60s)
	Stdin   io.Reader     // 可选输入
}

// DefaultTimeout 是命令执行的默认超时。
const DefaultTimeout = 60 * time.Second
