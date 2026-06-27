package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/grpcserver"
	"github.com/deepsea-ops/server/internal/version"
)

// --- v0.6.6 Agent 热更新与版本管理 ---

// handleServerVersion GET /api/version
// 返回控制面版本号, 前端据此判断 Agent 版本兼容性(低于控制面需升级)。
func handleServerVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	auth.WriteJSON(w, http.StatusOK, map[string]string{"server": version.Version})
}

// handleGetAgentVersion GET /api/agents/{id}/version
// 返回缓存的 Agent 版本号; 缓存为空时主动下发 GET_VERSION 查询。
func handleGetAgentVersion(w http.ResponseWriter, r *http.Request, gs *grpcserver.Server, agentID string) {
	ver := gs.GetCachedVersion(agentID)
	if ver == "" {
		// 缓存为空, 主动查询一次(可能是旧版本 Agent 不支持, 返回空)
		v, err := gs.GetAgentVersion(agentID, 8*time.Second)
		if err != nil {
			auth.WriteJSON(w, http.StatusOK, map[string]string{"version": "", "error": err.Error()})
			return
		}
		ver = v
	}
	auth.WriteJSON(w, http.StatusOK, map[string]string{"version": ver})
}

// handleUpgradeAgent POST /api/agents/{id}/upgrade
// 请求体: { url: string, checksum?: string }
// 单 Agent 升级: 下发 UPGRADE 指令, Agent 下载替换自身后退出由服务管理器重启。
func handleUpgradeAgent(w http.ResponseWriter, r *http.Request, gs *grpcserver.Server, agentID string) {
	var req struct {
		URL      string `json:"url"`
		Checksum string `json:"checksum"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	if req.URL == "" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "url 不能为空"})
		return
	}
	// 升级指令超时放宽到 5 分钟(下载大文件可能慢)
	output, err := gs.UpgradeAgent(agentID, req.URL, req.Checksum, 5*time.Minute)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "升级失败: " + err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "agentId": agentID, "output": output})
}

// handleBatchUpgradeAgents POST /api/agents/upgrade
// 请求体: { agentIds: string[], url: string, checksum?: string, waitSeconds?: number }
// 滚动升级: 逐个升级, 每个 Agent 升级后等待 waitSeconds 再升级下一个,
// 让服务管理器有时间重启并重新注册。返回每个 Agent 的升级结果。
func handleBatchUpgradeAgents(w http.ResponseWriter, r *http.Request, gs *grpcserver.Server) {
	var req struct {
		AgentIDs    []string `json:"agentIds"`
		URL         string   `json:"url"`
		Checksum    string   `json:"checksum"`
		WaitSeconds int      `json:"waitSeconds"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	if req.URL == "" || len(req.AgentIDs) == 0 {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agentIds 和 url 不能为空"})
		return
	}
	if req.WaitSeconds <= 0 {
		req.WaitSeconds = 10 // 默认每台间隔 10 秒, 等待重启注册
	}

	// 异步执行滚动升级, 立即返回"已开始"
	go func(agentIDs []string, url, checksum string, wait int) {
		for _, id := range agentIDs {
			output, err := gs.UpgradeAgent(id, url, checksum, 5*time.Minute)
			if err != nil {
				log.Printf("滚动升级: Agent %s 失败: %v", id, err)
			} else {
				log.Printf("滚动升级: Agent %s 成功: %s", id, output)
			}
			// 等待该 Agent 重启并重新注册后再升级下一个
			time.Sleep(time.Duration(wait) * time.Second)
		}
		log.Printf("滚动升级批次完成, 共 %d 个 Agent", len(agentIDs))
	}(req.AgentIDs, req.URL, req.Checksum, req.WaitSeconds)

	auth.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "started",
		"agentCount":  len(req.AgentIDs),
		"waitSeconds": req.WaitSeconds,
	})
}
