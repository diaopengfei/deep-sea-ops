<template>
  <div class="topology-view">
    <div class="panel">
      <div class="panel-toolbar">
        <span class="panel-title">集群拓扑</span>
        <div>
          <el-tag :type="cluster?.state === 'Leader' ? 'success' : 'info'" effect="dark" size="small">
            {{ cluster?.state || '加载中' }}
          </el-tag>
          <el-button @click="loadAll" :icon="Refresh" style="margin-left: 8px">刷新</el-button>
          <el-button type="primary" @click="openInject" :icon="Plus" style="margin-left: 8px">注入新节点</el-button>
        </div>
      </div>

      <!-- 集群概览 -->
      <el-row :gutter="16" style="margin-bottom: 16px">
        <el-col :span="6">
          <el-card shadow="hover" class="stat-card">
            <div class="stat-label">Raft 节点</div>
            <div class="stat-value">{{ cluster?.servers?.length || 0 }}</div>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="hover" class="stat-card">
            <div class="stat-label">在线 Agent</div>
            <div class="stat-value">{{ agents.length }}</div>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="hover" class="stat-card">
            <div class="stat-label">Leader</div>
            <div class="stat-value-sm">{{ cluster?.leader || '-' }}</div>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="hover" class="stat-card">
            <div class="stat-label">Term</div>
            <div class="stat-value">{{ cluster?.term || '-' }}</div>
          </el-card>
        </el-col>
      </el-row>

      <!-- G6 拓扑图 -->
      <div ref="containerRef" class="topology-container" v-loading="loading"></div>

      <!-- 图例 -->
      <div class="legend">
        <span class="legend-item"><span class="dot dot-leader"></span> Leader</span>
        <span class="legend-item"><span class="dot dot-follower"></span> Follower</span>
        <span class="legend-item"><span class="dot dot-agent"></span> Agent(健康)</span>
        <span class="legend-item"><span class="dot dot-warn"></span> Agent(水位告警)</span>
        <span class="legend-item"><span class="dot dot-critical"></span> Agent(告警 firing)</span>
        <span class="legend-item"><span class="dot dot-voter"></span> Voter</span>
      </div>
    </div>

    <!-- v0.6.8: 节点下钻详情对话框 -->
    <el-dialog v-model="detailVisible" :title="detailTitle" width="640px">
      <div v-if="detailNode" class="detail-content">
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="节点 ID">{{ detailNode.id }}</el-descriptions-item>
          <el-descriptions-item label="类型">{{ detailNode.kind === 'raft' ? 'Raft 节点' : 'Agent 节点' }}</el-descriptions-item>
          <el-descriptions-item v-if="detailNode.kind === 'raft'" label="角色">{{ detailNode.role }}</el-descriptions-item>
          <el-descriptions-item v-if="detailNode.kind === 'raft'" label="选举权">{{ detailNode.suffrage }}</el-descriptions-item>
          <el-descriptions-item v-if="detailNode.kind === 'raft'" label="Raft 地址">{{ detailNode.address }}</el-descriptions-item>
          <el-descriptions-item v-if="detailNode.kind === 'agent'" label="主机名">{{ detailNode.hostname }}</el-descriptions-item>
          <el-descriptions-item v-if="detailNode.kind === 'agent'" label="IP">{{ detailNode.ip }}</el-descriptions-item>
          <el-descriptions-item v-if="detailNode.kind === 'agent'" label="CPU">{{ (detailNode.cpuPercent ?? 0).toFixed(1) }}%</el-descriptions-item>
          <el-descriptions-item v-if="detailNode.kind === 'agent'" label="内存">{{ (detailNode.memPercent ?? 0).toFixed(1) }}%</el-descriptions-item>
          <el-descriptions-item v-if="detailNode.kind === 'agent'" label="版本">{{ detailNode.version || '-' }}</el-descriptions-item>
        </el-descriptions>
        <div v-if="detailNode.kind === 'agent' && detailAlerts.length > 0" class="detail-alerts">
          <div class="detail-alerts-title">当前告警 (firing)</div>
          <el-table :data="detailAlerts" size="small" empty-text="无告警">
            <el-table-column prop="rule.name" label="规则" min-width="120" />
            <el-table-column prop="rule.metric" label="指标" width="100" />
            <el-table-column label="当前值" width="100">
              <template #default="{ row }">
                <span class="alert-value">{{ row.value.toFixed(1) }}%</span>
              </template>
            </el-table-column>
            <el-table-column label="触发时间" min-width="160">
              <template #default="{ row }">
                {{ formatTime(row.firedAt) }}
              </template>
            </el-table-column>
          </el-table>
        </div>
        <div v-else-if="detailNode.kind === 'agent'" class="detail-alerts">
          <el-alert type="success" :closable="false" title="无 firing 告警, 节点健康" />
        </div>
      </div>
      <template #footer>
        <el-button @click="detailVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <!-- 节点详情表 -->
    <div class="panel" style="margin-top: 16px">
      <div class="panel-toolbar">
        <span class="panel-title">Raft 集群节点</span>
      </div>
      <el-table :data="cluster?.servers || []" style="width: 100%" empty-text="暂无集群信息">
        <el-table-column prop="id" label="节点 ID" min-width="140" />
        <el-table-column prop="address" label="Raft 地址" min-width="200" />
        <el-table-column label="角色" width="120">
          <template #default="{ row }">
            <el-tag size="small" :type="row.address === cluster?.leader ? 'success' : 'info'">
              {{ row.address === cluster?.leader ? 'Leader' : 'Follower' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="选举权" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="row.suffrage === 'Voter' ? 'primary' : 'warning'">{{ row.suffrage }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="本节点" width="80">
          <template #default="{ row }">
            <el-tag v-if="row.id === cluster?.id" size="small" type="success">本机</el-tag>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div class="panel" style="margin-top: 16px">
      <div class="panel-toolbar">
        <span class="panel-title">Agent 节点</span>
      </div>
      <el-table :data="agents" style="width: 100%" empty-text="暂无在线 Agent">
        <el-table-column prop="id" label="Agent ID" min-width="140" />
        <el-table-column prop="hostname" label="主机名" min-width="160" />
        <el-table-column prop="ip" label="IP" width="160" />
        <el-table-column label="CPU/内存" width="140">
          <template #default="{ row }">
            <span :class="agentMetricClass(row.cpuPercent)">{{ (row.cpuPercent ?? 0).toFixed(0) }}%</span>
            /
            <span :class="agentMetricClass(row.memPercent)">{{ (row.memPercent ?? 0).toFixed(0) }}%</span>
          </template>
        </el-table-column>
        <el-table-column label="版本" width="100">
          <template #default="{ row }">
            {{ row.version || '-' }}
          </template>
        </el-table-column>
        <el-table-column label="最后心跳" width="200">
          <template #default="{ row }">
            {{ formatTime(row.lastSeen) }}
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag v-if="alerts.some(a => a.agentId === row.id)" size="small" type="danger">告警</el-tag>
            <el-tag v-else size="small" type="success">在线</el-tag>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 注入新节点对话框 -->
    <el-dialog v-model="injectVisible" title="注入新节点(SSH 自动部署)" width="600px">
      <el-alert type="info" :closable="false" style="margin-bottom: 16px">
        通过 SSH 推送二进制 + systemd 配置到目标服务器, 远程拉起服务。
        Raft 节点自动 join 集群, Agent 节点自动连 Leader gRPC。需先在"SSH 凭据"页添加目标服务器的凭据。
      </el-alert>
      <el-form :model="injectForm" label-width="120px">
        <el-form-item label="角色">
          <el-radio-group v-model="injectForm.role">
            <el-radio value="raft">Raft 节点(控制面)</el-radio>
            <el-radio value="agent">Agent 节点(工作机)</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="SSH 凭据">
          <el-select v-model="injectForm.credentialId" placeholder="选择目标服务器的 SSH 凭据" style="width: 100%">
            <el-option v-for="c in credentials" :key="c.id"
              :label="(c.serverName || c.ip) + ' (' + c.ip + ':' + c.port + ')'" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="节点 ID">
          <el-input v-model="injectForm.nodeId" :placeholder="injectForm.role === 'raft' ? 'node2' : 'agent-2'" style="width: 100%" />
        </el-form-item>

        <template v-if="injectForm.role === 'raft'">
          <el-form-item label="Raft 地址">
            <el-input v-model="injectForm.raftAddr" placeholder="192.168.1.11:7000" style="width: 100%" />
          </el-form-item>
          <el-form-item label="Join 地址">
            <el-input v-model="injectForm.joinAddr" placeholder="Leader Raft 地址, 如 192.168.1.10:7000" style="width: 100%" />
          </el-form-item>
          <el-form-item v-if="raftVoterWarning">
            <el-alert type="warning" :title="raftVoterWarning" :closable="false" />
          </el-form-item>
        </template>

        <template v-if="injectForm.role === 'agent'">
          <el-form-item label="Leader gRPC">
            <el-input v-model="injectForm.leaderGrpcAddr" placeholder="192.168.1.10:9090" style="width: 100%" />
          </el-form-item>
        </template>

        <el-form-item label="二进制路径">
          <el-input v-model="injectForm.binaryPath" placeholder="留空用默认(./deepsea-server 或 ./deepsea-agent)" style="width: 100%" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="injectVisible = false">取消</el-button>
        <el-button type="primary" :loading="injecting" @click="onInject">开始注入</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Plus } from '@element-plus/icons-vue'
import { Graph } from '@antv/g6'
import { getClusterInfo, listAgents, listFiringAlerts, type ClusterInfo, type AgentInfo, type AlertEvent } from '../api/server'
import { listCredentials, type SSHCredential } from '../api/credentials'
import { injectNode, type InjectRole } from '../api/inject'

const cluster = ref<ClusterInfo | null>(null)
const agents = ref<AgentInfo[]>([])
const alerts = ref<AlertEvent[]>([])
const credentials = ref<SSHCredential[]>([])
const loading = ref(false)
const containerRef = ref<HTMLElement | null>(null)
const injectVisible = ref(false)
const injecting = ref(false)
let graph: Graph | null = null
let timer: number | undefined

// v0.6.8: 节点下钻详情
const detailVisible = ref(false)
const detailNode = ref<any>(null)
const detailTitle = computed(() => detailNode.value ? `节点详情 - ${detailNode.value.id}` : '节点详情')
const detailAlerts = computed(() => {
  if (!detailNode.value || detailNode.value.kind !== 'agent') return []
  return alerts.value.filter((a) => a.agentId === detailNode.value.id)
})

const injectForm = reactive({
  credentialId: '',
  role: 'agent' as InjectRole,
  nodeId: '',
  raftAddr: '',
  joinAddr: '',
  leaderGrpcAddr: '',
  binaryPath: '',
})

// Raft 节点数校验: 建议奇数 ≥3
const raftVoterWarning = computed(() => {
  if (injectForm.role !== 'raft' || !cluster.value) return ''
  const voterCount = cluster.value.servers.filter((s) => s.suffrage === 'Voter').length
  const newCount = voterCount + 1
  if (newCount < 3) return `当前 Voter ${voterCount} 个, 加入后 ${newCount} 个, 建议 ≥3 个`
  if (newCount % 2 === 0) return `当前 Voter ${voterCount} 个, 加入后 ${newCount} 个(偶数), 建议奇数`
  return ''
})

async function loadAll() {
  loading.value = true
  try {
    // v0.6.8: 并发拉取集群信息、Agent 列表、当前 firing 告警
    const [ci, ag, al] = await Promise.all([
      getClusterInfo(),
      listAgents(),
      listFiringAlerts().catch(() => [] as AlertEvent[]), // 告警未启用时不阻塞拓扑加载
    ])
    cluster.value = ci
    agents.value = ag
    alerts.value = al
    await nextTick()
    renderTopology()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '加载集群信息失败')
  } finally {
    loading.value = false
  }
}

// v0.6.8: 按 CPU/内存/告警状态给 Agent 节点染色
// firing 告警 → 红(#f56c6c); CPU/内存 ≥ 80% → 橙(#e6a23c); 否则绿(#67c23a)
function agentColor(ag: AgentInfo): { fill: string; stroke: string } {
  const hasAlert = alerts.value.some((a) => a.agentId === ag.id)
  if (hasAlert) return { fill: '#f56c6c', stroke: '#c45656' }
  const cpu = ag.cpuPercent ?? 0
  const mem = ag.memPercent ?? 0
  if (cpu >= 80 || mem >= 80) return { fill: '#e6a23c', stroke: '#b88230' }
  return { fill: '#67c23a', stroke: '#529b2e' }
}

// v0.6.8: 节点 tooltip 文本(HTML, G6 v5 tooltip behavior 用)
function agentTooltip(ag: AgentInfo): string {
  const hasAlert = alerts.value.some((a) => a.agentId === ag.id)
  const cpu = (ag.cpuPercent ?? 0).toFixed(1)
  const mem = (ag.memPercent ?? 0).toFixed(1)
  return [
    `<b>${ag.id}</b>`,
    `主机: ${ag.hostname || '-'}`,
    `IP: ${ag.ip || '-'}`,
    `版本: ${ag.version || '未知'}`,
    `CPU: ${cpu}% / 内存: ${mem}%`,
    hasAlert ? '<span style="color:#f56c6c">⚠ 有 firing 告警</span>' : '<span style="color:#67c23a">● 健康</span>',
  ].join('<br/>')
}

function raftTooltip(srv: any): string {
  const isLeader = srv.address === cluster.value?.leader
  return [
    `<b>${srv.id}</b>`,
    `角色: ${isLeader ? 'Leader' : 'Follower'}`,
    `选举权: ${srv.suffrage}`,
    `Raft 地址: ${srv.address}`,
  ].join('<br/>')
}

// v0.6.8: 表格中 CPU/内存数值的染色 class
// ≥90% 红, ≥80% 橙, 其余绿
function agentMetricClass(v?: number): string {
  if (v == null) return 'metric-ok'
  if (v >= 90) return 'metric-critical'
  if (v >= 80) return 'metric-warning'
  return 'metric-ok'
}

// renderTopology 用 G6 渲染拓扑图: 上层 Raft 节点, 下层 Agent 节点, 中间连线
function renderTopology() {
  if (!containerRef.value || !cluster.value) return

  // 销毁旧图
  if (graph) {
    graph.destroy()
    graph = null
  }

  const width = containerRef.value.clientWidth
  const height = 420

  const servers = cluster.value.servers || []
  const raftCount = servers.length
  const agentCount = agents.value.length

  // 构造节点: Raft 节点在上层, Agent 节点在下层
  const nodes: any[] = []
  const edges: any[] = []

  // Raft 节点(上层)
  const raftY = 80
  const raftSpacing = raftCount > 0 ? Math.min(200, (width - 120) / raftCount) : 200
  const raftStartX = (width - raftSpacing * (raftCount - 1)) / 2
  servers.forEach((srv, i) => {
    const isLeader = srv.address === cluster.value!.leader
    const isSelf = srv.id === cluster.value!.id
    nodes.push({
      id: 'raft-' + srv.id,
      data: {
        kind: 'raft',
        id: srv.id,
        label: srv.id + (isSelf ? '(本机)' : ''),
        role: isLeader ? 'Leader' : 'Follower',
        address: srv.address,
        suffrage: srv.suffrage,
        tooltip: raftTooltip(srv),
      },
      style: {
        x: raftStartX + i * raftSpacing,
        y: raftY,
        fill: isLeader ? '#faad14' : '#409eff',
        stroke: isLeader ? '#d48806' : '#337ecc',
        labelText: srv.id,
        labelFill: '#fff',
        labelFontSize: 13,
        labelFontWeight: 600,
        size: 56,
        radius: 28,
      },
    })
  })

  // Raft 节点之间互连(虚线, 表示集群内部通信)
  for (let i = 0; i < servers.length; i++) {
    for (let j = i + 1; j < servers.length; j++) {
      edges.push({
        source: 'raft-' + servers[i].id,
        target: 'raft-' + servers[j].id,
        style: {
          stroke: '#a0aec0',
          lineWidth: 1,
          lineDash: [4, 4],
        },
      })
    }
  }

  // Agent 节点(下层)
  const agentY = 320
  const agentSpacing = agentCount > 0 ? Math.min(160, (width - 120) / agentCount) : 160
  const agentStartX = (width - agentSpacing * (agentCount - 1)) / 2
  agents.value.forEach((ag, i) => {
    const nodeId = 'agent-' + ag.id
    const color = agentColor(ag)
    const hasAlert = alerts.value.some((a) => a.agentId === ag.id)
    nodes.push({
      id: nodeId,
      data: {
        kind: 'agent',
        id: ag.id,
        label: ag.id,
        hostname: ag.hostname,
        ip: ag.ip,
        cpuPercent: ag.cpuPercent,
        memPercent: ag.memPercent,
        version: ag.version,
        tooltip: agentTooltip(ag),
      },
      style: {
        x: agentStartX + i * agentSpacing,
        y: agentY,
        fill: color.fill,
        stroke: color.stroke,
        lineWidth: hasAlert ? 3 : 2, // 告警节点加粗边框突出
        labelText: ag.id,
        labelFill: '#fff',
        labelFontSize: 11,
        size: 44,
        radius: 22,
      },
    })

    // 每个 Agent 连到 Leader 节点; 告警 Agent 用红色边
    const leaderNode = servers.find((s) => s.address === cluster.value!.leader)
    if (leaderNode) {
      edges.push({
        source: 'raft-' + leaderNode.id,
        target: nodeId,
        style: {
          stroke: hasAlert ? '#f56c6c' : '#67c23a',
          lineWidth: hasAlert ? 2 : 1.5,
        },
      })
    }
  })

  // 控制面标签节点(中间)
  nodes.push({
    id: 'label-control',
    style: {
      x: width / 2,
      y: 200,
      fill: 'transparent',
      stroke: 'transparent',
      labelText: '控制面 (gRPC 长连接)',
      labelFill: '#909399',
      labelFontSize: 12,
    },
  } as any)

  graph = new Graph({
    container: containerRef.value,
    width,
    height,
    autoFit: 'view',
    data: { nodes, edges },
    node: {
      type: 'circle',
      style: {
        size: (d: any) => d.style?.size || 40,
        fill: (d: any) => d.style?.fill || '#409eff',
        stroke: (d: any) => d.style?.stroke || '#337ecc',
        lineWidth: (d: any) => d.style?.lineWidth || 2,
        labelText: (d: any) => d.style?.labelText || '',
        labelFill: '#fff',
        labelFontSize: 12,
        labelFontWeight: 600,
        labelPosition: 'center',
      },
    },
    edge: {
      type: 'line',
      style: {
        stroke: (d: any) => d.style?.stroke || '#c0c4cc',
        lineWidth: (d: any) => d.style?.lineWidth || 1,
        lineDash: (d: any) => d.style?.lineDash || [],
      },
    },
    behaviors: ['drag-canvas', 'zoom-canvas', 'drag-element'],
  })

  // v0.6.8: 节点点击下钻 - 打开详情对话框
  graph.on('node:click', (evt: any) => {
    const nodeId = evt.target?.id
    if (!nodeId || nodeId === 'label-control') return
    const node = nodes.find((n) => n.id === nodeId)
    if (node) {
      detailNode.value = node.data
      detailVisible.value = true
    }
  })

  graph.render().catch(() => {
    // G6 渲染失败时静默处理, 表格仍有数据
  })
}

function formatTime(t: string): string {
  if (!t) return '-'
  try {
    return new Date(t).toLocaleString('zh-CN')
  } catch {
    return t
  }
}

// --- 注入新节点(v0.4) ---

async function openInject() {
  // 加载凭据列表
  try {
    credentials.value = await listCredentials()
  } catch {
    ElMessage.error('加载 SSH 凭据失败, 请先到"SSH 凭据"页添加')
  }
  // 重置表单, 预填 Leader 地址
  injectForm.credentialId = ''
  injectForm.role = 'agent'
  injectForm.nodeId = ''
  injectForm.raftAddr = ''
  injectForm.binaryPath = ''
  if (cluster.value) {
    // 从 Leader Raft 地址推导 join 地址和 gRPC 地址
    const leaderRaft = cluster.value.leader || ''
    if (leaderRaft) {
      const ip = leaderRaft.split(':')[0]
      injectForm.joinAddr = leaderRaft
      injectForm.leaderGrpcAddr = ip + ':9090'
    }
  }
  injectVisible.value = true
}

async function onInject() {
  if (!injectForm.credentialId) {
    ElMessage.warning('请选择 SSH 凭据')
    return
  }
  if (!injectForm.nodeId) {
    ElMessage.warning('请填写节点 ID')
    return
  }
  if (injectForm.role === 'raft' && (!injectForm.raftAddr || !injectForm.joinAddr)) {
    ElMessage.warning('Raft 节点需要填写 Raft 地址和 Join 地址')
    return
  }
  if (injectForm.role === 'agent' && !injectForm.leaderGrpcAddr) {
    ElMessage.warning('Agent 节点需要填写 Leader gRPC 地址')
    return
  }

  injecting.value = true
  try {
    const resp = await injectNode({
      credentialId: injectForm.credentialId,
      role: injectForm.role,
      nodeId: injectForm.nodeId,
      raftAddr: injectForm.raftAddr,
      joinAddr: injectForm.joinAddr,
      leaderGrpcAddr: injectForm.leaderGrpcAddr,
      binaryPath: injectForm.binaryPath,
    })
    ElMessage.success(resp.msg || '注入任务已提交')
    injectVisible.value = false
    // 延迟刷新拓扑(注入需要时间)
    setTimeout(() => loadAll(), 5000)
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '注入失败')
  } finally {
    injecting.value = false
  }
}

