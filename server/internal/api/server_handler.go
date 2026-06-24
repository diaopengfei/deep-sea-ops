package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/crypto"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/sshclient"
	"github.com/deepsea-ops/server/internal/store"
)

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
		"ok":  true,
		"msg": "连接成功",
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
// 改用 UpdServerFields 原子部分更新, 消除读-改-写竞态。
// FSM 在同一个 Raft 日志中读取现有记录并合并非零值字段, 不再有并发覆盖问题。
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

	// 密码非空才加密(前端不回显密码, 修改时才传)
	encPassword := ""
	if req.Password != "" {
		encPassword, err = crypto.Encrypt(req.Password)
		if err != nil {
			auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "加密密码失败: " + err.Error()})
			return
		}
	}

	// 原子部分更新: FSM 在 Raft 日志中读取现有记录并合并, 无竞态
	upd := &store.ServerUpdate{
		ID:       id,
		Name:     req.Name,
		IP:       req.IP,
		Port:     req.Port,
		OS:       req.OS,
		Username: req.Username,
		Password: encPassword,
		Status:   req.Status,
	}
	if err := s.UpdServerFields(upd); err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// 返回更新后的完整记录
	updated, ok := s.GetServer(id)
	if !ok {
		auth.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "更新后服务器不存在"})
		return
	}
	auth.WriteJSON(w, http.StatusOK, updated)
}
