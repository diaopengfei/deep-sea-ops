package model

// Server 表示一台被管理的服务器。
// 字段后面的反引号标签告诉 JSON 编码器: 序列化时用这个 key。
type Server struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Status string `json:"status"` // online / offline
}