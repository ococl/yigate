<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getTraffic } from '@/api/system'
import LoadError from '@/components/LoadError.vue'
import MobileListSkeleton from '@/components/MobileListSkeleton.vue'
import { useIsMobile } from '@/composables/useIsMobile'
import { useVisiblePolling } from '@/composables/useVisiblePolling'
import type { TrafficEntrySnapshot } from '@/types/api'
import { formatBytes } from '@/utils/format'

const isMobile = useIsMobile()
const mobilePage = ref(1)
const dataSource = ref<TrafficEntrySnapshot[]>([])
// 首次/刷新失败时展示错误态与重试入口，避免与「暂无数据」混淆
const loadError = ref(false)
const enabled = ref(false)
const retentionDays = ref(30)
const loading = ref(false)
const route = useRoute()
const router = useRouter()
const keyword = ref(typeof route.query.q === 'string' ? route.query.q : '')

const columns = [
  { title: '用户', dataIndex: 'user', key: 'user', width: 140 },
  { title: '上游', dataIndex: 'upstream', key: 'upstream', width: 160 },
  { title: '目标', dataIndex: 'target', key: 'target', ellipsis: true },
  { title: '流入', dataIndex: 'bytes_in', key: 'bytes_in', width: 120 },
  { title: '流出', dataIndex: 'bytes_out', key: 'bytes_out', width: 120 },
  { title: '合计', dataIndex: 'total', key: 'total', width: 120, sorter: (a: TrafficEntrySnapshot, b: TrafficEntrySnapshot) => a.total - b.total },
]

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

const mobilePageSize = 20
const mobileItems = computed(() =>
  filtered.value.slice((mobilePage.value - 1) * mobilePageSize, mobilePage.value * mobilePageSize),
)

watch(keyword, (value) => {
  mobilePage.value = 1
  // 搜索词写入地址栏，刷新或返回时保留过滤条件
  router.replace({ query: { ...route.query, q: value || undefined, page: undefined } })
})

// 页码越界（手改 URL、搜索后条数变少、保留期清理）时会看到空表格，回落到最后一页
watch([() => filtered.value.length, () => route.query.size], () => {
  const size = Number(route.query.size) || 20
  const maxPage = Math.max(1, Math.ceil(filtered.value.length / size))
  const current = Number(route.query.page) || 1
  if (current > maxPage) {
    router.replace({ query: { ...route.query, page: maxPage > 1 ? String(maxPage) : undefined } })
  }
  const maxMobile = Math.max(1, Math.ceil(filtered.value.length / mobilePageSize))
  if (mobilePage.value > maxMobile) mobilePage.value = maxMobile
})

const tablePagination = computed(() => ({
  current: Number(route.query.page) || 1,
  pageSize: Number(route.query.size) || 20,
  pageSizeOptions: ['20', '50', '100'],
  showSizeChanger: true,
  showTotal: (total: number) => `共 ${total} 条`,
  onChange: (page: number, size: number) => {
    router.replace({ query: { ...route.query, page: String(page), size: String(size) } })
  },
}))

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
    const res = await getTraffic({ silent: !showLoading })
    enabled.value = !!res.enabled
    retentionDays.value = res.retention_days || 30
    dataSource.value = res.entries || []
    loadError.value = false
  } catch {
    // 具体错误已由请求拦截器提示，这里只切换列表区域的状态
    loadError.value = true
  } finally {
    // 轮询不碰 loading，否则会把手动刷新的 loading 提前清掉
    if (showLoading) loading.value = false
  }
}

fetchData()
useVisiblePolling(() => fetchData(false), 15000)
</script>

