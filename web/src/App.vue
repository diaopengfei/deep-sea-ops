<template>
  <div class="app">
    <el-card header="服务器列表">
      <!-- 新增表单: 填写后点击"新增"会 POST 到后端, 走 Raft Apply 写入 -->
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
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import axios from 'axios'
import { ElMessage } from 'element-plus'

interface Server {
  id: string
  name: string
  ip: string
  status: string
}

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
    const res = await axios.get('/api/servers')
    servers.value = res.data
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
    // POST 走后端 raft.Apply -> FSM.Apply, 数据过 Raft 一致性流程
    await axios.post('/api/servers', { ...form })
    ElMessage.success('新增成功(已过 Raft)')
    // 清空表单
    form.id = ''
    form.name = ''
    form.ip = ''
    // 刷新列表看新数据
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
.app {
  padding: 20px;
  max-width: 900px;
  margin: 0 auto;
}
.add-form {
  margin-bottom: 16px;
}
</style>