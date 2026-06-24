package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/grpcserver"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/store"
)

// New 构造 HTTP 路由, 注入 store、grpcServer 和 auth 服务。
func New(s *store.Store, gs *grpcserver.Server, as *auth.Service) http.Handler {
	mux := http.NewServeMux()

	// --- 白名单路由(无需登录) ---
	mux.HandleFunc("/api/healthz", func(w http.ResponseWriter, r *http.Request) {
		auth.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
			return
		}
		if req.Username == "" || req.Password == "" {
			auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "用户名/密码不能为空"})
			return
		}
		if !as.AllowLogin(req.Username) {
			auth.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "尝试过于频繁, 请稍后再试"})
			return
		}
		resp, err := as.Login(req.Username, req.Password)
		if err != nil {
			auth.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		auth.WriteJSON(w, http.StatusOK, resp)
	})

	// --- 受保护路由(需登录) ---
	mw := auth.NewMiddleware("/api/login", "/api/healthz")

	mux.HandleFunc("/api/auth/me", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		claims := auth.FromContext(r.Context())
		auth.WriteJSON(w, http.StatusOK, map[string]string{
			"username": claims.Username,
			"role":     claims.Role,
		})
	}))

	// 服务器管理
	mux.HandleFunc("/api/servers", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleListServers(w, r, s)
		case http.MethodPost:
			handleAddServer(w, r, s)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))

	// 服务器子路径: /api/servers/test-connection, /api/servers/{id}, /api/servers/{id}/inject
	mux.HandleFunc("/api/servers/", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/servers/")
		if path == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if path == "test-connection" {
			if r.Method == http.MethodPost {
				handleTestConnection(w, r)
				return
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// /api/servers/{id}/inject - 从服务器列表触发注入
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 && parts[1] == "inject" {
			if r.Method == http.MethodPost {
				handleInjectFromServer(w, r, s, parts[0])
				return
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// /api/servers/{id}
		switch r.Method {
		case http.MethodDelete:
			handleDeleteServer(w, r, s, path)
		case http.MethodPut:
			handleUpdateServer(w, r, s, path)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))

	// Agent 管理 + 读配置 + 配置比对 + 项目扫描
	mux.HandleFunc("/api/agents/", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
		parts := strings.Split(path, "/")
		if len(parts) != 2 {
			auth.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "未知路径"})
			return
		}
		agentID := parts[0]
		action := parts[1]

		switch action {
		case "read-config":
			handleReadConfig(w, r, gs, agentID)
		case "config-diff":
			handleConfigDiff(w, r, gs, agentID)
		case "scan-projects":
			handleScanProjects(w, r, gs, s, agentID)
		case "deploy":
			handleDeploy(w, r, gs, s, agentID)
		case "stop-project":
			handleStopProject(w, r, gs, agentID)
		default:
			auth.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "未知操作: " + action})
		}
	}))

	// Agent 在线列表(GET, 需鉴权)
	mux.HandleFunc("/api/agents", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		auth.WriteJSON(w, http.StatusOK, gs.ListAgents())
	}))

	// 项目记录(持久化的扫描结果)
	mux.HandleFunc("/api/projects", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		agentID := r.URL.Query().Get("agentId")
		projects := s.ListProjects(agentID)
		if projects == nil {
			projects = []model.ProjectRecord{}
		}
		auth.WriteJSON(w, http.StatusOK, projects)
	}))

	// 部署任务
	mux.HandleFunc("/api/deploy-tasks", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			tasks := s.ListDeployTasks()
			if tasks == nil {
				tasks = []model.DeployTask{}
			}
			auth.WriteJSON(w, http.StatusOK, tasks)
		case http.MethodPost:
			handleCreateDeployTask(w, r, s, gs)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))

	// SSH 凭据
	mux.HandleFunc("/api/credentials", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			creds := s.ListCredentials()
			if creds == nil {
				creds = []model.SSHCredential{}
			}
			auth.WriteJSON(w, http.StatusOK, creds)
		case http.MethodPost:
			handleAddCredential(w, r, s)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))

	mux.HandleFunc("/api/credentials/", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			id := strings.TrimPrefix(r.URL.Path, "/api/credentials/")
			if id == "" {
				auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少凭据 ID"})
				return
			}
			if err := s.DelCredential(id); err != nil {
				auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			auth.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))

	// 集群管理
	mux.HandleFunc("/api/cluster/", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/cluster/")
		switch path {
		case "join":
			handleClusterJoin(w, r, s)
		case "info":
			auth.WriteJSON(w, http.StatusOK, s.ClusterInfo())
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	// ops 服务节点合并视图(raft + agent)
	mux.HandleFunc("/api/ops-nodes", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleListOpsNodes(w, r, s, gs)
	}))

	// 自动注入: SSH 推送二进制 + systemd, 远程拉起节点
	mux.HandleFunc("/api/inject", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleInject(w, r, s)
	}))

	// 入口代理: 非 Leader 节点把写请求转发给 Leader, 任意节点 IP 可访问 UI
	handler := withLeaderProxy(mux, s)

	return handler
}

// handleClusterJoin 处理集群加入请求。
func handleClusterJoin(w http.ResponseWriter, r *http.Request, s *store.Store) {
	var req struct {
		NodeID string `json:"nodeId"`
		Addr   string `json:"addr"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	if err := s.AddVoter(req.NodeID, req.Addr); err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusOK, map[string]string{"id": req.NodeID, "status": "ok"})
}
