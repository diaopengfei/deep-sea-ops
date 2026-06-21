package main

// Server 表示一台被管理的服务器。
// 字段后面的反引号标签告诉 JSON 编码器:序列化时用这个 key。
type Server struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Status string `json:"status"` // online / offline
}

// command 是提交给 Raft 的写命令。
// 关键理解: Raft 本身不认识 Server 结构, 它只负责按顺序复制"一段字节"。
// 所以我们把命令序列化成 JSON 字节交给 raft.Apply, 等 Raft 确认后,
// 它会把同样的字节回放给 FSM.Apply, 由 FSM 决定怎么改状态。
type command struct {
	Op     string `json:"op"`     // 操作类型, 目前只有 "add"
	Server Server `json:"server"` // 操作目标
}