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
  cpuPercent?: number  // v0.6.3: 实时 CPU 使用率(心跳上报)
  memPercent?: number  // v0.6.3: 实时内存使用率(心跳上报)
}

export async function listAgents(): Promise<AgentInfo[]> {
  const res = await http.get<AgentInfo[]>('/agents')
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
  cpuPercent?: number    // v0.6.3: 实时 CPU 使用率(agent 才有)
  memPercent?: number    // v0.6.3: 实时内存使用率(agent 才有)
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
  // v0.5.3: 配置比对结果(JSON 字符串, 前端解析展示)
  configDiffJson?: string
  diffScannedAt?: number
}

export async function listProjects(agentId?: string): Promise<ProjectRecord[]> {
  const res = await http.get<ProjectRecord[]>('/projects', { params: { agentId } })
  return res.data
}

// --- v0.6.3: 资源监控指标 ---

export interface MetricsSample {
  time: string
  metrics: {
    timestamp: number
    cpu: { percent: number }
    memory: { percent: number; total: number; used: number; available: number }
    disk: { percent: number; total: number; used: number; free: number; path: string }
    net: { rxBytesPerSec: number; txBytesPerSec: number }
    load: { load1: number; load5: number; load15: number }
    os: string
  }
}

// 拉取指定 Agent 的最新指标快照
export async function getMetricsLatest(agentId: string): Promise<MetricsSample> {
  const res = await http.get<MetricsSample>(`/agents/${agentId}/metrics`)
  return res.data
}

// 拉取指定 Agent 的历史指标时序(供 ECharts 曲线)
export async function getMetricsHistory(agentId: string): Promise<MetricsSample[]> {
  const res = await http.get<MetricsSample[]>(`/agents/${agentId}/metrics/history`)
  return res.data
}

// --- v0.6.4: 操作审计日志 ---

export interface AuditLog {
  id: number
  timestamp: number       // unix 毫秒
  username: string
  role: string
  method: string
  path: string
  action: string          // login/create-server/delete-server/inject/deploy 等
  target: string
  status: number
  ip: string
  detail: string
  sensitive: boolean
}

export interface AuditLogPage {
  total: number
  items: AuditLog[]
}

export interface AuditLogParams {
  username?: string
  action?: string
  target?: string
  start?: number
  end?: number
  offset?: number
  limit?: number
}

export async function listAuditLogs(params?: AuditLogParams): Promise<AuditLogPage> {
  const res = await http.get<AuditLogPage>('/audit-logs', { params })
  return res.data
}