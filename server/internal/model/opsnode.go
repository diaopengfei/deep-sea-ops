package model

// OpsNode 是 ops 服务节点视图, 合并 raft 节点和 agent 节点。
type OpsNode struct {
	Type       string  `json:"type"`       // "raft" / "agent"
	ID         string  `json:"id"`         // 节点 ID
	Address    string  `json:"address"`    // 通信地址(raft: raft addr, agent: gRPC 来源)
	Hostname   string  `json:"hostname"`   // 主机名(agent 才有)
	IP         string  `json:"ip"`         // IP 地址
	State      string  `json:"state"`      // raft: Leader/Follower/Candidate; agent: online
	Suffrage   string  `json:"suffrage"`   // raft: Voter/Nonvoter; agent: ""
	LastSeen   int64   `json:"lastSeen"`   // 最后心跳时间(unix 秒, agent 才有)
	CPUPercent float64 `json:"cpuPercent"` // v0.6.3: 实时 CPU 使用率(agent 才有)
	MemPercent float64 `json:"memPercent"` // v0.6.3: 实时内存使用率(agent 才有)
	IsLeader   bool    `json:"isLeader"`   // 是否当前 Leader
	IsSelf     bool    `json:"isSelf"`     // 是否当前节点自己
}
