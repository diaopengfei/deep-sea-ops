<p align="center">
  <img src="docs/images/banner.svg" alt="deepsea-ops" width="640" />
</p>

<p align="center">
  <strong>分布式服务器运维平台 —— 一套工具管理 20+ 台服务器上的 Java 微服务、Python 程序与中间件</strong>
</p>

<p align="center">
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white" alt="Go" /></a>
  <a href="https://vuejs.org/"><img src="https://img.shields.io/badge/Vue-3-42b883?logo=vuedotjs&logoColor=white" alt="Vue 3" /></a>
  <a href="https://developer.mozilla.org/zh-CN/docs/Web/JavaScript"><img src="https://img.shields.io/badge/TypeScript-5.6-3178c6?logo=typescript&logoColor=white" alt="TypeScript" /></a>
  <img src="https://img.shields.io/badge/Raft-3%E8%8A%82%E7%82%B9%E5%AE%B9%E9%94%99-ff69b4" alt="Raft" />
  <img src="https://img.shields.io/badge/gRPC-%E5%8F%8C%E5%90%91%E6%B5%81-244c8e?logo=grpc&logoColor=white" alt="gRPC" />
  <img src="https://img.shields.io/badge/version-v0.6.0-blue" alt="v0.6.0" />
  <img src="https://img.shields.io/badge/license-MIT-green" alt="MIT" />
</p>

<p align="center">
  <a href="#快速开始">快速开始</a> ·
  <a href="#特性">特性</a> ·
  <a href="#架构">架构</a> ·
  <a href="#截图">截图</a> ·
  <a href="docs/">文档</a> ·
  <a href="#路线图">路线图</a> ·
  <a href="#贡献">贡献</a>
</p>

---

## 为什么造它

管理 20+ 台服务器上的多类中间件和服务, 传统方式靠 SSH + 脚本 + Excel, 配置漂移、扩容繁琐、迁移风险高。deepsea-ops 用一套分布式控制面统一管理:

- **配置一处维护** —— 连接 Nacos / 本地配置 / jar 内配置, 三方比对一目了然
- **扩容迁移一键编排** —— Leader 编排, 下发部署指令到目标 Agent, 状态实时回传
- **任意节点可访问** —— 入口代理让浏览器打任意节点 IP 都能访问 UI
- **故障自动切换** —— 3 节点 Raft 强一致, 容忍 1 节点故障, 秒级 Leader 切换

## 特性

- **分布式控制面** — 3 节点 Raft 强一致集群 (hashicorp/raft), 容忍 1 节点故障, 秒级 Leader 切换
- **Agent 架构** — 每台被管机器跑轻量 Agent, gRPC 双向流长连接, 心跳 + 指令下发
- **配置文件启动** — 参考 Kafka / ES, 通过 YAML 配置文件启动, 不再依赖命令行参数
- **配置治理** — 连接 Nacos / 本地配置 / jar 内配置, 三方比对, 基准版本走 Raft 强一致
- **自动扫描** — Agent 自动扫描 Java Spring / Java jar / Python 项目, 进程检测, 生效配置三路合并
- **扩容迁移** — Leader 编排部署任务, jar 分发、配置写入、进程启停, 状态实时回传
- **服务器管理** — 自增 ID, 支持 Linux / Windows 类型, SSH 连接测试, 全字段排序与模糊检索
- **SSH 自动注入** — SSH 推送二进制 + systemd 配置, 远程拉起服务, Raft 节点自动 join, Agent 自动连 Leader
- **拓扑可视化** — AntV G6 渲染 Raft 节点 + Agent 节点拓扑, Leader/Follower 状态高亮
- **安全鉴权** — JWT + bcrypt 密码哈希 + 登录限流防爆破, SSH 凭据 AES-GCM 加密存储
- **单二进制部署** — Go 交叉编译纯静态 ELF, Agent 推送即跑, 控制面自带前端

## 架构

