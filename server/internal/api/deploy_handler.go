package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/grpcserver"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/store"
)

// --- 部署任务 ---

// handleCreateDeployTask 创建部署任务并下发到目标 Agent 执行。
// 流程: 创建任务(Raft 持久化) → 下发 DEPLOY 指令到目标 Agent → 更新任务状态
func handleCreateDeployTask(w http.ResponseWriter, r *http.Request, s *store.Store, gs *grpcserver.Server) {
	var req struct {
		Type        string `json:"type"` // scale_out / migrate
		ProjectPath string `json:"projectPath"`
		ProjectName string `json:"projectName"`
		JarPath     string `json:"jarPath"`
		ConfigText  string `json:"configText"`
		TargetAgent string `json:"targetAgentId"`
		SourceAgent string `json:"sourceAgentId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	if req.TargetAgent == "" || req.JarPath == "" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "targetAgentId 和 jarPath 不能为空"})
		return
	}

	now := time.Now()
	task := model.DeployTask{
		ID:            uuid.NewString(),
		Type:          req.Type,
		ProjectPath:   req.ProjectPath,
		ProjectName:   req.ProjectName,
		JarPath:       req.JarPath,
		ConfigText:    req.ConfigText,
		TargetAgentID: req.TargetAgent,
		SourceAgentID: req.SourceAgent,
		Status:        model.DeployStatusPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.AddDeployTask(task); err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "创建任务失败: " + err.Error()})
		return
	}

	// 异步执行部署: 下发 DEPLOY 指令到目标 Agent
	go executeDeployTask(s, gs, task)

	auth.WriteJSON(w, http.StatusOK, task)
}

// executeDeployTask 异步执行部署任务: 下发指令到 Agent, 更新状态。
func executeDeployTask(s *store.Store, gs *grpcserver.Server, task model.DeployTask) {
	// 标记为运行中
	task.Status = model.DeployStatusRunning
	task.UpdatedAt = time.Now()
	if err := s.UpdDeployTask(task); err != nil {
		log.Printf("警告: 更新任务 %s 状态为 running 失败: %v", task.ID, err)
	}

	// 下发 DEPLOY 指令到目标 Agent
	params := map[string]string{
		"jarPath":     task.JarPath,
		"configText":  task.ConfigText,
		"projectName": task.ProjectName,
	}
	if task.Type == model.DeployTypeMigrate && task.SourceAgentID != "" {
		// 迁移: 先停源 Agent 上的项目
		stopParams := map[string]string{"projectPath": task.ProjectPath}
		if _, err := gs.SendCommand(task.SourceAgentID, "STOP_PROJECT", stopParams, 30*time.Second); err != nil {
			log.Printf("警告: 迁移任务 %s 停止源 Agent %s 项目失败: %v", task.ID, task.SourceAgentID, err)
		}
	}

	// 在目标 Agent 上执行部署
	output, err := gs.SendCommand(task.TargetAgentID, "DEPLOY", params, 120*time.Second)
	if err != nil {
		task.Status = model.DeployStatusFailed
		task.Error = err.Error()
	} else {
		task.Status = model.DeployStatusSuccess
		task.Error = ""
		log.Printf("部署任务 %s 成功: %s", task.ID, output)
	}
	task.UpdatedAt = time.Now()
	if err := s.UpdDeployTask(task); err != nil {
		log.Printf("警告: 更新任务 %s 最终状态失败: %v", task.ID, err)
	}
}
