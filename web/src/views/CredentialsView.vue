<template>
  <div class="cred-view">
    <div class="panel">
      <div class="panel-toolbar">
        <span class="panel-title">SSH 凭据管理</span>
        <div>
          <el-button @click="loadCredentials" :icon="Refresh">刷新</el-button>
          <el-button type="primary" @click="openAdd" :icon="Plus">添加凭据</el-button>
        </div>
      </div>

      <el-alert type="info" :closable="false" style="margin-bottom: 12px">
        SSH 凭据用于 v0.4 自动部署: 控制面通过 SSH 推送二进制 + 配置到目标服务器, 远程拉起 systemd。
        密码/私钥用 AES-GCM 加密后存入 Raft, 主密钥从环境变量 MASTER_KEY 读取, 不落盘。
      </el-alert>

      <el-table :data="credentials" style="width: 100%" v-loading="loading" empty-text="暂无凭据, 点击添加">
        <el-table-column prop="serverName" label="服务器名" min-width="140" />
        <el-table-column prop="ip" label="IP" width="160" />
        <el-table-column prop="port" label="端口" width="80" />
        <el-table-column prop="username" label="用户名" width="120" />
        <el-table-column label="认证方式" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="row.authType === 'key' ? 'warning' : 'primary'">
              {{ row.authType === 'key' ? '私钥' : '密码' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="180">
          <template #default="{ row }">
            {{ formatTime(row.createdAt) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="100">
          <template #default="{ row }">
            <el-button link type="danger" size="small" @click="onDel(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 添加凭据对话框 -->
    <el-dialog v-model="addVisible" title="添加 SSH 凭据" width="560px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="服务器名">
          <el-input v-model="form.serverName" placeholder="如 web-01" style="width: 100%" />
        </el-form-item>
        <el-form-item label="IP">
          <el-input v-model="form.ip" placeholder="192.168.1.10" style="width: 100%" />
        </el-form-item>
        <el-form-item label="端口">
          <el-input-number v-model="form.port" :min="1" :max="65535" controls-position="right" style="width: 160px" />
        </el-form-item>
        <el-form-item label="用户名">
          <el-input v-model="form.username" placeholder="root / deploy" style="width: 100%" />
        </el-form-item>
        <el-form-item label="认证方式">
          <el-radio-group v-model="form.authType">
            <el-radio value="password">密码</el-radio>
            <el-radio value="key">私钥</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="密码" v-if="form.authType === 'password'">
          <el-input v-model="form.password" type="password" show-password placeholder="SSH 密码" style="width: 100%" />
        </el-form-item>
        <el-form-item label="私钥" v-if="form.authType === 'key'">
          <el-input v-model="form.privateKey" type="textarea" :rows="6"
            placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;..." style="width: 100%" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="addVisible = false">取消</el-button>
        <el-button type="primary" :loading="adding" @click="onAdd">添加</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { listCredentials, addCredential, delCredential, type SSHCredential } from '../api/credentials'

const credentials = ref<SSHCredential[]>([])
const loading = ref(false)
const adding = ref(false)
const addVisible = ref(false)

const form = reactive({
  serverName: '',
  ip: '',
  port: 22,
  username: 'root',
  authType: 'password' as 'password' | 'key',
  password: '',
  privateKey: '',
})

async function loadCredentials() {
  loading.value = true
  try {
    credentials.value = await listCredentials()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '加载凭据失败')
  } finally {
    loading.value = false
  }
}

function openAdd() {
  form.serverName = ''
  form.ip = ''
  form.port = 22
  form.username = 'root'
  form.authType = 'password'
  form.password = ''
  form.privateKey = ''
  addVisible.value = true
}

async function onAdd() {
  if (!form.ip || !form.username) {
    ElMessage.warning('IP 和用户名不能为空')
    return
  }
  if (form.authType === 'password' && !form.password) {
    ElMessage.warning('请填写密码')
    return
  }
  if (form.authType === 'key' && !form.privateKey) {
    ElMessage.warning('请填写私钥')
    return
  }
  adding.value = true
  try {
    await addCredential({
      serverName: form.serverName,
      ip: form.ip,
      port: form.port,
      username: form.username,
      password: form.password,
      privateKey: form.privateKey,
      authType: form.authType,
    })
    ElMessage.success('凭据已添加(已加密存储)')
    addVisible.value = false
    await loadCredentials()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '添加失败')
  } finally {
    adding.value = false
  }
}

async function onDel(row: SSHCredential) {
  try {
    await ElMessageBox.confirm('确认删除 ' + (row.serverName || row.ip) + ' 的凭据?', '删除确认', {
      type: 'warning',
    })
  } catch {
    return
  }
  try {
    await delCredential(row.id)
    ElMessage.success('已删除')
    await loadCredentials()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '删除失败')
  }
}

function formatTime(ts: number): string {
  if (!ts) return '-'
  return new Date(ts * 1000).toLocaleString('zh-CN')
}

onMounted(loadCredentials)
</script>

<style scoped>
.panel { background: #fff; border: 1px solid #e4e7ed; border-radius: 8px; padding: 16px 20px; }
.panel-toolbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.panel-title { font-size: 15px; font-weight: 600; color: #303133; }
</style>
