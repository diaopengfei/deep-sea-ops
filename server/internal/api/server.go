package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/deepsea-ops/server/internal/agentclient"
	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/configdiff"
	"github.com/deepsea-ops/server/internal/crypto"
	"github.com/deepsea-ops/server/internal/grpcserver"
	"github.com/deepsea-ops/server/internal/inject"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/sshclient"
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
		// v0.5.2: /api/servers/{id}/inject - 从服务器列表触发注入
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

	// 项目记录(持久化的扫描结果, M4)
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

	// 部署任务(M5)
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

	// SSH 凭据(v0.4)
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

	// v0.5.2: ops 服务节点合并视图(raft + agent)
	mux.HandleFunc("/api/ops-nodes", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleListOpsNodes(w, r, s, gs)
	}))

	// 自动注入(v0.4): SSH 推送二进制 + systemd, 远程拉起节点
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

func handleListServers(w http.ResponseWriter, r *http.Request, s *store.Store) {
	servers := s.ListServers()
	if servers == nil {
		servers = []model.Server{}
	}

	// 模糊检索: keyword 匹配 name/ip/username/os/status
	keyword := strings.ToLower(r.URL.Query().Get("keyword"))
	if keyword != "" {
		filtered := make([]model.Server, 0, len(servers))
		for _, srv := range servers {
			if strings.Contains(strings.ToLower(srv.Name), keyword) ||
				strings.Contains(strings.ToLower(srv.IP), keyword) ||
				strings.Contains(strings.ToLower(srv.Username), keyword) ||
				strings.Contains(strings.ToLower(srv.OS), keyword) ||
				strings.Contains(strings.ToLower(srv.Status), keyword) ||
				strings.Contains(strconv.FormatInt(srv.ID, 10), keyword) {
				filtered = append(filtered, srv)
			}
		}
		servers = filtered
	}

	// 排序: sort 字段 + order 方向
	sortField := r.URL.Query().Get("sort")
	order := r.URL.Query().Get("order")
	if sortField != "" {
		sort.Slice(servers, func(i, j int) bool {
			less := serverLess(servers[i], servers[j], sortField)
			if order == "desc" {
				return !less
			}
			return less
		})
	}

	auth.WriteJSON(w, http.StatusOK, servers)
}

// serverLess 比较两个 Server 在指定字段上的大小。
func serverLess(a, b model.Server, field string) bool {
	switch field {
	case "id":
		return a.ID < b.ID
	case "name":
		return a.Name < b.Name
	case "ip":
		return a.IP < b.IP
	case "port":
		return a.Port < b.Port
	case "os":
		return a.OS < b.OS
	case "username":
		return a.Username < b.Username
	case "status":
		return a.Status < b.Status
	case "createdAt":
		return a.CreatedAt < b.CreatedAt
	default:
		return a.ID < b.ID
	}
}

func handleAddServer(w http.ResponseWriter, r *http.Request, s *store.Store) {
	var req struct {
		Name     string `json:"name"`
		IP       string `json:"ip"`
		Port     int    `json:"port"`
		OS       string `json:"os"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	if req.Name == "" || req.IP == "" || req.Username == "" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name/ip/username 不能为空"})
		return
	}
	if req.Port == 0 {
		req.Port = 22
	}
	if req.OS == "" {
		req.OS = model.OSLinux
	}

	// 加密 SSH 密码
	encPassword, err := crypto.Encrypt(req.Password)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "加密密码失败: " + err.Error()})
		return
	}

	srv := model.Server{
		Name:      req.Name,
		IP:        req.IP,
		Port:      req.Port,
		OS:        req.OS,
		Username:  req.Username,
		Password:  encPassword,
		Status:    "offline",
		CreatedAt: time.Now().UnixMilli(),
	}

	id, err := s.AddServer(srv)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "写入失败: " + err.Error()})
		return
	}
	srv.ID = id
	auth.WriteJSON(w, http.StatusCreated, srv)
}

// handleTestConnection 测试 SSH 连接(不落库, 仅验证连通性)。
func handleTestConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP       string `json:"ip"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	if req.IP == "" || req.Username == "" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "ip/username 不能为空"})
		return
	}
	if req.Port == 0 {
		req.Port = 22
	}

	client, err := sshclient.NewClient(sshclient.Config{
		Host:     req.IP,
		Port:     req.Port,
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		auth.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}
	client.Close()
	auth.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"ok":   true,
		"msg":  "连接成功",
	})
}

