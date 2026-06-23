// 全局共享的领域类型, 和后端 model 对应
export interface Server {
  id: number                  // 自增数字 ID
  name: string                // 服务器名称
  ip: string                  // 服务器 IP
  port: number                // SSH 端口, 默认 22
  os: string                  // 操作系统: 'linux' | 'windows'
  username: string            // SSH 用户名
  password?: string           // AES-GCM 加密密文(前端不回显, 仅写入时传明文)
  status: string              // 'online' | 'offline'
  createdAt: number           // 创建时间(unix 毫秒)
}

// 在线 Agent 信息(来自后端 gRPC 注册表)
export interface AgentInfo {
  id: string
  hostname: string
  ip: string
  lastSeen: string // ISO 时间
}
