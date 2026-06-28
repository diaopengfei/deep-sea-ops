package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/store"
)

// tokenOut 是返回给前端的 Token 视图, 剥离 TokenHash 避免泄露。
type tokenOut struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	TokenPrefix string `json:"tokenPrefix"` // 明文前缀, 便于前端识别"哪个 token"
	Role        string `json:"role"`
	CreatedBy   string `json:"createdBy"`
	CreatedAt   int64  `json:"createdAt"`
	LastUsedAt  int64  `json:"lastUsedAt"`
	ExpiresAt   int64  `json:"expiresAt"`
}

func toTokenOut(t model.APIToken) tokenOut {
	return tokenOut{
		ID: t.ID, Name: t.Name, TokenPrefix: t.TokenPrefix, Role: t.Role,
		CreatedBy: t.CreatedBy, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt, ExpiresAt: t.ExpiresAt,
	}
}

// handleListTokens GET /api/tokens — 列出所有 API Token(admin 专用)。
func handleListTokens(w http.ResponseWriter, r *http.Request, s *store.Store) {
	tokens := s.ListTokens()
	out := make([]tokenOut, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, toTokenOut(t))
	}
	auth.WriteJSON(w, http.StatusOK, out)
}

// handleCreateToken POST /api/tokens — 创建 API Token(admin 专用)。
// 请求体: { name, role, expiresAt? }
// 返回: { token: "dst_xxx", ...tokenOut } — token 明文只此一次返回, 后续不可查询。
func handleCreateToken(w http.ResponseWriter, r *http.Request, s *store.Store) {
	var req struct {
		Name      string `json:"name"`
		Role      string `json:"role"`
		ExpiresAt int64  `json:"expiresAt"` // 0 表示永不过期
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	if req.Name == "" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name 不能为空"})
		return
	}
	if !model.IsValidRole(req.Role) {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "角色非法, 可选: admin / operator / viewer"})
		return
	}
	// 生成明文 token(只返回一次) + 哈希(持久化)
	plaintext, hash, prefix, err := auth.GenerateAPIToken()
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "生成 token 失败: " + err.Error()})
		return
	}
	claims := auth.FromContext(r.Context())
	createdBy := ""
	if claims != nil {
		createdBy = claims.Username
	}
	t := model.APIToken{
		ID:          uuid.NewString(),
		Name:        req.Name,
		TokenHash:   hash,
		TokenPrefix: prefix,
		Role:        req.Role,
		CreatedBy:   createdBy,
		CreatedAt:   time.Now().UnixMilli(),
		ExpiresAt:   req.ExpiresAt,
	}
	if err := s.AddToken(t); err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"token": plaintext, // 明文 token, 只此一次
		"info":  toTokenOut(t),
	})
}

// handleDeleteToken DELETE /api/tokens/{id} — 删除 API Token(admin 专用)。
func handleDeleteToken(w http.ResponseWriter, r *http.Request, s *store.Store, id string) {
	if id == "" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 token ID"})
		return
	}
	if err := s.DelToken(id); err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// splitTokenPath 从 /api/tokens/{id} 路径解析 id。
func splitTokenPath(path string) string {
	return strings.TrimPrefix(path, "/api/tokens/")
}
