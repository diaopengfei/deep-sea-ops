// 项目扫描相关接口封装

import http from './server'

// 扫描到的项目信息
export interface Project {
  path: string
  type: string
  name: string
  configFiles: string[]
  jarPath: string
  jarEntry: string
  running: boolean
  pid: number
  effectiveConfig: EffectiveConfig | null
}

// 生效配置(三路合并后的最终配置)
export interface EffectiveConfig {
  items: ConfigItem[]
  nacosRaw: string
  localRaw: string
  jarRaw: string
  nacosErr: string
  localErr: string
  jarErr: string
}

// 单个配置项
export interface ConfigItem {
  key: string
  value: string
  source: string // 'nacos' | 'local' | 'jar'
  overridden: boolean
}

// 扫描结果
export interface ScanResult {
  projects: Project[]
  hosts: string
  hostsErr: string
  scanErr: string
}

// 持久化的项目记录(M4, 走 Raft)
export interface ProjectRecord {
  id: string
  agentId: string
  host: string
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

// scanProjects: 触发 Agent 扫描节点上的 Java/Python 项目
export async function scanProjects(agentId: string, scanDirs: string): Promise<ScanResult> {
  const res = await http.post(`agents/${agentId}/scan-projects`, { scanDirs })
  return res.data
}

// listProjects: 获取持久化的项目记录(可选按 agentId 过滤)
export async function listProjects(agentId?: string): Promise<ProjectRecord[]> {
  const params = agentId ? { agentId } : {}
  const res = await http.get<ProjectRecord[]>('/projects', { params })
  return Array.isArray(res.data) ? res.data : []
}