import axios from 'axios'

const TOKEN_KEY = 'deepsea_token'
const REFRESH_KEY = 'deepsea_refresh'
const USER_KEY = 'deepsea_user'
const ROLE_KEY = 'deepsea_role' // v0.6.9: 角色持久化

// 读取本地存储的 accessToken
export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) || ''
}

// 读取本地存储的 refreshToken
export function getRefreshToken(): string {
  return localStorage.getItem(REFRESH_KEY) || ''
}

// 保存 token 对到 localStorage
export function setToken(accessToken: string, refreshToken: string) {
  localStorage.setItem(TOKEN_KEY, accessToken)
  localStorage.setItem(REFRESH_KEY, refreshToken)
}

// 清除本地 token(登出)
export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(REFRESH_KEY)
  localStorage.removeItem(USER_KEY)
  localStorage.removeItem(ROLE_KEY)
}

// removeToken 是 clearToken 的别名, 供 App.vue 调用
export function removeToken() {
  clearToken()
}

// 获取当前登录用户名(从本地存储读)
export function getCurrentUser(): string {
  return localStorage.getItem(USER_KEY) || ''
}

// v0.6.9: 获取当前用户角色(admin / operator / viewer)
export function getCurrentRole(): string {
  return localStorage.getItem(ROLE_KEY) || 'viewer'
}

// v0.6.9: 是否为管理员
export function isAdmin(): boolean {
  return getCurrentRole() === 'admin'
}

// v0.6.9: 是否有写权限(admin / operator)
export function canWrite(): boolean {
  const r = getCurrentRole()
  return r === 'admin' || r === 'operator'
}

// v0.6.9: 拉取 /api/auth/me 恢复当前用户角色(刷新页面后 role 可能丢失时的兜底)
export async function fetchMe(): Promise<{ username: string; role: string }> {
  const res = await axios.get('/api/auth/me')
  localStorage.setItem(USER_KEY, res.data.username)
  localStorage.setItem(ROLE_KEY, res.data.role)
  return res.data
}

// 登录接口: POST /api/login
export async function login(username: string, password: string) {
  const res = await axios.post('/api/login', { username, password })
  setToken(res.data.accessToken, res.data.refreshToken)
  localStorage.setItem(USER_KEY, res.data.username)
  localStorage.setItem(ROLE_KEY, res.data.role) // v0.6.9: 持久化角色
  return res.data
}
