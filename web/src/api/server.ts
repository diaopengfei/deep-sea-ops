import axios from 'axios'
import type { Server, AgentInfo } from './types'

export function listServers(): Promise<Server[]> {
  return axios.get('/api/servers').then((res) => res.data)
}

export function addServer(srv: Server): Promise<Server> {
  return axios.post('/api/servers', srv).then((res) => res.data)
}

export function listAgents(): Promise<AgentInfo[]> {
  return axios.get('/api/agents').then((res) => res.data)
}

// 读取 Agent 上的配置文件内容
export function readAgentConfig(agentId: string, path: string): Promise<{ agentId: string; path: string; content: string }> {
  return axios.post(`/api/agents/${agentId}/read-config`, { path }).then((res) => res.data)
}