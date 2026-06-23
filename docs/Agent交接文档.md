# Agent 交接文档

> 本文档供下一个接手的 AI Agent 阅读, 包含项目全貌、历史进度、当前状态、下一步任务和关键代码位置。
> 最后更新: v0.4 完成 + 深度代码审查后

---

## 一、项目概述

**deepsea-ops** 是一个分布式服务器运维平台, 用于管理 20+ 台服务器上的 Java 微服务、Java/Python 程序、Redis、PostgreSQL、Kafka、Elasticsearch、ClickHouse、Nacos 等中间件和服务。

**核心价值**: 用一套分布式控制面统一管理 —— 配置一处维护、比对一目了然、扩容迁移一键编排, 任意节点可访问、故障自动切换。

**架构模式 C**: 3 节点 Raft 控制面(强一致) + 全员 Agent(混合拓扑)。控制面用 Raft 保证配置/部署计划的一致性, Agent 在线状态走内存(高频瞬时数据不付一致性成本)。

**技术栈**:
- 后端: Go 1.22+, hashicorp/raft v1.7.3, bbolt v1.3.5, gRPC, protobuf
- 前端: Vue 3 + TypeScript + Vite + Element Plus + ECharts + AntV G6 + Monaco
- 通信: gRPC 双向流(Agent ↔ 控制面长连接, 心跳 + 指令下发)

---

## 二、版本演进历史

### v0.1 单节点控制面 + Agent 骨架 ✅
- M1: Raft 单节点存储层(hashicorp/raft + FSM Apply/Snapshot/Restore)
- M2: bbolt 持久化 FSM(内存 map 换 bbolt, 接口不变)
- M3: gRPC 通信 + Agent 骨架(proto 定义, 双向流, Agent 注册/心跳)
- M4: 读 Java 配置回传(控制面下发 READ_CONFIG → Agent 读文件 → 回传展示)

### v0.2 3 节点 Raft 容错集群 ✅
- 控制面 1→3 节点, 动态 AddVoter 加节点
- 命令行 flag 参数化(nodeID/raftAddr/joinAddr)
- 集群管理接口(/api/cluster/join, /api/cluster/info)
- 验证: 杀 Leader 后秒级选出新 Leader, 已提交数据不丢

### v0.3 Java 运维 MVP + 安全鉴权 ✅
- **M1 登录鉴权** ✅ 登录页、JWT、API 中间件、bcrypt 密码哈希、路由守卫、限流防爆破
- **M2 配置比对** ✅ Agent 采集 Nacos/本地/jar 三路配置, 控制面做三路 diff, 支持 Nacos 认证
- **M3 Agent 自动扫描** ✅ 可配置扫描目录、识别 Java/Python 项目、读 hosts、进程检测、生效配置合并
- **M4 配置自动发现与比对增强** ✅ 配置比对页改为"选 Agent → 选项目 → 自动填充 → 比对"流程
- **M5 扩容迁移** ✅ Leader 编排, 下发部署指令到 Agent, 部署任务走 Raft 持久化
- **M6 拓扑可视化** ✅ G6 集成, 集群概览 + 节点拓扑

### v0.4 自动部署 + 入口代理 ✅
- SSH 凭据加密存 Raft (AES-GCM, MASTER_KEY 环境变量)
- 自动注入: SSH 推送二进制 + systemd, Raft 节点自动 join, Agent 自动连 Leader
- 入口代理: 任意节点 IP 可访问 UI, 写请求转发 Leader, 读请求本地
- 深度代码审查: 修复 17 处缺陷 (静默吞错、逻辑 bug、OOM 风险、死代码)

---

## 三、本次会话完成的工作

本次会话接手了之前 codex 对话的未完成工作, 主要完成了以下内容:

### 1. 后端: 进程检测和生效配置合并接入 Agent 扫描流程

**文件: [server/internal/agentclient/scanner.go](../server/internal/agentclient/scanner.go)**

