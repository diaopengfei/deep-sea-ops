# deepsea-ops

分布式服务器运维平台 —— 用一套工具管理 20+ 台服务器上的 Java 微服务、Java/Python 程序、Redis、PostgreSQL、Kafka、Elasticsearch、ClickHouse、Nacos。

> 状态: 早期开发中(v0.1)。首期聚焦 Java 微服务的配置管理与扩容迁移。

## 为什么造它

管理 20+ 台服务器上的多类中间件和服务, 传统方式靠 SSH + 脚本 + Excel, 配置漂移、扩容繁琐、迁移风险高。deepsea-ops 用一套分布式控制面统一管理: 配置一处维护、比对一目了然、扩容迁移一键编排, 且任意节点可访问、故障自动切换。

## 特性

- **分布式控制面**: 3 节点 Raft 强一致集群, 容忍 1 节点故障, 秒级 Leader 切换
- **Agent 架构**: 每台被管机器跑轻量 Agent, gRPC 长连接, 心跳 + 指令下发
- **配置治理**: 连接 Nacos / 本地配置 / jar 内配置, 三方比对, 基准版本走 Raft
- **扩容迁移**: Leader 编排, 下发部署指令到目标 Agent, 状态实时回传
- **可视化**: 服务器/服务拓扑图(AntV G6), 资源监控(ECharts), 配置 diff(Monaco)
- **单二进制部署**: Go 交叉编译, Agent 推送即跑, 控制面自带前端(embed)

## 架构

```
                ┌──────────────────────────────┐
   浏览器 ─────▶│  控制面 (3 节点 Raft)          │
                │  HTTP:8080  gRPC:9090  Raft:7000 │
                └──────────┬───────────────────┘
                  gRPC 长连 │ 心跳 + 指令
            ┌──────────────┼──────────────┐
            ▼              ▼              ▼
       Agent@工作机1   Agent@工作机2  ... Agent@工作机N
```

控制面用 Raft 保证强一致(配置、部署计划), Agent 在线状态走内存(高频瞬时数据不付一致性成本)。

## 技术栈

| 层 | 技术 |
|----|------|
| 后端 | Go 1.22+, hashicorp/raft, bbolt, gRPC |
| 前端 | Vue 3, TypeScript, Vite, Element Plus, ECharts, AntV G6 |
| 配置编辑 | Monaco Editor |

## 快速开始

### 环境要求

- Go 1.22+
- Node.js 18+
- Git

### 构建

```bash
# 构建全部(后端 + 前端, 当前平台)
make build         # 产出 dist/deepsea-server, dist/deepsea-agent, web/dist/

# 单独构建
make server        # 仅控制面 -> dist/deepsea-server
make agent         # 仅 Agent -> dist/deepsea-agent
make web           # 仅前端 -> web/dist/
```

### 构建 Linux 版本(在 Windows/Mac 开发机上交叉编译)

部署到 Linux 服务器时, 无需在 Linux 上安装 Go, 直接交叉编译:

```bash
# 只构建后端 Linux 二进制
make cross-linux          # 产出 dist/deepsea-server 和 dist/deepsea-agent (ELF, 纯静态)

# 后端 + 前端一起构建(部署用)
make build-linux          # = cross-linux + web, 产出 dist/ 和 web/dist/
```

产出的是纯静态 ELF 二进制(`CGO_ENABLED=0`), 任意 Linux 发行版直接 `./deepsea-server` 即可运行, 无 glibc 版本依赖。完整部署流程见部署指南文档。

### 本地运行(开发模式)

```bash
# 终端 1: 控制面
cd server
go run ./cmd/server

# 终端 2: Agent
cd server
go run ./cmd/agent -id agent-1 -server 127.0.0.1:9090

# 终端 3: 前端
cd web
npm run dev        # http://localhost:5173
```

打开 `http://localhost:5173`, 左侧"服务器管理"新增服务器, "Agent 节点"查看在线 Agent 并可读取其配置文件。

## 项目结构

```
deep-sea-ops/
├── server/                  Go 后端
│   ├── cmd/
│   │   ├── server/          控制面入口
│   │   └── agent/           Agent 入口
│   ├── internal/            私有包(外部不可 import)
│   │   ├── model/           领域模型
│   │   ├── store/           Raft 存储层(FSM/Store)
│   │   ├── api/             HTTP 路由
│   │   ├── grpcserver/      Agent 连接管理
│   │   ├── agentclient/     Agent 端连接逻辑
│   │   └── proto/agent/     protoc 生成代码
│   └── proto/agent.proto    gRPC 契约
├── web/                     Vue 前端
│   └── src/{api,views,styles}/
├── Makefile                 构建脚本
└── dist/                    构建产物(gitignore)
```

## 部署

生产部署到 Linux 集群见部署指南文档: 交叉编译、systemd、nginx、Agent 批量部署、滚动升级。

## 路线图

- **v0.1** 单节点控制面 + Agent 骨架 ✅ (M1-M4 完成)
  - Raft 单节点存储、bbolt 持久化、gRPC 双向流、Agent 心跳、读配置回传
- **v0.2** 3 节点容错集群(手动扩展)
  - 控制面 1 → 3 节点, 选举/复制/故障切换, 业务代码几乎不动(存储层已是 Raft)
- **v0.3** Java 运维 MVP + 安全鉴权
  - M1 登录鉴权: 登录页、JWT、API 中间件、bcrypt 密码哈希、路由守卫、限流防爆破
  - 配置比对(Nacos/本地/jar)、扩容迁移、拓扑可视化(G6)、配置编辑(Monaco)
  - 鉴权作为首个里程碑, 后续业务接口受其保护
- **v0.4** 自动部署 + 入口代理
  - 单机起步: 添加服务器 SSH 连接信息(加密存 Raft)
  - 角色选择 UI: 勾选 Raft 节点(校验奇数≥3)/ Agent 节点
  - 自动注入: SSH 推送二进制 + 配置, 远程拉起 systemd, Raft 节点自动 join, Agent 自动连 Leader
  - 入口代理: 任意节点 IP 可访问 UI, 自动转发当前 Leader
  - 凭据加密: AES-GCM 加密 SSH 私钥/密码, 主密钥从环境变量

## 开发

```bash
git clone <repo>
cd deepsea-ops
make dev         # 启动后端 + 前端开发服务
```

代码规范: Go 用 `gofmt`/`go vet`; 前端用 TypeScript strict。提交前 `make check`。

## 许可证

MIT