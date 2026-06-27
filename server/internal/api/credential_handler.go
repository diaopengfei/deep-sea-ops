package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/crypto"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/store"
)

// --- SSH 凭据 ---

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
		Owner:      ownerFromClaims(r), // v0.6.9: 资源归属
		CreatedAt:  time.Now().Unix(),
	}
	if err := s.AddCredential(cred); err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusCreated, cred)
}
