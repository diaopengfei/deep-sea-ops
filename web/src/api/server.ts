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

export async function listServers(): Promise<Server[]> {
  const res = await http.get<Server[]>('/servers')
  return res.data
}

export async function addServer(s: Server): Promise<Server> {
  const res = await http.post<Server>('/servers', s)
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