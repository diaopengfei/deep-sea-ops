<script setup lang="ts">
import { ref, onMounted } from 'vue'
import axios from 'axios'

// 服务器数据类型定义
interface Server {
  id: string
  name: string
  ip: string
  status: string
}

// 响应式数据: 服务器列表, 初始为空数组
const servers = ref<Server[]>([])
const loading = ref(false)

// 组件挂载后加载服务器列表
onMounted(async () => {
  loading.value = true
  try {
    const res = await axios.get('/api/servers')
    servers.value = res.data
  } catch (e) {
    console.error('加载服务器列表失败', e)
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div class="app">
    <h1>服务器管理</h1>
    <el-table :data="servers" v-loading="loading" border style="width: 100%">
      <el-table-column prop="id" label="ID" width="120" />
      <el-table-column prop="name" label="名称" width="200" />
      <el-table-column prop="ip" label="IP 地址" width="200" />
      <el-table-column prop="status" label="状态" width="120">
        <template #default="{ row }">
          <el-tag :type="row.status === 'online' ? 'success' : 'danger'">
            {{ row.status }}
          </el-tag>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<style scoped>
.app {
  max-width: 900px;
  margin: 40px auto;
  padding: 0 20px;
  font-family: system-ui, sans-serif;
}
h1 {
  margin-bottom: 20px;
}
</style>
