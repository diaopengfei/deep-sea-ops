package model

// User 表示一个可登录的账号。
// PasswordHash 存的是 bcrypt 哈希(加盐), 不存明文密码。
// 这个结构体会通过 Raft 复制到所有控制面节点, 保证多节点登录一致。
type User struct {
	ID           string `json:"id"`            // 用户唯一标识, 一般用用户名
	Username     string `json:"username"`      // 登录用户名
	PasswordHash string `json:"passwordHash"`  // bcrypt 哈希, 形如 $2a$10$...
	Role         string `json:"role"`          // 角色: admin / operator / viewer (v0.6.9)
	CreatedAt    int64  `json:"createdAt"`     // 创建时间(Unix 秒)
}

// 角色常量 (v0.6.9 多租户与权限分级)
const (
	RoleAdmin    = "admin"    // 管理员: 全部权限(含用户管理)
	RoleOperator = "operator" // 操作员: 读写资源, 不可管理用户
	RoleViewer   = "viewer"   // 只读: 仅可查看, 不可写
)

// CanManageUsers 返回该角色是否可管理用户(创建/删除/改角色)。
// 仅 admin 可管理用户。
func CanManageUsers(role string) bool { return role == RoleAdmin }

// CanWrite 返回该角色是否可执行写操作(增删改资源、部署、注入等)。
// admin 和 operator 可写, viewer 只读。
func CanWrite(role string) bool { return role == RoleAdmin || role == RoleOperator }

// IsValidRole 返回是否为合法角色。
func IsValidRole(role string) bool {
	return role == RoleAdmin || role == RoleOperator || role == RoleViewer
}