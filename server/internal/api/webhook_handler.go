package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/store"
)

// handleListWebhooks GET /api/webhooks — 列出所有 Webhook。
func handleListWebhooks(w http.ResponseWriter, r *http.Request, s *store.Store) {
	webhooks := s.ListWebhooks()
	// 剥离 Secret 字段, 避免泄露
	out := make([]map[string]interface{}, 0, len(webhooks))
	for _, wh := range webhooks {
		out = append(out, webhookToOut(wh))
	}
	auth.WriteJSON(w, http.StatusOK, out)
}

// handleCreateWebhook POST /api/webhooks — 新增 Webhook。
// 请求体: { name, url, events?, secret?, active? }
func handleCreateWebhook(w http.ResponseWriter, r *http.Request, s *store.Store) {
	var req struct {
		Name   string   `json:"name"`
		URL    string   `json:"url"`
		Events []string `json:"events"`
		Secret string   `json:"secret"`
		Active bool     `json:"active"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	if req.Name == "" || req.URL == "" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name 和 url 不能为空"})
		return
	}
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "url 必须以 http:// 或 https:// 开头"})
		return
	}
	claims := auth.FromContext(r.Context())
	createdBy := ""
	if claims != nil {
		createdBy = claims.Username
	}
	wh := model.Webhook{
		ID:        uuid.NewString(),
		Name:      req.Name,
		URL:       req.URL,
		Events:    req.Events,
		Secret:    req.Secret,
		Active:    req.Active,
		CreatedBy: createdBy,
		CreatedAt: time.Now().UnixMilli(),
	}
	if err := s.AddWebhook(wh); err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusCreated, webhookToOut(wh))
}

// handleUpdateWebhook PUT /api/webhooks/{id} — 更新 Webhook。
// 请求体: { name?, url?, events?, secret?, active? } — 非零字段更新。
func handleUpdateWebhook(w http.ResponseWriter, r *http.Request, s *store.Store, id string) {
	existing, ok := s.GetWebhook(id)
	if !ok {
		auth.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "webhook 不存在"})
		return
	}
	var req struct {
		Name   *string  `json:"name"`
		URL    *string  `json:"url"`
		Events []string `json:"events"`
		Secret *string  `json:"secret"`
		Active *bool    `json:"active"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.URL != nil {
		if !strings.HasPrefix(*req.URL, "http://") && !strings.HasPrefix(*req.URL, "https://") {
			auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "url 必须以 http:// 或 https:// 开头"})
			return
		}
		existing.URL = *req.URL
	}
	if req.Events != nil {
		existing.Events = req.Events
	}
	if req.Secret != nil {
		existing.Secret = *req.Secret
	}
	if req.Active != nil {
		existing.Active = *req.Active
	}
	if err := s.AddWebhook(*existing); // 同 ID 覆盖
	err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusOK, webhookToOut(*existing))
}

// handleDeleteWebhook DELETE /api/webhooks/{id} — 删除 Webhook。
func handleDeleteWebhook(w http.ResponseWriter, r *http.Request, s *store.Store, id string) {
	if err := s.DelWebhook(id); err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleTestWebhook POST /api/webhooks/{id}/test — 发送测试事件到 Webhook。
// 用于前端"测试推送"按钮, 验证 URL/Secret 配置是否正确。
func handleTestWebhook(w http.ResponseWriter, r *http.Request, s *store.Store, id string) {
	wh, ok := s.GetWebhook(id)
	if !ok {
		auth.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "webhook 不存在"})
		return
	}
	// 直接同步推送一个测试事件
	payload := map[string]interface{}{
		"type":      "webhook.test",
		"timestamp": time.Now().Format(time.RFC3339),
		"payload":   map[string]string{"message": "deepsea-ops webhook 测试事件"},
	}
	if err := PushWebhook(*wh, payload); err != nil {
		auth.WriteJSON(w, http.StatusOK, map[string]string{"status": "failed", "error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// webhookToOut 把 Webhook 转成输出视图(剥离 Secret)。
func webhookToOut(wh model.Webhook) map[string]interface{} {
	return map[string]interface{}{
		"id":        wh.ID,
		"name":      wh.Name,
		"url":       wh.URL,
		"events":    wh.Events,
		"active":    wh.Active,
		"createdBy": wh.CreatedBy,
		"createdAt": wh.CreatedAt,
		"hasSecret": wh.Secret != "", // 只返回是否有 Secret, 不返回明文
	}
}

// splitWebhookPath 从 /api/webhooks/{id} 或 /api/webhooks/{id}/test 路径解析 id 和 action。
// 返回 (id, action), action 为空表示根操作。
func splitWebhookPath(path string) (id, action string) {
	rest := strings.TrimPrefix(path, "/api/webhooks/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return parts[0], ""
}
