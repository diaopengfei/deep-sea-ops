<template>
  <div class="config-diff">
    <el-card header="配置比对">
      <el-form :model="form" label-width="140px" label-position="right">
        <el-divider content-position="left">选择 Agent</el-divider>
        <el-form-item label="目标 Agent">
          <el-select v-model="form.agentId" placeholder="选择在线 Agent" style="width: 300px">
            <el-option v-for="a in agents" :key="a.id" :label="a.hostname + ' (' + a.id + ')'" :value="a.id" />
          </el-select>
        </el-form-item>

        <el-divider content-position="left">Nacos 配置源</el-divider>
        <el-form-item label="Nacos 地址">
          <el-input v-model="form.nacosAddr" placeholder="http://192.168.1.10:8848" style="width: 320px" />
        </el-form-item>
        <el-form-item label="Data ID">
          <el-input v-model="form.nacosDataId" placeholder="service-a.yml" style="width: 240px" />
        </el-form-item>
        <el-form-item label="Group">
          <el-input v-model="form.nacosGroup" placeholder="DEFAULT_GROUP" style="width: 240px" />
        </el-form-item>
        <el-form-item label="命名空间">
          <el-input v-model="form.nacosNamespace" placeholder="留空=public" style="width: 240px" />
        </el-form-item>
        <el-form-item label="用户名">
          <el-input v-model="form.nacosUsername" placeholder="Nacos 开启鉴权时填写" style="width: 200px" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="form.nacosPassword" type="password" show-password placeholder="Nacos 开启鉴权时填写" style="width: 200px" />
        </el-form-item>
        <el-form-item label="AccessToken">
          <el-input v-model="form.nacosAccessToken" placeholder="已有 token 可直接填, 跳过登录" style="width: 320px" />
        </el-form-item>

        <el-divider content-position="left">本地配置文件</el-divider>
        <el-form-item label="文件路径">
          <el-input v-model="form.localPath" placeholder="/opt/app/application.yml" style="width: 400px" />
        </el-form-item>

        <el-divider content-position="left">jar 包内配置</el-divider>
        <el-form-item label="jar 路径">
          <el-input v-model="form.jarPath" placeholder="/opt/app/demo.jar" style="width: 400px" />
        </el-form-item>
        <el-form-item label="jar 内 entry">
          <el-input v-model="form.jarEntry" placeholder="BOOT-INF/classes/application.yml" style="width: 400px" />
        </el-form-item>

        <el-form-item>
          <el-button type="primary" :loading="loading" @click="onCompare">
            <el-icon><Search /></el-icon> 开始比对
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card v-if="report" header="比对结果" style="margin-top: 16px">
      <el-alert v-if="report.nacosErr" type="warning" :title="'Nacos 采集失败: ' + report.nacosErr" :closable="false" style="margin-bottom: 8px" />
      <el-alert v-if="report.localErr" type="warning" :title="'本地文件采集失败: ' + report.localErr" :closable="false" style="margin-bottom: 8px" />
      <el-alert v-if="report.jarErr" type="warning" :title="'jar 内配置采集失败: ' + report.jarErr" :closable="false" style="margin-bottom: 8px" />

      <el-row :gutter="16">
        <el-col :span="8">
          <div class="diff-block consistent">
            <div class="diff-title">三方一致 ({{ report.consistent?.length || 0 }})</div>
            <pre>{{ (report.consistent || []).join('\n') }}</pre>
          </div>
        </el-col>
        <el-col :span="8">
          <div class="diff-block only-nacos">
            <div class="diff-title">仅 Nacos ({{ report.onlyNacos?.length || 0 }})</div>
            <pre>{{ (report.onlyNacos || []).join('\n') }}</pre>
          </div>
        </el-col>
        <el-col :span="8">
          <div class="diff-block only-local">
            <div class="diff-title">仅本地 ({{ report.onlyLocal?.length || 0 }})</div>
            <pre>{{ (report.onlyLocal || []).join('\n') }}</pre>
          </div>
        </el-col>
      </el-row>
      <el-row :gutter="16" style="margin-top: 16px">
        <el-col :span="8">
          <div class="diff-block only-jar">
            <div class="diff-title">仅 jar ({{ report.onlyJar?.length || 0 }})</div>
            <pre>{{ (report.onlyJar || []).join('\n') }}</pre>
          </div>
        </el-col>
        <el-col :span="8">
          <div class="diff-block nacos-local">
            <div class="diff-title">Nacos+本地 ({{ report.nacosLocal?.length || 0 }})</div>
            <pre>{{ (report.nacosLocal || []).join('\n') }}</pre>
          </div>
        </el-col>
        <el-col :span="8">
          <div class="diff-block local-jar">
            <div class="diff-title">本地+jar ({{ report.localJar?.length || 0 }})</div>
            <pre>{{ (report.localJar || []).join('\n') }}</pre>
          </div>
        </el-col>
      </el-row>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { listAgents } from '../api/server'
import { configDiff } from '../api/config'
import type { AgentInfo } from '../api/types'
import type { DiffReport } from '../api/config'

const agents = ref<AgentInfo[]>([])
const loading = ref(false)
const report = ref<DiffReport | null>(null)

const form = reactive({
  agentId: '',
  nacosAddr: '',
  nacosDataId: '',
  nacosGroup: 'DEFAULT_GROUP',
  nacosUsername: '',
  nacosPassword: '',
  nacosAccessToken: '',
  localPath: '',
  jarPath: '',
  jarEntry: 'BOOT-INF/classes/application.yml'
})

async function loadAgents() {
  try {
    agents.value = await listAgents()
  } catch (e: any) {
    ElMessage.error('加载 Agent 列表失败')
  }
}

async function onCompare() {
  if (!form.agentId) {
    ElMessage.warning('请选择 Agent')
    return
  }
  loading.value = true
  report.value = null
  try {
    report.value = await configDiff(form.agentId, form)
    ElMessage.success('比对完成')
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '比对失败')
  } finally {
    loading.value = false
  }
}

onMounted(loadAgents)
</script>

<style scoped>
.config-diff { padding: 0; }
.diff-block {
  border: 1px solid #e4e7ed;
  border-radius: 4px;
  padding: 12px;
  min-height: 200px;
  max-height: 400px;
  overflow: auto;
}
.diff-title {
  font-weight: 600;
  margin-bottom: 8px;
  font-size: 13px;
}
.diff-block pre {
  margin: 0;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
}
.consistent { border-color: #67c23a; }
.consistent .diff-title { color: #67c23a; }
.only-nacos { border-color: #e6a23c; }
.only-nacos .diff-title { color: #e6a23c; }
.only-local { border-color: #409eff; }
.only-local .diff-title { color: #409eff; }
.only-jar { border-color: #f56c6c; }
.only-jar .diff-title { color: #f56c6c; }
.nacos-local { border-color: #909399; }
.nacos-local .diff-title { color: #909399; }
.local-jar { border-color: #909399; }
.local-jar .diff-title { color: #909399; }
</style>