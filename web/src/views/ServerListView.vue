<template>
  <el-card header="服务器列表">
    <el-form :inline="true" :model="form" class="add-form">
      <el-form-item label="ID">
        <el-input v-model="form.id" placeholder="如 s4" style="width: 100px" />
      </el-form-item>
      <el-form-item label="名称">
        <el-input v-model="form.name" placeholder="如 web-03" style="width: 140px" />
      </el-form-item>
      <el-form-item label="IP">
        <el-input v-model="form.ip" placeholder="192.168.1.x" style="width: 150px" />
      </el-form-item>
      <el-form-item>
        <el-button type="primary" :loading="submitting" @click="onAdd">新增(走 Raft)</el-button>
        <el-button @click="loadServers">刷新</el-button>
      </el-form-item>
    </el-form>

    <el-table :data="servers" border style="width: 100%" v-loading="loading">
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column prop="name" label="名称" />
      <el-table-column prop="ip" label="IP 地址" />
      <el-table-column prop="status" label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="row.status === 'online' ? 'success' : 'danger'">
            {{ row.status }}
          </el-tag>
        </template>
      </el-table-column>
    </el-table>
  </el-card>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { listServers, addServer } from '../api/server'
import type { Server } from '../api/types'

// 页面状态: 服务器列表
const servers = ref<Server[]>([])
const loading = ref(false)
const submitting = ref(false)

// reactive 用于对象/表单, ref 用于简单值
const form = reactive({
  id: '',
  name: '',
  ip: '',
  status: 'offline'
})

async function loadServers() {
  loading.value = true
  try {
    servers.value = await listServers()
  } finally {
    loading.value = false
  }
}

async function onAdd() {
  if (!form.id || !form.name || !form.ip) {
    ElMessage.warning('id / 名称 / ip 不能为空')
    return
  }
  submitting.value = true
  try {
    await addServer({ ...form })
    ElMessage.success('新增成功(已过 Raft)')
    form.id = ''
    form.name = ''
    form.ip = ''
    await loadServers()
  } catch (e: any) {
    ElMessage.error('新增失败: ' + (e.response?.data || e.message))
  } finally {
    submitting.value = false
  }
}

onMounted(loadServers)
</script>

<style scoped>
.add-form {
  margin-bottom: 16px;
}
</style>