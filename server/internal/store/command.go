package store

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hashicorp/raft"

	"github.com/deepsea-ops/server/internal/model"
)

// command 是提交给 Raft 的写命令。
//
// 关键理解: Raft 本身不认识 Server/User 结构, 它只负责按顺序复制"一段字节"。
// 所以我们把命令序列化成 JSON 字节交给 raft.Apply, 等 Raft 确认后,
// 它会把同样的字节回放给 FSM.Apply, 由 FSM 决定怎么改状态。
//
// Op 字段决定命令类型, 不同命令携带不同数据(用对应字段, 其余为零值)。
type command struct {
	Op     string        `json:"op"`     // 操作类型: "add_server" / "add_user"
	Server model.Server  `json:"server"` // add_server 时携带
	User   model.User    `json:"user"`   // add_user 时携带
}

// op 常量, 避免到处写字符串字面量(笔误难以排查)。
const (
	opAddServer = "add_server"
	opAddUser   = "add_user"
)

// _ 确保 time 包被引用(后续会加时间戳类命令)。
var _ = time.Now

// _ 确保 json 和 raft 被引用(Store 里会用到, 此处占位防编辑器误删 import)。
var _ = json.Marshal
var _ = raft.Log{}
var _ = fmt.Errorf