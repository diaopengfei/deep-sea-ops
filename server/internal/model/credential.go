package model

// SSHCredential 是一台服务器的 SSH 连接凭据。
// 用于自动注入: SSH 推送二进制 + 配置, 远程拉起 systemd。
//
// 安全设计:
//   - Password 和 PrivateKey 存的是 AES-GCM 加密后的密文, 不存明文
//   - 主密钥从环境变量 MASTER_KEY 读取, 不落盘
//   - 加密后的 Nonce 拼接在 Ciphertext 前面(前 12 字节)
type SSHCredential struct {
	ID          string `json:"id"`          // 唯一 ID(一般用服务器 ID 或 IP)
	ServerName  string `json:"serverName"`  // 服务器名称(冗余, 便于展示)
	IP          string `json:"ip"`          // 服务器 IP
	Port        int    `json:"port"`        // SSH 端口, 默认 22
	Username    string `json:"username"`    // SSH 用户名(如 root / deploy)
	Password    string `json:"password"`    // AES-GCM 加密后的密码密文(base64)
	PrivateKey  string `json:"privateKey"`  // AES-GCM 加密后的私钥密文(base64)
	AuthType    string `json:"authType"`    // "password" / "key"
	CreatedAt   int64  `json:"createdAt"`   // 创建时间
}

// AuthType 常量
const (
	AuthTypePassword = "password"
	AuthTypeKey      = "key"
)