// handleDeleteServer 按 ID 删除服务器。
func handleDeleteServer(w http.ResponseWriter, r *http.Request, s *store.Store, idStr string) {
	if err := s.DelServer(idStr); err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleUpdateServer 更新服务器信息。
func handleUpdateServer(w http.ResponseWriter, r *http.Request, s *store.Store, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的 ID"})
		return
	}
	var req struct {
		Name     string `json:"name"`
		IP       string `json:"ip"`
		Port     int    `json:"port"`
		OS       string `json:"os"`
		Username string `json:"username"`
		Password string `json:"password"` // 为空表示不修改密码
		Status   string `json:"status"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}

	// 查现有记录, 保留原值
	servers := s.ListServers()
	var existing *model.Server
	for i := range servers {
		if servers[i].ID == id {
			existing = &servers[i]
			break
		}
	}
	if existing == nil {
		auth.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "服务器不存在"})
		return
	}

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.IP != "" {
		existing.IP = req.IP
	}
	if req.Port != 0 {
		existing.Port = req.Port
	}
	if req.OS != "" {
		existing.OS = req.OS
	}
	if req.Username != "" {
		existing.Username = req.Username
	}
	if req.Status != "" {
		existing.Status = req.Status
	}
	// 密码非空才更新(前端不回显密码, 修改时才传)
	if req.Password != "" {
		encPassword, err := crypto.Encrypt(req.Password)
		if err != nil {
			auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "加密密码失败: " + err.Error()})
			return
		}
		existing.Password = encPassword
	}

	if err := s.UpdServer(*existing); err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusOK, existing)
}

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

// --- Agent 指令处理 ---

func handleReadConfig(w http.ResponseWriter, r *http.Request, gs *grpcserver.Server, agentID string) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	if req.Path == "" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "path 不能为空"})
		return
	}
	result, err := gs.ReadConfig(agentID, req.Path, 30*time.Second)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusOK, map[string]string{"content": result})
}

func handleConfigDiff(w http.ResponseWriter, r *http.Request, gs *grpcserver.Server, agentID string) {
	var req struct {
		NacosAddr        string `json:"nacosAddr"`
		NacosDataID      string `json:"nacosDataId"`
		NacosGroup       string `json:"nacosGroup"`
		NacosNamespace   string `json:"nacosNamespace"`
		NacosUsername    string `json:"nacosUsername"`
		NacosPassword    string `json:"nacosPassword"`
		NacosAccessToken string `json:"nacosAccessToken"`
		LocalPath        string `json:"localPath"`
		JarPath          string `json:"jarPath"`
		JarEntry         string `json:"jarEntry"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	params := map[string]string{
		"nacosAddr":        req.NacosAddr,
		"nacosDataId":      req.NacosDataID,
		"nacosGroup":       req.NacosGroup,
		"nacosNamespace":   req.NacosNamespace,
		"nacosUsername":    req.NacosUsername,
		"nacosPassword":    req.NacosPassword,
		"nacosAccessToken": req.NacosAccessToken,
		"localPath":        req.LocalPath,
		"jarPath":          req.JarPath,
		"jarEntry":         req.JarEntry,
	}
	snapJSON, err := gs.CollectConfigs(agentID, params, 30*time.Second)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	report := configdiff.BuildReport(snapJSON)
	auth.WriteJSON(w, http.StatusOK, report)
}

// handleScanProjects 扫描 Agent 节点上的项目, 并把结果持久化到 Raft(M4)。
func handleScanProjects(w http.ResponseWriter, r *http.Request, gs *grpcserver.Server, s *store.Store, agentID string) {
	var scanReq struct {
		ScanDirs string `json:"scanDirs"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&scanReq)
	scanResult, err := gs.ScanProjects(agentID, scanReq.ScanDirs, 60*time.Second)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// M4: 把扫描结果持久化到 Raft(projects bucket), 多节点共享视图
	// 先清除该 Agent 的旧项目记录, 再写入新的
	if err := s.ClearAgentProjects(agentID); err != nil {
		log.Printf("警告: 清除 Agent %s 旧项目记录失败: %v", agentID, err)
	}

	// 解析扫描结果 JSON, 转成 ProjectRecord 持久化
	var result struct {
		Projects []agentclient.ProjectInfo `json:"projects"`
		Hosts    string                    `json:"hosts"`
		HostsErr string                    `json:"hostsErr"`
	}
	if err := json.Unmarshal([]byte(scanResult), &result); err == nil {
		now := time.Now()
		for _, p := range result.Projects {
			rec := model.ProjectRecord{
				ID:          agentID + "|" + p.Path,
				AgentID:     agentID,
				Path:        p.Path,
				Type:        string(p.Type),
				Name:        p.Name,
				ConfigFiles: p.ConfigFiles,
				JarPath:     p.JarPath,
				JarEntry:    p.JarEntry,
				Running:     p.Running,
				PID:         p.PID,
				ScannedAt:   now,
			}
			if err := s.AddProject(rec); err != nil {
				log.Printf("警告: 持久化项目 %s 失败: %v", rec.ID, err)
			}
		}
	} else {
		log.Printf("警告: 解析扫描结果 JSON 失败, 未持久化: %v", err)
	}

	// 返回原始扫描结果(含生效配置等完整信息)
	auth.WriteJSON(w, http.StatusOK, scanResult)
}

// --- 部署任务(M5) ---

// handleCreateDeployTask 创建部署任务并下发到目标 Agent 执行。
// 流程: 创建任务(Raft 持久化) → 下发 DEPLOY 指令到目标 Agent → 更新任务状态
func handleCreateDeployTask(w http.ResponseWriter, r *http.Request, s *store.Store, gs *grpcserver.Server) {
	var req struct {
		Type         string `json:"type"`         // scale_out / migrate
		ProjectPath  string `json:"projectPath"`
		ProjectName  string `json:"projectName"`
		JarPath      string `json:"jarPath"`
		ConfigText   string `json:"configText"`
		TargetAgent  string `json:"targetAgentId"`
		SourceAgent  string `json:"sourceAgentId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	if req.TargetAgent == "" || req.JarPath == "" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "targetAgentId 和 jarPath 不能为空"})
		return
	}

	now := time.Now()
	task := model.DeployTask{
		ID:            uuid.NewString(),
		Type:          req.Type,
		ProjectPath:   req.ProjectPath,
		ProjectName:   req.ProjectName,
		JarPath:       req.JarPath,
		ConfigText:    req.ConfigText,
		TargetAgentID: req.TargetAgent,
		SourceAgentID: req.SourceAgent,
		Status:        model.DeployStatusPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.AddDeployTask(task); err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "创建任务失败: " + err.Error()})
		return
	}

	// 异步执行部署: 下发 DEPLOY 指令到目标 Agent
	go executeDeployTask(s, gs, task)

	auth.WriteJSON(w, http.StatusOK, task)
}

