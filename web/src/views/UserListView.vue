<template>
  <div class="user-view">
    <div class="panel">
      <div class="panel-toolbar">
        <span class="panel-title">用户管理</span>
        <div>
          <el-button @click="loadUsers" :icon="Refresh">刷新</el-button>
          <el-button type="primary" @click="openAdd" :icon="Plus">新建用户</el-button>
        </div>
      </div>

      <el-alert type="info" :closable="false" style="margin-bottom: 12px">
        角色说明: <b>admin</b> 全部权限(含用户管理); <b>operator</b> 读写资源(不可管用户); <b>viewer</b> 只读。
        非 admin 用户仅可见自己创建的或共享(Owner 为空)的资源。
      </el-alert>

      <el-table :data="users" style="width: 100%" v-loading="loading" empty-text="暂无用户">
        <el-table-column prop="username" label="用户名" min-width="160" />
        <el-table-column label="角色" width="120">
          <template #default="{ row }">
            <el-tag size="small" :type="roleTagType(row.role)">{{ roleLabel(row.role) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="200">
          <template #default="{ row }">
            {{ formatTime(row.createdAt) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openEdit(row)">编辑</el-button>
            <el-button link type="warning" size="small" @click="openResetPwd(row)">改密码</el-button>
            <el-button link type="danger" size="small" @click="onDel(row)" :disabled="row.username === 'admin'">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 新建用户对话框 -->
    <el-dialog v-model="addVisible" title="新建用户" width="480px">
      <el-form :model="addForm" label-width="90px">
        <el-form-item label="用户名">
          <el-input v-model="addForm.username" placeholder="如 alice / ops-team" style="width: 100%" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="addForm.password" type="password" show-password placeholder="初始密码" style="width: 100%" />
        </el-form-item>
        <el-form-item label="角色">
          <el-radio-group v-model="addForm.role">
            <el-radio value="admin">admin</el-radio>
            <el-radio value="operator">operator</el-radio>
            <el-radio value="viewer">viewer</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="onAdd">创建</el-button>
      </template>
    </el-dialog>

    <!-- 编辑角色对话框 -->
    <el-dialog v-model="editVisible" :title="'编辑用户 - ' + editForm.username" width="480px">
      <el-form :model="editForm" label-width="90px">
        <el-form-item label="用户名">
          <el-input :model-value="editForm.username" disabled style="width: 100%" />
        </el-form-item>
        <el-form-item label="角色">
          <el-radio-group v-model="editForm.role">
            <el-radio value="admin">admin</el-radio>
            <el-radio value="operator">operator</el-radio>
            <el-radio value="viewer">viewer</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="onEdit">保存</el-button>
      </template>
    </el-dialog>

    <!-- 改密码对话框 -->
    <el-dialog v-model="pwdVisible" :title="'重置密码 - ' + pwdForm.username" width="480px">
      <el-form :model="pwdForm" label-width="90px">
        <el-form-item label="新密码">
          <el-input v-model="pwdForm.password" type="password" show-password placeholder="新密码" style="width: 100%" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="pwdVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="onResetPwd">确认</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, Plus } from '@element-plus/icons-vue'
import { listUsers, createUser, updateUser, deleteUser, type User } from '../api/user'

const users = ref<User[]>([])
const loading = ref(false)
const submitting = ref(false)

const addVisible = ref(false)
const addForm = reactive({ username: '', password: '', role: 'viewer' })

const editVisible = ref(false)
const editForm = reactive({ username: '', role: 'viewer' })

const pwdVisible = ref(false)
const pwdForm = reactive({ username: '', password: '' })

async function loadUsers() {
  loading.value = true
  try {
    users.value = await listUsers()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '加载用户列表失败')
  } finally {
    loading.value = false
  }
}

function roleLabel(r: string): string {
  return r
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

function openAdd() {
  addForm.username = ''
  addForm.password = ''
  addForm.role = 'viewer'
  addVisible.value = true
}

async function onAdd() {
  if (!addForm.username || !addForm.password) {
    ElMessage.warning('用户名和密码不能为空')
    return
  }
  submitting.value = true
  try {
    await createUser({ username: addForm.username, password: addForm.password, role: addForm.role })
    ElMessage.success('用户已创建')
    addVisible.value = false
    await loadUsers()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '创建失败')
  } finally {
    submitting.value = false
  }
}

function openEdit(row: User) {
  editForm.username = row.username
  editForm.role = row.role
  editVisible.value = true
}

async function onEdit() {
  submitting.value = true
  try {
    await updateUser(editForm.username, { role: editForm.role })
    ElMessage.success('角色已更新')
    editVisible.value = false
    await loadUsers()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '更新失败')
  } finally {
    submitting.value = false
  }
}

function openResetPwd(row: User) {
  pwdForm.username = row.username
  pwdForm.password = ''
  pwdVisible.value = true
}

async function onResetPwd() {
  if (!pwdForm.password) {
    ElMessage.warning('请输入新密码')
    return
  }
  submitting.value = true
  try {
    await updateUser(pwdForm.username, { password: pwdForm.password })
    ElMessage.success('密码已重置')
    pwdVisible.value = false
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '重置失败')
  } finally {
    submitting.value = false
  }
}

async function onDel(row: User) {
  try {
    await ElMessageBox.confirm(`确认删除用户 "${row.username}"? 该操作不可恢复`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return // 用户取消
  }
  try {
    await deleteUser(row.username)
    ElMessage.success('用户已删除')
    await loadUsers()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '删除失败')
  }
}

onMounted(() => {
  loadUsers()
})
</script>

<style scoped>
.user-view { padding: 0; }
.panel { background: #fff; border: 1px solid #e4e7ed; border-radius: 8px; padding: 16px 20px; }
.panel-toolbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.panel-title { font-size: 15px; font-weight: 600; color: #303133; }
</style>
