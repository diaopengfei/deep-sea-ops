<template>
  <el-dialog v-model="visible" title="配置基准与版本管理" width="820px" @open="onOpen">
    <div v-if="project" class="baseline-meta">
      <span>项目: <b>{{ project.name }}</b></span>
      <span class="sub-text">{{ project.path }}</span>
      <el-tag v-if="project.baselineVersion" size="small" type="success">当前版本 v{{ project.baselineVersion }}</el-tag>
      <span v-else class="sub-text">尚未建立基准</span>
      <span v-if="project.baselineUpdatedAt" class="sub-text">
        {{ project.baselineUpdatedBy || '-' }} · {{ formatTime(project.baselineUpdatedAt) }}
      </span>
    </div>

    <el-tabs v-model="activeTab">
      <!-- 编辑基准配置 -->
      <el-tab-pane label="编辑基准" name="edit">
        <div class="editor-tip">
          <el-icon><InfoFilled /></el-icon>
          在此维护"应有配置", 保存即创建新版本(走 Raft 强一致), 可下发到 Agent 本地文件 / 回滚到任意历史版本。
        </div>
        <textarea
          v-model="editContent"
          class="config-editor"
          spellcheck="false"
          placeholder="server.port=8080&#10;spring.datasource.url=jdbc:mysql://...&#10;# YAML 或 Properties 文本"
        />
        <div class="edit-actions">
          <el-input v-model="comment" placeholder="版本备注(可选)" size="small" style="width: 320px" />
          <el-button type="primary" :loading="saving" :disabled="!editContent" @click="onSave">
            保存为新版本
          </el-button>
          <el-button :loading="deploying" :disabled="!project?.baselineVersion" @click="onDeploy">
            下发到 Agent
          </el-button>
        </div>
        <el-alert v-if="deployMsg" :title="deployMsg" :type="deployOk ? 'success' : 'error'" :closable="false" style="margin-top: 8px" />
      </el-tab-pane>

      <!-- 版本历史 -->
      <el-tab-pane :label="`版本历史 (${versions.length})`" name="history">
        <el-table :data="versions" style="width: 100%" v-loading="versionLoading" empty-text="暂无版本历史" size="small">
          <el-table-column prop="version" label="版本" width="80">
            <template #default="{ row }">
              <b>v{{ row.version }}</b>
            </template>
          </el-table-column>
          <el-table-column prop="updatedBy" label="更新人" width="120" />
          <el-table-column label="更新时间" width="180">
            <template #default="{ row }">{{ formatTime(row.updatedAt) }}</template>
          </el-table-column>
          <el-table-column prop="comment" label="备注" min-width="160" show-overflow-tooltip />
          <el-table-column label="操作" width="200" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" size="small" @click="onViewVersion(row)">查看内容</el-button>
              <el-button link type="warning" size="small" :disabled="row.version === project?.baselineVersion" @click="onRollback(row)">
                回滚到此版本
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>
    </el-tabs>

    <!-- 版本内容预览 -->
    <el-dialog v-model="previewVisible" title="版本内容" width="720px" append-to-body>
      <pre v-if="previewContent" class="config-preview">{{ previewContent }}</pre>
      <el-empty v-else description="无内容" />
    </el-dialog>

    <template #footer>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { InfoFilled } from '@element-plus/icons-vue'
import {
  getProjectBaseline,
  saveProjectBaseline,
  listConfigVersions,
  rollbackConfigVersion,
  deployProjectBaseline,
  type ProjectRecord,
  type ConfigVersion,
} from '../api/server'

const props = defineProps<{ modelValue: boolean; project: ProjectRecord | null }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void; (e: 'saved'): void }>()

const visible = ref(props.modelValue)
watch(() => props.modelValue, (v) => { visible.value = v })
watch(visible, (v) => { emit('update:modelValue', v) })

const activeTab = ref<'edit' | 'history'>('edit')
const editContent = ref('')
const comment = ref('')
const versions = ref<ConfigVersion[]>([])
const versionLoading = ref(false)
const saving = ref(false)
const deploying = ref(false)
const deployMsg = ref('')
const deployOk = ref(false)
const previewVisible = ref(false)
const previewContent = ref('')

