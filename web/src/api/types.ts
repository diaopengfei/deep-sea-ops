// 全局共享的领域类型, 和后端 model.Server 对应
export interface Server {
  id: string
  name: string
  ip: string
  status: string // 'online' | 'offline'
}