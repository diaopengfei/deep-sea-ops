import axios from 'axios'

const TOKEN_KEY = 'deepsea_token'
const REFRESH_KEY = 'deepsea_refresh'

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
}

// 登录接口: POST /api/login
export async function login(username: string, password: string) {
  const res = await axios.post('/api/login', { username, password })
  setToken(res.data.accessToken, res.data.refreshToken)
  return res.data
}

// 获取当前用户信息: GET /api/auth/me
export async function getMe() {
  const res = await axios.get('/api/auth/me', {
    headers: { Authorization: `Bearer ${getToken()}` }
  })
  return res.data
}
