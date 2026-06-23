// 自动注入相关接口封装(v0.4)

import http from './server'

// 注入角色
export type InjectRole = 'raft' | 'agent'

// 注入请求
export interface InjectReq {
  credentialId: string
  role: InjectRole
  nodeId: string
  // raft 角色参数
  raftAddr?: string
  joinAddr?: string
  // agent 角色参数
  leaderGrpcAddr?: string
  // 可选: 本机二进制路径
  binaryPath?: string
}

// 注入响应
export interface InjectResp {
  status: string
  nodeId: string
  role: string
  msg: string
}

// injectNode: 提交注入任务(异步执行, 立即返回)
export async function injectNode(req: InjectReq): Promise<InjectResp> {
  const res = await http.post<InjectResp>('/inject', req)
  return res.data
}
