# Agent 命令执行抽象层设计

> 版本: v0.6.0 (v0.6.1 结构优化)
> 状态: 已实现
> 日期: 2026-06-25

## 一、背景与目标

### 问题

Agent 在服务器上执行大量命令(进程检测、项目扫描、配置读取、进程启停、部署、服务注入),当前实现存在以下问题:

1. **无抽象层**: 命令直接用 `exec.Command` 或 `fmt.Sprintf` 内联拼接,散落在 6 个文件中
2. **硬编码 Linux**: `/etc/hosts`、`/opt`、`/home,/data`、`/etc/systemd/system/`、`/proc` 等路径硬编码,部分无 Windows 分支
3. **平台二分**: 仅 `runtime.GOOS` 区分 Linux/Windows,无发行版/init 系统检测,Alpine/CentOS 6 等非 systemd 环境会失败
4. **代码重复**: `shellQuote`/`safePath` 在 inject.go 和 sshclient/client.go 各有一份
5. **扩展困难**: 新增平台支持需修改多个文件,违反开闭原则

### 目标

用三层抽象(Builder + Command + Executor) + Domain Ops 重构命令执行模块,实现:

- 跨平台: Linux(systemd/SysVInit)、Windows、macOS 命令统一接口
- 可扩展: 新增平台只需新增 Builder 实现,不改调用方
- 解耦: 命令构建与执行分离,本地/远程统一接口
- 分批迁移: 5 个批次逐步迁移,每批可独立验证

## 二、整体架构

```
┌─────────────────────────────────────────────────────────────┐
│  调用方: agentclient(deploy/scanner/process/collector)        │
│         inject(远程注入)                                     │
└──────────────────────────┬──────────────────────────────────┘
                           │ 依赖
┌──────────────────────────▼──────────────────────────────────┐
│  第三层: Domain Ops(原子命令接口集合)                        │
│  ProcessOps / FileOps / ServiceOps / DeployOps / ScanOps     │
│  每个接口定义原子操作,不关心平台                              │
└──────────────────────────┬──────────────────────────────────┘
                           │ 实现
┌──────────────────────────▼──────────────────────────────────┐
│  第二层: Platform Implementations(按平台实现)                │
│  LinuxSystemdOps / LinuxSysVInitOps / WindowsOps / MacOSOps  │
│  每个 Ops 实现组合多个 CommandBuilder 调用                   │
└──────────────────────────┬──────────────────────────────────┘
                           │ 使用
┌──────────────────────────▼──────────────────────────────────┐
│  第一层: CommandBuilder + Command + Executor                 │
│  PlatformInfo(启动时检测) → Builder 选命令 → Executor 执行  │
└─────────────────────────────────────────────────────────────┘
```

**核心数据流**: `PlatformInfo(启动时检测) → CommandBuilder(按平台生成 Command) → Executor(本地/远程执行) → 调用方`

## 三、第一层: Command + Builder + Executor

### Command 结构体

表示层, 与执行后端解耦, 不含任何平台特定信息:

```go
type Command struct {
    Name    string        // 程序名: "systemctl" / "tasklist" / "java"
    Args    []string      // 参数: ["status", "nginx"]
    Timeout time.Duration // 超时, 0 表示用默认值(60s)
    Stdin   io.Reader     // 可选输入
}
```

### Executor 接口

执行后端抽象, 本地/远程统一接口:

```go
type Executor interface {
    // Run 同步执行命令, 返回 stdout/stderr/退出码
    Run(cmd Command) (stdout string, stderr string, exitCode int, err error)
    // RunBackground 后台启动命令, 返回 PID
    RunBackground(cmd Command) (pid int, err error)
    // UploadFile 上传文件(本地为 copy, 远程为 SCP)
    UploadFile(localPath, remotePath string) error
    // UploadContent 上传内容到远程文件
    UploadContent(content []byte, remotePath string) error
}
```

两个实现:
- `LocalExecutor` — 封装 `exec.Command`, 带超时和 goroutine 泄漏防护(复用 v0.5.3 的 RunCommandTimeout 模式)
- `SSHExecutor` — 封装 `sshclient.Client`, 实现同一接口

### CommandBuilder 接口

按平台生成命令, 无执行, 无状态:

