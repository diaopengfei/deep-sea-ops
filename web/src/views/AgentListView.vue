<template>
  <div class="agent-view">
    <div class="stat-row">
      <div class="stat-card">
        <div class="stat-icon online"><el-icon :size="22"><Connection /></el-icon></div>
        <div class="stat-body">
          <div class="stat-label">在线 Agent</div>
          <div class="stat-value">{{ agents.length }}</div>
        </div>
      </div>
    </div>

    <div class="panel">
      <div class="panel-toolbar">
        <span class="panel-title">Agent 节点</span>
        <el-button :icon="Refresh" @click="loadAgents">刷新</el-button>
      </div>
      <el-table :data="agents" style="width: 100%" v-loading="loading" empty-text="暂无 Agent 连接">
        <el-table-column label="状态" width="110">
          <template #default>
            <span class="status-cell">
              <i class="dot dot-online"></i> 在线
            </span>
          </template>
        </el-table-column>
        <el-table-column prop="id" label="Agent ID" min-width="140" />
        <el-table-column prop="hostname" label="主机名" min-width="160" />
        <el-table-column prop="ip" label="IP 地址" min-width="150" />
        <el-table-column label="最后心跳" min-width="180">
          <template #default="{ row }">
            {{ formatTime(row.lastSeen) }}
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { Connection, Refresh } from '@element-plus/icons-vue'
import { listAgents } from '../api/server'
import type { AgentInfo } from '../api/types'

const agents = ref<AgentInfo[]>([])
const loading = ref(false)
let timer: number | undefined

async function loadAgents() {
  loading.value = true
  try {
    agents.value = await listAgents()
  } finally {
    loading.value = false
  }
}

// 格式化时间: ISO 字符串 -> 本地可读格式
function formatTime(iso: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  return d.toLocaleString('zh-CN', { hour12: false })
}

// 每 5 秒自动刷新, 实时反映 Agent 在线状态
onMounted(() => {
  loadAgents()
  timer = window.setInterval(loadAgents, 5000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<style scoped>
.stat-row { display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px; margin-bottom: 16px; }
.stat-card { background: #fff; border: 1px solid #e4e7ed; border-radius: 8px; padding: 18px 20px; display: flex; align-items: center; gap: 14px; }
.stat-icon { width: 44px; height: 44px; border-radius: 8px; display: flex; align-items: center; justify-content: center; color: #fff; }
.stat-icon.online { background: #67c23a; }
.stat-label { font-size: 13px; color: #909399; margin-bottom: 4px; }
.stat-value { font-size: 24px; font-weight: 600; color: #303133; line-height: 1; }
.panel { background: #fff; border: 1px solid #e4e7ed; border-radius: 8px; padding: 16px 20px; }
.panel-toolbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.panel-title { font-size: 15px; font-weight: 600; color: #303133; }
.status-cell { display: inline-flex; align-items: center; gap: 6px; }
.dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; }
.dot-online { background: #67c23a; }
</style>