// executeDeployTask 异步执行部署任务: 下发指令到 Agent, 更新状态。
func executeDeployTask(s *store.Store, gs *grpcserver.Server, task model.DeployTask) {
	// 标记为运行中
	task.Status = model.DeployStatusRunning
	task.UpdatedAt = time.Now()
	if err := s.UpdDeployTask(task); err != nil {
		log.Printf("警告: 更新任务 %s 状态为 running 失败: %v", task.ID, err)
	}

	// 下发 DEPLOY 指令到目标 Agent
	params := map[string]string{
		"jarPath":     task.JarPath,
		"configText":  task.ConfigText,
		"projectName": task.ProjectName,
	}
	if task.Type == model.DeployTypeMigrate && task.SourceAgentID != "" {
		// 迁移: 先停源 Agent 上的项目
		stopParams := map[string]string{"projectPath": task.ProjectPath}
		if _, err := gs.SendCommand(task.SourceAgentID, "STOP_PROJECT", stopParams, 30*time.Second); err != nil {
			log.Printf("警告: 迁移任务 %s 停止源 Agent %s 项目失败: %v", task.ID, task.SourceAgentID, err)
		}
	}

	// 在目标 Agent 上执行部署
	output, err := gs.SendCommand(task.TargetAgentID, "DEPLOY", params, 120*time.Second)
	if err != nil {
		task.Status = model.DeployStatusFailed
		task.Error = err.Error()
	} else {
		task.Status = model.DeployStatusSuccess
		task.Error = ""
		log.Printf("部署任务 %s 成功: %s", task.ID, output)
	}
	task.UpdatedAt = time.Now()
	if err := s.UpdDeployTask(task); err != nil {
		log.Printf("警告: 更新任务 %s 最终状态失败: %v", task.ID, err)
	}
}

