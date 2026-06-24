package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/deepsea-ops/server/internal/agentclient"
	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/configdiff"
	"github.com/deepsea-ops/server/internal/grpcserver"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/store"
)

// --- Agent 指令处理 ---

func handleReadConfig(w http.ResponseWriter, r *http.Request, gs *grpcserver.Server, agentID string) {
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
}

func handleConfigDiff(w http.ResponseWriter, r *http.Request, gs *grpcserver.Server, agentID string) {
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
}

// handleScanProjects 扫描 Agent 节点上的项目, 并把结果持久化到 Raft。
func handleScanProjects(w http.ResponseWriter, r *http.Request, gs *grpcserver.Server, s *store.Store, agentID string) {
	var scanReq struct {
		ScanDirs string `json:"scanDirs"`
	}
	// scanDirs 是可选参数, 解码失败时用空值(扫描默认目录), 不阻断请求
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&scanReq); err != nil && err != io.EOF {
		log.Printf("警告: 解析 scanDirs 请求体失败(用默认值): %v", err)
	}
	scanResult, err := gs.ScanProjects(agentID, scanReq.ScanDirs, 60*time.Second)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// 把扫描结果持久化到 Raft(projects bucket), 多节点共享视图
	// 先清除该 Agent 的旧项目记录, 再写入新的
	if err := s.ClearAgentProjects(agentID); err != nil {
		log.Printf("警告: 清除 Agent %s 旧项目记录失败: %v", agentID, err)
	}

	// 解析扫描结果 JSON, 转成 ProjectRecord 持久化
	var result struct {
		Projects []agentclient.ProjectInfo `json:"projects"`
		Hosts    string                    `json:"hosts"`
		HostsErr string                    `json:"hostsErr"`
	}
	if err := json.Unmarshal([]byte(scanResult), &result); err == nil {
		now := time.Now()
		for _, p := range result.Projects {
			rec := model.ProjectRecord{
				ID:          agentID + "|" + p.Path,
				AgentID:     agentID,
				Path:        p.Path,
				Type:        string(p.Type),
				Name:        p.Name,
				ConfigFiles: p.ConfigFiles,
				JarPath:     p.JarPath,
				JarEntry:    p.JarEntry,
				Running:     p.Running,
				PID:         p.PID,
				ScannedAt:   now,
			}
			if err := s.AddProject(rec); err != nil {
				log.Printf("警告: 持久化项目 %s 失败: %v", rec.ID, err)
			}
		}
	} else {
		log.Printf("警告: 解析扫描结果 JSON 失败, 未持久化: %v", err)
	}

	// 返回原始扫描结果(含生效配置等完整信息)
	auth.WriteJSON(w, http.StatusOK, scanResult)
}

// handleDeploy 直接对指定 Agent 下发部署指令。
func handleDeploy(w http.ResponseWriter, r *http.Request, gs *grpcserver.Server, s *store.Store, agentID string) {
	var req struct {
		JarPath     string `json:"jarPath"`
		ConfigText  string `json:"configText"`
		ProjectName string `json:"projectName"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	params := map[string]string{
		"jarPath":     req.JarPath,
		"configText":  req.ConfigText,
		"projectName": req.ProjectName,
	}
	output, err := gs.SendCommand(agentID, "DEPLOY", params, 120*time.Second)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusOK, map[string]string{"output": output})
}

// handleStopProject 停止指定 Agent 上的项目。
func handleStopProject(w http.ResponseWriter, r *http.Request, gs *grpcserver.Server, agentID string) {
	var req struct {
		ProjectPath string `json:"projectPath"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	params := map[string]string{"projectPath": req.ProjectPath}
	output, err := gs.SendCommand(agentID, "STOP_PROJECT", params, 30*time.Second)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusOK, map[string]string{"output": output})
}
