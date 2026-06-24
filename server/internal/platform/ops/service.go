package ops

import (
	"github.com/deepsea-ops/server/internal/platform"
)

// ServiceState 服务状态。
type ServiceState int

const (
	ServiceRunning   ServiceState = iota // 运行中
	ServiceStopped                       // 已停止
	ServiceNotFound                      // 不存在
	ServiceUnknown                       // 未知
)

// ServiceOps 服务管理接口。
type ServiceOps interface {
	Start(name string) error
	Stop(name string) error
	Enable(name string) error
	Status(name string) (ServiceState, error)
	Install(name, binaryPath, configPath string) error
	Uninstall(name string) error
}

type serviceOps struct {
	builder  platform.CommandBuilder
	executor platform.Executor
}

func newServiceOps(b platform.CommandBuilder, e platform.Executor) *serviceOps {
	return &serviceOps{builder: b, executor: e}
}

func (o *serviceOps) Start(name string) error {
	_, _, _, err := o.executor.Run(o.builder.StartService(name))
	return err
}

func (o *serviceOps) Stop(name string) error {
	_, _, _, err := o.executor.Run(o.builder.StopService(name))
	return err
}

func (o *serviceOps) Enable(name string) error {
	_, _, _, err := o.executor.Run(o.builder.EnableService(name))
	return err
}

func (o *serviceOps) Status(name string) (ServiceState, error) {
	_, _, code, err := o.executor.Run(o.builder.ServiceStatus(name))
	if err != nil && code != 0 {
		// 非零退出码可能是服务未运行或不存在
		return ServiceUnknown, nil
	}
	if code == 0 {
		return ServiceRunning, nil
	}
	return ServiceStopped, nil
}

func (o *serviceOps) Install(name, binaryPath, configPath string) error {
	// 注意: 实际的 service 文件/plist 写入由 inject 模块处理
	// 这里只执行 install 命令(daemon-reload / chmod +x 等)
	_, _, _, err := o.executor.Run(o.builder.InstallService(name, binaryPath, configPath))
	return err
}

func (o *serviceOps) Uninstall(name string) error {
	_, _, _, err := o.executor.Run(o.builder.UninstallService(name))
	return err
}
