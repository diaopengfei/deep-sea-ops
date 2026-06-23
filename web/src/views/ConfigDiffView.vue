<template>
  <div class="config-diff">
    <el-card header="配置比对">
      <el-form :model="form" label-width="140px" label-position="right">
        <el-divider content-position="left">选择 Agent 和项目(M4 自动填充)</el-divider>
        <el-form-item label="目标 Agent">
          <el-select v-model="form.agentId" placeholder="选择在线 Agent" style="width: 300px" @change="onAgentChange">
            <el-option v-for="a in agents" :key="a.id" :label="a.hostname + ' (' + a.id + ')'" :value="a.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="选择项目">
          <el-select v-model="selectedProjectId" placeholder="选项目自动填充(需先扫描)" style="width: 500px" @change="onProjectSelect">
            <el-option
              v-for="p in projects"
              :key="p.id"
              :label="p.name + ' [' + p.type + '] ' + p.path"
              :value="p.id"
            />
          </el-select>
          <el-button text type="primary" @click="loadProjects" style="margin-left: 8px">刷新项目列表</el-button>
        </el-form-item>

        <el-divider content-position="left">Nacos 配置源(可手动修改)</el-divider>
        <el-form-item label="Nacos 地址">
          <el-input v-model="form.nacosAddr" placeholder="http://192.168.1.10:8848" style="width: 320px" />
        </el-form-item>
        <el-form-item label="Data ID">
          <el-input v-model="form.nacosDataId" placeholder="service-a.yml" style="width: 240px" />
        </el-form-item>
        <el-form-item label="Group">
          <el-input v-model="form.nacosGroup" placeholder="DEFAULT_GROUP" style="width: 240px" />
        </el-form-item>
        <el-form-item label="命名空间">
          <el-input v-model="form.nacosNamespace" placeholder="留空=public" style="width: 240px" />
        </el-form-item>
        <el-form-item label="用户名">
          <el-input v-model="form.nacosUsername" placeholder="Nacos 开启鉴权时填写" style="width: 200px" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="form.nacosPassword" type="password" show-password placeholder="Nacos 开启鉴权时填写" style="width: 200px" />
        </el-form-item>
        <el-form-item label="AccessToken">
          <el-input v-model="form.nacosAccessToken" placeholder="已有 token 可直接填, 跳过登录" style="width: 320px" />
        </el-form-item>

        <el-divider content-position="left">本地配置文件</el-divider>
        <el-form-item label="文件路径">
          <el-input v-model="form.localPath" placeholder="/opt/app/application.yml" style="width: 400px" />
        </el-form-item>

        <el-divider content-position="left">jar 包内配置</el-divider>
        <el-form-item label="jar 路径">
          <el-input v-model="form.jarPath" placeholder="/opt/app/demo.jar" style="width: 400px" />
        </el-form-item>
        <el-form-item label="jar 内 entry">
          <el-input v-model="form.jarEntry" placeholder="BOOT-INF/classes/application.yml" style="width: 400px" />
        </el-form-item>

        <el-form-item>
          <el-button type="primary" :loading="loading" @click="onCompare">
            <el-icon><Search /></el-icon> 开始比对
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card v-if="report" header="比对结果" style="margin-top: 16px">
      <el-alert v-if="report.nacosErr" type="warning" :title="'Nacos 采集失败: ' + report.nacosErr" :closable="false" style="margin-bottom: 8px" />
      <el-alert v-if="report.localErr" type="warning" :title="'本地文件采集失败: ' + report.localErr" :closable="false" style="margin-bottom: 8px" />
      <el-alert v-if="report.jarErr" type="warning" :title="'jar 内配置采集失败: ' + report.jarErr" :closable="false" style="margin-bottom: 8px" />

      <div class="diff-stats">
        <span class="stat-item stat-consistent">一致 {{ stats.consistent }}</span>
        <span class="stat-item stat-added">新增 {{ stats.added }}</span>
        <span class="stat-item stat-partial">部分 {{ stats.partial }}</span>
        <span class="stat-item stat-total">共 {{ stats.total }} 行</span>
      </div>

      <div class="diff-toolbar">
        <el-radio-group v-model="filter" size="small">
          <el-radio-button label="all">全部</el-radio-button>
          <el-radio-button label="consistent">一致</el-radio-button>
          <el-radio-button label="diff">仅差异</el-radio-button>
        </el-radio-group>
        <el-input v-model="searchKey" placeholder="过滤配置项" clearable size="small" style="width: 240px; margin-left: 12px" />
      </div>

      <div class="diff-viewer">
        <div class="diff-header">
          <span class="col-source">来源</span>
          <span class="col-line">配置行</span>
        </div>
        <div v-if="filteredLines.length === 0" class="diff-empty">无匹配的配置行</div>
        <div
          v-for="(line, idx) in filteredLines"
          :key="idx"
          :class="['diff-line', lineClass(line)]"
        >
          <span class="col-source">
            <span :class="['src-badge', line.inNacos ? 'src-on' : 'src-off']" title="Nacos">N</span>
            <span :class="['src-badge', line.inLocal ? 'src-on' : 'src-off']" title="本地">L</span>
            <span :class="['src-badge', line.inJar ? 'src-on' : 'src-off']" title="jar">J</span>
          </span>
          <span class="col-line">
            <span class="line-prefix">{{ linePrefix(line) }}</span>
            <span class="line-content">{{ line.content }}</span>
          </span>
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { listAgents } from '../api/server'
import { configDiff } from '../api/config'
import { listProjects, type ProjectRecord } from '../api/projects'
import type { AgentInfo } from '../api/types'
import type { DiffReport } from '../api/config'