```
                ┌──────────────────────────────────┐
   浏览器 ─────▶│  控制面 (3 节点 Raft 强一致集群)    │
                │  HTTP:8080  gRPC:9090  Raft:7000  │
                │  + 入口代理 (任意节点可访问)       │
                └──────────────┬───────────────────┘
                  gRPC 长连接   │ 心跳 + 指令下发
            ┌──────────────────┼──────────────────┐
            ▼                  ▼                  ▼
       Agent@工作机1      Agent@工作机2     ... Agent@工作机N
       (扫描/部署/读配置)  (扫描/部署/读配置)    (扫描/部署/读配置)
```

**设计原则**: 必须一致的数据 (服务器清单、配置基准、部署计划) 进 Raft 状态机; 瞬时高频数据 (Agent 心跳、负载、进程状态) 走内存, 不付一致性成本。

### 数据存放策略

| 数据                      | 存放               | 一致性 |
| ----------------------- | ---------------- | --- |
| 服务器清单、用户、项目、部署任务、SSH 凭据 | Raft 状态机 (bbolt) | 强一致 |
| Agent 实时心跳、负载、进程状态      | Leader 内存        | 弱一致 |

## 技术栈

| 层     | 技术                        | 说明                     |
| ----- | ------------------------- | ---------------------- |
| 后端语言  | Go 1.22+                  | 单二进制部署, Raft 生态最成熟     |
| 一致性   | hashicorp/raft v1.7.3     | Consul / Nomad 同款, 工业级 |
| 存储    | bbolt (raft-boltdb/v2)    | 嵌入式 KV, Raft FSM 后端    |
| 通信    | gRPC + protobuf           | 双向流, Agent ↔ 控制面长连接    |
| 前端    | Vue 3 + TypeScript + Vite |                        |
| UI 组件 | Element Plus              | 表格 / 表单 / 树 / 抽屉       |
| 拓扑可视化 | AntV G6 v5                | 关系图 / 拓扑               |
| 配置编辑  | Monaco Editor             | yml 编辑 + diff          |
| 加密    | AES-GCM                   | SSH 凭据加密, 主密钥从配置文件/环境变量注入     |

## 快速开始

### 环境要求

- **Go** 1.22+
- **Node.js** 18+
- **Git**

### 方式一: 启动脚本 (推荐)

```bash
# Linux / macOS / Git Bash
./scripts/start.sh            # 启动控制面 + Agent + 前端

# Windows PowerShell
.\scripts\start.ps1           # 同上
```

启动后访问 `http://localhost:5173`, 默认账号 `admin / admin123`。

支持的模式:

| 命令                           | 说明                          |
| ---------------------------- | --------------------------- |
| `./scripts/start.sh dev`     | 单节点控制面 + Agent + 前端 (默认)    |
| `./scripts/start.sh cluster` | 3 节点 Raft 本地集群 + Agent + 前端 |
| `./scripts/start.sh server`  | 仅控制面                        |
| `./scripts/start.sh agent`   | 仅 Agent                     |
| `./scripts/start.sh web`     | 仅前端                         |

停止所有进程:

```bash
./scripts/stop.sh             # Linux / macOS
.\scripts\stop.ps1            # Windows
```

### 方式二: 手动启动

```bash
# 终端 1: 控制面 (使用默认配置 config/server.yaml, 文件不存在则用内置默认值)
cd server && go run ./cmd/server

# 终端 2: Agent (使用默认配置 config/agent.yaml)
cd server && go run ./cmd/agent

# 终端 3: 前端
cd web && npm install && npm run dev    # http://localhost:5173
```

指定配置文件启动:

```bash
go run ./cmd/server -config /path/to/server.yaml
go run ./cmd/agent  -config /path/to/agent.yaml
```

### 方式三: Make 构建

```bash
make build          # 构建后端 + 前端 (当前平台)
make build-linux    # 交叉编译 Linux amd64 (部署用)
make dev            # 提示开发启动命令
make check          # 格式化 + 静态检查
```

## 截图

> 控制面提供以下页面:

