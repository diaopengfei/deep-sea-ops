<template>
  <!-- 未登录: 显示登录页 -->
  <LoginView v-if="!isLoggedIn" @login-success="onLoginSuccess" />
  <!-- 已登录: 主布局 -->
  <el-container v-else class="layout">
    <el-aside width="210px" class="sidebar">
      <div class="brand">
        <el-icon :size="22" color="#409eff"><Monitor /></el-icon>
        <span class="brand-name">deepsea-ops</span>
      </div>
      <el-menu :default-active="activeMenu" class="nav">
        <el-menu-item index="servers" @click="activeMenu = 'servers'">
          <el-icon><Coin /></el-icon><span>服务器管理</span>
        </el-menu-item>
        <el-menu-item index="agents" @click="activeMenu = 'agents'">
          <el-icon><Connection /></el-icon><span>Agent 节点</span>
        </el-menu-item>
        <el-menu-item index="projects" @click="activeMenu = 'projects'">
          <el-icon><FolderOpened /></el-icon><span>项目扫描</span>
        </el-menu-item>
        <el-menu-item index="config" @click="activeMenu = 'config'">
          <el-icon><Setting /></el-icon><span>配置管理</span>
        </el-menu-item>
        <el-menu-item index="cluster" disabled>
          <el-icon><Share /></el-icon><span>集群拓扑</span>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <el-container>
      <el-header class="header">
        <div class="header-title">{{ pageTitle }}</div>
        <div class="header-right">
          <el-tag type="success" size="small" effect="dark">Raft Leader</el-tag>
          <span class="user-info">{{ currentUser }}</span>
          <el-button text size="small" @click="onLogout">
            <el-icon><SwitchButton /></el-icon> 退出
          </el-button>
        </div>
      </el-header>
      <el-main class="main">
        <ServerListView v-if="activeMenu === 'servers'" />
        <AgentListView v-else-if="activeMenu === 'agents'" />
        <ProjectScanView v-else-if="activeMenu === 'projects'" />
        <ConfigDiffView v-else-if="activeMenu === 'config'" />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { Monitor, Coin, Connection, Share, Setting, SwitchButton, FolderOpened } from '@element-plus/icons-vue'
import LoginView from './views/LoginView.vue'
import ServerListView from './views/ServerListView.vue'
import AgentListView from './views/AgentListView.vue'
import ProjectScanView from './views/ProjectScanView.vue'
import ConfigDiffView from './views/ConfigDiffView.vue'
import { getToken, removeToken, getCurrentUser } from './api/auth'

const isLoggedIn = ref(false)
const currentUser = ref('')
const activeMenu = ref('servers')

const pageTitle = computed(() => {
  const map: Record<string, string> = {
    servers: '服务器管理',
    agents: 'Agent 节点',
    projects: '项目扫描',
    config: '配置管理',
    cluster: '集群拓扑',
  }
  return map[activeMenu.value] || ''
})

onMounted(() => {
  const token = getToken()
  if (token) {
    isLoggedIn.value = true
    currentUser.value = getCurrentUser() || 'admin'
  }
})

function onLoginSuccess(username: string) {
  isLoggedIn.value = true
  currentUser.value = username
}

function onLogout() {
  removeToken()
  isLoggedIn.value = false
  currentUser.value = ''
}
</script>

<style scoped>
.layout { height: 100vh; }
.sidebar { background: #304156; display: flex; flex-direction: column; }
.brand { display: flex; align-items: center; gap: 8px; padding: 16px; color: #fff; font-size: 16px; font-weight: 600; }
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