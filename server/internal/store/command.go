package store

import (
	"encoding/json"
	"fmt"

	"github.com/hashicorp/raft"

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
	Op      string             `json:"op"`      // 操作类型
	Server  model.Server       `json:"server"`  // add_server 时携带
	User    model.User         `json:"user"`    // add_user 时携带
	Project model.ProjectRecord `json:"project"` // add_project / del_project 时携带
	Task    model.DeployTask   `json:"task"`    // add_deploy_task / upd_deploy_task 时携带
	// SSH 凭据(v0.4)
	CredID      string `json:"credId"`      // 凭据 ID(del_credential 用)
	Credential  model.SSHCredential `json:"credential"` // add_credential 时携带
}

// op 常量, 避免到处写字符串字面量(笔误难以排查)。
const (
	opAddServer       = "add_server"
	opAddUser         = "add_user"
	opAddProject      = "add_project"
	opDelProject      = "del_project"
	opClearAgentProjects = "clear_agent_projects"
	opAddDeployTask   = "add_deploy_task"
	opUpdDeployTask   = "upd_deploy_task"
	opAddCredential   = "add_credential"
	opDelCredential   = "del_credential"
)

// _ 确保 json 和 raft 被引用(Store 里会用到, 此处占位防编辑器误删 import)。
var _ = json.Marshal
var _ = raft.Log{}
var _ = fmt.Errorf
