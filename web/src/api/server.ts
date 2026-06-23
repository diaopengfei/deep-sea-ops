import axios from 'axios'
import { getToken, clearToken } from './auth'

// axios 实例: 所有请求自动带 JWT Authorization 头
const http = axios.create({
  baseURL: '/api',
  timeout: 15000,
})

// 请求拦截器: 注入 token
http.interceptors.request.use((config) => {
  const token = getToken()
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// 响应拦截器: 401 时清 token 并跳登录页
http.interceptors.response.use(
  (resp) => resp,
  (error) => {
    if (error.response?.status === 401) {
      clearToken()
      // 不用 router 跳转(避免循环依赖), 直接刷新让 App 守卫处理
      window.location.reload()
    }
    return Promise.reject(error)
  }
)

export default http

// --- 服务器管理 ---
import type { Server } from './types'

// 列表查询参数
export interface ListServersParams {
  keyword?: string  // 模糊检索 (匹配 name/ip/username/os/status/id)
  sort?: string     // 排序字段: id/name/ip/port/os/username/status/createdAt
  order?: 'asc' | 'desc'
}

export async function listServers(params?: ListServersParams): Promise<Server[]> {
  const res = await http.get<Server[]>('/servers', { params })
  return res.data
}

// 新增服务器请求(不含 id, 后端自动分配)
export interface AddServerRequest {
  name: string
  ip: string
  port?: number
  os?: string        // 'linux' | 'windows', 默认 'linux'
  username: string
  password: string
}

export async function addServer(s: AddServerRequest): Promise<Server> {
  const res = await http.post<Server>('/servers', s)
  return res.data
}

export async function deleteServer(id: number): Promise<void> {
  await http.delete(`/servers/${id}`)
}

export async function updateServer(id: number, s: Partial<AddServerRequest> & { status?: string }): Promise<Server> {
  const res = await http.put<Server>(`/servers/${id}`, s)
  return res.data
}

// SSH 连接测试请求
export interface TestConnectionRequest {
  ip: string
  port?: number
  username: string
  password: string
}

export interface TestConnectionResult {
  ok: boolean
  error?: string
  msg?: string
}

export async function testConnection(req: TestConnectionRequest): Promise<TestConnectionResult> {
  const res = await http.post<TestConnectionResult>('/servers/test-connection', req)
  return res.data
}

// --- Agent 管理 ---
export interface AgentInfo {
  id: string
  hostname: string
  ip: string
  lastSeen: string
}

export async function listAgents(): Promise<AgentInfo[]> {
  const res = await http.get<AgentInfo[]>('/agents')
  return res.data
}

export interface ReadConfigResult {
  agentId: string
  path: string
  content: string
  error?: string
}

export async function readAgentConfig(agentId: string, path: string): Promise<ReadConfigResult> {
  const res = await http.post<ReadConfigResult>(`/agents/${agentId}/read-config`, { path })
  return res.data
}

// --- v0.5.2: 服务器列表触发注入 ---

export interface InjectFromServerRequest {
  role: 'raft' | 'agent'
  nodeId: string
  raftAddr?: string       // raft 角色必填
  joinAddr?: string       // raft 角色必填
  leaderGrpcAddr?: string // agent 角色必填
  binaryPath?: string     // 可选, 留空用默认值
}

export async function injectFromServer(serverId: number, req: InjectFromServerRequest): Promise<{ status: string; nodeId: string; role: string; msg: string }> {
  const res = await http.post(`/servers/${serverId}/inject`, req)
  return res.data
}

// --- v0.5.2: ops 服务节点合并视图 ---

export interface OpsNode {
  type: 'raft' | 'agent'
  id: string
  address?: string
  hostname?: string
  ip?: string
  state: string          // raft: Leader/Follower/Candidate; agent: online
  suffrage?: string      // raft: Voter/Nonvoter
  lastSeen?: number      // unix 秒
  isLeader?: boolean
  isSelf?: boolean
}

export async function listOpsNodes(): Promise<OpsNode[]> {
  const res = await http.get<OpsNode[]>('/ops-nodes')
  return res.data
}

// --- 集群信息(供注入对话框预览 Voter 数量) ---

export interface ClusterInfo {
  id: string
  state: string
  leader: string
  term: string
  servers: Array<{ id: string; address: string; suffrage: string }>
}

export async function getClusterInfo(): Promise<ClusterInfo> {
  const res = await http.get<ClusterInfo>('/cluster/info')
  return res.data
}

// --- 项目记录(持久化的扫描结果) ---

export interface ProjectRecord {
  id: string
  agentId: string
  path: string
  type: string
  name: string
  configFiles: string[]
  jarPath: string
  jarEntry: string
  running: boolean
  pid: number
  scannedAt: string
}

export async function listProjects(agentId?: string): Promise<ProjectRecord[]> {
  const res = await http.get<ProjectRecord[]>('/projects', { params: { agentId } })
  return res.data
}