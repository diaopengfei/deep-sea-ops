package model

import "time"

// ProjectRecord 是扫描结果持久化后的项目记录。
// 扫描结果走 Raft 复制, 保证多节点共享同一份视图, 不依赖单个 Agent 在线。
//
// key 设计: agentID + "|" + projectPath, 保证同一 Agent 下项目路径唯一,
// 不同 Agent 可以有同路径的项目(各自独立)。
type ProjectRecord struct {
	ID         string    `json:"id"`         // 唯一 ID: agentID + "|" + path
	AgentID    string    `json:"agentId"`    // 项目所在 Agent 的 ID
	Host       string    `json:"host"`       // Agent 主机名(冗余, 便于展示)
	Path       string    `json:"path"`       // 项目根目录或 jar 路径
	Type       string    `json:"type"`       // java-spring / java-jar / python
	Name       string    `json:"name"`       // 项目名(目录名或 jar 文件名)
	ConfigFiles []string `json:"configFiles"`// 配置文件路径列表
	JarPath    string    `json:"jarPath"`    // jar 路径(java-jar 类型)
	JarEntry   string    `json:"jarEntry"`   // jar 内默认配置 entry
	Running    bool      `json:"running"`    // 扫描时是否在运行
	PID        int       `json:"pid"`        // 运行中进程 PID
	ScannedAt  time.Time `json:"scannedAt"`  // 扫描时间

	// v0.5.3: 配置比对结果(JSON 字符串, 前端解析展示)
	// 由后台扫描调度器自动比对后写入, 空表示尚未比对
	ConfigDiffJSON string `json:"configDiffJson,omitempty"`
	DiffScannedAt  int64  `json:"diffScannedAt,omitempty"` // 比对时间(unix 毫秒)
}

// DeployTask 是一次扩容/迁移部署任务。
// 走 Raft 保证多节点一致, Leader 编排, Agent 执行。
type DeployTask struct {
	ID         string    `json:"id"`          // 任务唯一 ID(uuid)
	Type       string    `json:"type"`        // "scale_out"(扩容) / "migrate"(迁移)
	ProjectPath string   `json:"projectPath"` // 源项目路径
	ProjectName string   `json:"projectName"` // 项目名
	JarPath    string    `json:"jarPath"`     // jar 包路径
	ConfigText string    `json:"configText"`  // 要写入的配置内容
	TargetAgentID string `json:"targetAgentId"` // 目标 Agent
	SourceAgentID string `json:"sourceAgentId"` // 源 Agent(迁移时用)
	Status     string    `json:"status"`      // pending / running / success / failed
	Error      string    `json:"error"`       // 失败原因
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// 部署任务状态常量
const (
	DeployStatusPending = "pending"
	DeployStatusRunning = "running"
	DeployStatusSuccess = "success"
	DeployStatusFailed  = "failed"
)

// DeployTaskType 部署任务类型常量
const (
	DeployTypeScaleOut = "scale_out" // 扩容: 新节点起一份
	DeployTypeMigrate  = "migrate"   // 迁移: 旧节点停服 + 新节点起服
)
