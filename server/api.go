package main

import (
	"encoding/json"
	"net/http"
)

// newAPI 构造 HTTP 路由, 把 Store 注入到 handler 里。
// 用闭包而不是全局变量传递依赖, 这是 Go 里常见的轻量依赖注入方式。
func newAPI(s *Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/servers", func(w http.ResponseWriter, r *http.Request) {
		// 根据请求方法分发: GET 列表, POST 新增
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

func handleListServers(w http.ResponseWriter, s *Store) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.ListServers())
}

// handleAddServer 解析请求体里的 Server JSON, 调 Store.AddServer 走 Raft 写入。
func handleAddServer(w http.ResponseWriter, r *http.Request, s *Store) {
	var srv Server
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

	// 这里调 Store.AddServer, 内部会走 raft.Apply -> FSM.Apply
	if err := s.AddServer(srv); err != nil {
		http.Error(w, "写入失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(srv)
}