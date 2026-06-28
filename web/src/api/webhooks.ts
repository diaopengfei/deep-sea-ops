// v0.7.0: Webhook 订阅管理客户端
// Webhook 用于接收部署完成/扫描发现/告警触发等事件推送, 适合外部系统集成。

import http from './server'

// Webhook 列表项(Secret 不返回明文, 仅 hasSecret 标记)
export interface Webhook {
  id: string
  name: string
  url: string
  events: string[]     // 订阅的事件类型, 空数组表示订阅全部
  active: boolean
  createdBy: string
  createdAt: number
  hasSecret: boolean   // 是否设置了签名密钥
}

// 创建 Webhook 请求
export interface CreateWebhookReq {
  name: string
  url: string
  events?: string[]
  secret?: string
  active?: boolean
}

// 更新 Webhook 请求(所有字段可选, 非零值更新)
export interface UpdateWebhookReq {
  name?: string
  url?: string
  events?: string[]
  secret?: string
  active?: boolean
}

// 可订阅的事件类型(供前端选择展示)
export const WEBHOOK_EVENTS: { value: string; label: string }[] = [
  { value: 'deploy.completed', label: '部署完成' },
  { value: 'deploy.failed', label: '部署失败' },
  { value: 'scan.new_project', label: '发现新项目' },
  { value: 'node.offline', label: '节点离线' },
  { value: 'alert.firing', label: '告警触发' },
  { value: 'alert.resolved', label: '告警恢复' },
]

// 列出所有 Webhook
export async function listWebhooks(): Promise<Webhook[]> {
  const res = await http.get<Webhook[]>('/webhooks')
  return res.data
}

// 创建 Webhook
export async function createWebhook(req: CreateWebhookReq): Promise<Webhook> {
  const res = await http.post<Webhook>('/webhooks', req)
  return res.data
}

// 更新 Webhook(非零字段更新)
export async function updateWebhook(id: string, req: UpdateWebhookReq): Promise<Webhook> {
  const res = await http.put<Webhook>('/webhooks/' + encodeURIComponent(id), req)
  return res.data
}

// 删除 Webhook
export async function deleteWebhook(id: string): Promise<void> {
  await http.delete('/webhooks/' + encodeURIComponent(id))
}

// 测试推送(发送一个测试事件到 Webhook URL, 验证配置是否正确)
export async function testWebhook(id: string): Promise<{ status: string; error?: string }> {
  const res = await http.post<{ status: string; error?: string }>('/webhooks/' + encodeURIComponent(id) + '/test')
  return res.data
}