const agents = ref<AgentInfo[]>([])
const projects = ref<ProjectRecord[]>([])
const loading = ref(false)
const report = ref<DiffReport | null>(null)
const filter = ref<'all' | 'consistent' | 'diff'>('all')
const searchKey = ref('')
const selectedProjectId = ref('')

const form = reactive({
  agentId: '',
  nacosAddr: '',
  nacosDataId: '',
  nacosGroup: 'DEFAULT_GROUP',
  nacosUsername: '',
  nacosPassword: '',
  nacosAccessToken: '',
  nacosNamespace: '',
  localPath: '',
  jarPath: '',
  jarEntry: 'BOOT-INF/classes/application.yml'
})

interface DiffLine {
  content: string
  inNacos: boolean
  inLocal: boolean
  inJar: boolean
  status: 'consistent' | 'added' | 'partial'
}

const diffLines = computed<DiffLine[]>(() => {
  if (!report.value) return []
  const r = report.value
  const lines: DiffLine[] = []
  for (const c of r.consistent || []) {
    lines.push({ content: c, inNacos: true, inLocal: true, inJar: true, status: 'consistent' })
  }
  for (const c of r.onlyNacos || []) {
    lines.push({ content: c, inNacos: true, inLocal: false, inJar: false, status: 'added' })
  }
  for (const c of r.onlyLocal || []) {
    lines.push({ content: c, inNacos: false, inLocal: true, inJar: false, status: 'added' })
  }
  for (const c of r.onlyJar || []) {
    lines.push({ content: c, inNacos: false, inLocal: false, inJar: true, status: 'added' })
  }
  for (const c of r.nacosLocal || []) {
    lines.push({ content: c, inNacos: true, inLocal: true, inJar: false, status: 'partial' })
  }
  for (const c of r.nacosJar || []) {
    lines.push({ content: c, inNacos: true, inLocal: false, inJar: true, status: 'partial' })
  }
  for (const c of r.localJar || []) {
    lines.push({ content: c, inNacos: false, inLocal: true, inJar: true, status: 'partial' })
  }
  lines.sort((a, b) => a.content.localeCompare(b.content))
  return lines
})

const stats = computed(() => {
  const consistent = diffLines.value.filter((l) => l.status === 'consistent').length
  const added = diffLines.value.filter((l) => l.status === 'added').length
  const partial = diffLines.value.filter((l) => l.status === 'partial').length
  return { consistent, added, partial, total: diffLines.value.length }
})

const filteredLines = computed(() => {
  let lines = diffLines.value
  if (filter.value === 'consistent') {
    lines = lines.filter((l) => l.status === 'consistent')
  } else if (filter.value === 'diff') {
    lines = lines.filter((l) => l.status !== 'consistent')
  }
  const kw = searchKey.value.trim().toLowerCase()
  if (kw) {
    lines = lines.filter((l) => l.content.toLowerCase().includes(kw))
  }
  return lines
})

function lineClass(line: DiffLine): string {
  switch (line.status) {
    case 'consistent': return 'line-consistent'
    case 'added': return 'line-added'
    case 'partial': return 'line-partial'
    default: return ''
  }
}

