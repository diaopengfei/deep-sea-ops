// 部署任务相关接口封装

import http from './server'

// 部署任务
export interface DeployTask {
  id: string
  type: string // 'scale_out' | 'migrate'
  projectPath: string
  projectName: string
  jarPath: string
  configText: string
  targetAgentId: string
  sourceAgentId: string
  status: string // pending / running / success / failed
  error: string
  createdAt: string
  updatedAt: string
}

// 创建部署任务请求
export interface CreateDeployTaskReq {
  type: string
  projectPath: string
  projectName: string
  jarPath: string
  configText: string
  targetAgentId: string
  sourceAgentId?: string
}

// listDeployTasks: 获取所有部署任务
export async function listDeployTasks(): Promise<DeployTask[]> {
  const res = await http.get<DeployTask[]>('/deploy-tasks')
  return Array.isArray(res.data) ? res.data : []
}

// createDeployTask: 创建部署任务(扩容/迁移)
export async function createDeployTask(req: CreateDeployTaskReq): Promise<DeployTask> {
  const res = await http.post<DeployTask>('/deploy-tasks', req)
  return res.data
}
