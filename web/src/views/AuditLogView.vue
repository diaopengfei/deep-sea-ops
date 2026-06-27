<template>
  <div class="audit-view">
    <!-- 筛选区 -->
    <el-card shadow="never" class="filter-card">
      <div class="filter-row">
        <el-input v-model="filter.username" placeholder="操作人" clearable style="width: 160px" @keyup.enter="onSearch" />
        <el-select v-model="filter.action" placeholder="操作类型" clearable style="width: 180px" @change="onSearch">
          <el-option v-for="(label, key) in actionLabels" :key="key" :label="label" :value="key" />
        </el-select>
        <el-date-picker
          v-model="timeRange"
          type="datetimerange"
          range-separator="至"
          start-placeholder="开始时间"
          end-placeholder="结束时间"
          value-format="x"
          style="width: 380px"
          @change="onSearch"
        />
        <el-button type="primary" @click="onSearch">查询</el-button>
        <el-button @click="onReset">重置</el-button>
        <el-button text :icon="Refresh" @click="loadData">刷新</el-button>
      </div>
    </el-card>

    <!-- 日志表格 -->
    <el-card shadow="never" class="table-card">
      <el-table :data="logs" v-loading="loading" stripe size="small" style="width: 100%">
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ formatTime(row.timestamp) }}</template>
        </el-table-column>
        <el-table-column prop="username" label="操作人" width="110" />
        <el-table-column label="操作类型" width="130">
          <template #default="{ row }">
            <span>{{ actionLabels[row.action] || row.action }}</span>
            <el-tag v-if="row.sensitive" type="danger" size="small" effect="plain" style="margin-left: 6px">敏感</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="方法" width="80">
          <template #default="{ row }">
            <el-tag :type="methodTag(row.method)" size="small" effect="plain">{{ row.method }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="path" label="路径" min-width="200" show-overflow-tooltip />
        <el-table-column prop="target" label="目标" width="120" show-overflow-tooltip />
        <el-table-column label="状态码" width="90" align="center">
          <template #default="{ row }">
            <el-tag :type="statusTag(row.status)" size="small">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="ip" label="来源 IP" width="130" />
      </el-table>

      <div class="pager">
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :page-sizes="[20, 50, 100]"
          :total="total"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="onSearch"
          @current-change="loadData"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { listAuditLogs, type AuditLog } from '../api/server'

const loading = ref(false)
const logs = ref<AuditLog[]>([])
const total = ref(0)
const currentPage = ref(1)
const pageSize = ref(50)

const filter = ref({ username: '', action: '' })
const timeRange = ref<[string, string] | null>(null)

// 操作类型中文标签
const actionLabels: Record<string, string> = {
  'login': '登录',
  'login-failed': '登录失败',
  'create-server': '新增服务器',
  'update-server': '更新服务器',
  'delete-server': '删除服务器',
  'inject': '注入节点',
  'deploy': '部署项目',
  'stop-project': '停止项目',
  'scan': '扫描项目',
  'read-config': '读取配置',
  'config-diff': '配置比对',
  'create-credential': '新增凭据',
  'delete-credential': '删除凭据',
  'create-deploy-task': '创建部署任务',
  'cluster-join': '节点加入集群',
  'create': '新增',
  'update': '更新',
  'delete': '删除',
}

let timer: number | undefined

async function loadData() {
  loading.value = true
  try {
    const params: Record<string, unknown> = {
      offset: (currentPage.value - 1) * pageSize.value,
      limit: pageSize.value,
    }
    if (filter.value.username) params.username = filter.value.username
    if (filter.value.action) params.action = filter.value.action
    if (timeRange.value && timeRange.value.length === 2) {
      params.start = Number(timeRange.value[0])
      params.end = Number(timeRange.value[1])
    }
    const page = await listAuditLogs(params)
    logs.value = page.items || []
    total.value = page.total
  } catch (e: any) {
    ElMessage.error('加载审计日志失败: ' + (e?.message || e))
  } finally {
    loading.value = false
  }
}

function onSearch() {
  currentPage.value = 1
  loadData()
}

function onReset() {
  filter.value = { username: '', action: '' }
  timeRange.value = null
  currentPage.value = 1
  loadData()
}

function formatTime(ms: number): string {
  if (!ms) return '-'
  const d = new Date(ms)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function methodTag(m: string): '' | 'success' | 'warning' | 'danger' {
  if (m === 'GET') return 'success'
  if (m === 'POST') return 'warning'
  if (m === 'DELETE') return 'danger'
  return ''
}

function statusTag(s: number): '' | 'success' | 'warning' | 'danger' {
  if (s >= 200 && s < 300) return 'success'
  if (s >= 300 && s < 400) return ''
  if (s >= 400 && s < 500) return 'warning'
  return 'danger'
}

onMounted(() => {
  loadData()
  // 30s 自动刷新(审计日志低频, 但保持新鲜)
  timer = window.setInterval(loadData, 30000)
})

onUnmounted(() => {
  if (timer) window.clearInterval(timer)
})
</script>

<style scoped>
.audit-view { display: flex; flex-direction: column; gap: 12px; }
.filter-card :deep(.el-card__body) { padding: 14px; }
.filter-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.table-card :deep(.el-card__body) { padding: 0; }
.pager { display: flex; justify-content: flex-end; padding: 12px; }
</style>
