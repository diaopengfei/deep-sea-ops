// 配置比对相关接口封装

import request from './server'

// 配置比对请求参数
export interface ConfigDiffReq {
  nacosAddr?: string
  nacosDataId?: string
  nacosGroup?: string
  nacosNamespace?: string
  nacosUsername?: string
  nacosPassword?: string
  nacosAccessToken?: string
  localPath?: string
  jarPath?: string
  jarEntry?: string
}

// 配置比对结果
export interface DiffReport {
  nacosErr?: string
  localErr?: string
  jarErr?: string
  consistent: string[]
  onlyNacos: string[]
  onlyLocal: string[]
  onlyJar: string[]
  nacosLocal: string[]
  nacosJar: string[]
  localJar: string[]
}

// configDiff: 对指定 Agent 上的 Java 配置做三路比对
export async function configDiff(agentId: string, params: ConfigDiffReq): Promise<DiffReport> {
  const res = await request.post(`/api/agents/${agentId}/config-diff`, params)
  return res.data
}
