// v0.7.0: API Token 管理客户端(admin 专用)
// Token 明文只在创建时返回一次, 后续仅展示前缀用于识别。

import http from './server'

// Token 列表项(不含哈希, 前端不可见)
export interface TokenInfo {
  id: string
  name: string
  tokenPrefix: string // 明文前缀, 便于识别"哪个 token"
  role: string        // admin / operator / viewer
  createdBy: string
  createdAt: number
  lastUsedAt: number
  expiresAt: number   // 0 表示永不过期
}

// 创建 Token 请求
export interface CreateTokenReq {
  name: string
  role: string        // admin / operator / viewer
  expiresAt?: number  // unix 毫秒, 0 或不传表示永不过期
}

// 创建 Token 响应(明文 token 只此一次返回)
export interface CreateTokenResp {
  token: string       // 明文 token, 形如 dst_xxx, 创建后需立即复制保存
  info: TokenInfo
}

// 列出所有 API Token
export async function listTokens(): Promise<TokenInfo[]> {
  const res = await http.get<TokenInfo[]>('/tokens')
  return res.data
}

// 创建 API Token, 返回明文 token(只此一次)
export async function createToken(req: CreateTokenReq): Promise<CreateTokenResp> {
  const res = await http.post<CreateTokenResp>('/tokens', req)
  return res.data
}

// 删除 API Token
export async function deleteToken(id: string): Promise<void> {
  await http.delete('/tokens/' + encodeURIComponent(id))
}
