<template>
  <div class="server-view">
    <!-- 统计概览: 三个指标卡 -->
    <div class="stat-row">
      <div class="stat-card">
        <div class="stat-icon total"><el-icon :size="22"><Coin /></el-icon></div>
        <div class="stat-body">
          <div class="stat-label">服务器总数</div>
          <div class="stat-value">{{ stats.total }}</div>
        </div>
      </div>
      <div class="stat-card">
        <div class="stat-icon online"><el-icon :size="22"><CircleCheck /></el-icon></div>
        <div class="stat-body">
          <div class="stat-label">在线</div>
          <div class="stat-value">{{ stats.online }}</div>
        </div>
      </div>
      <div class="stat-card">
        <div class="stat-icon offline"><el-icon :size="22"><CircleClose /></el-icon></div>
        <div class="stat-body">
          <div class="stat-label">离线</div>
          <div class="stat-value">{{ stats.offline }}</div>
        </div>
      </div>
    </div>

    <!-- 数据面板: 工具栏 + 表格 -->
    <div class="panel">
      <div class="panel-toolbar">
        <el-input
          v-model="keyword"
          placeholder="搜索 ID / 名称 / IP / 用户名 / 系统 / 状态"
          :prefix-icon="Search"
          clearable
          style="width: 300px"
          @input="onSearchChange"
        />
        <div class="toolbar-right">
          <el-button :icon="Refresh" @click="loadServers">刷新</el-button>
          <el-button type="primary" :icon="Plus" @click="openAddDialog">新增服务器</el-button>
        </div>
      </div>

      <el-table :data="servers" style="width: 100%" v-loading="loading" empty-text="暂无服务器"
                @sort-change="onSortChange">
        <el-table-column prop="id" label="ID" width="80" sortable="custom" />
        <el-table-column prop="name" label="名称" min-width="140" sortable="custom" />
        <el-table-column prop="ip" label="IP 地址" min-width="140" sortable="custom" />
        <el-table-column prop="port" label="SSH 端口" width="100" sortable="custom" />
        <el-table-column prop="os" label="系统" width="100" sortable="custom">
          <template #default="{ row }">
            <el-tag size="small" :type="row.os === 'windows' ? 'info' : 'success'">
              {{ row.os === 'windows' ? 'Windows' : 'Linux' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="username" label="SSH 用户" min-width="120" sortable="custom" />
        <el-table-column prop="status" label="状态" width="100" sortable="custom">
          <template #default="{ row }">
            <span class="status-cell">
              <i :class="['dot', row.status === 'online' ? 'dot-online' : 'dot-offline']"></i>
              {{ row.status }}
            </span>
          </template>
        </el-table-column>
        <el-table-column prop="createdAt" label="创建时间" width="170" sortable="custom">
          <template #default="{ row }">
            {{ formatTime(row.createdAt) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="160" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="onTestRow(row)">测试连接</el-button>
            <el-button link type="danger" size="small" @click="onDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 新增弹窗 -->
    <el-dialog v-model="dialogVisible" title="新增服务器" width="480px">
      <el-form :model="form" label-width="90px">
        <el-form-item label="名称" required>
          <el-input v-model="form.name" placeholder="如 web-03" />
        </el-form-item>
        <el-form-item label="IP 地址" required>
          <el-input v-model="form.ip" placeholder="192.168.1.x" />
        </el-form-item>
        <el-form-item label="SSH 端口">
          <el-input-number v-model="form.port" :min="1" :max="65535" controls-position="right" style="width: 100%" />
        </el-form-item>
        <el-form-item label="系统类型">
          <el-radio-group v-model="form.os">
            <el-radio value="linux">Linux</el-radio>
            <el-radio value="windows">Windows</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="SSH 用户名" required>
          <el-input v-model="form.username" placeholder="如 root / deploy" />
        </el-form-item>
        <el-form-item label="SSH 密码" required>
          <el-input v-model="form.password" type="password" show-password placeholder="SSH 登录密码" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button :loading="testing" @click="onTestConnection">测试连接</el-button>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="onAdd">确认新增</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search, Refresh, Plus, Coin, CircleCheck, CircleClose } from '@element-plus/icons-vue'
import {
  listServers,
  addServer,
  deleteServer,
  testConnection,
  type ListServersParams,
  type AddServerRequest,
} from '../api/server'
import type { Server } from '../api/types'

const servers = ref<Server[]>([])
const loading = ref(false)
const submitting = ref(false)
const testing = ref(false)
const keyword = ref('')
const dialogVisible = ref(false)

// 排序状态
const sortParams = ref<{ sort: string; order: 'asc' | 'desc' }>({ sort: '', order: 'asc' })

const form = reactive<AddServerRequest>({
  name: '',
  ip: '',
  port: 22,
  os: 'linux',
  username: '',
  password: '',
})

// 统计: 从数据派生
const stats = computed(() => {
  const online = servers.value.filter((s) => s.status === 'online').length
  return {
    total: servers.value.length,
    online,
    offline: servers.value.length - online,
  }
})

// 搜索防抖
let searchTimer: ReturnType<typeof setTimeout> | undefined
function onSearchChange() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => loadServers(), 300)
}

// 排序变化
function onSortChange({ prop, order }: { prop: string; order: string | null }) {
  if (order) {
    sortParams.value = { sort: prop, order: order === 'ascending' ? 'asc' : 'desc' }
  } else {
    sortParams.value = { sort: '', order: 'asc' }
  }
  loadServers()
}

async function loadServers() {
  loading.value = true
  try {
    const params: ListServersParams = {}
    if (keyword.value.trim()) params.keyword = keyword.value.trim()
    if (sortParams.value.sort) {
      params.sort = sortParams.value.sort
      params.order = sortParams.value.order
    }
    const data = await listServers(params)
    servers.value = Array.isArray(data) ? data : []
  } catch (e: any) {
    servers.value = []
    ElMessage.error('加载服务器列表失败: ' + (e.response?.data?.error || e.message))
  } finally {
    loading.value = false
  }
}

function openAddDialog() {
  form.name = ''
  form.ip = ''
  form.port = 22
  form.os = 'linux'
  form.username = ''
  form.password = ''
  dialogVisible.value = true
}

// 测试连接(弹窗中的表单数据)
async function onTestConnection() {
  if (!form.ip || !form.username) {
    ElMessage.warning('IP 和用户名不能为空')
    return
  }
  testing.value = true
  try {
    const result = await testConnection({
      ip: form.ip,
      port: form.port || 22,
      username: form.username,
      password: form.password,
    })
    if (result.ok) {
      ElMessage.success(result.msg || '连接成功')
    } else {
      ElMessage.error('连接失败: ' + (result.error || '未知错误'))
    }
  } catch (e: any) {
    ElMessage.error('测试请求失败: ' + (e.response?.data?.error || e.message))
  } finally {
    testing.value = false
  }
}

// 测试已有服务器的连接(需先解密密码, 这里通过后端测试接口用已存凭据)
async function onTestRow(row: Server) {
  ElMessage.info(`正在测试 ${row.ip} 的连接... (需重新输入密码)`)
  // 已有服务器的密码已加密, 前端无法解密, 需用户重新输入密码测试
  try {
    const { value } = await ElMessageBox.prompt(
      `请输入 ${row.username}@${row.ip}:${row.port} 的 SSH 密码进行连接测试`,
      '测试 SSH 连接',
      {
        confirmButtonText: '测试',
        cancelButtonText: '取消',
        inputType: 'password',
        inputPlaceholder: 'SSH 密码',
      }
    )
    if (!value) return
    const result = await testConnection({
      ip: row.ip,
      port: row.port,
      username: row.username,
      password: value,
    })
    if (result.ok) {
      ElMessage.success(result.msg || '连接成功')
    } else {
      ElMessage.error('连接失败: ' + (result.error || '未知错误'))
    }
  } catch {
    // 用户取消
  }
}

async function onAdd() {
  if (!form.name || !form.ip || !form.username || !form.password) {
    ElMessage.warning('名称 / IP / 用户名 / 密码 不能为空')
    return
  }
  submitting.value = true
  try {
    await addServer({ ...form })
    ElMessage.success('新增成功(已过 Raft)')
    dialogVisible.value = false
    await loadServers()
  } catch (e: any) {
    ElMessage.error('新增失败: ' + (e.response?.data?.error || e.message))
  } finally {
    submitting.value = false
  }
}

async function onDelete(row: Server) {
  try {
    await ElMessageBox.confirm(`确认删除服务器 "${row.name}" (${row.ip})?`, '删除确认', {
      type: 'warning',
    })
    await deleteServer(row.id)
    ElMessage.success('删除成功')
    await loadServers()
  } catch (e: any) {
    if (e !== 'cancel') {
      ElMessage.error('删除失败: ' + (e.response?.data?.error || e.message))
    }
  }
}

function formatTime(ts: number): string {
  if (!ts) return '-'
  return new Date(ts).toLocaleString('zh-CN')
}

onMounted(loadServers)
</script>

<style scoped>
/* 统计卡片行 */
.stat-row {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 16px;
  margin-bottom: 16px;
}

.stat-card {
  background: #fff;
  border: 1px solid #e4e7ed;
  border-radius: 8px;
  padding: 18px 20px;
  display: flex;
  align-items: center;
  gap: 14px;
}

.stat-icon {
  width: 44px;
  height: 44px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
}

.stat-icon.total { background: #409eff; }
.stat-icon.online { background: #67c23a; }
.stat-icon.offline { background: #f56c6c; }

.stat-label {
  font-size: 13px;
  color: #909399;
  margin-bottom: 4px;
}

.stat-value {
  font-size: 24px;
  font-weight: 600;
  color: #303133;
  line-height: 1;
}

/* 数据面板 */
.panel {
  background: #fff;
  border: 1px solid #e4e7ed;
  border-radius: 8px;
  padding: 16px 20px;
}

.panel-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.toolbar-right {
  display: flex;
  gap: 8px;
}

/* 状态单元格: 圆点 + 文字 */
.status-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  display: inline-block;
}

.dot-online { background: #67c23a; }
.dot-offline { background: #f56c6c; }
</style>