function linePrefix(line: DiffLine): string {
  if (line.status === 'consistent') return ' '
  if (line.status === 'added') return '+'
  return '~'
}

async function loadAgents() {
  try {
    agents.value = await listAgents()
  } catch {
    ElMessage.error('加载 Agent 列表失败')
  }
}

// M4: 选 Agent 后加载该 Agent 的持久化项目列表
async function onAgentChange() {
  selectedProjectId.value = ''
  projects.value = []
  await loadProjects()
}

async function loadProjects() {
  if (!form.agentId) return
  try {
    projects.value = await listProjects(form.agentId)
  } catch {
    ElMessage.error('加载项目列表失败, 请先扫描')
  }
}

// M4: 选项目后自动填充比对参数
function onProjectSelect(id: string) {
  const p = projects.value.find((x) => x.id === id)
  if (!p) return
  // 自动填充: 本地配置路径(取第一个配置文件)、jar 路径、jar entry
  if (p.configFiles && p.configFiles.length > 0) {
    form.localPath = p.configFiles[0]
  }
  if (p.jarPath) {
    form.jarPath = p.jarPath
  }
  if (p.jarEntry) {
    form.jarEntry = p.jarEntry
  }
  // Nacos 地址需要从配置文件提取, 这里先留空让用户手填或通过扫描结果获取
  // (扫描结果的 effectiveConfig 里已含 Nacos 地址, 但持久化的 ProjectRecord 不含)
  ElMessage.success('已自动填充: ' + p.name)
}

async function onCompare() {
  if (!form.agentId) {
    ElMessage.warning('请选择 Agent')
    return
  }
  loading.value = true
  report.value = null
  try {
    report.value = await configDiff(form.agentId, form)
    ElMessage.success('比对完成')
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '比对失败')
  } finally {
    loading.value = false
  }
}

onMounted(loadAgents)
</script>

<style scoped>
.config-diff { padding: 0; }
.diff-stats { display: flex; gap: 16px; margin-bottom: 12px; padding: 8px 12px; background: #f5f7fa; border-radius: 4px; }
.stat-item { font-size: 13px; color: #606266; }
.stat-consistent { color: #67c23a; font-weight: 600; }
.stat-added { color: #f56c6c; font-weight: 600; }
.stat-partial { color: #e6a23c; font-weight: 600; }
.stat-total { color: #909399; margin-left: auto; }
.diff-toolbar { display: flex; align-items: center; margin-bottom: 12px; }
.diff-viewer { border: 1px solid #e4e7ed; border-radius: 4px; max-height: 600px; overflow: auto; font-family: 'Consolas', 'Monaco', 'Courier New', monospace; font-size: 12px; }
.diff-header { display: flex; background: #f5f7fa; border-bottom: 1px solid #e4e7ed; padding: 6px 0; position: sticky; top: 0; z-index: 1; }
.diff-header .col-source { width: 90px; padding: 0 12px; color: #909399; font-weight: 600; }
.diff-header .col-line { flex: 1; padding: 0 12px; color: #909399; font-weight: 600; }
.diff-empty { padding: 24px; text-align: center; color: #909399; }
.diff-line { display: flex; border-bottom: 1px solid #f0f0f0; min-height: 24px; align-items: center; }
.diff-line:hover { background: #f5f7fa; }
.col-source { width: 90px; padding: 0 12px; display: flex; gap: 3px; }
.col-line { flex: 1; padding: 0 12px; white-space: pre-wrap; word-break: break-all; display: flex; align-items: center; }
.src-badge { display: inline-block; width: 16px; height: 16px; line-height: 16px; text-align: center; border-radius: 3px; font-size: 10px; font-weight: 700; }
.src-on { background: #409eff; color: #fff; }
.src-off { background: #f0f0f0; color: #c0c4cc; }
.line-prefix { display: inline-block; width: 16px; color: #909399; font-weight: 700; }
.line-consistent { background: #f0f9eb; border-left: 3px solid #67c23a; }
.line-consistent .line-prefix { color: #67c23a; }
.line-added { background: #fef0f0; border-left: 3px solid #f56c6c; }
.line-added .line-prefix { color: #f56c6c; }
.line-partial { background: #fdf6ec; border-left: 3px solid #e6a23c; }
.line-partial .line-prefix { color: #e6a23c; }
</style>
