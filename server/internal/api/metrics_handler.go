package api

import (
	"net/http"
	"strings"

	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/metrics"
)

// handleMetricsLatest 返回指定 Agent 的最新指标快照。
// 路由: GET /api/agents/{id}/metrics
func handleMetricsLatest(w http.ResponseWriter, r *http.Request, ms *metrics.Store, agentID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	latest := ms.Latest(agentID)
	if latest == nil {
		auth.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "该 Agent 暂无指标数据"})
		return
	}
	auth.WriteJSON(w, http.StatusOK, latest)
}

// handleMetricsHistory 返回指定 Agent 的历史指标时序(供 ECharts 曲线)。
// 路由: GET /api/agents/{id}/metrics/history
func handleMetricsHistory(w http.ResponseWriter, r *http.Request, ms *metrics.Store, agentID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	history := ms.History(agentID)
	if history == nil {
		history = []metrics.Sample{}
	}
	auth.WriteJSON(w, http.StatusOK, history)
}

// extractAgentIDFromMetricsPath 从 /api/agents/{id}/metrics[...] 提取 agentID。
// 返回空串表示路径不匹配。
func extractAgentIDFromMetricsPath(path string) (agentID, suffix string) {
	// path 形如 /api/agents/{id}/metrics 或 /api/agents/{id}/metrics/history
	rest := strings.TrimPrefix(path, "/api/agents/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
