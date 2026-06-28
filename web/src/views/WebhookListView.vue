<template>
  <div class="webhook-view">
    <div class="panel">
      <div class="panel-toolbar">
        <span class="panel-title">Webhook 订阅</span>
        <div>
          <el-button @click="loadWebhooks" :icon="Refresh">刷新</el-button>
          <el-button type="primary" @click="openAdd" :icon="Plus">新建 Webhook</el-button>
        </div>
      </div>

      <el-alert type="info" :closable="false" style="margin-bottom: 12px">
        Webhook 在部署完成/扫描发现新项目/告警触发等事件时主动推送 JSON 到指定 URL, 适合触发外部 CI/CD 或通知系统。
        推送带 HMAC-SHA256 签名(<code>X-Deepsea-Signature</code> 头), 接收方可用 Secret 校验来源。
      </el-alert>

      <el-table :data="webhooks" style="width: 100%" v-loading="loading" empty-text="暂无 Webhook">
        <el-table-column prop="name" label="名称" min-width="140" />
        <el-table-column prop="url" label="URL" min-width="260" show-overflow-tooltip />
        <el-table-column label="订阅事件" min-width="200">
          <template #default="{ row }">
            <template v-if="row.events && row.events.length">
              <el-tag v-for="ev in row.events" :key="ev" size="small" style="margin-right: 4px">{{ eventLabel(ev) }}</el-tag>
            </template>
            <span v-else style="color: #909399">全部事件</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="row.active ? 'success' : 'info'">{{ row.active ? '启用' : '停用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="Secret" width="90">
          <template #default="{ row }">
            <el-tag v-if="row.hasSecret" size="small" type="warning">已设置</el-tag>
            <span v-else style="color: #909399">无</span>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="180">
          <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="220">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="onTest(row)">测试</el-button>
            <el-button link type="primary" size="small" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" size="small" @click="onDel(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 新建/编辑 Webhook 对话框 -->
    <el-dialog v-model="formVisible" :title="editing ? '编辑 Webhook' : '新建 Webhook'" width="560px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="名称">
          <el-input v-model="form.name" placeholder="如 ci-trigger / alert-notify" style="width: 100%" />
        </el-form-item>
        <el-form-item label="URL">
          <el-input v-model="form.url" placeholder="https://example.com/webhook" style="width: 100%" />
        </el-form-item>
        <el-form-item label="订阅事件">
          <el-select v-model="form.events" multiple placeholder="留空订阅全部事件" style="width: 100%">
            <el-option v-for="ev in WEBHOOK_EVENTS" :key="ev.value" :label="ev.label" :value="ev.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="签名 Secret">
          <el-input v-model="form.secret" placeholder="可选, 用于 HMAC-SHA256 签名校验" style="width: 100%" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.active" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="onSubmit">{{ editing ? '保存' : '创建' }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, Plus } from '@element-plus/icons-vue'
import { listWebhooks, createWebhook, updateWebhook, deleteWebhook, testWebhook, WEBHOOK_EVENTS, type Webhook } from '../api/webhooks'

const webhooks = ref<Webhook[]>([])
const loading = ref(false)
const submitting = ref(false)

const formVisible = ref(false)
const editing = ref(false)
const editId = ref('')
const form = reactive({
  name: '',
  url: '',
  events: [] as string[],
  secret: '',
  active: true,
})

async function loadWebhooks() {
  loading.value = true
  try {
    webhooks.value = await listWebhooks()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '加载 Webhook 列表失败')
  } finally {
    loading.value = false
  }
}

function eventLabel(ev: string): string {
  const found = WEBHOOK_EVENTS.find((e) => e.value === ev)
  return found ? found.label : ev
}

function formatTime(ts: number): string {
  if (!ts) return '-'
  return new Date(ts).toLocaleString('zh-CN')
}

function openAdd() {
  editing.value = false
  editId.value = ''
  form.name = ''
  form.url = ''
  form.events = []
  form.secret = ''
  form.active = true
  formVisible.value = true
}

function openEdit(row: Webhook) {
  editing.value = true
  editId.value = row.id
  form.name = row.name
  form.url = row.url
  form.events = row.events ? [...row.events] : []
  form.secret = '' // 编辑时不回显 Secret, 留空表示不修改
  form.active = row.active
  formVisible.value = true
}

async function onSubmit() {
  if (!form.name || !form.url) {
    ElMessage.warning('名称和 URL 不能为空')
    return
  }
  submitting.value = true
  try {
    if (editing.value) {
      const req: any = { name: form.name, url: form.url, events: form.events, active: form.active }
      if (form.secret) req.secret = form.secret // 留空不修改
      await updateWebhook(editId.value, req)
      ElMessage.success('Webhook 已更新')
    } else {
      await createWebhook({ name: form.name, url: form.url, events: form.events, secret: form.secret || undefined, active: form.active })
      ElMessage.success('Webhook 已创建')
    }
    formVisible.value = false
    await loadWebhooks()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '操作失败')
  } finally {
    submitting.value = false
  }
}

async function onTest(row: Webhook) {
  try {
    const res = await testWebhook(row.id)
    if (res.status === 'ok') {
      ElMessage.success('测试事件已成功推送')
    } else {
      ElMessage.warning('推送失败: ' + (res.error || '未知错误'))
    }
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '测试失败')
  }
}

async function onDel(row: Webhook) {
  try {
    await ElMessageBox.confirm(`确认删除 Webhook "${row.name}"?`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await deleteWebhook(row.id)
    ElMessage.success('Webhook 已删除')
    await loadWebhooks()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '删除失败')
  }
}

onMounted(() => {
  loadWebhooks()
})
</script>

<style scoped>
.webhook-view { padding: 0; }
.panel { background: #fff; border: 1px solid #e4e7ed; border-radius: 8px; padding: 16px 20px; }
.panel-toolbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.panel-title { font-size: 15px; font-weight: 600; color: #303133; }
</style>
