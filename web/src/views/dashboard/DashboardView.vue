<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { theme } from 'antdv-next'
import { getHealth, getStatus, getTraffic } from '@/api/system'
import type { HealthStatus, StatusSnapshot, TrafficSnapshot, UpstreamStatus } from '@/types/api'

const health = ref<HealthStatus | null>(null)
const status = ref<StatusSnapshot | null>(null)
const traffic = ref<TrafficSnapshot | null>(null)
const loading = ref(false)
const lastRefreshed = ref<Date | null>(null)

const { token } = theme.useToken()

let timer: ReturnType<typeof setInterval> | null = null

// silent=true 用于定时刷新：避免每次自动刷新都整页 loading 抖动
async function fetchData(silent = false) {
  if (!silent) loading.value = true
  try {
    const [h, s, t] = await Promise.all([
      getHealth().catch(() => null),
      getStatus().catch(() => null),
      getTraffic().catch(() => null),
    ])
    health.value = h
    status.value = s
    traffic.value = t
    lastRefreshed.value = new Date()
  } finally {
    if (!silent) loading.value = false
  }
}

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1)
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

function formatUptime(seconds: number): string {
  if (!seconds || seconds < 0) return '—'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d} 天 ${h} 小时`
  if (h > 0) return `${h} 小时 ${m} 分钟`
  return `${m} 分钟`
}

function formatTime(value?: string): string {
  if (!value) return '—'
  const t = new Date(value)
  if (Number.isNaN(t.getTime()) || t.getTime() <= 0) return '—'
  return t.toLocaleString()
}

const totals = computed(() => {
  const entries = traffic.value?.entries || []
  return {
    in: entries.reduce((sum, e) => sum + e.bytes_in, 0),
    out: entries.reduce((sum, e) => sum + e.bytes_out, 0),
    count: entries.length,
    users: new Set(entries.map((e) => e.user)).size,
  }
})

const upstreams = computed<UpstreamStatus[]>(() => status.value?.upstreams || [])
function checked(u: UpstreamStatus): boolean {
  return !!(u.last_check_at && new Date(u.last_check_at).getTime() > 0)
}
const aliveCount = computed(() => upstreams.value.filter((u) => checked(u) && !u.dead).length)
const deadCount = computed(() => upstreams.value.filter((u) => checked(u) && u.dead).length)
const uncheckedCount = computed(() => upstreams.value.filter((u) => !checked(u)).length)

const statusColor = computed(() => (health.value?.status === 'up' ? token.value.colorSuccess : token.value.colorError))
const readyColor = computed(() =>
  health.value?.router_ready && health.value?.manager_ready ? token.value.colorSuccess : token.value.colorError,
)

function upstreamStatusText(u: UpstreamStatus): string {
  if (!checked(u)) return '未检测'
  return u.dead ? '不可用' : '可用'
}

function upstreamStatusColor(u: UpstreamStatus): string {
  if (!checked(u)) return 'default'
  return u.dead ? 'error' : 'success'
}

const upstreamColumns = [
  { title: '名称', dataIndex: 'name', key: 'name', width: 140 },
  { title: 'URL', dataIndex: 'url', key: 'url', ellipsis: true },
  { title: '状态', key: 'status', width: 110 },
  { title: '失败 / 成功', key: 'counts', width: 130 },
  { title: '最后检查', key: 'last_check', width: 180 },
]

onMounted(() => {
  fetchData()
  timer = setInterval(() => fetchData(true), 10000)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <a-spin :spinning="loading">
    <div class="page-header">
      <div>
        <a-typography-title :level="4" style="margin: 0;">仪表盘</a-typography-title>
        <a-typography-text type="secondary">
          网关实时状态概览 · 每 10 秒自动刷新
          <span v-if="lastRefreshed"> · 最近更新 {{ lastRefreshed.toLocaleTimeString() }}</span>
        </a-typography-text>
      </div>
      <a-space>
        <a-tag color="blue">{{ status?.version || 'dev' }}</a-tag>
        <a-button @click="() => fetchData()" :loading="loading">刷新</a-button>
      </a-space>
    </div>

    <a-row :gutter="16" style="margin-bottom: 16px;">
      <a-col :xs="24" :sm="12" :lg="6">
        <a-card size="small">
          <a-statistic title="运行状态" :value="health?.status || '-'" :value-style="{ color: statusColor }" />
        </a-card>
      </a-col>
      <a-col :xs="24" :sm="12" :lg="6">
        <a-card size="small">
          <a-statistic title="可用上游" :value="aliveCount" :suffix="`/ ${upstreams.length || health?.upstream_count || 0}`" />
          <a-typography-text type="secondary" style="font-size: 12px;">
            不可用 {{ deadCount }} · 未检测 {{ uncheckedCount }}
          </a-typography-text>
        </a-card>
      </a-col>
      <a-col :xs="24" :sm="12" :lg="6">
        <a-card size="small">
          <a-statistic
            title="路由 / 管理就绪"
            :value="health?.router_ready && health?.manager_ready ? '就绪' : '异常'"
            :value-style="{ color: readyColor }"
          />
        </a-card>
      </a-col>
      <a-col :xs="24" :sm="12" :lg="6">
        <a-card size="small">
          <a-statistic title="运行时长" :value="formatUptime(status?.uptime_seconds || 0)" />
          <a-typography-text type="secondary" style="font-size: 12px;">
            {{ status?.go_version || '' }}
          </a-typography-text>
        </a-card>
      </a-col>
    </a-row>

    <a-row :gutter="16" style="margin-bottom: 16px;">
      <a-col :xs="24" :lg="8">
        <a-card size="small" title="监听信息" style="height: 100%;">
          <a-descriptions :column="1" size="small">
            <a-descriptions-item label="代理监听">
              <a-typography-text code>{{ status?.listen || '-' }}</a-typography-text>
            </a-descriptions-item>
            <a-descriptions-item label="管理端口">
              <a-typography-text code>{{ status?.management || '-' }}</a-typography-text>
            </a-descriptions-item>
            <a-descriptions-item label="pprof">
              <a-tag :color="status?.pprof_enabled ? 'green' : 'default'">
                {{ status?.pprof_enabled ? '已启用' : '未启用' }}
              </a-tag>
            </a-descriptions-item>
            <a-descriptions-item label="内置 WebUI">
              <a-tag :color="status?.webui_enabled === false ? 'default' : 'green'">
                {{ status?.webui_enabled === false ? '已关闭' : '已启用' }}
              </a-tag>
            </a-descriptions-item>
            <a-descriptions-item label="构建时间">
              {{ status?.build_time && status.build_time !== 'unknown' ? status.build_time : '—' }}
            </a-descriptions-item>
          </a-descriptions>
        </a-card>
      </a-col>

      <a-col :xs="24" :lg="16">
        <a-card size="small" title="流量概览" style="height: 100%;">
          <template v-if="traffic?.enabled">
            <a-row :gutter="16">
              <a-col :span="6">
                <a-statistic title="总流入" :value="formatBytes(totals.in)" :value-style="{ color: token.colorSuccess }" />
              </a-col>
              <a-col :span="6">
                <a-statistic title="总流出" :value="formatBytes(totals.out)" :value-style="{ color: token.colorInfo }" />
              </a-col>
              <a-col :span="6">
                <a-statistic title="合计" :value="formatBytes(totals.in + totals.out)" />
              </a-col>
              <a-col :span="6">
                <a-statistic title="统计条目" :value="totals.count" :suffix="`条 / ${totals.users} 用户`" />
              </a-col>
            </a-row>
          </template>
          <a-empty v-else description="流量审计未启用">
            <RouterLink to="/settings">
              <a-button type="primary" size="small">前往系统设置开启</a-button>
            </RouterLink>
          </a-empty>
        </a-card>
      </a-col>
    </a-row>

    <a-card size="small" title="上游健康状态">
      <template #extra>
        <RouterLink to="/upstreams">
          <a-button type="link" size="small">管理上游</a-button>
        </RouterLink>
      </template>
      <a-table
        :data-source="upstreams"
        :columns="upstreamColumns"
        row-key="name"
        size="small"
        :pagination="false"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'status'">
            <a-tag :color="upstreamStatusColor(record)">{{ upstreamStatusText(record) }}</a-tag>
          </template>
          <template v-else-if="column.key === 'counts'">
            {{ record.failures }} / {{ record.successes }}
          </template>
          <template v-else-if="column.key === 'last_check'">
            {{ formatTime(record.last_check_at) }}
          </template>
        </template>
        <template #emptyText>
          <a-empty description="尚未配置上游" />
        </template>
      </a-table>
    </a-card>
  </a-spin>
</template>