// handleDeploy 直接对指定 Agent 下发部署指令。
func handleDeploy(w http.ResponseWriter, r *http.Request, gs *grpcserver.Server, s *store.Store, agentID string) {
	var req struct {
		JarPath    string `json:"jarPath"`
		ConfigText string `json:"configText"`
		ProjectName string `json:"projectName"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	params := map[string]string{
		"jarPath":     req.JarPath,
		"configText":  req.ConfigText,
		"projectName": req.ProjectName,
	}
	output, err := gs.SendCommand(agentID, "DEPLOY", params, 120*time.Second)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusOK, map[string]string{"output": output})
}

// handleStopProject 停止指定 Agent 上的项目。
func handleStopProject(w http.ResponseWriter, r *http.Request, gs *grpcserver.Server, agentID string) {
	var req struct {
		ProjectPath string `json:"projectPath"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	params := map[string]string{"projectPath": req.ProjectPath}
	output, err := gs.SendCommand(agentID, "STOP_PROJECT", params, 30*time.Second)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusOK, map[string]string{"output": output})
}

// --- SSH 凭据(v0.4) ---

func handleAddCredential(w http.ResponseWriter, r *http.Request, s *store.Store) {
	var req struct {
		ID         string `json:"id"`
		ServerName string `json:"serverName"`
		IP         string `json:"ip"`
		Port       int    `json:"port"`
		Username   string `json:"username"`
		Password   string `json:"password"`
		PrivateKey string `json:"privateKey"`
		AuthType   string `json:"authType"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	if req.IP == "" || req.Username == "" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "ip 和 username 不能为空"})
		return
	}
	if req.ID == "" {
		req.ID = req.IP
	}
	if req.Port == 0 {
		req.Port = 22
	}
	if req.AuthType == "" {
		req.AuthType = model.AuthTypePassword
	}

	// 加密敏感字段
	encPassword, err := crypto.Encrypt(req.Password)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "加密密码失败: " + err.Error()})
		return
	}
	encKey, err := crypto.Encrypt(req.PrivateKey)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "加密私钥失败: " + err.Error()})
		return
	}

	cred := model.SSHCredential{
		ID:         req.ID,
		ServerName: req.ServerName,
		IP:         req.IP,
		Port:       req.Port,
		Username:   req.Username,
		Password:   encPassword,
		PrivateKey: encKey,
		AuthType:   req.AuthType,
		CreatedAt:  time.Now().Unix(),
	}
	if err := s.AddCredential(cred); err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusCreated, cred)
}

// --- 自动注入(v0.4) ---

// validateRaftNodeCount 校验 raft 节点数量是否安全。
// 规则: 首节点必须 1 个; 后续加入后必须保持奇数且 3-7 个范围。
// 返回 error 表示不允许加入。
func validateRaftNodeCount(s *store.Store) error {
	info := s.ClusterInfo()
	voterCount := 0
	for _, srv := range info.Servers {
		if srv.Suffrage == "Voter" {
			voterCount++
		}
	}
	newCount := voterCount + 1
	// 范围校验: 3-7 个 Voter (Raft 推荐 3/5/7)
	if newCount > 7 {
		return fmt.Errorf("raft 集群建议不超过 7 个 Voter 节点(当前 %d, 加入后 %d)", voterCount, newCount)
	}
	// 偶数过渡态允许(Raft 容忍瞬态偶数), 但打日志提醒
	if newCount%2 == 0 {
		log.Printf("[提示] raft 集群加入后为偶数 %d 个 Voter, 建议尽快再加一个达到奇数稳态", newCount)
	}
	return nil
}

// handleInject 处理自动注入请求: SSH 推送二进制 + systemd, 远程拉起节点。
func handleInject(w http.ResponseWriter, r *http.Request, s *store.Store) {
	var req struct {
		CredentialID   string `json:"credentialId"`
		Role           string `json:"role"`           // raft / agent
		NodeID         string `json:"nodeId"`         // 节点 ID
		RaftAddr       string `json:"raftAddr"`       // Raft 通信地址(raft 角色)
		JoinAddr       string `json:"joinAddr"`       // Leader Raft 地址(raft 角色)
		LeaderGRPCAddr string `json:"leaderGrpcAddr"` // Leader gRPC 地址(agent 角色)
		BinaryPath     string `json:"binaryPath"`     // 本机二进制路径(可选)
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	if req.CredentialID == "" || req.Role == "" || req.NodeID == "" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "credentialId/role/nodeId 不能为空"})
		return
	}
	if req.Role != "raft" && req.Role != "agent" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "role 必须是 raft 或 agent"})
		return
	}

	// v0.5.2: raft 节点数量安全校验
	if req.Role == "raft" {
		if err := validateRaftNodeCount(s); err != nil {
			auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}

	injReq := inject.InjectRequest{
		CredentialID:   req.CredentialID,
		Role:           inject.Role(req.Role),
		NodeID:         req.NodeID,
		RaftAddr:       req.RaftAddr,
		JoinAddr:       req.JoinAddr,
		LeaderGRPCAddr: req.LeaderGRPCAddr,
		BinaryPath:     req.BinaryPath,
	}

	inj := inject.NewInjector(s)
	// 注入是耗时操作(SSH + 上传), 异步执行, 立即返回
	go func() {
		result := inj.Inject(injReq)
		if result.Success {
			log.Printf("注入成功: node=%s role=%s, 耗时=%s\n%s", req.NodeID, req.Role, result.Duration, result.Output)
		} else {
			log.Printf("注入失败: node=%s role=%s, 错误=%s", req.NodeID, req.Role, result.Output)
		}
	}()

	auth.WriteJSON(w, http.StatusAccepted, map[string]string{
		"status": "accepted",
		"nodeId": req.NodeID,
		"role":   req.Role,
		"msg":    "注入任务已提交, 正在后台执行(SSH 推送 + systemd 启动)",
	})
}

// handleInjectFromServer 从服务器列表触发注入(v0.5.2)。
// 直接用 Server 表中存储的 SSH 凭据(解密后传给 inject), 不再依赖 credentialId。
// POST /api/servers/{id}/inject
// Body: {"role":"raft|agent", "nodeId":"node2", "raftAddr":"...", "joinAddr":"...", "leaderGrpcAddr":"...", "binaryPath":"..."}
func handleInjectFromServer(w http.ResponseWriter, r *http.Request, s *store.Store, serverIDStr string) {
	serverID, err := strconv.ParseInt(serverIDStr, 10, 64)
	if err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的服务器 ID"})
		return
	}
	srv, ok := s.GetServer(serverID)
	if !ok {
		auth.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "服务器不存在"})
		return
	}
	// v0.5.2: 注入依赖 systemd, 仅支持 Linux 服务器
	if srv.OS != model.OSLinux {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "注入仅支持 Linux 服务器, 当前服务器 OS 为 " + srv.OS})
		return
	}

	var req struct {
		Role           string `json:"role"`
		NodeID         string `json:"nodeId"`
		RaftAddr       string `json:"raftAddr"`
		JoinAddr       string `json:"joinAddr"`
		LeaderGRPCAddr string `json:"leaderGrpcAddr"`
		BinaryPath     string `json:"binaryPath"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	if req.Role == "" || req.NodeID == "" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "role/nodeId 不能为空"})
		return
	}
	if req.Role != "raft" && req.Role != "agent" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "role 必须是 raft 或 agent"})
		return
	}

	// v0.5.2: raft 节点数量安全校验
	if req.Role == "raft" {
		if err := validateRaftNodeCount(s); err != nil {
			auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}

	// 解密 Server 表中的 SSH 密码
	password, err := crypto.Decrypt(srv.Password)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "解密 SSH 密码失败: " + err.Error()})
		return
	}

	injReq := inject.InjectRequest{
		SSH: &inject.SSHConfig{
			Host:     srv.IP,
			Port:     srv.Port,
			Username: srv.Username,
			Password: password,
		},
		Role:           inject.Role(req.Role),
		NodeID:         req.NodeID,
		RaftAddr:       req.RaftAddr,
		JoinAddr:       req.JoinAddr,
		LeaderGRPCAddr: req.LeaderGRPCAddr,
		BinaryPath:     req.BinaryPath,
	}

	inj := inject.NewInjector(s)
	go func() {
		result := inj.Inject(injReq)
		if result.Success {
			log.Printf("服务器注入成功: server=%d node=%s role=%s, 耗时=%s\n%s", serverID, req.NodeID, req.Role, result.Duration, result.Output)
		} else {
			log.Printf("服务器注入失败: server=%d node=%s role=%s, 错误=%s", serverID, req.NodeID, req.Role, result.Output)
		}
	}()

	auth.WriteJSON(w, http.StatusAccepted, map[string]string{
		"status": "accepted",
		"nodeId": req.NodeID,
		"role":   req.Role,
		"msg":    "注入任务已提交, 正在后台执行(SSH 推送 + systemd 启动)",
	})
}

