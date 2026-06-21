// 全局共享的领域类型, 和后端 model 对应
export interface Server {
  id: string
  name: string
  ip: string
  status: string // 'online' | 'offline'
}

// 在线 Agent 信息(来自后端 gRPC 注册表)
export interface AgentInfo {
  id: string
  hostname: string
  ip: string
  lastSeen: string // ISO 时间
}