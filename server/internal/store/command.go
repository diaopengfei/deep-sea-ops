package store

import (
	"github.com/deepsea-ops/server/internal/model"
)

// command 是提交给 Raft 的写命令。
//
// 关键理解: Raft 本身不认识 Server/User/Project 结构, 它只负责按顺序复制"一段字节"。
// 所以我们把命令序列化成 JSON 字节交给 raft.Apply, 等 Raft 确认后,
// 它会把同样的字节回放给 FSM.Apply, 由 FSM 决定怎么改状态。
//
// Op 字段决定命令类型, 不同命令携带不同数据(用对应字段, 其余为零值)。
type command struct {
	Op          string              `json:"op"`          // 操作类型
	Server      model.Server        `json:"server"`      // add_server / upd_server 时携带
	ServerID    string              `json:"serverId"`    // del_server 时携带
	ServerUpd   *ServerUpdate       `json:"serverUpd"`   // upd_server_fields 时携带(原子部分更新)
	User        model.User          `json:"user"`        // add_user 时携带
	Project     model.ProjectRecord `json:"project"`     // add_project / del_project 时携带
	Task        model.DeployTask    `json:"task"`        // add_deploy_task / upd_deploy_task 时携带
	CredID      string              `json:"credId"`      // 凭据 ID(del_credential 用)
	Credential  model.SSHCredential `json:"credential"`  // add_credential 时携带
	ConfigDiff  *ConfigDiffUpdate   `json:"configDiff"`  // set_config_diff 时携带
}

// ServerUpdate 是原子部分更新服务器的参数(解决读-改-写竞态)。
// FSM.Apply 中读取现有记录, 只更新非零值字段, 整个操作在一个 Raft 日志中原子完成。
type ServerUpdate struct {
	ID       int64  `json:"id"`       // 必填: 要更新的服务器 ID
	Name     string `json:"name"`     // 空表示不修改
	IP       string `json:"ip"`       // 空表示不修改
	Port     int    `json:"port"`     // 0 表示不修改
	OS       string `json:"os"`       // 空表示不修改
	Username string `json:"username"` // 空表示不修改
	Password string `json:"password"` // 空表示不修改(已加密的密文)
	Status   string `json:"status"`   // 空表示不修改
}

// ConfigDiffUpdate 是更新项目配置比对结果的参数。
type ConfigDiffUpdate struct {
	ProjectID    string `json:"projectId"`    // 项目 ID
	ConfigDiff   string `json:"configDiff"`   // 配置比对结果 JSON
	DiffScannedAt int64 `json:"diffScannedAt"` // 比对时间(unix 毫秒)
}

// op 常量, 避免到处写字符串字面量(笔误难以排查)。
const (
	opAddServer          = "add_server"
	opUpdServer          = "upd_server"
	opUpdServerFields    = "upd_server_fields" // 原子部分更新, 解决读-改-写竞态
	opDelServer          = "del_server"
	opAddUser            = "add_user"
	opAddProject         = "add_project"
	opClearAgentProjects = "clear_agent_projects"
	opAddDeployTask      = "add_deploy_task"
	opUpdDeployTask      = "upd_deploy_task"
	opAddCredential      = "add_credential"
	opDelCredential      = "del_credential"
	opSetConfigDiff      = "set_config_diff" // 持久化配置比对结果
)
