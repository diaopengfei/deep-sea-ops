import http from './server'

// v0.6.9: 用户管理 API 客户端(admin 专用)

export interface User {
  id: string
  username: string
  role: string // admin / operator / viewer
  createdAt: number
}

export interface CreateUserReq {
  username: string
  password: string
  role: string
}

export interface UpdateUserReq {
  password?: string
  role?: string
}

// 列出所有用户
export async function listUsers(): Promise<User[]> {
  const res = await http.get<User[]>('/users')
  return res.data
}

// 创建用户
export async function createUser(req: CreateUserReq): Promise<User> {
  const res = await http.post<User>('/users', req)
  return res.data
}

// 修改用户(改密码/改角色)
export async function updateUser(username: string, req: UpdateUserReq): Promise<User> {
  const res = await http.put<User>('/users/' + encodeURIComponent(username), req)
  return res.data
}

// 删除用户
export async function deleteUser(username: string): Promise<void> {
  await http.delete('/users/' + encodeURIComponent(username))
}
