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
        <el-table-column label="操作" width="120">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openReadConfig(row)">读配置</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 读配置对话框 -->
    <el-dialog v-model="configDialog" title="读取配置文件" width="640px">
      <el-form label-width="100px">
        <el-form-item label="Agent">
          <el-input :model-value="currentAgentId" disabled />
        </el-form-item>
        <el-form-item label="文件路径">
          <el-input v-model="configPath" placeholder="如 /opt/app/application.yml" />
        </el-form-item>
      </el-form>
      <el-form-item v-if="configContent" label="文件内容">
        <el-input
          v-model="configContent"
          type="textarea"
          :rows="12"
          readonly
          class="config-output"
        />
      </el-form-item>
      <template #footer>
        <el-button @click="configDialog = false">关闭</el-button>
        <el-button type="primary" :loading="reading" @click="onReadConfig">读取</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Connection, Refresh } from '@element-plus/icons-vue'
import { listAgents, readAgentConfig } from '../api/server'
import type { AgentInfo } from '../api/types'

const agents = ref<AgentInfo[]>([])
const loading = ref(false)
let timer: number | undefined

const configDialog = ref(false)
const currentAgentId = ref('')
const configPath = ref('')
const configContent = ref('')
const reading = ref(false)

async function loadAgents() {
  loading.value = true
  try {
    agents.value = await listAgents()
  } finally {
    loading.value = false
  }
}

function formatTime(iso: string): string {
  if (!iso) return '-'
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

function openReadConfig(row: AgentInfo) {
  currentAgentId.value = row.id
  configPath.value = ''
  configContent.value = ''
  configDialog.value = true
}

async function onReadConfig() {
  if (!configPath.value) {
    ElMessage.warning('请输入文件路径')
    return
  }
  reading.value = true
  configContent.value = ''
  try {
    const res = await readAgentConfig(currentAgentId.value, configPath.value)
    configContent.value = res.content
    ElMessage.success('读取成功')
  } catch (e: any) {
    const msg = e.response?.data?.error || e.message
    ElMessage.error('读取失败: ' + msg)
  } finally {
    reading.value = false
  }
}

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
.config-output { font-family: 'Consolas', 'Monaco', monospace; font-size: 12px; }
</style>