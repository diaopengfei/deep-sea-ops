<template>
  <div class="deploy-view">
    <!-- 创建部署任务 -->
    <div class="panel">
      <div class="panel-toolbar">
        <span class="panel-title">扩容 / 迁移部署</span>
        <el-button type="primary" @click="openCreate" :icon="Plus">新建部署任务</el-button>
      </div>

      <el-table :data="tasks" style="width: 100%" v-loading="loading" empty-text="暂无部署任务">
        <el-table-column prop="id" label="任务 ID" width="280" show-overflow-tooltip />
        <el-table-column label="类型" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="row.type === 'migrate' ? 'warning' : 'success'">
              {{ row.type === 'migrate' ? '迁移' : '扩容' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="projectName" label="项目" min-width="140" />
        <el-table-column label="源 Agent" min-width="140">
          <template #default="{ row }">
            <span v-if="row.sourceAgentId">{{ row.sourceAgentId }}</span>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column prop="targetAgentId" label="目标 Agent" min-width="140" />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="statusTagType(row.status)">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="updatedAt" label="更新时间" width="180">
          <template #default="{ row }">
            {{ formatTime(row.updatedAt) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="100">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="showDetail(row)">详情</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 新建任务对话框 -->
    <el-dialog v-model="createVisible" title="新建部署任务" width="640px">
      <el-form :model="form" label-width="120px">
        <el-form-item label="部署类型">
          <el-radio-group v-model="form.type">
            <el-radio value="scale_out">扩容(新节点起一份)</el-radio>
            <el-radio value="migrate">迁移(旧节点停服 → 新节点起服)</el-radio>
          </el-radio-group>
        </el-form-item>

        <el-form-item label="源 Agent">
          <el-select v-model="form.sourceAgentId" placeholder="选择源 Agent(项目所在节点)" style="width: 100%"
            @change="onSourceAgentChange">
            <el-option v-for="a in agents" :key="a.id" :label="a.hostname + ' (' + a.id + ')'" :value="a.id" />
          </el-select>
        </el-form-item>

        <el-form-item label="选择项目">
          <el-select v-model="selectedProjectId" placeholder="选项目自动填充(需先扫描)" style="width: 100%"
            @change="onProjectSelect" :disabled="!form.sourceAgentId">
            <el-option v-for="p in sourceProjects" :key="p.id"
              :label="p.name + ' [' + p.type + '] ' + p.path" :value="p.id" />
          </el-select>
          <el-button text type="primary" @click="loadSourceProjects" style="margin-top: 4px">刷新项目列表</el-button>
        </el-form-item>

        <el-form-item label="目标 Agent">
          <el-select v-model="form.targetAgentId" placeholder="选择目标 Agent(部署到该节点)" style="width: 100%">
            <el-option v-for="a in targetAgents" :key="a.id" :label="a.hostname + ' (' + a.id + ')'" :value="a.id" />
          </el-select>
        </el-form-item>

        <el-form-item label="项目名">
          <el-input v-model="form.projectName" placeholder="项目名(用于部署目录 /opt/<name>)" style="width: 100%" />
        </el-form-item>

        <el-form-item label="jar 路径">
          <el-input v-model="form.jarPath" placeholder="/opt/app/demo.jar" style="width: 100%" />
        </el-form-item>

        <el-form-item label="配置内容">
          <el-input v-model="form.configText" type="textarea" :rows="6"
            placeholder="application.yml 内容(写入目标节点, 留空则不写)" style="width: 100%" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="onCreate">创建并执行</el-button>
      </template>
    </el-dialog>

    <!-- 任务详情对话框 -->
    <el-dialog v-model="detailVisible" title="任务详情" width="600px">
      <template v-if="currentTask">
        <el-descriptions :column="1" border>
          <el-descriptions-item label="任务 ID">{{ currentTask.id }}</el-descriptions-item>
          <el-descriptions-item label="类型">
            {{ currentTask.type === 'migrate' ? '迁移' : '扩容' }}
          </el-descriptions-item>
          <el-descriptions-item label="项目名">{{ currentTask.projectName }}</el-descriptions-item>
          <el-descriptions-item label="项目路径">{{ currentTask.projectPath || '-' }}</el-descriptions-item>
          <el-descriptions-item label="jar 路径">{{ currentTask.jarPath }}</el-descriptions-item>
          <el-descriptions-item label="源 Agent">{{ currentTask.sourceAgentId || '-' }}</el-descriptions-item>
          <el-descriptions-item label="目标 Agent">{{ currentTask.targetAgentId }}</el-descriptions-item>
          <el-descriptions-item label="状态">
            <el-tag size="small" :type="statusTagType(currentTask.status)">{{ statusLabel(currentTask.status) }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="错误信息" v-if="currentTask.error">
            <span class="err-text">{{ currentTask.error }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="创建时间">{{ formatTime(currentTask.createdAt) }}</el-descriptions-item>
          <el-descriptions-item label="更新时间">{{ formatTime(currentTask.updatedAt) }}</el-descriptions-item>
        </el-descriptions>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { listAgents } from '../api/server'
import { listProjects, type ProjectRecord } from '../api/projects'
import { listDeployTasks, createDeployTask, type DeployTask } from '../api/deploy'
import type { AgentInfo } from '../api/types'

const agents = ref<AgentInfo[]>([])
const sourceProjects = ref<ProjectRecord[]>([])
const tasks = ref<DeployTask[]>([])
const loading = ref(false)
const creating = ref(false)
const createVisible = ref(false)
const detailVisible = ref(false)
const currentTask = ref<DeployTask | null>(null)
const selectedProjectId = ref('')

const form = reactive({
  type: 'scale_out' as 'scale_out' | 'migrate',
  sourceAgentId: '',
  targetAgentId: '',
  projectName: '',
  jarPath: '',
  configText: '',
})

// 目标 Agent = 所有 Agent(排除源 Agent, 避免迁回自己)
const targetAgents = computed(() =>
  agents.value.filter((a) => a.id !== form.sourceAgentId)
)

let timer: number | undefined

async function loadAgents() {
  try {
    agents.value = await listAgents()
  } catch {
    ElMessage.error('加载 Agent 列表失败')
  }
}

async function loadTasks() {
  loading.value = true
  try {
    tasks.value = await listDeployTasks()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '加载任务失败')
  } finally {
    loading.value = false
  }
}

async function loadSourceProjects() {
  if (!form.sourceAgentId) {
    ElMessage.warning('请先选择源 Agent')
    return
  }
  try {
    sourceProjects.value = await listProjects(form.sourceAgentId)
    if (sourceProjects.value.length === 0) {
      ElMessage.info('该项目列表为空, 请先到"项目扫描"页扫描该 Agent')
    }
  } catch {
    ElMessage.error('加载项目列表失败')
  }
}

function onSourceAgentChange() {
  selectedProjectId.value = ''
  sourceProjects.value = []
  form.projectName = ''
  form.jarPath = ''
  loadSourceProjects()
}

function onProjectSelect(id: string) {
  const p = sourceProjects.value.find((x) => x.id === id)
  if (!p) return
  form.projectName = p.name
  if (p.jarPath) {
    form.jarPath = p.jarPath
  }
  ElMessage.success('已填充: ' + p.name)
}

function openCreate() {
  form.type = 'scale_out'
  form.sourceAgentId = ''
  form.targetAgentId = ''
  form.projectName = ''
  form.jarPath = ''
  form.configText = ''
  selectedProjectId.value = ''
  sourceProjects.value = []
  createVisible.value = true
}

async function onCreate() {
  if (!form.sourceAgentId) {
    ElMessage.warning('请选择源 Agent')
    return
  }
  if (!form.targetAgentId) {
    ElMessage.warning('请选择目标 Agent')
    return
  }
  if (!form.jarPath) {
    ElMessage.warning('请填写 jar 路径')
    return
  }
  creating.value = true
  try {
    const task = await createDeployTask({
      type: form.type,
      projectPath: selectedProjectId.value ? sourceProjects.value.find((p) => p.id === selectedProjectId.value)?.path || '' : '',
      projectName: form.projectName,
      jarPath: form.jarPath,
      configText: form.configText,
      targetAgentId: form.targetAgentId,
      sourceAgentId: form.sourceAgentId,
    })
    ElMessage.success('部署任务已创建: ' + task.id)
    createVisible.value = false
    await loadTasks()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '创建任务失败')
  } finally {
    creating.value = false
  }
}

function showDetail(row: DeployTask) {
  currentTask.value = row
  detailVisible.value = true
}

function statusTagType(s: string): string {
  const map: Record<string, string> = {
    pending: 'info',
    running: 'warning',
    success: 'success',
    failed: 'danger',
  }
  return map[s] || ''
}

function statusLabel(s: string): string {
  const map: Record<string, string> = {
    pending: '待执行',
    running: '执行中',
    success: '成功',
    failed: '失败',
  }
  return map[s] || s
}

function formatTime(t: string): string {
  if (!t) return '-'
  try {
    return new Date(t).toLocaleString('zh-CN')
  } catch {
    return t
  }
}

onMounted(() => {
  loadAgents()
  loadTasks()
  // 每 5 秒刷新任务列表, 实时看状态变化
  timer = window.setInterval(loadTasks, 5000)
})

onUnmounted(() => {
  if (timer) window.clearInterval(timer)
})
</script>

<style scoped>
.panel { background: #fff; border: 1px solid #e4e7ed; border-radius: 8px; padding: 16px 20px; }
.panel-toolbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.panel-title { font-size: 15px; font-weight: 600; color: #303133; }
.err-text { color: #f56c6c; word-break: break-all; }
</style>