| 页面     | 功能                                              |
| ------ | ----------------------------------------------- |
| 服务器管理  | 管理被控服务器, 查看 Agent 在线状态                          |
| 集群拓扑   | G6 可视化 Raft 节点 + Agent 节点拓扑, Leader/Follower 高亮 |
| 项目扫描   | Agent 自动扫描 Java/Python 项目, 展示运行状态 + 生效配置        |
| 配置比对   | Nacos / 本地 / jar 三路配置 git 风格 diff               |
| 扩容迁移   | 创建部署任务, 实时查看执行状态                                |
| SSH 凭据 | 管理 SSH 连接凭据 (AES-GCM 加密存储)                      |
| SSH 注入 | 一键推送二进制 + systemd, 自动加入集群                       |

## 项目结构

```
deepsea-ops/
├── server/                      Go 后端
│   ├── cmd/
│   │   ├── server/              控制面入口 (HTTP + gRPC + Raft)
│   │   └── agent/               Agent 入口
│   ├── internal/                私有包 (Go internal 强制封装)
│   │   ├── model/               领域模型 (Server/User/Project/DeployTask/SSHCredential)
│   │   ├── store/               Raft 存储层 (FSM/Store/Command, 5 个 bbolt bucket)
│   │   ├── api/                 HTTP 路由 + 入口代理 (Leader 转发)
│   │   ├── grpcserver/          Agent gRPC 连接管理
│   │   ├── agentclient/         Agent 端逻辑 (连接/扫描/部署/进程检测)
│   │   ├── auth/                JWT + bcrypt + 登录限流
│   │   ├── crypto/              AES-GCM 加密 (SSH 凭据)
│   │   ├── sshclient/           SSH 远程操作 (连接/上传/命令)
│   │   ├── inject/              自动注入 (SSH 推送 + systemd)
│   │   ├── config/              YAML 配置文件加载
│   │   ├── configdiff/          三路配置 diff
│   │   └── proto/agent/         protoc 生成代码
│   └── proto/agent.proto        gRPC 通信契约
├── web/                         Vue 3 前端
│   └── src/{api,views,styles}/
├── config/                      配置文件示例
│   ├── server.yaml.example      控制面配置示例
│   └── agent.yaml.example       Agent 配置示例
├── scripts/                     启动 / 停止脚本
│   ├── start.sh / start.ps1     开发环境启动 (自动生成配置文件)
│   └── stop.sh / stop.ps1       停止所有进程
├── docs/                        项目文档
│   ├── images/banner.svg        项目 banner
│   ├── 架构设计.md
│   ├── 后端代码导读.md
│   ├── Raft原理详解.md
│   └── 部署指南.md
├── Makefile                     构建脚本
└── dist/                        构建产物 (gitignore)
```

依赖方向: `main → api → store → model` 单向不循环。`internal/` 外部 module 不可 import, Go 语言级封装。

## 配置

v0.5 起改为 YAML 配置文件启动 (参考 Kafka / Elasticsearch), 不再依赖命令行参数。

### 启动参数

| 参数       | 默认值                    | 说明                                |
| -------- | ---------------------- | --------------------------------- |
| `-config` | `config/server.yaml` (控制面) / `config/agent.yaml` (Agent) | 配置文件路径, 不指定则查找默认路径, 文件不存在用内置默认值 |

### 控制面配置 `config/server.yaml`

```yaml
# Raft 节点 ID (集群内唯一)
node_id: node1

raft:
  addr: 127.0.0.1:7000      # Raft 通信地址 (多节点用内网 IP)
  data_dir: raft-data        # Raft 数据目录 (必须持久化)
  join: ""                   # 加入已有集群时填 Leader 的 Raft 地址; 为空表示首节点

http:
  addr: :8080                # HTTP 监听 (前端 + REST API)

grpc:
  addr: :9090                # gRPC 监听 (Agent 连接)

# 安全相关配置 (v0.5.1+)
# 多节点 Raft 集群中, jwt_secret 和 master_key 必须在所有节点保持一致
security:
  jwt_secret: "deepsea-dev-secret-change-me"   # JWT 签名密钥 (生产必须修改)
  admin_password: "admin123"                   # 初始管理员密码 (仅首次启动生效)
  master_key: ""                               # SSH凭据加密主密钥(32字节base64, 留空则开发模式随机生成)
```

### Agent 配置 `config/agent.yaml`

