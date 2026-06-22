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
// 路由分两类:
//   - 白名单: /api/login、/api/healthz 等无需鉴权
//   - 受保护: 其余 /api/* 全部经过 JWT 中间件校验
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
			handleListServers(w, s)
		case http.MethodPost:
			handleAddServer(w, r, s)
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

		case "config-diff":
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

		case "scan-projects":
			var scanReq struct {
				ScanDirs string `json:"scanDirs"`
			}
			_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&scanReq)
			scanResult, err := gs.ScanProjects(agentID, scanReq.ScanDirs, 60*time.Second)
			if err != nil {
				auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			auth.WriteJSON(w, http.StatusOK, scanResult)

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

	return mux
}

func handleListServers(w http.ResponseWriter, s *store.Store) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.ListServers())
}

func handleAddServer(w http.ResponseWriter, r *http.Request, s *store.Store) {
	var srv model.Server
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&srv); err != nil {
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