async function onOpen() {
  if (!props.project) return
  editContent.value = props.project.configBaseline || ''
  comment.value = ''
  deployMsg.value = ''
  activeTab.value = 'edit'
  await loadBaseline()
  await loadVersions()
}

async function loadBaseline() {
  if (!props.project) return
  try {
    const p = await getProjectBaseline(props.project.id)
    // 更新编辑区为最新基准内容
    editContent.value = p.configBaseline || ''
  } catch (e: any) {
    ElMessage.error('加载基准失败: ' + (e.response?.data?.error || e.message))
  }
}

async function loadVersions() {
  if (!props.project) return
  versionLoading.value = true
  try {
    const list = await listConfigVersions(props.project.id)
    versions.value = Array.isArray(list) ? list : []
  } catch (e: any) {
    versions.value = []
    ElMessage.error('加载版本历史失败: ' + (e.response?.data?.error || e.message))
  } finally {
    versionLoading.value = false
  }
}

async function onSave() {
  if (!props.project || !editContent.value) return
  saving.value = true
  try {
    await saveProjectBaseline(props.project.id, { content: editContent.value, comment: comment.value })
    ElMessage.success('已保存为新版本')
    comment.value = ''
    await loadBaseline()
    await loadVersions()
    emit('saved')
  } catch (e: any) {
    ElMessage.error('保存失败: ' + (e.response?.data?.error || e.message))
  } finally {
    saving.value = false
  }
}

async function onDeploy() {
  if (!props.project) return
  try {
    await ElMessageBox.confirm(
      `将当前基准(v${props.project.baselineVersion})下发到 Agent ${props.project.agentId} 的本地配置文件, 写入前会自动备份原文件。确认下发?`,
      '下发确认',
      { type: 'warning' }
    )
  } catch {
    return
  }
  deploying.value = true
  deployMsg.value = ''
  try {
    const res = await deployProjectBaseline(props.project.id)
    deployOk.value = true
    deployMsg.value = '下发成功: ' + res.output
    ElMessage.success('基准配置已下发')
  } catch (e: any) {
    deployOk.value = false
    deployMsg.value = '下发失败: ' + (e.response?.data?.error || e.message)
    ElMessage.error(deployMsg.value)
  } finally {
    deploying.value = false
  }
}

function onViewVersion(row: ConfigVersion) {
  previewContent.value = row.content
  previewVisible.value = true
}

async function onRollback(row: ConfigVersion) {
  if (!props.project) return
  try {
    await ElMessageBox.confirm(
      `回滚到版本 v${row.version}? 将创建一个新版本(内容为 v${row.version}), 原版本历史保留。`,
      '回滚确认',
      { type: 'warning' }
    )
  } catch {
    return
  }
  try {
    await rollbackConfigVersion(props.project.id, row.version)
    ElMessage.success(`已回滚到 v${row.version}`)
    await loadBaseline()
    await loadVersions()
    emit('saved')
  } catch (e: any) {
    ElMessage.error('回滚失败: ' + (e.response?.data?.error || e.message))
  }
}

function formatTime(ms?: number): string {
  if (!ms) return '-'
  return new Date(ms).toLocaleString('zh-CN', { hour12: false })
}
</script>

<style scoped>
.baseline-meta {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
  font-size: 14px;
  color: #303133;
  flex-wrap: wrap;
}
.sub-text { color: #909399; font-size: 12px; }
.editor-tip {
  display: flex;
  align-items: center;
  gap: 6px;
  background: #ecf5ff;
  border: 1px solid #d9ecff;
  border-radius: 6px;
  padding: 8px 12px;
  margin-bottom: 10px;
  font-size: 13px;
  color: #409eff;
}
.config-editor {
  width: 100%;
  height: 320px;
  font-family: 'Courier New', Consolas, monospace;
  font-size: 13px;
  line-height: 1.6;
  padding: 10px 12px;
  border: 1px solid #dcdfe6;
  border-radius: 6px;
  resize: vertical;
  outline: none;
  box-sizing: border-box;
  background: #fafafa;
}
.config-editor:focus { border-color: #409eff; background: #fff; }
.edit-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 10px;
  flex-wrap: wrap;
}
.config-preview {
  font-family: 'Courier New', Consolas, monospace;
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 60vh;
  overflow: auto;
  background: #fafafa;
  padding: 12px;
  border-radius: 6px;
  border: 1px solid #ebeef5;
}
</style>
