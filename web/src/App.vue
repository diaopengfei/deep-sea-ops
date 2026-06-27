<template>
  <!-- 未登录: 显示登录页 -->
  <LoginView v-if="!isLoggedIn" @login-success="onLoginSuccess" />
  <!-- 已登录: 主布局 -->
  <el-container v-else class="layout">
    <el-aside width="210px" class="sidebar">
      <div class="brand">
        <img src="/favicon.svg" alt="logo" class="brand-logo" />
        <span class="brand-name">deepsea-ops</span>
      </div>
      <el-menu :default-active="activeMenu" class="nav">
        <el-menu-item index="servers" @click="activeMenu = 'servers'">
          <el-icon><Coin /></el-icon><span>服务器管理</span>
        </el-menu-item>
        <el-menu-item index="ops-nodes" @click="activeMenu = 'ops-nodes'">
          <el-icon><Connection /></el-icon><span>ops 服务节点</span>
        </el-menu-item>
        <el-menu-item index="deploy" @click="activeMenu = 'deploy'">
          <el-icon><Promotion /></el-icon><span>扩容迁移</span>
        </el-menu-item>
        <el-menu-item index="credentials" @click="activeMenu = 'credentials'">
          <el-icon><Key /></el-icon><span>SSH 凭据</span>
        </el-menu-item>
        <el-menu-item index="cluster" @click="activeMenu = 'cluster'">
          <el-icon><Share /></el-icon><span>集群拓扑</span>
        </el-menu-item>
        <el-menu-item index="audit" @click="activeMenu = 'audit'">
          <el-icon><Document /></el-icon><span>操作日志</span>
        </el-menu-item>
        <!-- v0.6.9: 用户管理仅 admin 可见 -->
        <el-menu-item v-if="currentRole === 'admin'" index="users" @click="activeMenu = 'users'">
          <el-icon><User /></el-icon><span>用户管理</span>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <el-container>
      <el-header class="header">
        <div class="header-title">{{ pageTitle }}</div>
        <div class="header-right">
          <el-tag type="success" size="small" effect="dark">Raft Leader</el-tag>
          <el-tag :type="roleTagType" size="small" effect="plain">{{ roleLabel }}</el-tag>
          <span class="user-info">{{ currentUser }}</span>
          <el-button text size="small" @click="onLogout">
            <el-icon><SwitchButton /></el-icon> 退出
          </el-button>
        </div>
      </el-header>
      <el-main class="main">
        <ServerListView v-if="activeMenu === 'servers'" />
        <OpsNodeListView v-else-if="activeMenu === 'ops-nodes'" />
        <DeployView v-else-if="activeMenu === 'deploy'" />
        <CredentialsView v-else-if="activeMenu === 'credentials'" />
        <ClusterTopologyView v-else-if="activeMenu === 'cluster'" />
        <AuditLogView v-else-if="activeMenu === 'audit'" />
        <UserListView v-else-if="activeMenu === 'users'" />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { Coin, Connection, Share, SwitchButton, Promotion, Key, Document, User } from '@element-plus/icons-vue'
import LoginView from './views/LoginView.vue'
import ServerListView from './views/ServerListView.vue'
import OpsNodeListView from './views/OpsNodeListView.vue'
import DeployView from './views/DeployView.vue'
import CredentialsView from './views/CredentialsView.vue'
import ClusterTopologyView from './views/ClusterTopologyView.vue'
import AuditLogView from './views/AuditLogView.vue'
import UserListView from './views/UserListView.vue'
import { getToken, removeToken, getCurrentUser, getCurrentRole, fetchMe } from './api/auth'

const isLoggedIn = ref(false)
const currentUser = ref('')
const currentRole = ref('viewer')
const activeMenu = ref('servers')

const pageTitle = computed(() => {
  const map: Record<string, string> = {
    servers: '服务器管理',
    'ops-nodes': 'ops 服务节点',
    deploy: '扩容迁移',
    credentials: 'SSH 凭据',
    cluster: '集群拓扑',
    audit: '操作日志',
    users: '用户管理',
  }
  return map[activeMenu.value] || ''
})

// v0.6.9: 角色标签
const roleLabel = computed(() => currentRole.value)
const roleTagType = computed<'danger' | 'primary' | 'info'>(() => {
  if (currentRole.value === 'admin') return 'danger'
  if (currentRole.value === 'operator') return 'primary'
  return 'info'
})

onMounted(async () => {
  const token = getToken()
  if (token) {
    isLoggedIn.value = true
    currentUser.value = getCurrentUser()
    currentRole.value = getCurrentRole()
    // v0.6.9: 兜底拉取 /api/auth/me 恢复角色(刷新页面后 localStorage 可能缺失 role)
    try {
      const me = await fetchMe()
      currentUser.value = me.username
      currentRole.value = me.role
      // 非 admin 默认进服务器列表, 避免 viewer 默认进无权限页
      if (currentRole.value !== 'admin' && activeMenu.value === 'users') {
        activeMenu.value = 'servers'
      }
    } catch {
      // token 失效时 401 拦截器会清 token 刷新页面
    }
  }
})

function onLoginSuccess(username: string) {
  isLoggedIn.value = true
  currentUser.value = username
  currentRole.value = getCurrentRole()
}

function onLogout() {
  removeToken()
  isLoggedIn.value = false
  currentUser.value = ''
  currentRole.value = 'viewer'
}
</script>

<style scoped>
.layout { height: 100vh; }
.sidebar { background: #304156; display: flex; flex-direction: column; }
.brand { display: flex; align-items: center; gap: 8px; padding: 16px; color: #fff; font-size: 16px; font-weight: 600; }
.brand-logo { width: 26px; height: 26px; border-radius: 6px; flex-shrink: 0; }
.brand-name { letter-spacing: 0.5px; }
.nav { border-right: none; background: transparent; }
:deep(.nav .el-menu-item) { color: #bfcbd9; }
:deep(.nav .el-menu-item.is-active) { color: #fff; background: #263445; }
:deep(.nav .el-menu-item:hover) { background: #263445; }
.header { background: #fff; border-bottom: 1px solid #e6e6e6; display: flex; align-items: center; justify-content: space-between; padding: 0 20px; }
.header-title { font-size: 18px; font-weight: 600; color: #303133; }
.header-right { display: flex; align-items: center; gap: 12px; }
.user-info { color: #606266; font-size: 14px; }
.main { background: #f0f2f5; padding: 20px; }
</style>