<template>
  <div class="page-shell">
    <div class="page-header">
      <div>
        <a-typography-title :level="4" style="margin: 0;">流量审计</a-typography-title>
        <a-typography-text type="secondary">
          按用户 / 上游 / 目标聚合最近 {{ retentionDays }} 天的流量，每 15 秒自动刷新。
        </a-typography-text>
      </div>
      <a-space wrap class="traffic-actions">
        <a-input
          v-model:value="keyword"
          class="traffic-search"
          placeholder="搜索用户、上游或目标"
          allow-clear
          aria-label="搜索用户、上游或目标"
        />
        <a-button @click="() => fetchData()" :loading="loading">刷新</a-button>
      </a-space>
    </div>

    <a-card class="fill-card">
      <a-alert
        v-if="!enabled"
        class="fill-card-toolbar"
        type="warning"
        show-icon
        style="margin-bottom: 16px;"
        message="流量审计未启用"
      >
        <template #description>
          未开启时不再记录新流量，已有历史数据仍会保留。请前往
          <RouterLink to="/settings" class="link">系统设置</RouterLink>
          开启「流量审计」。
        </template>
      </a-alert>

      <a-row class="fill-card-toolbar" :gutter="16" style="margin-bottom: 16px;">
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

      <div v-if="isMobile" class="mobile-list">
        <LoadError v-if="loadError && dataSource.length === 0" :retrying="loading" @retry="() => fetchData()" />
        <MobileListSkeleton v-else-if="loading && mobileItems.length === 0" />
        <a-empty
          v-else-if="mobileItems.length === 0"
          :description="enabled ? '暂无流量统计数据' : '流量审计未启用，暂无数据'"
        >
          <RouterLink v-if="!enabled" to="/settings?section=quota">
            <a-button type="primary" size="small">前往系统设置</a-button>
          </RouterLink>
        </a-empty>
        <a-card v-for="record in mobileItems" :key="`${record.user}|${record.upstream}|${record.target}`" size="small" class="mobile-card">
          <div class="mobile-card-head">
            <a-space size="small" wrap>
              <a-tag v-if="record.user">{{ record.user }}</a-tag>
              <span v-else class="muted">匿名</span>
              <a-tag color="purple">{{ record.upstream || '—' }}</a-tag>
            </a-space>
            <a-typography-text strong>{{ formatBytes(record.total) }}</a-typography-text>
          </div>
          <div class="mobile-card-row">
            <span class="mobile-label">目标</span>
            <span class="mobile-value">{{ record.target }}</span>
          </div>
          <div class="mobile-card-row">
            <span class="mobile-label">流入/流出</span>
            <span class="mobile-value">
              {{ formatBytes(record.bytes_in) }} / {{ formatBytes(record.bytes_out) }}
            </span>
          </div>
        </a-card>
        <a-pagination
          v-if="filtered.length > mobilePageSize"
          v-model:current="mobilePage"
          :total="filtered.length"
          :page-size="mobilePageSize"
          size="small"
          show-less-items
          class="mobile-pagination"
        />
      </div>

      <div v-else class="fill-table">
        <a-table
          :data-source="filtered"
          :columns="columns"
          :loading="loading"
          :row-key="(r: TrafficEntrySnapshot) => `${r.user}|${r.upstream}|${r.target}`"
          :pagination="tablePagination"
          :scroll="{ x: 760, y: 1 }"
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
          <LoadError v-if="loadError" :retrying="loading" @retry="() => fetchData()" />
          <a-empty v-else :description="enabled ? '暂无流量统计数据' : '流量审计未启用，暂无数据'">
            <RouterLink v-if="!enabled" to="/settings?section=quota">
              <a-button type="primary" size="small">前往系统设置</a-button>
            </RouterLink>
          </a-empty>
        </template>
        </a-table>
      </div>
    </a-card>
  </div>
</template>

<style scoped>
.traffic-search {
  width: 240px;
}

@media (max-width: 768px) {
  .traffic-actions {
    width: 100%;
  }

  .traffic-search {
    width: 100%;
  }
}

.mobile-pagination {
  justify-content: center;
  margin-top: 4px;
}
</style>
