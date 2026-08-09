<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { getHealth, getStatus, getTraffic } from '@/api/system'
import type { HealthStatus, StatusSnapshot, TrafficSnapshot } from '@/types/api'

const health = ref<HealthStatus | null>(null)
const status = ref<StatusSnapshot | null>(null)
const traffic = ref<TrafficSnapshot | null>(null)
const loading = ref(false)

let timer: ReturnType<typeof setInterval> | null = null

async function fetchData() {
  loading.value = true
  try {
    const [h, s, t] = await Promise.all([
      getHealth().catch(() => null),
      getStatus().catch(() => null),
      getTraffic().catch(() => null),
    ])
    health.value = h
    status.value = s
    traffic.value = t
  } finally {
    loading.value = false
  }
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

const totalBytesIn = ref(0)
const totalBytesOut = ref(0)

function updateTotals() {
  if (traffic.value?.entries) {
    totalBytesIn.value = traffic.value.entries.reduce((sum, e) => sum + e.bytes_in, 0)
    totalBytesOut.value = traffic.value.entries.reduce((sum, e) => sum + e.bytes_out, 0)
  } else {
    totalBytesIn.value = 0
    totalBytesOut.value = 0
  }
}

async function refresh() {
  await fetchData()
  updateTotals()
}

onMounted(() => {
  refresh()
  timer = setInterval(refresh, 10000)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <a-spin :spinning="loading">
    <div style="display: flex; align-items: baseline; gap: 12px; padding: 0 0 16px 0;">
      <a-typography-title :level="4" style="margin: 0;">仪表盘</a-typography-title>
      <a-typography-text type="secondary">网关实时状态概览</a-typography-text>
    </div>

    <!-- 状态卡片 -->
    <a-row :gutter="16" style="margin-bottom: 16px;">
      <a-col :span="6">
        <a-card size="small">
          <a-statistic
            title="运行状态"
            :value="health?.status || '-'"
            :value-style="{ color: health?.status === 'up' ? 'var(--ant-color-success, #3f8600)' : 'var(--ant-color-error, #cf1322)' }"
          />
        </a-card>
      </a-col>
      <a-col :span="6">
        <a-card size="small">
          <a-statistic title="上游数量" :value="health?.upstream_count ?? 0" />
        </a-card>
      </a-col>
      <a-col :span="6">
        <a-card size="small">
          <a-statistic
            title="路由就绪"
            :value="health?.router_ready ? '是' : '否'"
            :value-style="{ color: health?.router_ready ? 'var(--ant-color-success, #3f8600)' : 'var(--ant-color-error, #cf1322)' }"
          />
        </a-card>
      </a-col>
      <a-col :span="6">
        <a-card size="small">
          <a-statistic
            title="管理就绪"
            :value="health?.manager_ready ? '是' : '否'"
            :value-style="{ color: health?.manager_ready ? 'var(--ant-color-success, #3f8600)' : 'var(--ant-color-error, #cf1322)' }"
          />
        </a-card>
      </a-col>
    </a-row>

    <!-- 监听信息 -->
    <a-row :gutter="16" style="margin-bottom: 16px;">
      <a-col :span="8">
        <a-card size="small" title="代理监听">
          <a-typography-text code>{{ status?.listen || '-' }}</a-typography-text>
        </a-card>
      </a-col>
      <a-col :span="8">
        <a-card size="small" title="管理端口">
          <a-typography-text code>{{ status?.management || '-' }}</a-typography-text>
        </a-card>
      </a-col>
      <a-col :span="8">
        <a-card size="small" title="pprof">
          <a-tag :color="status?.pprof_enabled ? 'green' : 'default'">
            {{ status?.pprof_enabled ? '已启用' : '未启用' }}
          </a-tag>
        </a-card>
      </a-col>
    </a-row>

    <!-- 流量概览 -->
    <a-card title="流量概览" size="small">
      <a-row :gutter="16">
        <a-col :span="8">
          <a-statistic
            title="总流入"
            :value="formatBytes(totalBytesIn)"
            :value-style="{ color: 'var(--ant-color-success, #3f8600)' }"
          />
        </a-col>
        <a-col :span="8">
          <a-statistic
            title="总流出"
            :value="formatBytes(totalBytesOut)"
            :value-style="{ color: '#1677ff' }"
          />
        </a-col>
        <a-col :span="8">
          <a-statistic
            title="合计"
            :value="formatBytes(totalBytesIn + totalBytesOut)"
          />
        </a-col>
      </a-row>
      <a-divider style="margin: 12px 0;" />
      <a-typography-text type="secondary">
        流量条目数：{{ traffic?.entries?.length || 0 }} 条
      </a-typography-text>
    </a-card>
  </a-spin>
</template>
