<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { getTraffic } from '@/api/system'
import type { TrafficEntrySnapshot } from '@/types/api'

const dataSource = ref<TrafficEntrySnapshot[]>([])
const enabled = ref(false)
const loading = ref(false)
const keyword = ref('')
let timer: number | undefined

const columns = [
  { title: '用户', dataIndex: 'user', key: 'user', width: 140 },
  { title: '上游', dataIndex: 'upstream', key: 'upstream', width: 160 },
  { title: '目标', dataIndex: 'target', key: 'target', ellipsis: true },
  { title: '流入', dataIndex: 'bytes_in', key: 'bytes_in', width: 120 },
  { title: '流出', dataIndex: 'bytes_out', key: 'bytes_out', width: 120 },
  { title: '合计', dataIndex: 'total', key: 'total', width: 120, sorter: (a: TrafficEntrySnapshot, b: TrafficEntrySnapshot) => a.total - b.total },
]

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1)
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return dataSource.value
  return dataSource.value.filter(
    (e) =>
      e.user.toLowerCase().includes(kw) ||
      e.upstream.toLowerCase().includes(kw) ||
      e.target.toLowerCase().includes(kw),
  )
})

const summary = computed(() => {
  let total = 0
  const users = new Map<string, number>()
  const upstreams = new Map<string, number>()
  for (const e of dataSource.value) {
    total += e.total
    users.set(e.user, (users.get(e.user) || 0) + e.total)
    upstreams.set(e.upstream, (upstreams.get(e.upstream) || 0) + e.total)
  }
  const topUser = [...users.entries()].sort((a, b) => b[1] - a[1])[0]
  const topUpstream = [...upstreams.entries()].sort((a, b) => b[1] - a[1])[0]
  return {
    total,
    entries: dataSource.value.length,
    userCount: users.size,
    topUser: topUser ? `${topUser[0]}（${formatBytes(topUser[1])}）` : '—',
    topUpstream: topUpstream ? `${topUpstream[0]}（${formatBytes(topUpstream[1])}）` : '—',
  }
})

async function fetchData(showLoading = true) {
  if (showLoading) loading.value = true
  try {
    const res = await getTraffic()
    enabled.value = !!res.enabled
    dataSource.value = res.entries || []
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchData()
  timer = window.setInterval(() => fetchData(false), 15000)
})

onUnmounted(() => {
  if (timer !== undefined) window.clearInterval(timer)
})
</script>

<template>
  <div class="page-header">
    <div>
      <a-typography-title :level="4" style="margin: 0;">流量审计</a-typography-title>
      <a-typography-text type="secondary">
        按用户 / 上游 / 目标聚合的流量统计，每 15 秒自动刷新。
      </a-typography-text>
    </div>
    <a-space>
      <a-input-search v-model:value="keyword" placeholder="搜索用户、上游或目标" allow-clear style="width: 240px;" />
      <a-button @click="() => fetchData()" :loading="loading">刷新</a-button>
    </a-space>
  </div>

  <a-alert
    v-if="!enabled"
    type="warning"
    show-icon
    style="margin-bottom: 16px;"
    message="流量审计未启用"
  >
    <template #description>
      未开启时不会统计任何流量，页面将始终为空。请前往
      <RouterLink to="/settings" class="link">系统设置</RouterLink>
      开启「流量审计」。
    </template>
  </a-alert>

  <a-row :gutter="16" style="margin-bottom: 16px;">
    <a-col :xs="24" :sm="12" :lg="6">
      <a-card size="small">
        <a-statistic title="总流量" :value="formatBytes(summary.total)" />
      </a-card>
    </a-col>
    <a-col :xs="24" :sm="12" :lg="6">
      <a-card size="small">
        <a-statistic title="统计条目" :value="summary.entries" :suffix="`条 / ${summary.userCount} 个用户`" />
      </a-card>
    </a-col>
    <a-col :xs="24" :sm="12" :lg="6">
      <a-card size="small">
        <a-statistic title="流量最大用户" :value="summary.topUser" :value-style="{ fontSize: '16px' }" />
      </a-card>
    </a-col>
    <a-col :xs="24" :sm="12" :lg="6">
      <a-card size="small">
        <a-statistic title="流量最大上游" :value="summary.topUpstream" :value-style="{ fontSize: '16px' }" />
      </a-card>
    </a-col>
  </a-row>

  <a-table
    :data-source="filtered"
    :columns="columns"
    :loading="loading"
    :row-key="(r: TrafficEntrySnapshot) => `${r.user}|${r.upstream}|${r.target}`"
    :pagination="{ pageSize: 20, showSizeChanger: true, showTotal: (total: number) => `共 ${total} 条` }"
  >
    <template #bodyCell="{ column, record }">
      <template v-if="column.key === 'user'">
        <a-tag v-if="record.user">{{ record.user }}</a-tag>
        <span v-else class="muted">匿名</span>
      </template>
      <template v-else-if="column.key === 'upstream'">
        <a-tag color="purple">{{ record.upstream || '—' }}</a-tag>
      </template>
      <template v-else-if="column.key === 'bytes_in'">
        {{ formatBytes(record.bytes_in) }}
      </template>
      <template v-else-if="column.key === 'bytes_out'">
        {{ formatBytes(record.bytes_out) }}
      </template>
      <template v-else-if="column.key === 'total'">
        <a-typography-text strong>{{ formatBytes(record.total) }}</a-typography-text>
      </template>
    </template>
    <template #emptyText>
      <a-empty :description="enabled ? '暂无流量统计数据' : '流量审计未启用，暂无数据'" />
    </template>
  </a-table>
</template>
