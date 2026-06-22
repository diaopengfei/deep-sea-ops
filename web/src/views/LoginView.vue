<template>
  <div class="login-container">
    <el-card class="login-card">
      <template #header>
        <div class="login-header">
          <h2>DeepSea Ops</h2>
          <p>分布式服务器运维平台</p>
        </div>
      </template>
      <el-form :model="form" @keyup.enter="onLogin" v-loading="loading">
        <el-form-item>
          <el-input v-model="form.username" placeholder="用户名" :prefix-icon="User" size="large" />
        </el-form-item>
        <el-form-item>
          <el-input v-model="form.password" type="password" placeholder="密码" :prefix-icon="Lock" size="large" show-password />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" size="large" style="width: 100%" :loading="loading" @click="onLogin">登录</el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { User, Lock } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { login, setToken } from '../api/auth'

// 通知父组件登录成功, 由父组件切换到主界面
const emit = defineEmits<{ (e: 'success'): void }>()

const loading = ref(false)
const form = reactive({ username: '', password: '' })

async function onLogin() {
  if (!form.username || !form.password) {
    ElMessage.warning('请输入用户名和密码')
    return
  }
  loading.value = true
  try {
    const resp = await login(form.username, form.password)
    setToken(resp.accessToken, resp.refreshToken)
    ElMessage.success('登录成功')
    emit('success')
  } catch (e: any) {
    const msg = e.response?.data?.error || e.message || '登录失败'
    ElMessage.error(msg)
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-container {
  height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #1a2a3a 0%, #2c3e50 100%);
}
.login-card {
  width: 380px;
}
.login-header {
  text-align: center;
}
.login-header h2 {
  margin: 0 0 8px;
  color: #303133;
}
.login-header p {
  margin: 0;
  color: #909399;
  font-size: 13px;
}
</style>