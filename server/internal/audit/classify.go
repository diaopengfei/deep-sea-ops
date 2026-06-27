package audit

import (
	"net"
	"net/http"
	"strings"
)

// Classify 根据请求方法和路径推断操作类型、目标 ID、是否敏感。
// 路径形如 /api/servers/{id}/inject, 用 "/" 分段解析。
func Classify(method, path string) (action, target string, sensitive bool) {
	// /api/login 登录(写但非敏感)
	if path == "/api/login" {
		return "login", "", false
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	// parts[0] = "api", parts[1] = 资源名
	if len(parts) < 2 {
		return strings.ToLower(method), "", method == "DELETE"
	}
	resource := parts[1]

	switch resource {
	case "servers":
		// /api/servers
		if len(parts) == 2 && method == "POST" {
			return "create-server", "", true
		}
		// /api/servers/{id}
		if len(parts) == 3 {
			if method == "PUT" {
				return "update-server", parts[2], false
			}
			if method == "DELETE" {
				return "delete-server", parts[2], true
			}
		}
		// /api/servers/{id}/inject
		if len(parts) == 4 && parts[3] == "inject" {
			return "inject", parts[2], true
		}
	case "agents":
		// /api/agents/{id}/{action}
		if len(parts) >= 4 {
			tgt := parts[2]
			switch parts[3] {
			case "deploy":
				return "deploy", tgt, true
			case "stop-project":
				return "stop-project", tgt, true
			case "scan-projects":
				return "scan", tgt, false
			case "read-config":
				return "read-config", tgt, false
			case "config-diff":
				return "config-diff", tgt, false
			}
		}
	case "credentials":
		if len(parts) == 2 && method == "POST" {
			return "create-credential", "", true
		}
		if len(parts) == 3 && method == "DELETE" {
			return "delete-credential", parts[2], true
		}
	case "deploy-tasks":
		if method == "POST" {
			return "create-deploy-task", "", false
		}
	case "cluster":
		if len(parts) == 3 && parts[2] == "join" {
			return "cluster-join", "", true
		}
	case "inject":
		return "inject", "", true
	}

	// 兜底: 按方法推断
	switch method {
	case "POST":
		return "create", "", false
	case "PUT":
		return "update", "", false
	case "DELETE":
		return "delete", "", true
	}
	return strings.ToLower(method), "", false
}

// StatusRecorder 包装 http.ResponseWriter, 捕获响应状态码供审计使用。
type StatusRecorder struct {
	http.ResponseWriter
	Status int
}

// NewStatusRecorder 创建记录器, 默认状态码 200。
func NewStatusRecorder(w http.ResponseWriter) *StatusRecorder {
	return &StatusRecorder{ResponseWriter: w, Status: http.StatusOK}
}

func (r *StatusRecorder) WriteHeader(code int) {
	r.Status = code
	r.ResponseWriter.WriteHeader(code)
}

// ClientIP 从请求中提取客户端真实 IP。
// 优先取 X-Forwarded-For 首段(入口代理/反代场景), 否则取 RemoteAddr。
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx > 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
