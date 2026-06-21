package api

import (
	"encoding/json"
	"net/http"

	"github.com/deepsea-ops/server/internal/grpcserver"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/store"
)

// maxBodyBytes 限制请求体大小, 防止恶意大请求体耗尽内存。
const maxBodyBytes = 1 << 20 // 1 MiB

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
	// Agent 在线列表(来自 gRPC 注册表)
	mux.HandleFunc("/api/agents", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(gs.ListAgents())
	})
	return mux
}

func handleListServers(w http.ResponseWriter, s *store.Store) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.ListServers())
}

func handleAddServer(w http.ResponseWriter, r *http.Request, s *store.Store) {
	// 限制请求体大小, 防止 DoS
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