package model

import "time"

// APIToken 表示一个长期有效的 API 访问令牌(v0.7.0 API 开放与集成)。
//
// 与 JWT 的区别:
//   - JWT 短时效(30min), 适合前端用户会话; API Token 长期有效, 适合 CI/CD、脚本、外部系统集成。
//   - JWT 携带用户身份和角色; API Token 只关联创建者, 角色继承创建者。
//   - JWT 由密钥签名可解码; API Token 是不透明随机串, 服务端按 sha256 哈希存储。
//
// 设计要点:
//   - 明文 Token 只在创建时返回一次, 后续只存 sha256(token), 避免泄露。
//   - Token 格式: "dst_<32字节base64url>", 前缀便于识别和检索。
//   - LastUsedAt 在每次鉴权时更新(走 Raft 保证多节点一致)。
//   - ExpiresAt 为 0 表示永不过期。
type APIToken struct {
	ID         string    `json:"id"`         // Token 唯一 ID(uuid), 也是 Raft key
	Name       string    `json:"name"`       // 人类可读名称, 如 "ci-cd-pipeline"
	TokenHash  string    `json:"tokenHash"`  // sha256(明文 token) 的 hex, 用于校验
	TokenPrefix string   `json:"tokenPrefix"` // 明文 token 前 8 字符, 便于前端识别(如 "dst_abc1...")
	Role       string    `json:"role"`       // 继承创建者角色: admin / operator / viewer
	CreatedBy  string    `json:"createdBy"`  // 创建者用户名
	CreatedAt  int64     `json:"createdAt"`  // 创建时间(unix 毫秒)
	LastUsedAt int64     `json:"lastUsedAt"` // 最近使用时间(unix 毫秒, 0 表示未用过)
	ExpiresAt  int64     `json:"expiresAt"`  // 过期时间(unix 毫秒, 0 表示永不过期)
}

// Webhook 表示一个事件订阅端点(v0.7.0 API 开放与集成)。
//
// 控制面在事件发生时(部署完成、扫描发现新项目、节点离线、告警触发等),
// 按订阅的 Events 列表过滤后, 向 URL POST JSON 负载, 带 HMAC-SHA256 签名头。
//
// 设计要点:
//   - Secret 用于 HMAC 签名, 服务端和订阅方共享, 防止伪造请求。
//   - Events 为空表示订阅全部事件; 非空时只推送列表中的事件类型。
//   - Active 为 false 时暂停推送(不删除配置)。
type Webhook struct {
	ID        string    `json:"id"`        // Webhook 唯一 ID(uuid)
	Name      string    `json:"name"`      // 人类可读名称
	URL       string    `json:"url"`       // 推送目标 URL
	Events    []string  `json:"events"`    // 订阅的事件类型列表, 空表示全部
	Secret    string    `json:"secret"`    // HMAC 签名密钥(明文存储, 走 Raft)
	Active    bool      `json:"active"`    // 是否启用
	CreatedBy string    `json:"createdBy"` // 创建者用户名
	CreatedAt int64     `json:"createdAt"` // 创建时间(unix 毫秒)
}

// 事件类型常量(v0.7.0 Webhook 事件推送)。
// 命名规范: <模块>.<动作>, 全小写下划线分隔。
const (
	EventDeployCompleted = "deploy.completed"     // 部署任务完成(含成功/失败, payload 含 status)
	EventDeployFailed    = "deploy.failed"        // 部署任务失败(状态机失败分支)
	EventScanNewProject  = "scan.new_project"     // 扫描发现新项目(此前未持久化的项目)
	EventNodeOffline     = "node.offline"         // Agent 离线(gRPC 连接断开)
	EventAlertFiring     = "alert.firing"         // 告警触发
	EventAlertResolved   = "alert.resolved"       // 告警恢复
)

// Event 是事件总线上流转的事件结构。
// Payload 为任意 JSON 可序列化数据, 由发布方填充。
type Event struct {
	Type      string      `json:"type"`      // 事件类型, 见 Event* 常量
	Timestamp time.Time   `json:"timestamp"` // 事件发生时间
	Payload   interface{} `json:"payload"`   // 事件负载(任意结构)
}
