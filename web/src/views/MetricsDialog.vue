<template>
  <el-dialog
    v-model="visible"
    :title="`资源监控 - ${agentId}`"
    width="900px"
    @open="onOpen"
    @closed="onClosed"
  >
    <div v-loading="loading">
      <!-- 最新指标卡片 -->
      <div v-if="latest" class="metric-cards">
        <div class="metric-card">
          <div class="metric-label">CPU</div>
          <div class="metric-value" :class="levelClass(latest.metrics.cpu.percent)">
            {{ latest.metrics.cpu.percent.toFixed(1) }}%
          </div>
        </div>
        <div class="metric-card">
          <div class="metric-label">内存</div>
          <div class="metric-value" :class="levelClass(latest.metrics.memory.percent)">
            {{ latest.metrics.memory.percent.toFixed(1) }}%
          </div>
          <div class="metric-sub">{{ formatBytes(latest.metrics.memory.used) }} / {{ formatBytes(latest.metrics.memory.total) }}</div>
        </div>
        <div class="metric-card">
          <div class="metric-label">磁盘 ({{ latest.metrics.disk.path }})</div>
          <div class="metric-value" :class="levelClass(latest.metrics.disk.percent)">
            {{ latest.metrics.disk.percent.toFixed(1) }}%
          </div>
          <div class="metric-sub">{{ formatBytes(latest.metrics.disk.used) }} / {{ formatBytes(latest.metrics.disk.total) }}</div>
        </div>
        <div class="metric-card">
          <div class="metric-label">负载 (1/5/15min)</div>
          <div class="metric-value metric-small">
            {{ latest.metrics.load.load1.toFixed(2) }} / {{ latest.metrics.load.load5.toFixed(2) }} / {{ latest.metrics.load.load15.toFixed(2) }}
          </div>
        </div>
      </div>

      <div v-if="latest" class="metric-net">
        网络吞吐 — 入站: {{ formatRate(latest.metrics.net.rxBytesPerSec) }} / 出站: {{ formatRate(latest.metrics.net.txBytesPerSec) }}
      </div>

      <!-- ECharts 曲线 -->
      <div ref="chartRef" class="chart-container"></div>

      <div v-if="history.length === 0 && !loading" class="empty-tip">
        暂无历史数据, 等待采集器写入(每 30 秒一次, 保留约 2 小时)
      </div>
    </div>
    <template #footer>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch, nextTick, onBeforeUnmount } from 'vue'
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent, TitleComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { ElMessage } from 'element-plus'
import { getMetricsLatest, getMetricsHistory, type MetricsSample } from '../api/server'

echarts.use([LineChart, GridComponent, TooltipComponent, LegendComponent, TitleComponent, CanvasRenderer])

const props = defineProps<{ modelValue: boolean; agentId: string }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()

const visible = ref(props.modelValue)
watch(() => props.modelValue, (v) => { visible.value = v })
watch(visible, (v) => emit('update:modelValue', v))

const loading = ref(false)
const latest = ref<MetricsSample | null>(null)
const history = ref<MetricsSample[]>([])
const chartRef = ref<HTMLElement | null>(null)
let chart: echarts.ECharts | null = null
let refreshTimer: number | undefined

function levelClass(v: number): string {
  if (v >= 90) return 'level-critical'
  if (v >= 80) return 'level-warning'
  return 'level-ok'
}

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = bytes
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  return `${v.toFixed(1)} ${units[i]}`
}

function formatRate(bytesPerSec: number): string {
  return formatBytes(bytesPerSec) + '/s'
}

async function onOpen() {
  loading.value = true
  try {
    const [lat, hist] = await Promise.all([
      getMetricsLatest(props.agentId),
      getMetricsHistory(props.agentId),
    ])
    latest.value = lat
    history.value = Array.isArray(hist) ? hist : []
    await nextTick()
    renderChart()
  } catch (e: any) {
    ElMessage.error('加载监控数据失败: ' + (e.response?.data?.error || e.message))
  } finally {
    loading.value = false
  }
  // 每 30s 刷新一次, 与采集周期对齐
  refreshTimer = window.setInterval(refresh, 30000)
}

async function refresh() {
  try {
    const [lat, hist] = await Promise.all([
      getMetricsLatest(props.agentId),
      getMetricsHistory(props.agentId),
    ])
    latest.value = lat
    history.value = Array.isArray(hist) ? hist : []
    renderChart()
  } catch {
    // 静默失败, 避免刷新时弹窗打扰
  }
}

function renderChart() {
  if (!chartRef.value) return
  if (!chart) {
    chart = echarts.init(chartRef.value)
  }
  const times = history.value.map((s) => new Date(s.time).toLocaleTimeString('zh-CN', { hour12: false }))
  const cpu = history.value.map((s) => Number(s.metrics.cpu.percent.toFixed(1)))
  const mem = history.value.map((s) => Number(s.metrics.memory.percent.toFixed(1)))
  const disk = history.value.map((s) => Number(s.metrics.disk.percent.toFixed(1)))

  chart.setOption({
    tooltip: { trigger: 'axis' },
    legend: { data: ['CPU %', '内存 %', '磁盘 %'] },
    grid: { left: 50, right: 20, top: 40, bottom: 30 },
    xAxis: { type: 'category', data: times, boundaryGap: false },
    yAxis: { type: 'value', max: 100, axisLabel: { formatter: '{value}%' } },
    series: [
      { name: 'CPU %', type: 'line', data: cpu, smooth: true, itemStyle: { color: '#409eff' } },
      { name: '内存 %', type: 'line', data: mem, smooth: true, itemStyle: { color: '#67c23a' } },
      { name: '磁盘 %', type: 'line', data: disk, smooth: true, itemStyle: { color: '#e6a23c' } },
    ],
  })
}

function onClosed() {
  if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = undefined }
  if (chart) { chart.dispose(); chart = null }
  latest.value = null
  history.value = []
}

onBeforeUnmount(() => {
  if (chart) chart.dispose()
  if (refreshTimer) clearInterval(refreshTimer)
})
</script>

<style scoped>
.metric-cards {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
  margin-bottom: 12px;
}

.metric-card {
  background: #f5f7fa;
  border-radius: 6px;
  padding: 12px;
  text-align: center;
}

.metric-label {
  font-size: 12px;
  color: #909399;
  margin-bottom: 6px;
}

.metric-value {
  font-size: 22px;
  font-weight: 600;
  line-height: 1.2;
}

.metric-small {
  font-size: 14px;
}

.metric-sub {
  font-size: 11px;
  color: #909399;
  margin-top: 4px;
}

.level-ok { color: #67c23a; }
.level-warning { color: #e6a23c; }
.level-critical { color: #f56c6c; }

.metric-net {
  font-size: 13px;
  color: #606266;
  margin-bottom: 12px;
}

.chart-container {
  width: 100%;
  height: 360px;
}

.empty-tip {
  text-align: center;
  color: #909399;
  padding: 40px 0;
  font-size: 13px;
}
</style>