- 扩展 `ProjectInfo` 结构体, 新增字段:
  - `Running bool` — 是否在运行(通过进程列表匹配)
  - `PID int` — 运行中的进程 PID, 未运行为 0
  - `EffectiveConfig *EffectiveConfig` — 三路合并后的生效配置(仅 Spring 项目)

- 新增 `EnrichScanResult(result *ScanResult)` 函数:
  - 扫描完成后补充进程状态和生效配置
  - 调 `ListProcesses()` 获取进程列表, 匹配每个项目判断是否在运行
  - 对 Spring 项目调 `buildEffectiveConfig()` 采集三路配置并合并

- 新增 `buildEffectiveConfig(p *ProjectInfo) *EffectiveConfig` 函数:
  - 读本地配置文件(application.yml 等), 拼成一份本地配置文本
  - 从本地配置提取 Nacos 地址(spring.cloud.nacos.config.server-addr 等)
  - 从 spring.application.name 推断默认 dataId
  - 读 jar 内 BOOT-INF/classes/application.yml
  - 调 Nacos OpenAPI 拉远程配置
  - 三路合并: jar(低) < 本地 < Nacos(高), 产出生效配置
  - 捕获各源采集错误, 填充到 EffectiveConfig 的 Err 字段

- 新增 `findJarInDir(dir string) string` 辅助函数

**文件: [server/internal/agentclient/process.go](../server/internal/agentclient/process.go)**

- `IsProjectRunning` 签名从 `bool` 改为 `(bool, int)`, 返回 PID
- 匹配策略: 进程命令行包含项目路径

**文件: [server/internal/agentclient/client.go](../server/internal/agentclient/client.go)**

- `SCAN_PROJECTS` 指令处理中调用 `EnrichScanResult(&scanResult)`, 在扫描后补充进程状态和生效配置

**文件: [server/internal/agentclient/configmerge.go](../server/internal/agentclient/configmerge.go)**

- `EffectiveConfig` 结构体从 map 字段改为扁平字段(与前端对齐):
  - `NacosRaw / LocalRaw / JarRaw string` — 三路原始配置文本
  - `NacosErr / LocalErr / JarErr string` — 各源采集错误
- `MergeConfigs` 函数更新, 使用新的扁平字段初始化

### 2. 后端: 修复 /api/servers 返回 null 问题

**文件: [server/internal/api/server.go](../server/internal/api/server.go)**

- `handleListServers` 改用 `auth.WriteJSON`, 并添加防御: 确保 nil slice 被转为 `[]model.Server{}`, 避免 JSON 编码为 `null`

### 3. 前端: 服务器列表加载防御

**文件: [web/src/views/ServerListView.vue](../web/src/views/ServerListView.vue)**

- `loadServers` 添加 try-catch 错误处理
- 防御: 后端返回 null 时, `servers.value` 设为空数组而非 null
- 加载失败时显示错误提示

### 4. 前端: 配置比对改为 git 风格展示

**文件: [web/src/views/ConfigDiffView.vue](../web/src/views/ConfigDiffView.vue)**

完全重写比对结果展示, 从原来的 6 个 diff-block 网格改为 git 风格的统一 diff 视图:

- **统计概览**: 一致/新增/部分/总行数
- **过滤工具栏**: 全部/一致/仅差异 切换 + 关键字搜索
- **git 风格 diff 展示**:
  - 每行显示来源徽章(N=Nacos, L=本地, J=jar), 蓝色=有, 灰色=无
  - 行前缀模仿 git diff: ` `=一致, `+`=仅一方, `~`=部分
  - 颜色标注: 绿色=三方一致, 红色=仅一方, 黄色=两方
  - 左侧彩色边框, 等宽字体, 可滚动
- 把 DiffReport 的 7 个分类(含之前遗漏的 nacosJar)展平成统一的 DiffLine 数组, 按内容排序

### 5. 文档更新

