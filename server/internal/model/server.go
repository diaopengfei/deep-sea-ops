package model

// Server 表示一台被管理的服务器。
// ID 为自增数字, 由 FSM 在 Apply 时分配(并发安全, Raft 顺序执行)。
// Password 存的是 AES-GCM 加密后的密文, 不存明文。
type Server struct {
	ID        int64  `json:"id"`        // 自增 ID (FSM 分配, 调用方传 0)
	Name      string `json:"name"`      // 服务器名称
	IP        string `json:"ip"`        // 服务器 IP
	Port      int    `json:"port"`      // SSH 端口, 默认 22
	OS        string `json:"os"`        // 操作系统: "linux" / "windows", 默认 "linux"
	Username  string `json:"username"`  // SSH 用户名
	Password  string `json:"password"`  // AES-GCM 加密后的密码密文(base64)
	Status    string `json:"status"`    // online / offline
	Owner     string `json:"owner"`     // v0.6.9: 所有者(创建者 username), 空表示共享
	CreatedAt int64  `json:"createdAt"` // 创建时间(unix 毫秒)
}

// OS 类型常量
const (
	OSLinux   = "linux"
	OSWindows = "windows"
)
