package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/deepsea-ops/server/internal/audit"
	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/grpcserver"
	"github.com/deepsea-ops/server/internal/metrics"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/scheduler"
	"github.com/deepsea-ops/server/internal/store"
	"github.com/deepsea-ops/server/internal/webassets"
)

// New 构造 HTTP 路由, 注入 store、grpcServer、auth 服务、扫描调度器、指标存储和审计存储。
// sc 可为 nil(开发环境不联动扫描), 非 nil 时部署成功后自动触发目标 Agent 扫描。
// ms 可为 nil(未启用监控), 非 nil 时提供指标查询接口。
// aud 可为 nil(未启用审计), 非 nil 时写操作自动记录审计日志。
func New(s *store.Store, gs *grpcserver.Server, as *auth.Service, sc *scheduler.Scheduler, ms *metrics.Store, aud *audit.Store) http.Handler {
	mux := http.NewServeMux()

	// --- 白名单路由(无需登录) ---
	mux.HandleFunc("/api/healthz", func(w http.ResponseWriter, r *http.Request) {
		auth.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// v0.6.6: 控制面版本号(无需登录, 前端登录页/兼容性判断用)
	mux.HandleFunc("/api/version", handleServerVersion)

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
			if aud != nil {
				go aud.Record(audit.Log{Timestamp: time.Now().UnixMilli(), Username: req.Username, Method: r.Method, Path: r.URL.Path, Action: "login-failed", Status: http.StatusTooManyRequests, IP: audit.ClientIP(r)})
			}
			auth.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "尝试过于频繁, 请稍后再试"})
			return
		}
		resp, err := as.Login(req.Username, req.Password)
		if err != nil {
			if aud != nil {
				go aud.Record(audit.Log{Timestamp: time.Now().UnixMilli(), Username: req.Username, Method: r.Method, Path: r.URL.Path, Action: "login-failed", Status: http.StatusUnauthorized, IP: audit.ClientIP(r)})
			}
			auth.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		if aud != nil {
			go aud.Record(audit.Log{Timestamp: time.Now().UnixMilli(), Username: req.Username, Method: r.Method, Path: r.URL.Path, Action: "login", Status: http.StatusOK, IP: audit.ClientIP(r)})
		}
		auth.WriteJSON(w, http.StatusOK, resp)
	})

	// --- 受保护路由(需登录) ---
	mw := auth.NewMiddleware("/api/login", "/api/healthz", "/api/version")
	mw.SetAudit(aud) // v0.6.4: 写操作自动记录审计

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

	// Agent 管理 + 读配置 + 配置比对 + 项目扫描 + 指标查询(v0.6.3)
	mux.HandleFunc("/api/agents/", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
		parts := strings.Split(path, "/")
		// v0.6.6: 批量滚动升级 POST /api/agents/upgrade { agentIds, url, checksum, waitSeconds }
		if len(parts) == 1 && parts[0] == "upgrade" {
			if r.Method == http.MethodPost {
				handleBatchUpgradeAgents(w, r, gs)
			} else {
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
			return
		}
		if len(parts) < 2 {
			auth.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "未知路径"})
			return
		}
		agentID := parts[0]
		action := parts[1]

		// v0.6.3: GET 指标查询(metrics / metrics/history)
		if r.Method == http.MethodGet && action == "metrics" {
			if ms == nil {
				auth.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "监控未启用"})
				return
			}
			if len(parts) >= 3 && parts[2] == "history" {
				handleMetricsHistory(w, r, ms, agentID)
			} else {
				handleMetricsLatest(w, r, ms, agentID)
			}
			return
		}

		// v0.6.6: GET 版本查询(返回缓存版本, 无则主动查询)
		if r.Method == http.MethodGet && action == "version" {
			handleGetAgentVersion(w, r, gs, agentID)
			return
		}

		// 其余为写操作(POST)
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
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
		case "upgrade":
			// v0.6.6: 单 Agent 升级
			handleUpgradeAgent(w, r, gs, agentID)
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

	// v0.6.5: 项目配置基准与版本管理(/api/projects/{id}/baseline 等)
	// projectID = agentID + "|" + projectPath, 含 "/", 路径解析用关键字匹配
	mux.HandleFunc("/api/projects/", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/projects/")
		if rest == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		projectID, parts := parseProjectPath(rest)
		if projectID == "" || parts == nil || len(parts) == 0 {
			auth.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "未知路径"})
			return
		}
		action := parts[0]
		switch action {
		case "baseline":
			switch r.Method {
			case http.MethodGet:
				handleGetBaseline(w, r, s, projectID)
			case http.MethodPost:
				handleSaveBaseline(w, r, s, projectID)
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
		case "config-versions":
			// GET 列出版本历史; POST /{ver}/rollback 回滚到指定版本
			if r.Method == http.MethodGet {
				handleListConfigVersions(w, r, s, projectID)
				return
			}
			if r.Method == http.MethodPost && len(parts) >= 3 && parts[2] == "rollback" {
				ver, err := strconv.Atoi(parts[1])
				if err != nil {
					auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "版本号格式错误"})
					return
				}
				handleRollbackBaseline(w, r, s, projectID, ver)
				return
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
		case "deploy-baseline":
			if r.Method == http.MethodPost {
				handleDeployBaseline(w, r, s, gs, projectID)
			} else {
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
		default:
			auth.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "未知操作: " + action})
		}
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
			handleCreateDeployTask(w, r, s, gs, sc)
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

	// v0.6.4: 操作审计日志查询(需登录)
	mux.HandleFunc("/api/audit-logs", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		if aud == nil {
			auth.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "审计未启用"})
			return
		}
		handleListAuditLogs(w, r, aud)
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

	// 前端静态文件(embed 单二进制): 非 /api/ 路径从嵌入的 web/dist/ 提供服务。
	// 开发环境未构建前端时 dist 为占位文件, 走 vite dev server。
	return withStaticFiles(handler, webassets.FS())
}

// withStaticFiles 在 /api/ 之外的路径上提供前端静态文件服务 (SPA 模式)。
// 已注册的 /api/ 路由优先; 其余路径尝试从静态文件系统读取, 找不到则回退 index.html。
func withStaticFiles(apiHandler http.Handler, assets fs.FS) http.Handler {
	if assets == nil {
		return apiHandler
	}
	fileServer := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			apiHandler.ServeHTTP(w, r)
			return
		}
		// 检查静态文件是否存在, 不存在则回退到 index.html (SPA 路由)
		if _, err := fs.Stat(assets, strings.TrimPrefix(r.URL.Path, "/")); err != nil {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
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
