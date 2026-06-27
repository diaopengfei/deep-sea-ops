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
        <div class="panel-actions">
          <el-button type="warning" :icon="Upload" :disabled="selectedAgentIds.length === 0" @click="openBatchUpgradeDialog">
            批量升级 ({{ selectedAgentIds.length }})
          </el-button>
          <el-button :icon="Refresh" @click="loadOpsNodes">刷新</el-button>
        </div>
      </div>
      <el-table :data="opsNodes" style="width: 100%" v-loading="loading" empty-text="暂无节点" @selection-change="onSelectionChange">
        <el-table-column type="selection" width="42" :selectable="(row: OpsNode) => row.type === 'agent'" />
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
        <el-table-column label="Agent 版本" width="160">
          <template #default="{ row }">
            <span v-if="row.type === 'agent'" class="version-cell">
              <span :class="versionClass(row.version)">{{ row.version || '未知' }}</span>
              <el-tag v-if="versionOutdated(row.version)" size="small" type="warning" effect="plain">需升级</el-tag>
              <el-tag v-else-if="row.version" size="small" type="success" effect="plain">最新</el-tag>
            </span>
            <span v-else class="sub-text">{{ serverVersion || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="240" fixed="right">
          <template #default="{ row }">
            <el-button v-if="row.type === 'agent'" link type="primary" size="small" @click="openProjectDrawer(row)">查看项目</el-button>
            <el-button v-if="row.type === 'agent'" link type="success" size="small" @click="openMetricsDialog(row)">监控</el-button>
            <el-button v-if="row.type === 'agent'" link type="warning" size="small" @click="openUpgradeDialog(row)">升级</el-button>
            <el-button v-else link type="info" size="small" @click="viewRaftDetail(row)">查看详情</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- v0.6.6: 单/批量 Agent 升级对话框 -->
    <el-dialog v-model="upgradeDialogVisible" :title="upgradeDialogTitle" width="560px">
      <el-form :model="upgradeForm" label-width="100px">
        <el-form-item label="目标 Agent">
          <span class="upgrade-targets">{{ upgradeForm.agentIds.join(', ') }}</span>
        </el-form-item>
        <el-form-item label="下载地址" required>
          <el-input v-model="upgradeForm.url" placeholder="http://oss.example.com/deepsea-agent/v0.6.6/deepsea-agent" />
          <div class="form-tip">Agent 将从该 URL 下载新二进制并替换自身, 退出后由服务管理器(systemd)重启</div>
        </el-form-item>
        <el-form-item label="SHA-256">
          <el-input v-model="upgradeForm.checksum" placeholder="可选, 校验下载文件完整性" />
        </el-form-item>
        <el-form-item v-if="upgradeForm.agentIds.length > 1" label="间隔秒数">
          <el-input-number v-model="upgradeForm.waitSeconds" :min="0" :max="600" :step="5" />
          <div class="form-tip">滚动升级: 每个 Agent 升级后等待该秒数再升级下一个, 默认 10 秒</div>
        </el-form-item>
        <el-form-item label="控制面版本">
          <span class="sub-text">{{ serverVersion || '-' }}</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="upgradeDialogVisible = false">取消</el-button>
        <el-button type="warning" :loading="upgradeLoading" @click="submitUpgrade">确认升级</el-button>
      </template>
    </el-dialog>

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
        <el-table-column label="类型" width="120">
          <template #default="{ row }">
            <el-tag size="small" :type="projectTypeTag(row.type)">{{ row.type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="path" label="路径" min-width="200" show-overflow-tooltip />
        <el-table-column label="配置文件数" width="110">
          <template #default="{ row }">
            {{ row.configFiles ? row.configFiles.length : 0 }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openConfigDialog(row)">查看配置</el-button>
            <el-button link type="warning" size="small" @click="openBaselineDialog(row)">配置基准</el-button>
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

    <!-- v0.6.5: 配置基准与版本管理对话框 -->
    <ConfigBaselineDialog v-model="baselineDialogVisible" :project="baselineProject" @saved="onBaselineSaved" />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Share, Connection, Monitor, FolderOpened, Refresh, InfoFilled, Upload } from '@element-plus/icons-vue'
import {
  listOpsNodes, listProjects, getServerVersion, upgradeAgent, batchUpgradeAgents,
  type OpsNode, type ProjectRecord
} from '../api/server'
import MetricsDialog from './MetricsDialog.vue'
import ConfigBaselineDialog from './ConfigBaselineDialog.vue'

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

// v0.6.7: 项目/中间件类型标签颜色
// Java 项目: info(蓝灰); Python: success(绿); 中间件: warning(橙)/danger(红)
function projectTypeTag(type: string): 'info' | 'success' | 'warning' | 'danger' | 'primary' {
  const middlewareColors: Record<string, 'warning' | 'danger' | 'primary'> = {
    redis: 'warning',
    postgresql: 'primary',
    mysql: 'primary',
    kafka: 'danger',
    zookeeper: 'warning',
    elasticsearch: 'warning',
    clickhouse: 'danger',
  }
  if (middlewareColors[type]) return middlewareColors[type]
  if (type === 'python') return 'success'
  return 'info'
}

// v0.6.3: 资源监控对话框
const metricsDialogVisible = ref(false)
const metricsAgentId = ref('')
function openMetricsDialog(row: OpsNode) {
  metricsAgentId.value = row.id
  metricsDialogVisible.value = true
}

// v0.6.6: Agent 版本号管理
const serverVersion = ref('')

// 简单语义版本比较: 仅比较 v 主.次.补丁; 缺位补 0; 非法返回 null
function parseSemver(v?: string): number[] | null {
  if (!v) return null
  const m = v.match(/^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?/)
  if (!m) return null
  return [parseInt(m[1], 10), m[2] ? parseInt(m[2], 10) : 0, m[3] ? parseInt(m[3], 10) : 0]
}

// Agent 版本低于控制面 => 需升级
function versionOutdated(agentVer?: string): boolean {
  if (!agentVer || !serverVersion.value) return false
  const a = parseSemver(agentVer)
  const s = parseSemver(serverVersion.value)
  if (!a || !s) return false
  for (let i = 0; i < 3; i++) {
    if (a[i]! < s[i]!) return true
    if (a[i]! > s[i]!) return false
  }
  return false
}

function versionClass(agentVer?: string): string {
  if (!agentVer) return 'sub-text'
  return versionOutdated(agentVer) ? 'ver-outdated' : 'ver-latest'
}

// --- v0.6.6: Agent 升级(单 + 批量滚动) ---
const selectedAgentIds = ref<string[]>([])
function onSelectionChange(rows: OpsNode[]) {
  selectedAgentIds.value = rows.filter((r) => r.type === 'agent').map((r) => r.id)
}

const upgradeDialogVisible = ref(false)
const upgradeLoading = ref(false)
const upgradeForm = ref<{ agentIds: string[]; url: string; checksum: string; waitSeconds: number }>({
  agentIds: [],
  url: '',
  checksum: '',
  waitSeconds: 10,
})

const upgradeDialogTitle = computed(() =>
  upgradeForm.value.agentIds.length > 1
    ? `批量升级 ${upgradeForm.value.agentIds.length} 个 Agent`
    : '升级 Agent'
)

function openUpgradeDialog(row: OpsNode) {
  upgradeForm.value = { agentIds: [row.id], url: '', checksum: '', waitSeconds: 10 }
  upgradeDialogVisible.value = true
}

function openBatchUpgradeDialog() {
  if (selectedAgentIds.value.length === 0) {
    ElMessage.warning('请先勾选要升级的 Agent')
    return
  }
  upgradeForm.value = {
    agentIds: [...selectedAgentIds.value],
    url: '',
    checksum: '',
    waitSeconds: 10,
  }
  upgradeDialogVisible.value = true
}

async function submitUpgrade() {
  if (!upgradeForm.value.url) {
    ElMessage.warning('请填写下载地址')
    return
  }
  const isBatch = upgradeForm.value.agentIds.length > 1
  // 二次确认: 升级会重启 Agent
  try {
    await ElMessageBox.confirm(
      `${isBatch ? `将滚动升级 ${upgradeForm.value.agentIds.length} 个 Agent` : `将升级 Agent ${upgradeForm.value.agentIds[0]}`}, 升级过程会下载二进制并重启服务, 确认继续?`,
      '升级确认',
      { type: 'warning', confirmButtonText: '确认升级', cancelButtonText: '取消' }
    )
  } catch {
    return
  }

  upgradeLoading.value = true
  try {
    if (isBatch) {
      const resp = await batchUpgradeAgents({
        agentIds: upgradeForm.value.agentIds,
        url: upgradeForm.value.url,
        checksum: upgradeForm.value.checksum || undefined,
        waitSeconds: upgradeForm.value.waitSeconds,
      })
      ElMessage.success(`滚动升级已启动, 共 ${resp.agentCount} 个 Agent, 间隔 ${resp.waitSeconds} 秒`)
      upgradeDialogVisible.value = false
      // 滚动升级是异步任务, 等 30 秒后刷新列表(让新版本号反映出来)
      setTimeout(loadOpsNodes, 30000)
    } else {
      const resp = await upgradeAgent(upgradeForm.value.agentIds[0], {
        url: upgradeForm.value.url,
        checksum: upgradeForm.value.checksum || undefined,
      })
      ElMessage.success(`Agent 升级成功: ${resp.output || 'ok'}`)
      upgradeDialogVisible.value = false
      // 升级后 Agent 会重启, 等 10 秒后刷新
      setTimeout(loadOpsNodes, 10000)
    }
  } catch (e: any) {
    ElMessage.error('升级失败: ' + (e.response?.data?.error || e.message))
  } finally {
    upgradeLoading.value = false
  }
}

// v0.6.5: 配置基准对话框
const baselineDialogVisible = ref(false)
const baselineProject = ref<ProjectRecord | null>(null)
function openBaselineDialog(row: ProjectRecord) {
  baselineProject.value = row
  baselineDialogVisible.value = true
}
// 基准保存后刷新抽屉里的项目列表(让版本号等同步)
async function onBaselineSaved() {
  if (drawerAgentId.value) {
    try {
      const list = await listProjects(drawerAgentId.value)
      drawerProjects.value = Array.isArray(list) ? list : []
    } catch {
      // 忽略, 抽屉数据下次刷新
    }
  }
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
  // v0.6.6: 加载控制面版本号, 用于 Agent 版本对比
  getServerVersion()
    .then((v) => { serverVersion.value = v })
    .catch(() => { /* 控制面未启用版本接口时忽略 */ })
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

.panel-actions {
  display: flex;
  gap: 8px;
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

/* v0.6.6: Agent 版本列与升级对话框 */
.version-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.ver-latest { color: #67c23a; font-weight: 600; }
.ver-outdated { color: #e6a23c; font-weight: 600; }

.upgrade-targets {
  font-family: 'Courier New', monospace;
  font-size: 13px;
  color: #303133;
  word-break: break-all;
}

.form-tip {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
  line-height: 1.4;
}
</style>
