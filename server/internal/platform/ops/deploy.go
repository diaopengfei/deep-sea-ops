package ops

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

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

// copyFile 复制文件, 用 io.Copy 流式复制避免大 jar 文件 OOM。
// 从原 agentclient/deploy.go 迁移。
func copyFile(src, dst string) error {
	srcF, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcF.Close()
	dstF, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstF.Close()
	_, err = io.Copy(dstF, srcF)
	return err
}

// deployDirPath 返回部署目录(兼容旧代码)。
func deployDirPath(builder platform.CommandBuilder, projectName string) string {
	return builder.DeployDir(projectName)
}

// targetJarPath 返回目标 jar 路径。
func targetJarPath(builder platform.CommandBuilder, projectName, jarPath string) string {
	return filepath.Join(builder.DeployDir(projectName), filepath.Base(jarPath))
}
