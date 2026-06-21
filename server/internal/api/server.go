package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/deepsea-ops/server/internal/grpcserver"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/store"
)

const maxBodyBytes = 1 << 20

// New 构造 HTTP 路由, 注入 store 和 grpcServer。
func New(s *store.Store, gs *grpcserver.Server) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/servers", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleListServers(w, s)
		case http.MethodPost:
			handleAddServer(w, r, s)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/agents", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(gs.ListAgents())
	})
	mux.HandleFunc("/api/agents/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/read-config") && r.Method == http.MethodPost {
			handleReadConfig(w, r, gs)
			return
		}
		http.NotFound(w, r)
	})
	// 集群管理: 查询状态 / 加入新节点
	mux.HandleFunc("/api/cluster/info", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.ClusterInfo())
	})
	mux.HandleFunc("/api/cluster/join", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleClusterJoin(w, r, s)
	})
	return mux
}

func handleListServers(w http.ResponseWriter, s *store.Store) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.ListServers())
}

func handleAddServer(w http.ResponseWriter, r *http.Request, s *store.Store) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var srv model.Server
	if err := json.NewDecoder(r.Body).Decode(&srv); err != nil {
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

func handleReadConfig(w http.ResponseWriter, r *http.Request, gs *grpcserver.Server) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	agentID := strings.TrimSuffix(trimmed, "/read-config")
	if agentID == "" || strings.Contains(agentID, "/") {
		http.Error(w, "无效的 agent ID", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求体格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		http.Error(w, "path 不能为空", http.StatusBadRequest)
		return
	}
	content, err := gs.ReadConfig(agentID, req.Path, 10*time.Second)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{
		"agentId": agentID,
		"path":    req.Path,
		"content": content,
	})
}

// handleClusterJoin: Leader 把一个新节点 AddVoter 纳入集群。
// 请求体: {"id":"node2","addr":"127.0.0.1:7001"}
func handleClusterJoin(w http.ResponseWriter, r *http.Request, s *store.Store) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req struct {
		ID   string `json:"id"`
		Addr string `json:"addr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求体格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ID == "" || req.Addr == "" {
		http.Error(w, "id/addr 不能为空", http.StatusBadRequest)
		return
	}
	if err := s.AddVoter(req.ID, req.Addr); err != nil {
		http.Error(w, "加入集群失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "id": req.ID})
}