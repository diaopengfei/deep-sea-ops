package api

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/store"
)

// --- 入口代理 ---

// withLeaderProxy 包装 handler, 实现"任意节点可访问, 自动转发 Leader"。
//
// 规则:
//   - GET 请求(读 FSM 数据): 本地处理(Follower 也能读 Raft 强一致数据)
//   - agent 相关 GET (/api/agents, /api/ops-nodes): 转发到 Leader(Agent 只连 Leader gRPC, Follower 无数据)
//   - 写请求(POST/PUT/DELETE): 如果本节点不是 Leader, 转发给 Leader
//   - /api/healthz, /api/login, /api/cluster/info: 始终本地处理
//
// Leader 的 HTTP 地址从 Raft Leader 地址推导: 同 IP, 端口替换为 8080。
func withLeaderProxy(next http.Handler, s *store.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 非 /api/ 路径(如前端静态文件)直接本地处理
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		// 白名单: 始终本地处理
		if isLocalOnlyPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// agent 相关 GET 请求: Agent 只连 Leader 的 gRPC, Follower 本地无数据, 必须转发
		if r.Method == http.MethodGet && isAgentDataPath(r.URL.Path) {
			info := s.ClusterInfo()
			if info.State == "Leader" {
				next.ServeHTTP(w, r)
				return
			}
			forwardToLeader(w, r, info, "agent-data GET")
			return
		}

		// 其他 GET 请求(FSM 数据): 本地处理
		if r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}

		// 写请求: 检查是否 Leader
		info := s.ClusterInfo()
		if info.State == "Leader" {
			next.ServeHTTP(w, r)
			return
		}

		// 非 Leader: 转发给 Leader
		forwardToLeader(w, r, info, "write")
	})
}

// forwardToLeader 把请求转发给 Leader 节点。
func forwardToLeader(w http.ResponseWriter, r *http.Request, info store.ClusterInfo, tag string) {
	leaderHTTPAddr := deriveHTTPAddr(info.Leader)
	if leaderHTTPAddr == "" {
		auth.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "当前节点不是 Leader, 且无法确定 Leader HTTP 地址",
		})
		return
	}

	target, err := url.Parse("http://" + leaderHTTPAddr)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "解析 Leader 地址失败"})
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		log.Printf("入口代理[%s]: 转发到 Leader %s 失败: %v", tag, leaderHTTPAddr, err)
		auth.WriteJSON(rw, http.StatusBadGateway, map[string]string{
			"error": "转发到 Leader 失败: " + err.Error(),
		})
	}
	log.Printf("入口代理[%s]: 转发 %s %s -> Leader %s", tag, r.Method, r.URL.Path, leaderHTTPAddr)
	proxy.ServeHTTP(w, r)
}

// isAgentDataPath 判断路径是否为 Agent 实时数据路径(数据只在 Leader 内存中)。
// 这些路径的 GET 请求在 Follower 上必须转发到 Leader。
func isAgentDataPath(path string) bool {
	switch path {
	case "/api/agents", "/api/ops-nodes":
		return true
	}
	// /api/agents/{id}/* 的写操作也会走到这里, 但写操作本就转发, 不影响
	return false
}

// isLocalOnlyPath 判断路径是否始终本地处理(不转发)。
func isLocalOnlyPath(path string) bool {
	switch path {
	case "/api/healthz", "/api/login", "/api/auth/me":
		return true
	}
	// /api/cluster/info 始终本地(每个节点都能报告自己的状态)
	if path == "/api/cluster/info" {
		return true
	}
	return false
}

// deriveHTTPAddr 从 Raft Leader 地址推导 HTTP 地址(同 IP, 端口 8080)。
func deriveHTTPAddr(raftAddr string) string {
	if raftAddr == "" {
		return ""
	}
	// raftAddr 形如 "192.168.1.10:7000", 取 IP 部分拼 :8080
	idx := strings.LastIndex(raftAddr, ":")
	if idx < 0 {
		return raftAddr + ":8080"
	}
	return raftAddr[:idx] + ":8080"
}
