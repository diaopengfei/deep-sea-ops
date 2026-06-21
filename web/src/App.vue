<template>
  <el-container class="layout">
    <!-- 侧边栏: 品牌 + 导航菜单 -->
    <el-aside width="210px" class="sidebar">
      <div class="brand">
        <el-icon :size="22" color="#409eff"><Monitor /></el-icon>
        <span class="brand-name">deepsea-ops</span>
      </div>
      <el-menu :default-active="activeMenu" class="nav">
        <el-menu-item index="servers" @click="activeMenu = 'servers'">
          <el-icon><Coin /></el-icon>
          <span>服务器管理</span>
        </el-menu-item>
        <el-menu-item index="config" disabled>
          <el-icon><Document /></el-icon>
          <span>配置管理</span>
        </el-menu-item>
        <el-menu-item index="topology" disabled>
          <el-icon><Share /></el-icon>
          <span>拓扑视图</span>
        </el-menu-item>
        <el-menu-item index="settings" disabled>
          <el-icon><Setting /></el-icon>
          <span>系统设置</span>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <el-container>
      <!-- 顶栏: 当前页标题 + 集群状态 -->
      <el-header class="topbar">
        <div class="page-title">服务器管理</div>
        <div class="cluster-badge">
          <el-tag type="success" effect="plain" size="small">
            <el-icon><Connection /></el-icon> Raft 单节点
          </el-tag>
        </div>
      </el-header>

      <!-- 主内容区 -->
      <el-main class="main">
        <ServerListView v-if="activeMenu === 'servers'" />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { Monitor, Coin, Document, Share, Setting, Connection } from '@element-plus/icons-vue'
import ServerListView from './views/ServerListView.vue'

// 当前激活的菜单项。现在只有 servers 可用, 其余是未来功能的占位。
const activeMenu = ref('servers')
</script>

<style scoped>
.layout {
  height: 100vh;
}

/* 侧边栏: 浅灰底, 与主内容区形成层次 */
.sidebar {
  background: #f5f7fa;
  border-right: 1px solid #e4e7ed;
  display: flex;
  flex-direction: column;
}

.brand {
  height: 56px;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 18px;
  border-bottom: 1px solid #e4e7ed;
}

.brand-name {
  font-size: 16px;
  font-weight: 600;
  color: #303133;
  letter-spacing: 0;
}

.nav {
  border-right: none;
  flex: 1;
}

/* 顶栏 */
.topbar {
  height: 56px;
  background: #fff;
  border-bottom: 1px solid #e4e7ed;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
}

.page-title {
  font-size: 16px;
  font-weight: 600;
  color: #303133;
}

.cluster-badge .el-icon {
  vertical-align: -1px;
  margin-right: 2px;
}

/* 主内容区: 浅底, 内部用白面板承载数据 */
.main {
  background: #f0f2f5;
  padding: 20px 24px;
  overflow-y: auto;
}
</style>