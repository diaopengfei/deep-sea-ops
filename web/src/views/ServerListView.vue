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
          placeholder="搜索名称或 IP"
          :prefix-icon="Search"
          clearable
          style="width: 240px"
        />
        <div class="toolbar-right">
          <el-button :icon="Refresh" @click="loadServers">刷新</el-button>
          <el-button type="primary" :icon="Plus" @click="openAddDialog">新增服务器</el-button>
        </div>
      </div>

      <el-table :data="filteredServers" style="width: 100%" v-loading="loading" empty-text="暂无服务器">
        <el-table-column label="状态" width="110">
          <template #default="{ row }">
            <span class="status-cell">
              <i :class="['dot', row.status === 'online' ? 'dot-online' : 'dot-offline']"></i>
              {{ row.status }}
            </span>
          </template>
        </el-table-column>
        <el-table-column prop="id" label="ID" width="90" />
        <el-table-column prop="name" label="名称" min-width="140" />
        <el-table-column prop="ip" label="IP 地址" min-width="150" />
        <el-table-column label="操作" width="120">
          <template #default>
            <el-button link type="primary" size="small">查看</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 新增弹窗: 表单放弹窗里, 比内联表单更符合运维工具习惯 -->
    <el-dialog v-model="dialogVisible" title="新增服务器" width="440px">
      <el-form :model="form" label-width="72px">
        <el-form-item label="ID">
          <el-input v-model="form.id" placeholder="如 s4" />
        </el-form-item>
        <el-form-item label="名称">
          <el-input v-model="form.name" placeholder="如 web-03" />
        </el-form-item>
        <el-form-item label="IP">
          <el-input v-model="form.ip" placeholder="192.168.1.x" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="onAdd">确认新增</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search, Refresh, Plus, Coin, CircleCheck, CircleClose } from '@element-plus/icons-vue'
import { listServers, addServer } from '../api/server'
import type { Server } from '../api/types'

const servers = ref<Server[]>([])
const loading = ref(false)
const submitting = ref(false)
const keyword = ref('')
const dialogVisible = ref(false)

const form = reactive({
  id: '',
  name: '',
  ip: '',
  status: 'offline'
})

// 统计: 从数据派生, 不单独存状态
const stats = computed(() => {
  const online = servers.value.filter((s) => s.status === 'online').length
  return {
    total: servers.value.length,
    online,
    offline: servers.value.length - online
  }
})

// 搜索过滤: 按名称或 IP 模糊匹配
const filteredServers = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return servers.value
  return servers.value.filter(
    (s) => s.name.toLowerCase().includes(kw) || s.ip.toLowerCase().includes(kw)
  )
})

async function loadServers() {
  loading.value = true
  try {
    const data = await listServers()
    // 防御: 后端可能返回 null(空列表), 确保 servers.value 始终是数组
    servers.value = Array.isArray(data) ? data : []
  } catch (e: any) {
    servers.value = []
    ElMessage.error('加载服务器列表失败: ' + (e.response?.data?.error || e.message))
  } finally {
    loading.value = false
  }
}

function openAddDialog() {
  form.id = ''
  form.name = ''
  form.ip = ''
  dialogVisible.value = true
}

async function onAdd() {
  if (!form.id || !form.name || !form.ip) {
    ElMessage.warning('id / 名称 / ip 不能为空')
    return
  }
  submitting.value = true
  try {
    await addServer({ ...form })
    ElMessage.success('新增成功(已过 Raft)')
    dialogVisible.value = false
    await loadServers()
  } catch (e: any) {
    ElMessage.error('新增失败: ' + (e.response?.data || e.message))
  } finally {
    submitting.value = false
  }
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