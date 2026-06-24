package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/crypto"
	"github.com/deepsea-ops/server/internal/inject"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/store"
)

// --- 自动注入 ---

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

	// raft 节点数量安全校验
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

// handleInjectFromServer 从服务器列表触发注入。
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
	// 注入依赖 systemd, 仅支持 Linux 服务器
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

	// raft 节点数量安全校验
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
