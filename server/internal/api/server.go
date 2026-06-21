package api

import (
	"encoding/json"
	"net/http"

	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/store"
)

// New 构造 HTTP 路由, 把 Store 注入到 handler 里。
// 用闭包而不是全局变量传递依赖, 这是 Go 里常见的轻量依赖注入方式。
// 首字母大写 New 表示这是包的导出 API(外部可调), Go 按首字母大小写决定可见性。
func New(s *store.Store) http.Handler {
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
	return mux
}

func handleListServers(w http.ResponseWriter, s *store.Store) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.ListServers())
}

func handleAddServer(w http.ResponseWriter, r *http.Request, s *store.Store) {
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