```yaml
agent_id: agent-1
server: 127.0.0.1:9090       # 控制面 gRPC 地址
```

完整示例见 [config/server.yaml.example](config/server.yaml.example) 和 [config/agent.yaml.example](config/agent.yaml.example)。

### 配置优先级与多节点一致性

**优先级** (从高到低):
1. **环境变量** — `JWT_SECRET` / `ADMIN_PASSWORD` / `MASTER_KEY` (容器化部署时用, 如 K8s Secret)
2. **YAML 配置文件** — `security.jwt_secret` 等
3. **内置默认值** — 开发环境用, 启动时打印警告

**多节点 Raft 集群一致性要求**:

| 配置项 | 是否必须一致 | 原因 |
|---|---|---|
| `jwt_secret` | **必须一致** | 入口代理转发请求到任意节点, JWT Token 必须被所有节点验证通过 |
| `master_key` | **必须一致** | SSH 凭据加密后存 Raft 复制到所有节点, Follower 当选 Leader 后需解密凭据 |
| `admin_password` | 非必须 | 仅首节点首次启动创建 admin 时生效, 之后密码 hash 存 Raft 复制 |

> **生产部署**: 务必在 `server.yaml` 中显式设置 `jwt_secret` 和 `master_key`, 或通过环境变量注入。生成新 `master_key`: `openssl rand -base64 32`

## 部署

生产部署到 Linux 集群见 [部署指南](docs/部署指南.md): 交叉编译、systemd、nginx、Agent 批量部署、滚动升级。

快速交叉编译:

```bash
make build-linux    # 产出 dist/deepsea-server, dist/deepsea-agent (纯静态 ELF), web/dist/
```

产出的是纯静态 ELF 二进制 (`CGO_ENABLED=0`), 任意 Linux 发行版直接 `./deepsea-server` 即可运行, 无 glibc 版本依赖。

## 文档

完整文档在 [`docs/`](docs/) 目录:

| 文档                              | 内容                                 |
| ------------------------------- | ---------------------------------- |
| [架构设计](docs/架构设计.md)            | 项目目标、拓扑选型、技术栈、演进路径                 |
| [后端代码导读](docs/后端代码导读.md)        | Go 语法速查 + 逐文件解读 + 数据流, 零 Go 基础可读   |
| [Raft 原理详解](docs/Raft原理详解.md)   | Raft 每个机制的必要性, Leader/多数派/日志/快照/脑裂 |
| [部署指南](docs/部署指南.md)            | Linux 集群打包、交叉编译、systemd、nginx、升级   |

## 路线图

- **v0.1** 单节点控制面 + Agent 骨架 ✅
  
  - Raft 单节点存储、bbolt 持久化、gRPC 双向流、Agent 心跳、读配置回传

- **v0.2** 3 节点 Raft 容错集群 ✅
  
  - 控制面 1→3 节点, 动态 AddVoter 加节点, 选举/复制/故障切换
  - 杀 Leader 后 Follower 秒级当选, 已提交数据不丢

- **v0.3** Java 运维 MVP + 安全鉴权 ✅
  
  - M1 登录鉴权 (JWT + bcrypt + 限流)
  - M2 三路配置比对 (Nacos / 本地 / jar)
  - M3 Agent 自动扫描 (Java/Python 项目识别 + 进程检测 + 生效配置合并)
  - M4 配置自动发现增强 (选 Agent → 选项目 → 自动填充 → 比对)
  - M5 扩容迁移编排 (jar 分发 + 配置写入 + 进程启停)
  - M6 拓扑可视化 (AntV G6)

- **v0.4** 自动部署 + 入口代理 ✅
  
  - SSH 凭据加密存储 (AES-GCM)
  - SSH 自动注入 (推送二进制 + systemd, Raft 节点自动 join, Agent 自动连 Leader)
  - 入口代理 (任意节点 IP 可访问 UI, 写请求转发 Leader, 读请求本地)
  - 深度代码审查, 修复 17 处缺陷