- **README.md**: 更新状态描述和路线图, 标记 M3 完成
- **docs/README.md**: 更新当前阶段描述
- **docs/架构设计.md**: 更新 v0.3 演进路径, 标记 M1/M2/M3 完成

---

## 四、当前代码状态

### 后端编译状态
后端编译通过(`go build -buildvcs=false ./...`)。

> **注意**: 由于工作目录上层有 SVN, 需要 `-buildvcs=false` 参数禁用 VCS stamping。

### 关键文件清单

#### 后端(Go)

| 文件 | 职责 |
|------|------|
| [server/cmd/server/main.go](../server/cmd/server/main.go) | 控制面入口 |
| [server/cmd/agent/main.go](../server/cmd/agent/main.go) | Agent 入口 |
| [server/internal/api/server.go](../server/internal/api/server.go) | HTTP 路由层 |
| [server/internal/auth/auth.go](../server/internal/auth/auth.go) | JWT 鉴权服务 |
| [server/internal/store/store.go](../server/internal/store/store.go) | Raft 存储层 |
| [server/internal/store/fsm.go](../server/internal/store/fsm.go) | FSM 状态机(bbolt) |
| [server/internal/grpcserver/server.go](../server/internal/grpcserver/server.go) | gRPC 服务端(Agent 连接管理) |
| [server/internal/agentclient/client.go](../server/internal/agentclient/client.go) | Agent 端连接逻辑 |
| [server/internal/agentclient/scanner.go](../server/internal/agentclient/scanner.go) | 项目扫描 + 进程检测 + 生效配置合并 |
| [server/internal/agentclient/process.go](../server/internal/agentclient/process.go) | 进程列表获取 |
| [server/internal/agentclient/collector.go](../server/internal/agentclient/collector.go) | 三路配置采集(Nacos/本地/jar) |
| [server/internal/agentclient/configmerge.go](../server/internal/agentclient/configmerge.go) | 三路配置合并(按 Spring 优先级) |
| [server/internal/configdiff/diff.go](../server/internal/configdiff/diff.go) | 三路配置 diff 报告生成 |
| [server/internal/model/server.go](../server/internal/model/server.go) | Server 领域模型 |
| [server/internal/model/user.go](../server/internal/model/user.go) | User 领域模型 |
| [server/proto/agent.proto](../server/proto/agent.proto) | gRPC 契约定义 |

#### 前端(Vue)

| 文件 | 职责 |
|------|------|
| [web/src/main.ts](../web/src/main.ts) | 应用入口 |
| [web/src/App.vue](../web/src/App.vue) | 主布局 + 登录守卫 + 菜单切换 |
| [web/src/views/LoginView.vue](../web/src/views/LoginView.vue) | 登录页 |
| [web/src/views/ServerListView.vue](../web/src/views/ServerListView.vue) | 服务器管理页 |
| [web/src/views/AgentListView.vue](../web/src/views/AgentListView.vue) | Agent 节点列表页 |
| [web/src/views/ProjectScanView.vue](../web/src/views/ProjectScanView.vue) | 项目扫描页(含生效配置展示) |
| [web/src/views/ConfigDiffView.vue](../web/src/views/ConfigDiffView.vue) | 配置比对页(git 风格 diff) |
| [web/src/api/auth.ts](../web/src/api/auth.ts) | 登录鉴权 API |
| [web/src/api/server.ts](../web/src/api/server.ts) | 服务器/Agent API + axios 实例 |
| [web/src/api/config.ts](../web/src/api/config.ts) | 配置比对 API |
| [web/src/api/projects.ts](../web/src/api/projects.ts) | 项目扫描 API |
| [web/src/api/types.ts](../web/src/api/types.ts) | 共享类型定义 |

---

## 五、核心数据结构

### EffectiveConfig(生效配置)

后端 `server/internal/agentclient/configmerge.go`:

