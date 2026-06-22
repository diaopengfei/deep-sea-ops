package model

// User 表示一个可登录的管理员账号。
// PasswordHash 存的是 bcrypt 哈希(加盐), 不存明文密码。
// 这个结构体会通过 Raft 复制到所有控制面节点, 保证多节点登录一致。
type User struct {
	ID           string `json:"id"`            // 用户唯一标识, 一般用用户名
	Username     string `json:"username"`      // 登录用户名
	PasswordHash string `json:"passwordHash"`  // bcrypt 哈希, 形如 $2a$10$...
	Role         string `json:"role"`          // 角色: admin / operator(后续扩展)
	CreatedAt    int64  `json:"createdAt"`     // 创建时间(Unix 秒)
}

// RoleAdmin 管理员角色常量。
const RoleAdmin = "admin"