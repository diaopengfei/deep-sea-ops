<template>
  <div class="project-view">
    <div class="panel">
      <div class="panel-toolbar">
        <span class="panel-title">项目扫描结果</span>
        <div>
          <el-select v-model="agentId" placeholder="选择 Agent" style="width: 220px" @change="onAgentChange">
            <el-option v-for="a in agents" :key="a.id" :label="a.hostname + ' (' + a.id + ')'" :value="a.id" />
          </el-select>
          <el-input v-model="scanDirs" placeholder="扫描目录(逗号分隔)" style="width: 280px; margin-left: 8px" />
          <el-button type="primary" :loading="scanning" @click="onScan" style="margin-left: 8px">
            <el-icon><Search /></el-icon> 扫描
          </el-button>
        </div>
      </div>

      <el-table :data="projects" style="width: 100%" v-loading="scanning" empty-text="暂无扫描结果, 点击扫描按钮">
        <el-table-column label="运行状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.running ? 'success' : 'info'" size="small">
              {{ row.running ? '运行中(PID:' + row.pid + ')' : '未运行' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="项目名" min-width="140" />
        <el-table-column label="类型" width="120">
          <template #default="{ row }">
            <el-tag size="small" :type="row.type === 'python' ? 'warning' : 'primary'">{{ row.type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="path" label="路径" min-width="250" show-overflow-tooltip />
        <el-table-column label="配置文件" min-width="180">
          <template #default="{ row }">
            <span v-if="row.configFiles && row.configFiles.length">{{ row.configFiles.length }} 个</span>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="100">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openDetail(row)">详情</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-drawer v-model="detailVisible" :title="currentProject?.name + ' - 配置详情'" size="70%">
      <template v-if="currentProject">
        <el-tabs v-model="detailTab">
          <el-tab-pane label="生效配置" name="effective">
            <el-alert v-if="!currentProject.effectiveConfig || currentProject.effectiveConfig.items.length === 0"
              title="无配置或未采集" type="info" :closable="false" />
            <el-table v-else :data="currentProject.effectiveConfig.items" style="width: 100%">
              <el-table-column prop="key" label="配置项" min-width="200" />
              <el-table-column prop="value" label="生效值" min-width="200" show-overflow-tooltip />
              <el-table-column label="来源" width="100">
                <template #default="{ row }">
                  <el-tag size="small" :type="sourceTagType(row.source)">{{ row.source }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="被覆盖" width="80">
                <template #default="{ row }">
                  <el-tag v-if="row.overridden" size="small" type="warning">是</el-tag>
                  <span v-else>-</span>
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>

          <el-tab-pane label="Nacos 配置(原始)" name="nacos">
            <el-alert v-if="currentProject.effectiveConfig?.nacosErr" type="warning"
              :title="'采集失败: ' + currentProject.effectiveConfig.nacosErr" :closable="false" />
            <pre v-else class="raw-config">{{ currentProject.effectiveConfig?.nacosRaw || '(未配置 Nacos)' }}</pre>
          </el-tab-pane>

          <el-tab-pane label="本地配置(原始)" name="local">
            <el-alert v-if="currentProject.effectiveConfig?.localErr" type="warning"
              :title="'采集失败: ' + currentProject.effectiveConfig.localErr" :closable="false" />
            <pre v-else class="raw-config">{{ currentProject.effectiveConfig?.localRaw || '(无本地配置文件)' }}</pre>
          </el-tab-pane>

          <el-tab-pane label="jar 内配置(原始)" name="jar">
            <el-alert v-if="currentProject.effectiveConfig?.jarErr" type="warning"
              :title="'采集失败: ' + currentProject.effectiveConfig.jarErr" :closable="false" />
            <pre v-else class="raw-config">{{ currentProject.effectiveConfig?.jarRaw || '(无 jar 配置)' }}</pre>
          </el-tab-pane>

          <el-tab-pane label="hosts 文件" name="hosts">
            <pre class="raw-config">{{ hostsContent || '(未读取)' }}</pre>
          </el-tab-pane>
        </el-tabs>
      </template>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { listAgents } from '../api/server'
import { scanProjects } from '../api/projects'
import type { AgentInfo } from '../api/types'

interface ConfigItem {
  key: string
  value: string
  source: string
  overridden: boolean
}
interface EffectiveConfig {
  items: ConfigItem[]
  nacosRaw: string
  localRaw: string
  jarRaw: string
  nacosErr: string
  localErr: string
  jarErr: string
}
interface Project {
  path: string
  type: string
  name: string
  configFiles: string[]
  jarPath: string
  jarEntry: string
  running: boolean
  pid: number
  effectiveConfig: EffectiveConfig | null
}

const agents = ref<AgentInfo[]>([])
const agentId = ref('')
const scanDirs = ref('')
const scanning = ref(false)
const projects = ref<Project[]>([])
const hostsContent = ref('')

const detailVisible = ref(false)
const currentProject = ref<Project | null>(null)
const detailTab = ref('effective')

async function loadAgents() {
  try {
    agents.value = await listAgents()
  } catch { ElMessage.error('加载 Agent 失败') }
}

function onAgentChange() { projects.value = [] }

async function onScan() {
  if (!agentId.value) { ElMessage.warning('请选择 Agent'); return }
  scanning.value = true
  projects.value = []
  try {
    const res = await scanProjects(agentId.value, scanDirs.value)
    projects.value = res.projects || []
    hostsContent.value = res.hosts || ''
    ElMessage.success('扫描完成: ' + projects.value.length + ' 个项目')
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '扫描失败')
  } finally {
    scanning.value = false
  }
}

function openDetail(row: Project) {
  currentProject.value = row
  detailTab.value = 'effective'
  detailVisible.value = true
}

function sourceTagType(s: string): string {
  const map: Record<string, string> = { nacos: 'success', local: 'primary', jar: 'info' }
  return map[s] || ''
}

onMounted(loadAgents)
</script>

<style scoped>
.panel { background: #fff; border: 1px solid #e4e7ed; border-radius: 8px; padding: 16px 20px; }
.panel-toolbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.panel-title { font-size: 15px; font-weight: 600; color: #303133; }
.raw-config {
  background: #f5f7fa; border: 1px solid #e4e7ed; border-radius: 4px;
  padding: 12px; font-size: 12px; white-space: pre-wrap; word-break: break-all;
  max-height: 500px; overflow: auto; font-family: 'Consolas', 'Monaco', monospace;
}
</style>