<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { getTraffic } from '@/api/system'
import type { TrafficEntrySnapshot } from '@/types/api'

const dataSource = ref<TrafficEntrySnapshot[]>([])
const loading = ref(false)

const columns = [
  { title: '用户', dataIndex: 'user', key: 'user' },
  { title: '上游', dataIndex: 'upstream', key: 'upstream' },
  { title: '目标', dataIndex: 'target', key: 'target', ellipsis: true },
  { title: '流入 (B)', dataIndex: 'bytes_in', key: 'bytes_in', width: 120 },
  { title: '流出 (B)', dataIndex: 'bytes_out', key: 'bytes_out', width: 120 },
  { title: '合计 (B)', dataIndex: 'total', key: 'total', width: 120 },
]

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

async function fetchData() {
  loading.value = true
  try {
    const res = await getTraffic()
    dataSource.value = res.entries || []
  } finally {
    loading.value = false
  }
}

onMounted(fetchData)
</script>

<template>
  <a-page-header title="流量审计" style="padding: 0 0 16px 0;">
    <template #extra>
      <a-button @click="fetchData" :loading="loading">刷新</a-button>
    </template>
  </a-page-header>

  <a-table
    :data-source="dataSource"
    :columns="columns"
    :loading="loading"
    row-key="target"
    :pagination="{ pageSize: 20 }"
  >
    <template #bodyCell="{ column, record }">
      <template v-if="column.key === 'bytes_in'">
        {{ formatBytes(record.bytes_in) }}
      </template>
      <template v-else-if="column.key === 'bytes_out'">
        {{ formatBytes(record.bytes_out) }}
      </template>
      <template v-else-if="column.key === 'total'">
        {{ formatBytes(record.total) }}
      </template>
    </template>
  </a-table>
</template>
