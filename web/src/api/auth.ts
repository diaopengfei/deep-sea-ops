import axios from 'axios'

const TOKEN_KEY = 'deepsea_token'
const REFRESH_KEY = 'deepsea_refresh'
const USER_KEY = 'deepsea_user'

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
}

// removeToken 是 clearToken 的别名, 供 App.vue 调用
export function removeToken() {
  clearToken()
}

// 获取当前登录用户名(从本地存储读)
export function getCurrentUser(): string {
  return localStorage.getItem(USER_KEY) || ''
}

// 登录接口: POST /api/login
export async function login(username: string, password: string) {
  const res = await axios.post('/api/login', { username, password })
  setToken(res.data.accessToken, res.data.refreshToken)
  localStorage.setItem(USER_KEY, res.data.username)
  return res.data
}