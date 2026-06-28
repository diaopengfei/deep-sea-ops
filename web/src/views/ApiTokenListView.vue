<template>
  <div class="token-view">
    <div class="panel">
      <div class="panel-toolbar">
        <span class="panel-title">API Token 管理</span>
        <div>
          <el-button @click="loadTokens" :icon="Refresh">刷新</el-button>
          <el-button type="primary" @click="openAdd" :icon="Plus">新建 Token</el-button>
        </div>
      </div>

      <el-alert type="info" :closable="false" style="margin-bottom: 12px">
        API Token 用于外部系统集成调用 REST API, 区别于 JWT 长期有效。
        请求头传 <code>Authorization: Bearer dst_xxx</code> 或 <code>X-API-Token: dst_xxx</code>。
        <b>明文 Token 仅在创建时显示一次, 请立即复制保存。</b>
      </el-alert>

      <el-table :data="tokens" style="width: 100%" v-loading="loading" empty-text="暂无 Token">
        <el-table-column prop="name" label="名称" min-width="160" />
        <el-table-column label="Token 前缀" width="180">
          <template #default="{ row }">
            <code>{{ row.tokenPrefix }}…</code>
          </template>
        </el-table-column>
        <el-table-column label="角色" width="120">
          <template #default="{ row }">
            <el-tag size="small" :type="roleTagType(row.role)">{{ row.role }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建人" width="120" prop="createdBy" />
        <el-table-column label="创建时间" width="180">
          <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="最后使用" width="180">
          <template #default="{ row }">{{ formatTime(row.lastUsedAt) }}</template>
        </el-table-column>
        <el-table-column label="过期时间" width="180">
          <template #default="{ row }">{{ row.expiresAt ? formatTime(row.expiresAt) : '永不过期' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="100">
          <template #default="{ row }">
            <el-button link type="danger" size="small" @click="onDel(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 新建 Token 对话框 -->
    <el-dialog v-model="addVisible" title="新建 API Token" width="480px">
      <el-form :model="addForm" label-width="90px">
        <el-form-item label="名称">
          <el-input v-model="addForm.name" placeholder="如 ci-cd / monitoring" style="width: 100%" />
        </el-form-item>
        <el-form-item label="角色">
          <el-radio-group v-model="addForm.role">
            <el-radio value="admin">admin</el-radio>
            <el-radio value="operator">operator</el-radio>
            <el-radio value="viewer">viewer</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="过期时间">
          <el-radio-group v-model="addForm.expireMode" @change="onExpireModeChange">
            <el-radio value="never">永不过期</el-radio>
            <el-radio value="custom">自定义</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="addForm.expireMode === 'custom'" label="">
          <el-date-picker
            v-model="addForm.expireDate"
            type="datetime"
            placeholder="选择过期时间"
            style="width: 100%"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="onAdd">创建</el-button>
      </template>
    </el-dialog>

    <!-- Token 明文展示对话框(创建后只显示一次) -->
    <el-dialog v-model="plainVisible" title="Token 已创建" width="560px" :close-on-click-modal="false">
      <el-alert type="warning" :closable="false" style="margin-bottom: 12px">
        这是 Token 的唯一明文, 关闭后无法再次查看。请立即复制保存到安全位置。
      </el-alert>
      <el-input :model-value="plainToken" readonly type="textarea" :rows="3" />
      <template #footer>
        <el-button type="primary" @click="copyToken">复制 Token</el-button>
        <el-button @click="plainVisible = false">我已保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, Plus } from '@element-plus/icons-vue'
import { listTokens, createToken, deleteToken, type TokenInfo } from '../api/tokens'

const tokens = ref<TokenInfo[]>([])
const loading = ref(false)
const submitting = ref(false)

const addVisible = ref(false)
const addForm = reactive({
  name: '',
  role: 'viewer',
  expireMode: 'never' as 'never' | 'custom',
  expireDate: null as Date | null,
})

const plainVisible = ref(false)
const plainToken = ref('')

async function loadTokens() {
  loading.value = true
  try {
    tokens.value = await listTokens()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '加载 Token 列表失败')
  } finally {
    loading.value = false
  }
}

function roleTagType(r: string): 'danger' | 'primary' | 'info' {
  if (r === 'admin') return 'danger'
  if (r === 'operator') return 'primary'
  return 'info'
}

function formatTime(ts: number): string {
  if (!ts) return '-'
  return new Date(ts).toLocaleString('zh-CN')
}

function onExpireModeChange() {
  if (addForm.expireMode === 'never') {
    addForm.expireDate = null
  }
}

function openAdd() {
  addForm.name = ''
  addForm.role = 'viewer'
  addForm.expireMode = 'never'
  addForm.expireDate = null
  addVisible.value = true
}

async function onAdd() {
  if (!addForm.name) {
    ElMessage.warning('名称不能为空')
    return
  }
  let expiresAt = 0
  if (addForm.expireMode === 'custom' && addForm.expireDate) {
    expiresAt = addForm.expireDate.getTime()
  }
  submitting.value = true
  try {
    const resp = await createToken({ name: addForm.name, role: addForm.role, expiresAt })
    addVisible.value = false
    plainToken.value = resp.token
    plainVisible.value = true
    await loadTokens()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '创建失败')
  } finally {
    submitting.value = false
  }
}

async function copyToken() {
  try {
    await navigator.clipboard.writeText(plainToken.value)
    ElMessage.success('已复制到剪贴板')
  } catch {
    ElMessage.warning('复制失败, 请手动选择文本复制')
  }
}

async function onDel(row: TokenInfo) {
  try {
    await ElMessageBox.confirm(`确认删除 Token "${row.name}"? 删除后该 Token 立即失效。`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await deleteToken(row.id)
    ElMessage.success('Token 已删除')
    await loadTokens()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '删除失败')
  }
}

onMounted(() => {
  loadTokens()
})
</script>

<style scoped>
.token-view { padding: 0; }
.panel { background: #fff; border: 1px solid #e4e7ed; border-radius: 8px; padding: 16px 20px; }
.panel-toolbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.panel-title { font-size: 15px; font-weight: 600; color: #303133; }
</style>