```go
type CommandBuilder interface {
    // 进程
    ListProcesses() Command
    KillProcess(pid int, signal ProcessSignal) Command
    IsProcessAlive(pid int) Command
    // 文件
    ReadFile(path string) Command
    WriteFile(path string) Command
    ReadHostsFile() Command
    CreateDir(path string) Command
    Chmod(path string, mode string) Command
    // Java 部署
    StartJava(jarPath string, args []string) Command
    DeployDir(projectName string) string
    // 服务管理
    StartService(name string) Command
    StopService(name string) Command
    EnableService(name string) Command
    DisableService(name string) Command
    ServiceStatus(name string) Command
    InstallService(name, binaryPath, configPath string) Command
    UninstallService(name string) Command
    // 扫描
    DefaultScanDirs() []string
}
```

`ProcessSignal` 类型:

```go
type ProcessSignal int
const (
    SignalTerm ProcessSignal = iota  // SIGTERM (15)
    SignalKill                        // SIGKILL (9)
    SignalCheck                       // kill -0 检测
)
```

## 四、PlatformInfo(启动时检测)

```go
type PlatformInfo struct {
    OS         string // "linux" / "windows" / "darwin"
    Distro     string // "ubuntu" / "centos" / "debian" / "alpine" / ""
    InitSystem string // "systemd" / "sysvinit" / "openrc" / "windows-service" / "launchd"
    PkgManager string // "apt" / "yum" / "dnf" / "apk" / "" (本次不实现包安装)
}

func DetectPlatform() PlatformInfo  // 启动时调用一次, 缓存结果
```

### 检测逻辑

- `runtime.GOOS` 定 OS
- Linux:
  - 读 `/etc/os-release` 取 `ID=` 字段定发行版
  - 检测 `/run/systemd/system` 是否存在 → `systemd`
  - 回退检测 `/sbin/init` 是否为 systemd 软链接 → `systemd`
  - 否则检测 `/etc/init.d` 存在 → `sysvinit`
  - 否则检测 `rc-update` 命令存在 → `openrc`
- Windows: init 系统固定 `windows-service`
- macOS: `launchd`

### 工厂函数

```go
func NewCommandBuilder(p PlatformInfo) CommandBuilder {
    switch p.OS {
    case "linux":
        if p.InitSystem == "systemd" {
            return &LinuxSystemdBuilder{}
        }
        return &LinuxSysVInitBuilder{}
    case "windows":
        return &WindowsBuilder{}
    case "darwin":
        return &MacOSBuilder{}
    }
    return &LinuxSystemdBuilder{} // 兜底
}
```

## 五、第二层: 平台实现

### 本次实现的 Builder

| Builder | OS | Init 系统 | 覆盖场景 |
|---------|----|----------|---------|
| `LinuxSystemdBuilder` | Linux | systemd | Ubuntu 16+/CentOS 7+/Debian 8+(主流) |
| `LinuxSysVInitBuilder` | Linux | sysvinit/openrc | CentOS 6/Alpine/老系统 |
| `WindowsBuilder` | Windows | windows-service | Windows Server 2016+ |
| `MacOSBuilder` | macOS | launchd | 开发环境(基础) |

### 各 Builder 命令映射

#### LinuxSystemdBuilder

| 方法 | 命令 |
|------|------|
| ListProcesses | 读 `/proc` 目录(不通过 Command, 直接 os.ReadDir) |
| KillProcess | `kill -{signal} {pid}` |
| IsProcessAlive | `kill -0 {pid}` |
| ReadFile | `cat {path}`(或直接 os.ReadFile, 见下文决策) |
| ReadHostsFile | 读 `/etc/hosts` |
| CreateDir | `mkdir -p {path}` |
| Chmod | `chmod {mode} {path}` |
| StartJava | `java -jar {jarPath} {args}` |
| DeployDir | `/opt/{projectName}` |
| StartService | `systemctl start {name}` |
| StopService | `systemctl stop {name}` |
| EnableService | `systemctl enable {name}` |
| ServiceStatus | `systemctl status {name}` |
| InstallService | 写 `/etc/systemd/system/{name}.service` + `systemctl daemon-reload` |
| DefaultScanDirs | `["/home", "/data"]` |

#### LinuxSysVInitBuilder

与 systemd 的差异:
- `StartService` → `service {name} start`
- `StopService` → `service {name} stop`
- `EnableService` → `update-rc.d {name} defaults`(Debian)或 `chkconfig {name} on`(RHEL)
- `InstallService` → 写 `/etc/init.d/{name}` 脚本 + `chmod +x`

#### WindowsBuilder

