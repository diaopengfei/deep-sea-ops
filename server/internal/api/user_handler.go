package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/store"
)

// userOut 是返回给前端的用户视图, 剥离 PasswordHash 避免泄露。
type userOut struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt int64  `json:"createdAt"`
}

func toUserOut(u model.User) userOut {
	return userOut{ID: u.ID, Username: u.Username, Role: u.Role, CreatedAt: u.CreatedAt}
}

// handleListUsers GET /api/users — 列出所有用户(admin 专用)。
func handleListUsers(w http.ResponseWriter, r *http.Request, s *store.Store) {
	users := s.ListUsers()
	out := make([]userOut, 0, len(users))
	for _, u := range users {
		out = append(out, toUserOut(u))
	}
	auth.WriteJSON(w, http.StatusOK, out)
}

// handleCreateUser POST /api/users — 创建用户(admin 专用)。
// 请求体: { username, password, role }
func handleCreateUser(w http.ResponseWriter, r *http.Request, s *store.Store) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	if req.Username == "" || req.Password == "" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "用户名/密码不能为空"})
		return
	}
	if !model.IsValidRole(req.Role) {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "角色非法, 可选: admin / operator / viewer"})
		return
	}
	if _, exists := s.GetUser(req.Username); exists {
		auth.WriteJSON(w, http.StatusConflict, map[string]string{"error": "用户名已存在"})
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "密码哈希失败"})
		return
	}
	u := model.User{
		ID:           req.Username,
		Username:     req.Username,
		PasswordHash: hash,
		Role:         req.Role,
		CreatedAt:    time.Now().UnixMilli(),
	}
	if err := s.AddUser(u); err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusCreated, toUserOut(u))
}

// handleUpdateUser PUT /api/users/{username} — 修改用户(admin 专用)。
// 请求体: { password?, role? } — password 非空改密码, role 非空改角色。
// 保护: 不允许删除/降级最后一个 admin(避免无管理员锁死)。
func handleUpdateUser(w http.ResponseWriter, r *http.Request, s *store.Store, username string) {
	existing, ok := s.GetUser(username)
	if !ok {
		auth.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "用户不存在"})
		return
	}
	var req struct {
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	// 角色校验 + 最后一个 admin 保护
	if req.Role != "" {
		if !model.IsValidRole(req.Role) {
			auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "角色非法, 可选: admin / operator / viewer"})
			return
		}
		if existing.Role == model.RoleAdmin && req.Role != model.RoleAdmin {
			if countAdmins(s) <= 1 {
				auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "不能降级最后一个 admin, 否则将无法管理用户"})
				return
			}
		}
	}
	upd := model.User{Username: username}
	if req.Password != "" {
		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "密码哈希失败"})
			return
		}
		upd.PasswordHash = hash
	}
	upd.Role = req.Role
	if err := s.UpdateUser(upd); err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	after, _ := s.GetUser(username)
	auth.WriteJSON(w, http.StatusOK, toUserOut(*after))
}

// handleDeleteUser DELETE /api/users/{username} — 删除用户(admin 专用)。
// 保护: admin 不可自删; 不可删除最后一个 admin。
func handleDeleteUser(w http.ResponseWriter, r *http.Request, s *store.Store, as *auth.Service, username string) {
	if username == "admin" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "不能删除内置 admin 账号"})
		return
	}
	target, ok := s.GetUser(username)
	if !ok {
		auth.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "用户不存在"})
		return
	}
	// 不允许自删(避免当前登录用户删自己)
	if claims := auth.FromContext(r.Context()); claims != nil && claims.Username == username {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "不能删除当前登录的用户"})
		return
	}
	if target.Role == model.RoleAdmin && countAdmins(s) <= 1 {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "不能删除最后一个 admin"})
		return
	}
	if err := s.DeleteUser(username); err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	_ = as // 保留参数便于未来扩展(如踢掉该用户的活动 session)
}

// countAdmins 统计 admin 角色用户数, 用于最后一个 admin 保护。
func countAdmins(s *store.Store) int {
	count := 0
	for _, u := range s.ListUsers() {
		if u.Role == model.RoleAdmin {
			count++
		}
	}
	return count
}

// splitUserPath 从 /api/users/{username} 路径解析 username。
// username 不含 "/", 直接取剩余部分。
func splitUserPath(path string) string {
	return strings.TrimPrefix(path, "/api/users/")
}
