import axios from 'axios'
import type { Server } from './types'

// 获取服务器列表
export function listServers(): Promise<Server[]> {
  return axios.get('/api/servers').then((res) => res.data)
}

// 新增服务器(走后端 Raft Apply)
export function addServer(srv: Server): Promise<Server> {
  return axios.post('/api/servers', srv).then((res) => res.data)
}