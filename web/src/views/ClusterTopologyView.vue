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
        <span class="legend-item"><span class="dot dot-agent"></span> Agent(在线)</span>
        <span class="legend-item"><span class="dot dot-voter"></span> Voter</span>
      </div>
    </div>

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
        <el-table-column label="最后心跳" width="200">
          <template #default="{ row }">
            {{ formatTime(row.lastSeen) }}
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default>
            <el-tag size="small" type="success">在线</el-tag>
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
import { getClusterInfo, type ClusterInfo } from '../api/cluster'
import { listAgents } from '../api/server'
import { listCredentials, type SSHCredential } from '../api/credentials'
import { injectNode, type InjectRole } from '../api/inject'
import type { AgentInfo } from '../api/types'

const cluster = ref<ClusterInfo | null>(null)
const agents = ref<AgentInfo[]>([])
const credentials = ref<SSHCredential[]>([])
const loading = ref(false)
const containerRef = ref<HTMLElement | null>(null)
const injectVisible = ref(false)
const injecting = ref(false)
let graph: Graph | null = null
let timer: number | undefined

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
    const [ci, ag] = await Promise.all([getClusterInfo(), listAgents()])
    cluster.value = ci
    agents.value = ag
    await nextTick()
    renderTopology()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '加载集群信息失败')
  } finally {
    loading.value = false
  }
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
        label: srv.id + (isSelf ? '(本机)' : ''),
        role: isLeader ? 'Leader' : 'Follower',
        address: srv.address,
        suffrage: srv.suffrage,
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
    nodes.push({
      id: nodeId,
      data: { kind: 'agent', label: ag.id, hostname: ag.hostname, ip: ag.ip },
      style: {
        x: agentStartX + i * agentSpacing,
        y: agentY,
        fill: '#67c23a',
        stroke: '#529b2e',
        labelText: ag.id,
        labelFill: '#fff',
        labelFontSize: 11,
        size: 44,
        radius: 22,
      },
    })

    // 每个 Agent 连到 Leader 节点
    const leaderNode = servers.find((s) => s.address === cluster.value!.leader)
    if (leaderNode) {
      edges.push({
        source: 'raft-' + leaderNode.id,
        target: nodeId,
        style: {
          stroke: '#67c23a',
          lineWidth: 1.5,
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
        lineWidth: 2,
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
.dot-voter { background: #a0aec0; }
</style>
