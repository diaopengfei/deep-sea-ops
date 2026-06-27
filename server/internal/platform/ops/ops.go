// Package ops 提供基于 platform 抽象的领域操作接口。
// 每个接口(ProcessOps/FileOps/ServiceOps 等)组合 CommandBuilder + Executor,
// 为调用方提供与平台无关的业务语义。
package ops

import (
	"github.com/deepsea-ops/server/internal/platform"
)

// Ops 是所有领域操作的聚合体, 调用方持有 *Ops 即可访问全部能力。
type Ops struct {
	Process    ProcessOps
	File       FileOps
	Service    ServiceOps
	Deploy     DeployOps
	Scan       ScanOps
	Middleware MiddlewareOps // v0.6.7: 中间件状态查询(扩展点, 当前供未来指令调用)
	builder    platform.CommandBuilder
}

// NewOps 根据 PlatformInfo 和 Executor 创建 Ops 聚合。
func NewOps(p platform.PlatformInfo, exec platform.Executor) *Ops {
	builder := platform.NewCommandBuilder(p)
	return &Ops{
		Process:    newProcessOps(builder, exec),
		File:       newFileOps(builder, exec),
		Service:    newServiceOps(builder, exec),
		Deploy:     newDeployOps(builder, exec),
		Scan:       newScanOps(builder, exec),
		Middleware: newMiddlewareOps(builder, exec),
		builder:    builder,
	}
}

// Builder 返回底层 CommandBuilder, 供需要直接构建命令的场景使用(如设置工作目录的 Java 启动)。
func (o *Ops) Builder() platform.CommandBuilder {
	return o.builder
}

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