| 方法 | 命令 |
|------|------|
| ListProcesses | `tasklist /FO CSV /NH` |
| KillProcess | `taskkill /F /PID {pid}` |
| IsProcessAlive | `tasklist /FI "PID eq {pid}"` |
| ReadHostsFile | 读 `C:\Windows\System32\drivers\etc\hosts` |
| CreateDir | `New-Item -ItemType Directory -Force -Path {path}` |
| StartJava | `javaw -jar {jarPath} {args}` |
| DeployDir | `%ProgramData%\deepsea\{projectName}` |
| StartService | `sc start {name}` |
| StopService | `sc stop {name}` |
| InstallService | `sc create {name} binPath= {binaryPath}` + 配置 |
| DefaultScanDirs | `["C:\\Program Files", "C:\\data"]` |

### 关键决策: 文件读取走 Executor 还是直接 os.ReadFile

**决策: 简单文件读取直接用 os.ReadFile, 不走 Executor**

理由:
1. `os.ReadFile` 本身跨平台, 无需 Builder 生成 `cat`/`Type` 命令
2. 走 Executor 会引入不必要的进程开销和错误路径
3. 只有需要 shell 特性的操作(如 chmod、systemctl)才走 Builder + Executor

因此 `ReadFile`/`WriteFile`/`ReadHostsFile` 在 Builder 中返回路径提示, 实际读取由 FileOps 直接调用 `os.ReadFile`。Builder 仅提供 `HostsFilePath()` 返回路径字符串。

修正后的 Builder 接口(文件部分):

```go
type CommandBuilder interface {
    // 进程(走 Executor)
    ListProcesses() Command
    KillProcess(pid int, signal ProcessSignal) Command
    IsProcessAlive(pid int) Command
    // 文件路径(不走 Executor, 直接 os 调用)
    HostsFilePath() string
    DeployDir(projectName string) string
    DefaultScanDirs() []string
    // 需要执行的操作(走 Executor)
    CreateDir(path string) Command
    Chmod(path string, mode string) Command
    StartJava(jarPath string, args []string) Command
    // 服务管理(走 Executor)
    StartService(name string) Command
    StopService(name string) Command
    EnableService(name string) Command
    ServiceStatus(name string) Command
    InstallService(name, binaryPath, configPath string) Command
    UninstallService(name string) Command
}
```

## 六、第三层: Domain Ops

按原子命令划分, 每个接口小而专注:

```go
type ProcessOps interface {
    List() ([]ProcessInfo, error)
    Kill(pid int, signal ProcessSignal) error
    IsAlive(pid int) (bool, error)
}

type FileOps interface {
    Read(path string) ([]byte, error)
    Write(path string, content []byte) error
    ReadHosts() ([]byte, error)
    CreateDir(path string) error
    Chmod(path string, mode string) error
}

type ServiceOps interface {
    Start(name string) error
    Stop(name string) error
    Enable(name string) error
    Status(name string) (ServiceState, error)
    Install(name, binaryPath, configPath string) error
    Uninstall(name string) error
}

type DeployOps interface {
    StartJava(jarPath string, args []string) (pid int, error)
    StopJava(pid int) error
    DeployDir(projectName string) string
}

type ScanOps interface {
    DefaultDirs() []string
}
```

### 实现类组合 Builder + Executor

```go
type processOps struct {
    builder  CommandBuilder
    executor Executor
}

func (o *processOps) List() ([]ProcessInfo, error) {
    cmd := o.builder.ListProcesses()
    out, _, _, err := o.executor.Run(cmd)
    if err != nil {
        return nil, err
    }
    return parseProcessList(out, o.builder)  // 按 Builder 类型选解析器
}

func (o *processOps) Kill(pid int, signal ProcessSignal) error {
    cmd := o.builder.KillProcess(pid, signal)
    _, _, _, err := o.executor.Run(cmd)
    return err
}
```

### Ops 工厂

```go
type Ops struct {
    Process ProcessOps
    File    FileOps
    Service ServiceOps
    Deploy  DeployOps
    Scan    ScanOps
}

func NewOps(p PlatformInfo, exec Executor) *Ops {
    builder := NewCommandBuilder(p)
    return &Ops{
        Process: newProcessOps(builder, exec),
        File:    newFileOps(builder, exec),
        Service: newServiceOps(builder, exec),
        Deploy:  newDeployOps(builder, exec),
        Scan:    newScanOps(builder, exec),
    }
}
```