```go
type EffectiveConfig struct {
    Items     []ConfigItem     `json:"items"`      // 所有生效配置项(按 key 排序)
    Overrides []OverrideRecord `json:"overrides"`  // 被覆盖的记录
    NacosRaw  string           `json:"nacosRaw"`   // Nacos 原始配置文本
    LocalRaw  string           `json:"localRaw"`   // 本地原始配置文本
    JarRaw    string           `json:"jarRaw"`     // jar 内原始配置文本
    NacosErr  string           `json:"nacosErr"`   // Nacos 采集错误
    LocalErr  string           `json:"localErr"`   // 本地采集错误
    JarErr    string           `json:"jarErr"`     // jar 采集错误
}
```

前端 `web/src/views/ProjectScanView.vue` 中的 `EffectiveConfig` interface 与此对齐。

### ProjectInfo(项目信息)

后端 `server/internal/agentclient/scanner.go`:

```go
type ProjectInfo struct {
    Path            string           `json:"path"`
    Type            ProjectType      `json:"type"`          // java-spring / java-jar / python
    Name            string           `json:"name"`
    ConfigFiles     []string         `json:"configFiles"`
    JarPath         string           `json:"jarPath"`
    JarEntry        string           `json:"jarEntry"`
    Running         bool             `json:"running"`       // 进程检测结果
    PID             int              `json:"pid"`           // 运行中进程 PID
    EffectiveConfig *EffectiveConfig `json:"effectiveConfig"` // 仅 Spring 项目
}
```

### DiffReport(配置比对报告)

后端 `server/internal/configdiff/diff.go`:

```go
type DiffReport struct {
    NacosErr   string   `json:"nacosErr,omitempty"`
    LocalErr   string   `json:"localErr,omitempty"`
    JarErr     string   `json:"jarErr,omitempty"`
    Consistent []string `json:"consistent"`    // 三方一致
    OnlyNacos  []string `json:"onlyNacos"`     // 仅 Nacos
    OnlyLocal  []string `json:"onlyLocal"`     // 仅本地
    OnlyJar    []string `json:"onlyJar"`       // 仅 jar
    NacosLocal []string `json:"nacosLocal"`    // Nacos+本地
    NacosJar   []string `json:"nacosJar"`      // Nacos+jar
    LocalJar   []string `json:"localJar"`      // 本地+jar
}
```

---

## 六、API 接口清单

### 白名单(无需鉴权)
- `GET /api/healthz` — 健康检查
- `POST /api/login` — 登录, 返回 `{accessToken, refreshToken, username, role}`

### 受保护(需 JWT)
- `GET /api/auth/me` — 获取当前用户信息
- `GET /api/servers` — 服务器列表
- `POST /api/servers` — 新增服务器(走 Raft)
- `GET /api/agents` — 在线 Agent 列表
- `POST /api/agents/{id}/read-config` — 读 Agent 上指定路径的文件
- `POST /api/agents/{id}/config-diff` — 三路配置比对(下发采集指令 + 生成 diff 报告)
- `POST /api/agents/{id}/scan-projects` — 项目扫描(含进程检测 + 生效配置合并)
- `POST /api/cluster/join` — Raft 集群加节点
- `GET /api/cluster/info` — 集群状态

### Agent 指令(通过 gRPC 双向流下发)
- `READ_CONFIG` — 读指定路径文件
- `COLLECT_CONFIGS` — 采集三路配置(Nacos/本地/jar)
- `SCAN_PROJECTS` — 扫描项目(含 EnrichScanResult 补充进程状态和生效配置)

---

## 七、配置合并算法详解

Spring 项目的配置存在覆盖关系, 优先级从低到高: **jar 内嵌配置 < 本地外部配置 < Nacos 远程配置**。

### 合并流程(`MergeConfigs` 函数)

1. **展平**: 把 YAML 或 properties 格式的配置文本展平成 `map[string]string`(dot-notation 键, 如 `spring.datasource.url`)
2. **分层合并**:
   - 第一层: 放入 jar 配置(最低优先级)
   - 第二层: 用本地配置覆盖 jar, 记录 OverrideRecord
   - 第三层: 用 Nacos 配置覆盖本地和 jar, 记录 OverrideRecord
