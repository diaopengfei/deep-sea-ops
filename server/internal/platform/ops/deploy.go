package ops

import (
	"fmt"

	"github.com/deepsea-ops/server/internal/platform"
)

// DeployOps 部署操作接口。
type DeployOps interface {
	StartJava(jarPath string, args []string) (pid int, err error)
	StopJava(pid int) error
	DeployDir(projectName string) string
}

type deployOps struct {
	builder  platform.CommandBuilder
	executor platform.Executor
	process  ProcessOps
}

func newDeployOps(b platform.CommandBuilder, e platform.Executor) *deployOps {
	return &deployOps{
		builder:  b,
		executor: e,
		process:  newProcessOps(b, e),
	}
}

func (o *deployOps) StartJava(jarPath string, args []string) (int, error) {
	cmd := o.builder.StartJava(jarPath, args)
	pid, err := o.executor.RunBackground(cmd)
	if err != nil {
		return 0, fmt.Errorf("启动 Java 失败: %w", err)
	}
	return pid, nil
}

func (o *deployOps) StopJava(pid int) error {
	return killProcessWithWait(o.process, pid)
}

func (o *deployOps) DeployDir(projectName string) string {
	return o.builder.DeployDir(projectName)
}
