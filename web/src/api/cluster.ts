// 集群管理相关接口封装

import http from './server'

// Raft 集群节点信息
export interface ClusterServerInfo {
  id: string
  address: string
  suffrage: string // 'Voter' | 'Nonvoter'
}

// 集群状态
export interface ClusterInfo {
  id: string // 当前节点 ID
  state: string // 'Leader' | 'Follower' | 'Candidate'
  leader: string // Leader 地址
  term: string
  servers: ClusterServerInfo[]
}

// getClusterInfo: 获取 Raft 集群状态
export async function getClusterInfo(): Promise<ClusterInfo> {
  const res = await http.get<ClusterInfo>('/cluster/info')
  return res.data
}

// joinCluster: 把一个节点加入集群(Leader 调用)
export async function joinCluster(nodeId: string, addr: string): Promise<{ id: string; status: string }> {
  const res = await http.post<{ id: string; status: string }>('/cluster/join', { nodeId, addr })
  return res.data
}