- **v0.5** 配置文件启动 + 服务器管理重构 ✅
  
  - YAML 配置文件启动 (参考 Kafka / ES, `-config` 指定路径, 默认 `config/server.yaml`)
  - 服务器模型重构: ID 改为数字自增, 新增 OS 类型 (Linux/Windows, 默认 Linux)
  - 服务器导入支持 SSH 用户名/密码, 可测试连接状态
  - 服务器列表支持全字段排序 + 全字段模糊检索
  - 清理无用文档和脚本

- **v0.5.1** 安全配置纳入配置文件 ✅
  
  - `server.yaml` 新增 `security` 段: `jwt_secret` / `admin_password` / `master_key`
  - 配置优先级: 环境变量 > YAML 配置 > 内置默认值
  - 多节点 Raft 集群一致性校验: `jwt_secret` 和 `master_key` 必须所有节点一致
  - 启动时安全配置校验与警告 (默认值/未设置/join 模式强警告)
  - `crypto` 和 `auth` 包改为显式初始化, 消除隐式环境变量依赖

- **v0.5.2** 动态扩容 + ops 服务节点 + 自动扫描 ✅
  
  - 服务器列表直接触发 raft/agent 注入 (用 Server 表 SSH 密码, 不再依赖 credentialId)
  - Raft 节点数量安全校验 (后端硬校验 3-7 范围 + 前端实时预览)
  - 「Agent 节点」菜单改名为「ops 服务节点」, 整合 raft/agent 状态 + 项目扫描 + 配置管理
  - 后台自动扫描调度器 (每 10 分钟扫描所有在线 Agent)
  - 扫描后自动触发配置比对 (从 effectiveConfig 提取 Nacos 地址)
  - 注入前校验目标服务器 OS (仅支持 Linux)
  - 深度代码审查, 修复 raft 校验逻辑/ops-nodes 状态/前端预填等 5 处问题

- **v0.5.3** 深度代码审查修复 12 处遗留问题 ✅
  
  - SSH shell 注入防护: 路径安全校验 + shellQuote 转义 + 二进制文件名白名单
  - SSH 命令超时保护: RunCommandTimeout 防止 goroutine 泄漏 (默认 60s, 可配置)
  - SSH HostKey 校验: 支持配置主机公钥, 未配置时打印一次警告
  - 优雅关闭: 替换 log.Fatal 为信号监听 + http.Server.Shutdown, defer 链正常执行
  - Follower GET 转发: /api/agents 和 /api/ops-nodes 的 GET 请求转发到 Leader (Agent 只连 Leader)
  - 服务器更新原子化: UpdServerFields 在 FSM 中读-改-写原子完成, 消除竞态
  - Raft voter 数量 TOCTOU: AddVoter 前做最终校验, 缩小竞态窗口
  - 扫描并发互斥: per-agent sync.Mutex + TryLock, 防止后台与手动扫描并发覆盖
  - 配置比对持久化: 比对结果存入 ProjectRecord.ConfigDiffJSON, 前端直接展示
  - Nacos 认证参数: 从 effectiveConfig 提取 username/password/accessToken/namespace
  - raft.Leader 常量: 替换硬编码 "Leader" 字符串
  - 错误日志化: FSM Snapshot/Restore/ClearAgentProjects 中的静默错误改为日志输出

- **后续**
  
  - 资源监控 (ECharts 曲线)
  - 操作审计日志
  - 配置中心化 (从 Nacos 拉取 server 配置)
  - 更多中间件管理 (Redis / PostgreSQL / Kafka / ES / ClickHouse)

## 开发

```bash
git clone https://github.com/<your-org>/deepsea-ops.git
cd deepsea-ops
./scripts/start.sh         # 一键启动开发环境
```

代码规范:

- Go: `gofmt` + `go vet`, 提交前 `make check`
- 前端: TypeScript strict mode, `vue-tsc --noEmit` 零错误
- 注释用中文, 技术术语用英文

## 贡献

欢迎 Issue 和 PR! 提交前请:

1. `make check` 确保格式化和静态检查通过
2. `go build ./...` 和 `vue-tsc --noEmit` 零错误
3. Commit message 遵循 [Conventional Commits](https://www.conventionalcommits.org/) 规范

## 许可证

[MIT](LICENSE)
