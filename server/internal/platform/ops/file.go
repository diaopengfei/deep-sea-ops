package ops

import (
	"os"
	"path/filepath"

	"github.com/deepsea-ops/server/internal/platform"
)

// FileOps 文件操作接口。
// 简单文件读写直接用 os 标准库(本身跨平台), 不走 Executor。
// 只有需要 shell 特性的操作(CreateDir/Chmod)才走 Builder+Executor。
type FileOps interface {
	Read(path string) ([]byte, error)
	Write(path string, content []byte) error
	ReadHosts() ([]byte, error)
	CreateDir(path string) error
	Chmod(path string, mode string) error
}

type fileOps struct {
	builder  platform.CommandBuilder
	executor platform.Executor
}

func newFileOps(b platform.CommandBuilder, e platform.Executor) *fileOps {
	return &fileOps{builder: b, executor: e}
}

func (o *fileOps) Read(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (o *fileOps) Write(path string, content []byte) error {
	// 确保父目录存在
	dir := filepath.Dir(path)
	if dir != "." && dir != "/" {
		_ = o.CreateDir(dir)
	}
	return os.WriteFile(path, content, 0o644)
}

func (o *fileOps) ReadHosts() ([]byte, error) {
	return os.ReadFile(o.builder.HostsFilePath())
}

func (o *fileOps) CreateDir(path string) error {
	cmd := o.builder.CreateDir(path)
	_, _, _, err := o.executor.Run(cmd)
	return err
}

func (o *fileOps) Chmod(path string, mode string) error {
	cmd := o.builder.Chmod(path, mode)
	_, _, _, err := o.executor.Run(cmd)
	return err
}