onMounted(() => {
  loadAll()
  timer = window.setInterval(loadAll, 10000)
})

onUnmounted(() => {
  if (timer) window.clearInterval(timer)
  if (graph) {
    graph.destroy()
    graph = null
  }
})
</script>

<style scoped>
.topology-view { padding: 0; }
.panel { background: #fff; border: 1px solid #e4e7ed; border-radius: 8px; padding: 16px 20px; }
.panel-toolbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.panel-title { font-size: 15px; font-weight: 600; color: #303133; }
.stat-card { text-align: center; }
.stat-label { font-size: 12px; color: #909399; margin-bottom: 4px; }
.stat-value { font-size: 24px; font-weight: 700; color: #303133; }
.stat-value-sm { font-size: 14px; font-weight: 600; color: #303133; word-break: break-all; }
.topology-container { width: 100%; height: 420px; background: #fafafa; border: 1px solid #e4e7ed; border-radius: 8px; }
.legend { display: flex; gap: 20px; margin-top: 12px; justify-content: center; }
.legend-item { display: flex; align-items: center; gap: 6px; font-size: 12px; color: #606266; }
.dot { display: inline-block; width: 12px; height: 12px; border-radius: 50%; }
.dot-leader { background: #faad14; }
.dot-follower { background: #409eff; }
.dot-agent { background: #67c23a; }
.dot-warn { background: #e6a23c; }
.dot-critical { background: #f56c6c; }
.dot-voter { background: #a0aec0; }
.detail-content { padding: 0 4px; }
.detail-alerts { margin-top: 16px; }
.detail-alerts-title { font-size: 13px; font-weight: 600; color: #f56c6c; margin-bottom: 8px; }
.alert-value { color: #f56c6c; font-weight: 600; }
.metric-ok { color: #67c23a; font-weight: 600; }
.metric-warning { color: #e6a23c; font-weight: 600; }
.metric-critical { color: #f56c6c; font-weight: 700; }
</style>