3. **排序输出**: 按 key 排序, 生成 `[]ConfigItem`

### ConfigItem 结构

```go
type ConfigItem struct {
    Key        string       `json:"key"`        // spring.datasource.url
    Value      string       `json:"value"`      // 最终生效值
    Source     ConfigSource `json:"source"`      // 值来自哪个源(jar/local/nacos)
    Overridden bool         `json:"overridden"`  // 是否被更高优先级覆盖过
}
```

### OverrideRecord 结构

```go
type OverrideRecord struct {
    Key    string       `json:"key"`
    From   ConfigSource `json:"from"`    // 被覆盖的源
    To     ConfigSource `json:"to"`      // 覆盖它的源
    OldVal string       `json:"oldVal"`
    NewVal string       `json:"newVal"`
}
```

---

## 八、进程检测实现

### 跨平台支持(`process.go`)

- **Linux**: 读 `/proc/*/cmdline`, 用 `\0` 分隔参数, 替换成空格
- **Windows**: 调 `tasklist /FO CSV /NH` 解析 CSV
- **macOS/其他 Unix**: 调 `ps -eo pid=,comm= --no-headers`

### 匹配策略

`IsProjectRunning(projectPath, processes) (bool, int)`:
- 遍历进程列表, 如果进程的 CmdLine 包含 projectPath, 则认为项目在运行, 返回 `(true, pid)`

---

## 九、已知问题和注意事项

### 1. Go build 的 VCS 问题
由于工作目录上层有 SVN, 直接 `go build ./...` 会报错 `multiple VCS detected`。需要加 `-buildvcs=false` 参数:
```bash
go build -buildvcs=false ./...
```
Makefile 中的构建命令可能需要确认是否已加此参数。

### 2. /api/servers 空列表问题(已修复)
原来 `handleListServers` 直接用 `json.NewEncoder(w).Encode(s.ListServers())`, 如果返回 nil slice 会编码为 `null`。已修复为使用 `auth.WriteJSON` 并确保 nil 转为空 slice。前端也添加了防御: `Array.isArray(data) ? data : []`。

### 3. 登录跳转问题
登录流程代码审查结果:
- `LoginView.vue` 的 `onLogin` 调 `login()` 后 emit `login-success` 事件
- `App.vue` 的 `onLoginSuccess` 设置 `isLoggedIn.value = true`
- 代码逻辑正确, 跳转应该能正常工作
- 如果仍有问题, 检查: 后端是否正常返回 `username` 字段、浏览器 console 是否有 JS 错误

### 4. 空白页问题
访问 `http://localhost:5173/` 空白页的可能原因:
- 后端未启动, Vite 代理 `/api` 失败(但登录页应该仍能渲染)
- 浏览器缓存了旧的 JS, 尝试硬刷新(Ctrl+Shift+R)
- JS 运行时错误, 检查浏览器 console
- token 过期导致 401 → 响应拦截器清 token + reload, 可能短暂闪烁

### 5. ConfigDiffView 的 NacosJar 分类
原来的前端只展示了 6 个分类(遗漏了 `nacosJar`), 新的 git 风格展示已包含全部 7 个分类。

### 6. 扫描结果未持久化
当前 M3 的扫描结果每次都是实时扫描, 没有走 Raft 持久化。M4 计划把扫描结果存入 `projects bucket` 实现多节点共享视图。

---

## 十、下一步任务(M4 配置自动发现与比对增强)

### 目标
把配置比对页从"手填所有参数"改为"选 Agent → 选项目 → 自动填充 → 比对"的流程, 复用 M3 扫描结果。

### 具体任务

