package platform

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// Executor 是命令执行后端抽象, 本地和远程统一接口。
type Executor interface {
	// Run 同步执行命令, 返回 stdout/stderr/退出码。
	Run(cmd Command) (stdout string, stderr string, exitCode int, err error)
	// RunBackground 后台启动命令, 返回 PID。
	RunBackground(cmd Command) (pid int, err error)
}

// LocalExecutor 本地执行命令, 封装 os/exec。
type LocalExecutor struct{}

// NewLocalExecutor 创建本地执行器。
func NewLocalExecutor() *LocalExecutor { return &LocalExecutor{} }

// Run 同步执行命令, 带超时和 goroutine 泄漏防护。
// 超时后 kill 进程并等待 goroutine 退出, 防止泄漏。
func (e *LocalExecutor) Run(cmd Command) (string, string, int, error) {
	timeout := cmd.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	execCmd := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	if cmd.Stdin != nil {
		execCmd.Stdin = cmd.Stdin
	}
	var stdout, stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr
	err := execCmd.Run()
	code := 0
	if execCmd.ProcessState != nil {
		code = execCmd.ProcessState.ExitCode()
	}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return stdout.String(), stderr.String(), code, fmt.Errorf("命令执行超时 (%v): %s %v", timeout, cmd.Name, cmd.Args)
		}
		return stdout.String(), stderr.String(), code, fmt.Errorf("命令执行失败: %w", err)
	}
	return stdout.String(), stderr.String(), code, nil
}

// RunBackground 后台启动命令, 不等待完成, 返回 PID。
func (e *LocalExecutor) RunBackground(cmd Command) (int, error) {
	execCmd := exec.Command(cmd.Name, cmd.Args...)
	if cmd.Stdin != nil {
		execCmd.Stdin = cmd.Stdin
	}
	if err := execCmd.Start(); err != nil {
		return 0, fmt.Errorf("后台启动失败: %w", err)
	}
	return execCmd.Process.Pid, nil
}
