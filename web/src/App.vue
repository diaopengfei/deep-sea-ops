<template>
  <el-container class="layout">
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
        <el-menu-item index="config" disabled>
          <el-icon><Document /></el-icon><span>配置管理</span>
        </el-menu-item>
        <el-menu-item index="topology" disabled>
          <el-icon><Share /></el-icon><span>拓扑视图</span>
        </el-menu-item>
        <el-menu-item index="settings" disabled>
          <el-icon><Setting /></el-icon><span>系统设置</span>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <el-container>
      <el-header class="topbar">
        <div class="page-title">{{ pageTitle }}</div>
        <div class="cluster-badge">
          <el-tag type="success" effect="plain" size="small">
            <el-icon><Connection /></el-icon> Raft 单节点
          </el-tag>
        </div>
      </el-header>

      <el-main class="main">
        <ServerListView v-if="activeMenu === 'servers'" />
        <AgentListView v-else-if="activeMenu === 'agents'" />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { Monitor, Coin, Document, Share, Setting, Connection } from '@element-plus/icons-vue'
import ServerListView from './views/ServerListView.vue'
import AgentListView from './views/AgentListView.vue'

const activeMenu = ref('servers')

const titleMap: Record<string, string> = {
  servers: '服务器管理',
  agents: 'Agent 节点',
  config: '配置管理',
  topology: '拓扑视图',
  settings: '系统设置'
}
const pageTitle = computed(() => titleMap[activeMenu.value] || '')
</script>

<style scoped>
.layout { height: 100vh; }
.sidebar { background: #f5f7fa; border-right: 1px solid #e4e7ed; display: flex; flex-direction: column; }
.brand { height: 56px; display: flex; align-items: center; gap: 10px; padding: 0 18px; border-bottom: 1px solid #e4e7ed; }
.brand-name { font-size: 16px; font-weight: 600; color: #303133; }
.nav { border-right: none; flex: 1; }
.topbar { height: 56px; background: #fff; border-bottom: 1px solid #e4e7ed; display: flex; align-items: center; justify-content: space-between; padding: 0 24px; }
.page-title { font-size: 16px; font-weight: 600; color: #303133; }
.cluster-badge .el-icon { vertical-align: -1px; margin-right: 2px; }
.main { background: #f0f2f5; padding: 20px 24px; overflow-y: auto; }
</style>