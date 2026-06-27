package api

import (
	"net/http"

	"github.com/hashicorp/raft"

	"github.com/deepsea-ops/server/internal/auth"
	"github.com/deepsea-ops/server/internal/grpcserver"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/store"
)

// handleListOpsNodes 返回 raft + agent 合并视图。
func handleListOpsNodes(w http.ResponseWriter, r *http.Request, s *store.Store, gs *grpcserver.Server) {
	clusterInfo := s.ClusterInfo()
	nodes := make([]model.OpsNode, 0, len(clusterInfo.Servers)+len(gs.ListAgents()))

	// raft 节点
	for _, srv := range clusterInfo.Servers {
		isLeader := clusterInfo.Leader == srv.Address
		isSelf := srv.ID == clusterInfo.ID
		// State 只对本节点有意义(本节点知道自己是 Leader/Follower/Candidate)
		// 其他节点的状态无法从 Raft API 获取, 标注为 "unknown"
		state := "unknown"
		if isSelf {
			state = clusterInfo.State
		} else if isLeader {
			// 用 raft.Leader 常量, 不硬编码字符串
			state = raft.Leader.String()
		}
		nodes = append(nodes, model.OpsNode{
			Type:     "raft",
			ID:       srv.ID,
			Address:  srv.Address,
			State:    state,
			Suffrage: srv.Suffrage,
			IsLeader: isLeader,
			IsSelf:   isSelf,
		})
	}

	// agent 节点
	for _, a := range gs.ListAgents() {
		nodes = append(nodes, model.OpsNode{
			Type:       "agent",
			ID:         a.ID,
			Hostname:   a.Hostname,
			IP:         a.IP,
			State:      "online",
			LastSeen:   a.LastSeen.Unix(),
			CPUPercent: a.CPUPercent,
			MemPercent: a.MemPercent,
			Version:    a.Version, // v0.6.6: Agent 版本号
		})
	}

	auth.WriteJSON(w, http.StatusOK, nodes)
}
