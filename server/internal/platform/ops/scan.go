package ops

import "github.com/deepsea-ops/server/internal/platform"

// ScanOps 扫描操作接口。
type ScanOps interface {
	DefaultDirs() []string
}

type scanOps struct {
	builder  platform.CommandBuilder
	executor platform.Executor
}

func newScanOps(b platform.CommandBuilder, e platform.Executor) *scanOps {
	return &scanOps{builder: b, executor: e}
}

func (o *scanOps) DefaultDirs() []string {
	return o.builder.DefaultScanDirs()
}