1. **配置比对页接入项目扫描结果**:
   - `ConfigDiffView.vue` 增加"选 Agent → 选项目"下拉
   - 选项目后自动填充: Nacos 地址(从扫描结果提取)、本地配置路径、jar 路径、jar 内 entry
   - 可以直接复用 `ProjectScanView` 的扫描 API, 或先扫描再选项目

2. **扫描结果走 Raft 持久化**:
   - FSM 新增 `projects bucket`
   - 新增 `add_project` 命令, 扫描结果走 Raft 复制
   - 新增 `ListProjects(agentID)` 读接口
   - 多节点共享扫描视图, 不依赖单个 Agent 在线

3. **配置比对页直接展示生效配置**:
   - 选项目后, 直接展示 M3 已合并的 `EffectiveConfig`(无需再次采集)
   - 保留"重新采集"按钮, 用于强制刷新

### 后续 M5 扩容迁移

- 选项目 + 选目标 Agent, 控制面下发部署指令
- jar 分发、配置写入、进程启动
- 旧节点停服 → 新节点起服, 编排走 Raft 保证一致性

---

## 十一、本地运行指南

### 环境要求
- Go 1.22+
- Node.js 18+
- Git

### 启动开发模式

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

### 默认登录
- 用户名: `admin`
- 密码: `admin123`(或环境变量 `ADMIN_PASSWORD`)

### 构建

```bash
make build         # 构建全部(当前平台)
make cross-linux   # 交叉编译 Linux 二进制
make build-linux   # 后端 + 前端 Linux 构建
```

---

## 十二、代码风格约定

- Go: `gofmt` / `go vet`, 注释用中文
- 前端: TypeScript strict, 注释用中文
- 提交前: `make check`
- 技术术语保留英文(Raft, gRPC, Agent 等), 描述性内容用中文
- 文件编码: UTF-8(曾出现过 Makefile 中文乱码问题, 注意编码)

---

## 十三、关键设计决策

1. **存储层一开始就用 Raft**: 即使 v0.1 单节点也用 Raft 库存数据, 避免"单机 DB 换 Raft"的大返工。M2 升级 bbolt 时上层零改动验证了其价值。

2. **Agent 在线状态走内存**: Agent 心跳频繁, 瞬时数据不值得付 Raft 一致性成本。在线状态用 `grpcserver.Server` 的内存 map 管理, 断连即移除。

3. **配置合并按 Spring 优先级**: jar(低) < 本地 < Nacos(高), 这是 Spring Boot 的标准行为。合并时记录 OverrideRecord, 让用户能看到覆盖关系。

4. **三路配置独立采集**: `CollectConfigs` 对三个源独立采集, 单个失败不影响其他, 错误记录在对应字段。这样即使 Nacos 不可达, 仍能看到本地和 jar 配置。

5. **diff 用集合差异而非 Myers 算法**: 配置项的顺序差异通常不重要, 内容差异才重要。`configdiff.Compare` 按行集合做差异分类, 不做顺序敏感的最小编辑距离。

6. **EffectiveConfig 用扁平字段而非 map**: 前端 TypeScript interface 用扁平字段(`nacosRaw`/`localRaw`/`jarRaw`), 后端也改为扁平字段对齐, 避免 map 序列化的类型不一致问题。

---

## 十四、文档索引

| 文档 | 内容 |
|------|------|
| [README.md](../README.md) | 项目总览、快速开始、路线图 |
| [docs/README.md](./README.md) | 文档索引 |
| [docs/架构设计.md](./架构设计.md) | 项目目标、拓扑选型、技术栈、目录结构、演进路径 |
| [docs/后端代码导读.md](./后端代码导读.md) | Go 语法速查 + 逐文件解读 + 数据流 |
| [docs/Raft原理详解.md](./Raft原理详解.md) | Raft 每个机制的必要性(反证式) |
| [docs/部署指南.md](./部署指南.md) | Linux 集群打包、交叉编译、systemd、nginx |
| [docs/Agent交接文档.md](./Agent交接文档.md) | 本文档, 供 Agent 交接用 |
