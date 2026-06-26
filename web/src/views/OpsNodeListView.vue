<template>
  <div class="ops-node-view">
    <!-- 顶部统计卡片 -->
    <div class="stat-row">
      <div class="stat-card">
        <div class="stat-icon raft"><el-icon :size="22"><Share /></el-icon></div>
        <div class="stat-body">
          <div class="stat-label">Raft 节点 (Voter)</div>
          <div class="stat-value">{{ raftVoterCount }}</div>
        </div>
      </div>
      <div class="stat-card">
        <div class="stat-icon agent"><el-icon :size="22"><Connection /></el-icon></div>
        <div class="stat-body">
          <div class="stat-label">Agent 节点</div>
          <div class="stat-value">{{ agentCount }}</div>
        </div>
      </div>
      <div class="stat-card">
        <div class="stat-icon leader"><el-icon :size="22"><Monitor /></el-icon></div>
        <div class="stat-body">
          <div class="stat-label">Leader 节点 ID</div>
          <div class="stat-value stat-value-text">{{ leaderId || '-' }}</div>
        </div>
      </div>
      <div class="stat-card">
        <div class="stat-icon project"><el-icon :size="22"><FolderOpened /></el-icon></div>
        <div class="stat-body">
          <div class="stat-label">在线项目数</div>
          <div class="stat-value">{{ projectCount }}</div>
        </div>
      </div>
    </div>

    <!-- 节点列表(合并 raft + agent) -->
    <div class="panel">
      <div class="panel-toolbar">
        <span class="panel-title">ops 服务节点</span>
        <el-button :icon="Refresh" @click="loadOpsNodes">刷新</el-button>
      </div>
      <el-table :data="opsNodes" style="width: 100%" v-loading="loading" empty-text="暂无节点">
        <el-table-column label="类型" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="row.type === 'raft' ? 'primary' : 'success'">
              {{ row.type }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="id" label="节点 ID" min-width="160" />
        <el-table-column label="地址 / IP" min-width="180">
          <template #default="{ row }">
            <span v-if="row.type === 'raft'">{{ row.address || '-' }}</span>
            <span v-else>
              {{ row.ip || '-' }}
              <span v-if="row.hostname" class="sub-text">({{ row.hostname }})</span>
            </span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="130">
          <template #default="{ row }">
            <el-tag size="small" :type="statusTagType(row)">{{ row.state }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="选举权" width="110">
          <template #default="{ row }">
            <span v-if="row.type === 'raft'">{{ row.suffrage || '-' }}</span>
            <span v-else class="sub-text">-</span>
          </template>
        </el-table-column>
        <el-table-column label="最后心跳" min-width="180">
          <template #default="{ row }">
            {{ formatLastSeen(row.lastSeen) }}
          </template>
        </el-table-column>
        <el-table-column label="CPU/内存" width="160">
          <template #default="{ row }">
            <span v-if="row.type === 'agent'" class="metric-inline">
              <span :class="metricLevel(row.cpuPercent)">{{ (row.cpuPercent ?? 0).toFixed(0) }}%</span>
              /
              <span :class="metricLevel(row.memPercent)">{{ (row.memPercent ?? 0).toFixed(0) }}%</span>
            </span>
            <span v-else class="sub-text">-</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <el-button v-if="row.type === 'agent'" link type="primary" size="small" @click="openProjectDrawer(row)">查看项目</el-button>
            <el-button v-if="row.type === 'agent'" link type="success" size="small" @click="openMetricsDialog(row)">监控</el-button>
            <el-button v-else link type="info" size="small" @click="viewRaftDetail(row)">查看详情</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 项目列表抽屉 -->
    <el-drawer v-model="drawerVisible" :title="`Agent ${drawerAgentId} 的项目`" size="60%">
      <div class="drawer-tip">
        <el-icon><InfoFilled /></el-icon>
        项目扫描和配置比对由后台自动执行, 每 10 分钟更新一次
      </div>
      <el-table :data="drawerProjects" style="width: 100%" v-loading="drawerLoading" empty-text="该 Agent 暂无项目记录">
        <el-table-column label="运行状态" width="100">
          <template #default="{ row }">
            <span class="status-cell">
              <i :class="['dot', row.running ? 'dot-online' : 'dot-offline']"></i>
              {{ row.running ? '运行中' : '已停止' }}
            </span>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="项目名" min-width="150" />
        <el-table-column label="类型" width="100">
          <template #default="{ row }">
            <el-tag size="small" type="info">{{ row.type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="path" label="路径" min-width="200" show-overflow-tooltip />
        <el-table-column label="配置文件数" width="110">
          <template #default="{ row }">
            {{ row.configFiles ? row.configFiles.length : 0 }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="110" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openConfigDialog(row)">查看配置</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-drawer>

    <!-- 配置比对结果对话框 -->
    <el-dialog v-model="configDialogVisible" title="配置比对结果" width="780px">
      <div v-if="configProject" class="config-meta">
        <span>项目: <b>{{ configProject.name }}</b></span>
        <span class="sub-text">{{ configProject.path }}</span>
        <span v-if="configProject.diffScannedAt" class="sub-text">
          比对时间: {{ new Date(configProject.diffScannedAt).toLocaleString('zh-CN', { hour12: false }) }}
        </span>
      </div>
      <!-- 采集错误提示 -->
      <el-alert
        v-for="err in configErrors"
        :key="err.source"
        :title="err.source + ' 采集失败: ' + err.msg"
        type="error"
        :closable="false"
        style="margin-bottom: 8px"
      />
      <!-- 差异展示: v0.6.2 起默认展示键值级语义差异, 可切换行级差异 -->
      <el-tabs v-model="configActiveTab">
        <el-tab-pane label="键值差异" name="semantic">
          <el-table
            :data="configSemantic"
            style="width: 100%"
            empty-text="等待后台自动比对(每 10 分钟)或无键值数据"
            :row-class-name="semanticRowClass"
            size="small"
          >
            <el-table-column prop="key" label="配置项 Key" min-width="180" show-overflow-tooltip />
            <el-table-column label="Nacos" min-width="160">
              <template #default="{ row }">
                <span :class="{ 'val-missing': !row.nacos }">{{ row.nacos || '-' }}</span>
              </template>
            </el-table-column>
            <el-table-column label="本地" min-width="160">
              <template #default="{ row }">
                <span :class="{ 'val-missing': !row.local }">{{ row.local || '-' }}</span>
              </template>
            </el-table-column>
            <el-table-column label="Jar" min-width="160">
              <template #default="{ row }">
                <span :class="{ 'val-missing': !row.jar }">{{ row.jar || '-' }}</span>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="100" fixed="right">
              <template #default="{ row }">
                <el-tag size="small" :type="row.consistent ? 'success' : 'warning'">
                  {{ row.consistent ? '一致' : '不一致' }}
                </el-tag>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="行级差异" name="lines">
          <el-table :data="configItems" style="width: 100%" empty-text="无行级差异或等待后台自动比对">
            <el-table-column prop="category" label="差异类别" width="220" />
            <el-table-column label="配置行" min-width="500">
              <template #default="{ row }">
                <div v-for="(line, i) in row.lines" :key="i" class="config-line">{{ line }}</div>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
      <div v-if="configProject && configProject.configFiles && configProject.configFiles.length" class="config-files">
        <div class="config-files-title">涉及的配置文件:</div>
        <el-tag v-for="f in configProject.configFiles" :key="f" size="small" class="config-file-tag">{{ f }}</el-tag>
      </div>
      <template #footer>
        <el-button @click="configDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <!-- v0.6.3: 资源监控曲线对话框 -->
    <MetricsDialog v-model="metricsDialogVisible" :agent-id="metricsAgentId" />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Share, Connection, Monitor, FolderOpened, Refresh, InfoFilled } from '@element-plus/icons-vue'
import { listOpsNodes, listProjects, type OpsNode, type ProjectRecord } from '../api/server'
import MetricsDialog from './MetricsDialog.vue'

const opsNodes = ref<OpsNode[]>([])
const loading = ref(false)
let timer: number | undefined

// 统计卡片
const projectCount = ref(0)
const raftVoterCount = computed(() =>
  opsNodes.value.filter((n) => n.type === 'raft' && n.suffrage === 'Voter').length
)
const agentCount = computed(() => opsNodes.value.filter((n) => n.type === 'agent').length)
const leaderId = computed(() => opsNodes.value.find((n) => n.isLeader)?.id || '')

// 状态标签颜色: Leader 黄 / Follower 蓝 / Candidate 灰 / Agent 在线绿
function statusTagType(row: OpsNode): 'warning' | 'primary' | 'info' | 'success' {
  if (row.type === 'agent') return 'success'
  if (row.state === 'Leader') return 'warning'
  if (row.state === 'Candidate') return 'info'
  return 'primary'
}

// 最后心跳格式化 (lastSeen 为 unix 秒)
function formatLastSeen(ts?: number): string {
  if (!ts) return '-'
  return new Date(ts * 1000).toLocaleString('zh-CN', { hour12: false })
}

async function loadOpsNodes() {
  loading.value = true
  try {
    const data = await listOpsNodes()
    opsNodes.value = Array.isArray(data) ? data : []
  } catch (e: any) {
    opsNodes.value = []
    ElMessage.error('加载节点列表失败: ' + (e.response?.data?.error || e.message))
  } finally {
    loading.value = false
  }
}

// 加载在线项目总数(全量, 不按 agent 过滤)
async function loadProjectCount() {
  try {
    const list = await listProjects()
    projectCount.value = Array.isArray(list) ? list.length : 0
  } catch {
    projectCount.value = 0
  }
}

// --- 项目抽屉 ---
const drawerVisible = ref(false)
const drawerAgentId = ref('')
const drawerProjects = ref<ProjectRecord[]>([])
const drawerLoading = ref(false)

async function openProjectDrawer(row: OpsNode) {
  drawerAgentId.value = row.id
  drawerProjects.value = []
  drawerVisible.value = true
  drawerLoading.value = true
  try {
    const list = await listProjects(row.id)
    drawerProjects.value = Array.isArray(list) ? list : []
  } catch (e: any) {
    drawerProjects.value = []
    ElMessage.error('加载项目列表失败: ' + (e.response?.data?.error || e.message))
  } finally {
    drawerLoading.value = false
  }
}

// raft 节点详情(暂仅提示)
function viewRaftDetail(row: OpsNode) {
  ElMessage.info(`Raft 节点 ${row.id} (${row.state || '-'})`)
}

// v0.6.3: CPU/内存使用率分级着色
function metricLevel(v?: number): string {
  if (v == null) return ''
  if (v >= 90) return 'metric-critical'
  if (v >= 80) return 'metric-warning'
  return 'metric-ok'
}

// v0.6.3: 资源监控对话框
const metricsDialogVisible = ref(false)
const metricsAgentId = ref('')
function openMetricsDialog(row: OpsNode) {
  metricsAgentId.value = row.id
  metricsDialogVisible.value = true
}

// --- 配置比对对话框 ---
// DiffReport 对应后端 configdiff.DiffReport 的 JSON 结构
interface KeyDiff {
  key: string
  nacos?: string
  local?: string
  jar?: string
  consistent: boolean
}

interface DiffReport {
  nacosErr?: string
  localErr?: string
  jarErr?: string
  consistent?: string[]
  onlyNacos?: string[]
  onlyLocal?: string[]
  onlyJar?: string[]
  nacosLocal?: string[]
  nacosJar?: string[]
  localJar?: string[]
  // v0.6.2: 键值级语义差异, 直接展示同一 key 在三路中的值
  semantic?: KeyDiff[]
}

interface ConfigDiffItem {
  category: string
  lines: string[]
}

const configDialogVisible = ref(false)
const configProject = ref<ProjectRecord | null>(null)
const configItems = ref<ConfigDiffItem[]>([])
const configSemantic = ref<KeyDiff[]>([])
const configErrors = ref<{ source: string; msg: string }[]>([])
const configActiveTab = ref<'semantic' | 'lines'>('semantic')

function openConfigDialog(row: ProjectRecord) {
  configProject.value = row
  configItems.value = []
  configSemantic.value = []
  configErrors.value = []
  // 默认优先展示语义差异; 若无语义数据则回退行级
  configActiveTab.value = 'semantic'

  // v0.5.3: 从持久化的 configDiffJson 解析比对结果
  if (row.configDiffJson) {
    try {
      const report: DiffReport = JSON.parse(row.configDiffJson)
      // 采集错误
      if (report.nacosErr) configErrors.value.push({ source: 'Nacos', msg: report.nacosErr })
      if (report.localErr) configErrors.value.push({ source: '本地', msg: report.localErr })
      if (report.jarErr) configErrors.value.push({ source: 'Jar', msg: report.jarErr })
      // v0.6.2: 语义级差异(键值对比), 优先展示
      if (report.semantic && report.semantic.length > 0) {
        configSemantic.value = report.semantic
      } else {
        // 无语义数据时回退到行级视图
        configActiveTab.value = 'lines'
      }
      // 按差异类别分组展示(行级)
      const categories: { label: string; lines?: string[] }[] = [
        { label: '三方一致', lines: report.consistent },
        { label: '仅 Nacos 有', lines: report.onlyNacos },
        { label: '仅本地有', lines: report.onlyLocal },
        { label: '仅 Jar 有', lines: report.onlyJar },
        { label: 'Nacos + 本地 (Jar 缺失)', lines: report.nacosLocal },
        { label: 'Nacos + Jar (本地缺失)', lines: report.nacosJar },
        { label: '本地 + Jar (Nacos 缺失)', lines: report.localJar },
      ]
      configItems.value = categories
        .filter((c) => c.lines && c.lines.length > 0)
        .map((c) => ({ category: c.label, lines: c.lines! }))
    } catch (e) {
      ElMessage.warning('解析配置比对结果失败: ' + (e as Error).message)
    }
  }
  configDialogVisible.value = true
}

// 语义差异行样式: 不一致的行高亮
function semanticRowClass({ row }: { row: KeyDiff }): string {
  return row.consistent ? '' : 'row-inconsistent'
}

onMounted(() => {
  loadOpsNodes()
  loadProjectCount()
  timer = window.setInterval(loadOpsNodes, 5000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<style scoped>
/* 统计卡片行 */
.stat-row {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
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
  flex-shrink: 0;
}

.stat-icon.raft { background: #409eff; }
.stat-icon.agent { background: #67c23a; }
.stat-icon.leader { background: #e6a23c; }
.stat-icon.project { background: #909399; }

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

/* Leader ID 可能较长, 单独缩小 */
.stat-value-text {
  font-size: 15px;
  word-break: break-all;
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

.panel-title {
  font-size: 15px;
  font-weight: 600;
  color: #303133;
}

.sub-text {
  color: #909399;
  font-size: 12px;
}

/* 状态单元格 */
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

/* 抽屉提示 */
.drawer-tip {
  display: flex;
  align-items: center;
  gap: 6px;
  background: #ecf5ff;
  border: 1px solid #d9ecff;
  border-radius: 6px;
  padding: 8px 12px;
  margin-bottom: 12px;
  font-size: 13px;
  color: #409eff;
}

/* 配置对话框 */
.config-meta {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
  font-size: 14px;
  color: #303133;
}

.config-files {
  margin-top: 16px;
}

.config-files-title {
  font-size: 13px;
  color: #606266;
  margin-bottom: 8px;
}

.config-file-tag {
  margin: 0 6px 6px 0;
}

.config-line {
  font-family: 'Courier New', monospace;
  font-size: 12px;
  color: #303133;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}

/* 语义差异: 不一致行底色高亮, 缺失值灰色 */
:deep(.row-inconsistent) {
  background: #fdf6ec;
}

.val-missing {
  color: #c0c4cc;
  font-style: italic;
}

/* v0.6.3: CPU/内存分级着色 */
.metric-inline {
  font-variant-numeric: tabular-nums;
}

.metric-ok { color: #67c23a; }
.metric-warning { color: #e6a23c; font-weight: 600; }
.metric-critical { color: #f56c6c; font-weight: 600; }
</style>
