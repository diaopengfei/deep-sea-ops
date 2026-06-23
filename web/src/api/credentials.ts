// SSH 凭据相关接口封装(v0.4)

import http from './server'

// SSH 凭据(密码/私钥已加密, 前端不存明文)
export interface SSHCredential {
  id: string
  serverName: string
  ip: string
  port: number
  username: string
  authType: string // 'password' | 'key'
  createdAt: number
}

// 添加凭据请求(明文, 后端加密后存储)
export interface AddCredentialReq {
  id?: string
  serverName: string
  ip: string
  port?: number
  username: string
  password?: string
  privateKey?: string
  authType: string
}

// listCredentials: 列出所有 SSH 凭据
export async function listCredentials(): Promise<SSHCredential[]> {
  const res = await http.get<SSHCredential[]>('/credentials')
  return Array.isArray(res.data) ? res.data : []
}

// addCredential: 添加 SSH 凭据(后端 AES-GCM 加密存储)
export async function addCredential(req: AddCredentialReq): Promise<SSHCredential> {
  const res = await http.post<SSHCredential>('/credentials', req)
  return res.data
}

// delCredential: 删除 SSH 凭据
export async function delCredential(id: string): Promise<void> {
  await http.delete(`credentials/${id}`)
}