调用方只需持有 `*Ops`,通过 `ops.Process.List()` 等调用,完全屏蔽平台差异。

## 七、目录结构

```
server/internal/
├── platform/                  # 平台抽象层
│   ├── platform.go            # PlatformInfo + DetectPlatform()
│   ├── builder.go             # CommandBuilder 接口 + 工厂 + Command 结构体 + ProcessSignal 类型
│   ├── executor.go            # Executor 接口 + LocalExecutor
│   ├── ssh_executor.go       # SSHExecutor(包装 sshclient.Client)
│   ├── linux_systemd.go      # LinuxSystemdBuilder
│   ├── linux_sysvinit.go     # LinuxSysVInitBuilder
│   ├── windows.go            # WindowsBuilder
│   ├── macos.go              # MacOSBuilder
│   └── ops/                  # Domain Ops 接口与实现
│       ├── ops.go            # Ops 结构体 + NewOps 工厂 + ScanOps(v0.6.1 合并)
│       ├── process.go        # ProcessOps 接口 + 实现 + 解析器
│       ├── file.go           # FileOps
│       ├── service.go        # ServiceOps
│       └── deploy.go         # DeployOps
├── shellutil/                 # v0.6.1 新增: 公共 shell 工具(Quote/SafePath), 被 platform 和 sshclient 共享
│   └── shellutil.go
```

## 八、分批迁移计划

| 批次 | 范围 | 验证点 | 风险 |
|------|------|--------|------|
| **批次1** | 建 platform 包骨架 + 迁移 `process.go` | `ListProcesses` 在 Linux/Windows 都通过 | 低, process.go 已有 GOOS 分发, 迁移最简单 |
| **批次2** | 迁移 `scanner.go`(hosts + 默认目录) | hosts 读取在 Windows 正常 | 低, 仅路径替换 |
| **批次3** | 迁移 `deploy.go`(启停 Java + 部署目录) | Java 启停跨平台 | 中, 涉及后台进程启动 |
| **批次4** | 迁移 `collector.go`(配置文件读取) | 三路采集不变 | 低, 主要 os.ReadFile |
| **批次5** | 迁移 `inject.go`(SSH 远程注入) | systemd/SysV/Windows Service 注入 | 高, inject 改动大, 需充分测试 |

每批迁移后:
1. 删除对应旧代码
2. `go build -buildvcs=false ./...` 验证
3. 提交一次(便于回滚)

## 九、错误处理

定义错误类型, 调用方可按错误类型降级:

```go
var (
    ErrNotSupported    = errors.New("platform: operation not supported")
    ErrCommandTimeout = errors.New("platform: command timeout")
    ErrServiceNotFound = errors.New("platform: service not found")
    ErrProcessNotFound = errors.New("platform: process not found")
)
```

Executor 返回的 `exitCode` 非 0 时, 调用方根据错误类型判断:
- `ErrCommandTimeout` → 重试或降级
- `ErrNotSupported` → 跳过该功能
- 其他 → 记录日志, 返回错误

## 十、关键设计决策

1. **Builder 无状态, Executor 有状态**: Builder 只生成 Command, 不执行, 可全局单例; Executor 持有连接(SSH)或无状态(Local), 按需创建
2. **Command 结构体统一表示**: 本地 `exec.Command` 和远程 SSH 命令都从 Command 转换, 消除 `fmt.Sprintf` 拼接
3. **简单文件操作走 os 标准库**: `os.ReadFile`/`os.WriteFile` 本身跨平台, 不走 Executor, 避免进程开销
4. **shellQuote/safePath 合并**: v0.6.1 起统一到独立的 `shellutil` 包(避免 platform 与 sshclient 互相依赖), inject.go 和 sshclient.go 的重复定义删除
5. **向后兼容**: 迁移期间旧代码与新抽象层并存, 每批迁移后删除对应旧代码
6. **PlatformInfo 启动时检测一次**: 运行中切换系统需重启 Agent(可接受, 服务器不会动态切换 OS)

## 十一、不实现(YAGNI)

以下能力本次不做, 留待后续:

- 包管理器抽象(apt/yum/apk/pacman)— 未来支持自动安装依赖时再做
- 容器内执行抽象(docker exec)— 当前无此需求
- SELinux/AppArmor 策略处理 — 当前无此需求
- 多架构(arm64/amd64)命令差异 — 命令本身无差异, 仅路径可能不同, 由 Builder 处理