// OpsNode 是 ops 服务节点视图, 合并 raft 节点和 agent 节点。
type OpsNode struct {
	Type     string `json:"type"`     // "raft" / "agent"
	ID       string `json:"id"`       // 节点 ID
	Address  string `json:"address"`  // 通信地址(raft: raft addr, agent: gRPC 来源)
	Hostname string `json:"hostname"` // 主机名(agent 才有)
	IP       string `json:"ip"`       // IP 地址
	State    string `json:"state"`    // raft: Leader/Follower/Candidate; agent: online
	Suffrage string `json:"suffrage"` // raft: Voter/Nonvoter; agent: ""
	LastSeen int64  `json:"lastSeen"` // 最后心跳时间(unix 秒, agent 才有)
	IsLeader bool   `json:"isLeader"` // 是否当前 Leader
	IsSelf   bool   `json:"isSelf"`   // 是否当前节点自己
}

// handleListOpsNodes 返回 raft + agent 合并视图。
func handleListOpsNodes(w http.ResponseWriter, r *http.Request, s *store.Store, gs *grpcserver.Server) {
	clusterInfo := s.ClusterInfo()
	nodes := make([]OpsNode, 0, len(clusterInfo.Servers)+len(gs.ListAgents()))

	// raft 节点
	for _, srv := range clusterInfo.Servers {
		isLeader := clusterInfo.Leader == srv.Address
		isSelf := srv.ID == clusterInfo.ID
		// State 只对本节点有意义(本节点知道自己是 Leader/Follower/Candidate)
		// 其他节点的状态无法从 Raft API 获取, 标注为 "unknown"
		state := "unknown"
		if isSelf {
			state = clusterInfo.State
		} else if isLeader {
			state = "Leader"
		}
		nodes = append(nodes, OpsNode{
			Type:     "raft",
			ID:       srv.ID,
			Address:  srv.Address,
			State:    state,
			Suffrage: srv.Suffrage,
			IsLeader: isLeader,
			IsSelf:   isSelf,
		})
	}

	// agent 节点
	for _, a := range gs.ListAgents() {
		nodes = append(nodes, OpsNode{
			Type:     "agent",
			ID:       a.ID,
			Hostname: a.Hostname,
			IP:       a.IP,
			State:    "online",
			LastSeen: a.LastSeen.Unix(),
		})
	}

	auth.WriteJSON(w, http.StatusOK, nodes)
}

// --- 入口代理(v0.4) ---

// withLeaderProxy 包装 handler, 实现"任意节点可访问, 自动转发 Leader"。
//
// 规则:
//   - GET 请求(读): 本地处理(FSM 读是最终一致的, Follower 也能读)
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

		// GET 请求: 本地处理
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
		// 自定义错误处理: Leader 不可达时返回明确错误, 而非空响应
		proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
			log.Printf("入口代理: 转发到 Leader %s 失败: %v", leaderHTTPAddr, err)
			auth.WriteJSON(rw, http.StatusBadGateway, map[string]string{
				"error": "转发到 Leader 失败: " + err.Error(),
			})
		}
		// 记录转发日志
		log.Printf("入口代理: 转发 %s %s -> Leader %s", r.Method, r.URL.Path, leaderHTTPAddr)
		proxy.ServeHTTP(w, r)
	})
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
