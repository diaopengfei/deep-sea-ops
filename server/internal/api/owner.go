package api

import (
	"net/http"

	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/model"
)

// ownerVisible 判断当前用户是否可见某资源(v0.6.9 多租户隔离)。
// 规则: admin 可见全部; 非 admin 仅可见 Owner 为空(共享)或 Owner 等于自己用户名的资源。
func ownerVisible(r *http.Request, owner string) bool {
	claims := auth.FromContext(r.Context())
	if claims == nil {
		return false
	}
	if claims.Role == "admin" {
		return true
	}
	return owner == "" || owner == claims.Username
}

// ownerFromClaims 从请求中提取当前用户名, 作为新建资源的 Owner。
// admin 创建的资源 Owner 留空(共享给所有人); 非 admin 创建的资源 Owner 为自己。
func ownerFromClaims(r *http.Request) string {
	claims := auth.FromContext(r.Context())
	if claims == nil {
		return ""
	}
	if claims.Role == "admin" {
		return "" // admin 创建的资源默认共享
	}
	return claims.Username
}

// denyViewer 拦截只读角色(viewer)的写操作(v0.6.9)。
// 返回 true 表示已拒绝(已写入 403 响应), 调用方应直接 return。
func denyViewer(w http.ResponseWriter, r *http.Request) bool {
	claims := auth.FromContext(r.Context())
	if claims != nil && !model.CanWrite(claims.Role) {
		auth.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "只读角色无写权限"})
		return true
	}
	return false
}

