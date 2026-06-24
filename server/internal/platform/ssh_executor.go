package platform

import (
	"fmt"
	"strings"

	"github.com/deepsea-ops/server/internal/sshclient"
)

// SSHExecutor 通过 SSH 远程执行命令, 实现 Executor 接口。
// 用于 inject 模块向远程服务器推送二进制并拉起服务。
type SSHExecutor struct {
	client *sshclient.Client
}

// NewSSHExecutor 创建 SSH 执行器, 需要先建立 SSH 连接。
func NewSSHExecutor(client *sshclient.Client) *SSHExecutor {
	return &SSHExecutor{client: client}
}

// Run 通过 SSH 执行命令, 返回 stdout/stderr/退出码。
// SSH 协议不区分 stdout/stderr, 合并返回; exitCode 从错误中推断。
func (e *SSHExecutor) Run(cmd Command) (string, string, int, error) {
	// 构造完整命令字符串
	fullCmd := cmd.Name
	for _, arg := range cmd.Args {
		fullCmd += " " + shellQuote(arg)
	}
	timeout := cmd.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	out, err := e.client.RunCommandTimeout(fullCmd, timeout)
	if err != nil {
		// SSH 错误中可能包含退出码信息, 但 sshclient 不返回 exitCode
		// 简化处理: 有错误时 exitCode=1, 无错误时 exitCode=0
		return out, "", 1, err
	}
	return out, "", 0, nil
}

// RunBackground 通过 SSH 后台启动命令。
// SSH 远程后台启动用 nohup + & 的方式。
func (e *SSHExecutor) RunBackground(cmd Command) (int, error) {
	fullCmd := cmd.Name
	for _, arg := range cmd.Args {
		fullCmd += " " + shellQuote(arg)
	}
	// nohup ... > /dev/null 2>&1 &  后台启动, 立即返回
	bgCmd := fmt.Sprintf("nohup %s > /dev/null 2>&1 & echo $!", fullCmd)
	out, err := e.client.RunCommand(bgCmd)
	if err != nil {
		return 0, fmt.Errorf("SSH 后台启动失败: %w", err)
	}
	// 解析 PID(命令输出最后一行是 PID)
	out = trimWhitespace(out)
	var pid int
	for _, c := range out {
		if c >= '0' && c <= '9' {
			pid = pid*10 + int(c-'0')
		} else if pid > 0 {
			break
		}
	}
	return pid, nil
}

// shellQuote 对参数做 shell 单引号转义(从 sshclient 迁移, 统一使用)。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// trimWhitespace 去除首尾空白。
func trimWhitespace(s string) string {
	return strings.TrimSpace(s)
}
