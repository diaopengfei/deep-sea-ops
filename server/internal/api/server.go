package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/configdiff"
	"github.com/deepsea-ops/server/internal/grpcserver"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/store"
)

// New 构造 HTTP 路由, 注入 store、grpcServer 和 auth 服务。
//
// 路由分两类:
//   - 白名单: /api/login、/api/healthz 等无需鉴权
//   - 受保护: 其余 /api/* 全部经过 JWT 中间件校验
//
// 鉴权中间件从 Authorization: Bearer <token> 解析 JWT, 校验签名和有效期,
// 通过则把用户信息放入 context, 失败返回 401。
func New(s *store.Store, gs *grpcserver.Server, as *auth.Service) http.Handler {
	mux := http.NewServeMux()

	// --- 白名单路由(无需登录) ---

	// 健康检查, 给负载均衡/监控系统用
	mux.HandleFunc("/api/healthz", func(w http.ResponseWriter, r *http.Request) {
		auth.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// 登录: 校验密码, 签发 JWT + 刷新 token
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		// MaxBytesReader 限制请求体 16KiB, 防止恶意大请求体耗尽内存
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
			return
		}
		if req.Username == "" || req.Password == "" {
			auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "用户名/密码不能为空"})
			return
		}

		// 登录限流: 同一用户名连续失败 5 次会被临时锁定 2 分钟
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

	// 鉴权中间件: 白名单里的路径直接放行, 其余要求有效 JWT
	mw := auth.NewMiddleware("/api/login", "/api/healthz")

	// 当前登录用户信息
	mux.HandleFunc("/api/auth/me", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		claims := auth.FromContext(r.Context())
		auth.WriteJSON(w, http.StatusOK, map[string]string{
			"username": claims.Username,
			"role":     claims.Role,
		})
	}))

	// 服务器列表(增查)
	mux.HandleFunc("/api/servers", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleListServers(w, s)
		case http.MethodPost:
			handleAddServer(w, r, s)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))

	// Agent 在线列表
	mux.HandleFunc("/api/agents", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		auth.WriteJSON(w, http.StatusOK, gs.ListAgents())
	}))

	// Agent 操作: /api/agents/{id}/read-config, /api/agents/{id}/config-diff
	mux.HandleFunc("/api/agents/", mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// 解析路径: /api/agents/{id}/{action}
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
			// 读单个配置文件: { path: "/opt/app/application.yml" }
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
			result, err := gs.ReadConfig(agentID, req.Path, 15*time.Second)
			if err != nil {
				auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			auth.WriteJSON(w, http.StatusOK, map[string]string{"content": result})

		case "config-diff":
			// 配置比对: 采集 Nacos/本地/jar 三路配置并 diff
			// 请求体: { nacosAddr, nacosDataId, nacosGroup, nacosNamespace, localPath, jarPath, jarEntry }
			var req struct {
				NacosAddr      string `json:"nacosAddr"`
				NacosDataID    string `json:"nacosDataId"`
				NacosGroup     string `json:"nacosGroup"`
				NacosNamespace string `json:"nacosNamespace"`
				LocalPath      string `json:"localPath"`
				JarPath        string `json:"jarPath"`
				JarEntry       string `json:"jarEntry"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
				auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
				return
			}
			// 下发采集指令, 拿到三路配置 JSON
			params := map[string]string{
				"nacosAddr":      req.NacosAddr,
				"nacosDataId":    req.NacosDataID,
				"nacosGroup":     req.NacosGroup,
				"nacosNamespace": req.NacosNamespace,
				"localPath":      req.LocalPath,
				"jarPath":        req.JarPath,
				"jarEntry":       req.JarEntry,
			}
			snapJSON, err := gs.CollectConfigs(agentID, params, 30*time.Second)
			if err != nil {
				auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			// 控制面做 diff
			report := configdiff.BuildReport(snapJSON)
			auth.WriteJSON(w, http.StatusOK, report)

		default:
			auth.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "未知操作: " + action})
		}
	}))

	// 集群管理: 加入节点 / 查询状态
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

	return mux
}

// handleListServers 返回所有服务器(JSON 数组)。
func handleListServers(w http.ResponseWriter, s *store.Store) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.ListServers())
}

// handleAddServer 新增一台服务器(走 Raft 一致性写)。
func handleAddServer(w http.ResponseWriter, r *http.Request, s *store.Store) {
	var srv model.Server
	// 限制请求体 1MiB
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&srv); err != nil {
		http.Error(w, "请求体格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if srv.ID == "" || srv.Name == "" || srv.IP == "" {
		http.Error(w, "id/name/ip 不能为空", http.StatusBadRequest)
		return
	}
	if srv.Status == "" {
		srv.Status = "offline"
	}

	if err := s.AddServer(srv); err != nil {
		http.Error(w, "写入失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(srv)
}

// handleClusterJoin 处理 POST /api/cluster/join, 把一个新节点加入 Raft 集群。
// 请求体: {"nodeId": "node2", "raftAddr": "127.0.0.1:7002"}
func handleClusterJoin(w http.ResponseWriter, r *http.Request, s *store.Store) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		NodeID    string `json:"nodeId"`
		RaftAddr  string `json:"raftAddr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求体格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.NodeID == "" || req.RaftAddr == "" {
		http.Error(w, "nodeId/raftAddr 不能为空", http.StatusBadRequest)
		return
	}
	if err := s.AddVoter(req.NodeID, req.RaftAddr); err != nil {
		http.Error(w, "加入集群失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	auth.WriteJSON(w, http.StatusOK, map[string]string{"id": req.NodeID, "status": "ok"})
}
