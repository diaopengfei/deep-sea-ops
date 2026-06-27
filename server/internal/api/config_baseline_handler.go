package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/grpcserver"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/store"
)

// --- v0.6.5 配置中心化与版本管理 ---

// handleGetBaseline GET /api/projects/{id}/baseline
// 返回项目记录(含当前配置基准内容、版本号、更新人、更新时间)。
func handleGetBaseline(w http.ResponseWriter, r *http.Request, s *store.Store, projectID string) {
	p, ok := s.GetProject(projectID)
	if !ok {
		auth.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "项目不存在"})
		return
	}
	auth.WriteJSON(w, http.StatusOK, p)
}

// handleSaveBaseline POST /api/projects/{id}/baseline
// 请求体: { content: string, comment?: string }
// 保存新的配置基准内容, 走 Raft 自增版本号并追加版本历史。
func handleSaveBaseline(w http.ResponseWriter, r *http.Request, s *store.Store, projectID string) {
	var req struct {
		Content string `json:"content"`
		Comment string `json:"comment"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体格式错误"})
		return
	}
	if req.Content == "" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "content 不能为空"})
		return
	}
	claims := auth.FromContext(r.Context())
	updater := ""
	if claims != nil {
		updater = claims.Username
	}
	upd := &store.ConfigBaselineUpdate{
		ProjectID: projectID,
		Content:   req.Content,
		UpdatedBy: updater,
		Comment:   req.Comment,
	}
	if err := s.SetConfigBaseline(upd); err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "保存基准失败: " + err.Error()})
		return
	}
	p, _ := s.GetProject(projectID)
	auth.WriteJSON(w, http.StatusOK, p)
}

// handleListConfigVersions GET /api/projects/{id}/config-versions
// 返回指定项目的配置基准版本历史(按版本号升序)。
func handleListConfigVersions(w http.ResponseWriter, r *http.Request, s *store.Store, projectID string) {
	versions := s.ListConfigVersions(projectID)
	if versions == nil {
		versions = []model.ConfigVersion{}
	}
	auth.WriteJSON(w, http.StatusOK, versions)
}

// handleRollbackBaseline POST /api/projects/{id}/config-versions/{ver}/rollback
// 回滚到指定版本: 取该版本内容, 作为新的基准版本保存(不自增内容, 但创建新版本号, 保留历史可追溯)。
func handleRollbackBaseline(w http.ResponseWriter, r *http.Request, s *store.Store, projectID string, version int) {
	cv, ok := s.GetConfigVersion(projectID, version)
	if !ok {
		auth.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "版本不存在"})
		return
	}
	claims := auth.FromContext(r.Context())
	updater := "system"
	if claims != nil {
		updater = claims.Username
	}
	upd := &store.ConfigBaselineUpdate{
		ProjectID: projectID,
		Content:   cv.Content,
		UpdatedBy: updater,
		Comment:   "回滚到版本 " + strconv.Itoa(version),
	}
	if err := s.SetConfigBaseline(upd); err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "回滚失败: " + err.Error()})
		return
	}
	p, _ := s.GetProject(projectID)
	auth.WriteJSON(w, http.StatusOK, p)
}

// handleDeployBaseline POST /api/projects/{id}/deploy-baseline
// 把当前基准配置下发到 Agent 本地配置文件(走 WRITE_CONFIG 指令)。
// 请求体: { path?: string } 可选指定目标文件路径, 默认用项目第一个配置文件。
func handleDeployBaseline(w http.ResponseWriter, r *http.Request, s *store.Store, gs *grpcserver.Server, projectID string) {
	p, ok := s.GetProject(projectID)
	if !ok {
		auth.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "项目不存在"})
		return
	}
	if p.ConfigBaseline == "" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "尚未建立配置基准, 请先保存"})
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	// 请求体可选
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req)
	targetPath := req.Path
	if targetPath == "" {
		if len(p.ConfigFiles) > 0 {
			targetPath = p.ConfigFiles[0]
		} else if p.Path != "" {
			// 兜底: 用项目路径
			targetPath = p.Path
		}
	}
	if targetPath == "" {
		auth.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "无法确定目标配置文件路径, 请显式传 path"})
		return
	}
	params := map[string]string{
		"path":    targetPath,
		"content": p.ConfigBaseline,
		"backup":  "1",
	}
	output, err := gs.SendCommand(p.AgentID, "WRITE_CONFIG", params, 30*time.Second)
	if err != nil {
		auth.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "下发失败: " + err.Error()})
		return
	}
	auth.WriteJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"path":   targetPath,
		"output": output,
	})
}

// parseProjectPath 解析 /api/projects/{id}/<action>... 路径, 返回 projectID 和剩余路径段。
// projectID = agentID + "|" + projectPath, projectPath 含 "/", 故无法按 "/" 简单分割。
// 约定: action 是固定关键字(baseline/config-versions/deploy-baseline), 且必须是完整路径段
// (前由 "/" 引导, 后由 "/" 或字符串结尾界定), 避免项目路径含 "baseline-server" 等子串误匹配。
func parseProjectPath(rest string) (projectID string, parts []string) {
	actions := []string{"baseline", "config-versions", "deploy-baseline"}
	for _, a := range actions {
		needle := "/" + a
		from := 0
		for {
			j := strings.Index(rest[from:], needle)
			if j < 0 {
				break
			}
			pos := from + j
			end := pos + len(needle)
			// action 段必须后接 "/" 或字符串结尾, 才是合法边界
			if end == len(rest) || rest[end] == '/' {
				projectID = rest[:pos]
				remaining := rest[pos+1:] // 跳过 action 前的 "/"
				parts = strings.Split(remaining, "/")
				return projectID, parts
			}
			from = pos + 1
		}
	}
	return rest, nil
}
