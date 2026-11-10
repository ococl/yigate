<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { message, theme } from 'antdv-next'
import { useVisiblePolling } from '@/composables/useVisiblePolling'
import { useAppStore } from '@/stores/app'
import { getStatus, getTraffic } from '@/api/system'
import { formatTimeOnly } from '@/utils/datetime'
import { formatBytes, formatUptime } from '@/utils/format'
import { copyText } from '@/utils/clipboard'
import type { ProxyEndpoint, StatusSnapshot, TrafficSnapshot, UpstreamStatus } from '@/types/api'

const { t } = useI18n()
// 「直连」既作为分组键也作为展示名，内部统一用常量，展示时再翻译
const DIRECT_NAME = '直连'

const status = ref<StatusSnapshot | null>(null)
const traffic = ref<TrafficSnapshot | null>(null)
const loading = ref(false)
const lastRefreshed = ref<Date | null>(null)

const { token } = theme.useToken()
const appStore = useAppStore()
// antd 的 colorSuccess 是 #52c41a，白底上只有 2.27:1；状态值是大字号，至少要 3:1
const successText = computed(() => (appStore.isDark ? '#49aa19' : '#237804'))
const errorText = computed(() => (appStore.isDark ? '#ff7875' : '#cf1322'))

// silent=true 用于定时刷新：避免每次自动刷新都整页 loading 抖动。
// /health 只表示进程存活，Dashboard 需要的就绪与上游信息全部来自 /api/v1/status。
async function fetchData(silent = false) {
  if (!silent) loading.value = true
  try {
    const [s, t] = await Promise.all([
      getStatus({ silent }).catch(() => null),
      getTraffic({ silent }).catch(() => null),
    ])
    status.value = s
    traffic.value = t
    // 全部接口都失败时保留上一次成功时间，避免把刷新失败显示成「刚刚更新」
    if (s || t) lastRefreshed.value = new Date()
  } finally {
    if (!silent) loading.value = false
  }
}

function formatTime(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime()) || date.getTime() <= 0) return '—'
  return date.toLocaleString()
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

const totalBytes = computed(() => totals.value.in + totals.value.out)
const inPercent = computed(() => (totalBytes.value ? (totals.value.in / totalBytes.value) * 100 : 0))
const outPercent = computed(() => (totalBytes.value ? 100 - inPercent.value : 0))
const donutCircumference = 2 * Math.PI * 50
const inArcLength = computed(() => (inPercent.value / 100) * donutCircumference)

function formatPercent(value: number): string {
  return `${value.toFixed(1).replace(/\.0$/, '')}%`
}

const topUpstreams = computed(() => {
  const grouped = new Map<string, number>()
  // 直连始终参与 Top 5 比较，即使当前没有直连流量
  grouped.set(DIRECT_NAME, 0)
  // 纳入所有已配置上游，即使当前没有流量也占一个位置
  for (const upstream of status.value?.upstreams || []) {
    grouped.set(upstream.name, 0)
  }
  for (const entry of traffic.value?.entries || []) {
    const total = entry.bytes_in + entry.bytes_out
    if (total <= 0) continue
    const name = entry.upstream || DIRECT_NAME
    grouped.set(name, (grouped.get(name) || 0) + total)
  }
  const list = [...grouped.entries()]
    .map(([name, total]) => ({ name, total }))
    .sort((a, b) => b.total - a.total || a.name.localeCompare(b.name))

  // 始终保留一个“直连”名额：其余上游只取前 4 名参与比较。
  const direct = list.find((item) => item.name === DIRECT_NAME) || { name: DIRECT_NAME, total: 0 }
  const selected = list.filter((item) => item.name !== DIRECT_NAME).slice(0, 4)
  selected.push(direct)
  selected.sort((a, b) => b.total - a.total || a.name.localeCompare(b.name))

  const max = selected[0]?.total || 0
  return selected.map((item) => ({
    ...item,
    ratio: totalBytes.value ? (item.total / totalBytes.value) * 100 : 0,
    width: max ? (item.total / max) * 100 : 0,
  }))
})

interface UpstreamChartRow {
  name: string
  total: number
  ratio: number
  width: number
  placeholder: boolean
}

const chartRows = computed<UpstreamChartRow[]>(() => {
  const rows: UpstreamChartRow[] = topUpstreams.value.map((item) => ({ ...item, placeholder: false }))
  while (rows.length < 5) {
    rows.push({ name: '', total: 0, ratio: 0, width: 0, placeholder: true })
  }
  return rows
})

const EXTRA_PROXY_HOSTS_KEY = 'yigate:extra-proxy-hosts'

function readExtraProxyHosts(): string[] {
  if (typeof localStorage === 'undefined') return []
  try {
    const raw = localStorage.getItem(EXTRA_PROXY_HOSTS_KEY)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter((item): item is string => typeof item === 'string' && item.trim() !== '')
  } catch {
    return []
  }
}

const proxyModalOpen = ref(false)
const proxyProtocol = ref('http')
const includeProxyCredentials = ref(false)
const proxyUsername = ref('')
const proxyPassword = ref('')
const proxyHostInput = ref('')
const extraProxyHosts = ref<string[]>(readExtraProxyHosts())
const proxyProtocolOptions = [
  { label: 'HTTP(S)', value: 'http' },
  { label: 'SOCKS5', value: 'socks5' },
]

// 模板里需要判断直连项，暴露给模板使用
const DIRECT_LABEL = computed(() => t('pages.dashboard.direct'))

interface ProxyTarget extends ProxyEndpoint {
  custom?: boolean
}

function splitListenAddress(value: string): { host: string; port: number } | null {
  const text = value.trim()
  if (!text) return null

  if (text.startsWith('[')) {
    const end = text.indexOf(']')
    if (end < 0) return null
    const host = text.slice(1, end)
    const portText = text.slice(end + 1).replace(/^:/, '')
    const port = Number(portText)
    return host && Number.isInteger(port) ? { host, port } : null
  }

  const separator = text.lastIndexOf(':')
  if (separator < 0) return null
  const host = text.slice(0, separator)
  const port = Number(text.slice(separator + 1))
  return Number.isInteger(port) ? { host, port } : null
}

function fallbackProxyTargets(listen: string): ProxyEndpoint[] {
  const targets: ProxyEndpoint[] = []
  const seen = new Set<string>()
  for (const part of listen.split(',')) {
    const parsed = splitListenAddress(part)
    if (!parsed || parsed.port <= 0) continue
    let host = parsed.host
    if (!host || host === '0.0.0.0' || host === '::') {
      host = window.location.hostname || '127.0.0.1'
    }
    const key = `${host}|${parsed.port}`
    if (seen.has(key)) continue
    seen.add(key)
    targets.push({
      address: host,
      port: parsed.port,
      family: host.includes(':') ? 'ipv6' : 'hostname',
      loopback: host === 'localhost' || host === '127.0.0.1' || host === '::1',
    })
  }
  return targets
}

const detectedProxyTargets = computed<ProxyEndpoint[]>(() => {
  const endpoints = status.value?.proxy_endpoints
  if (endpoints && endpoints.length > 0) return endpoints
  return fallbackProxyTargets(status.value?.listen || '')
})

const proxyTargets = computed<ProxyTarget[]>(() => {
  const targets: ProxyTarget[] = []
  const seen = new Set<string>()
  const add = (target: ProxyTarget) => {
    const key = `${target.address.toLowerCase()}|${target.port}`
    if (seen.has(key)) return
    seen.add(key)
    targets.push(target)
  }

  const portTLS = new Map<number, boolean>()
  for (const endpoint of detectedProxyTargets.value) {
    add({ ...endpoint })
    portTLS.set(endpoint.port, (portTLS.get(endpoint.port) ?? false) || !!endpoint.tls)
  }
  for (const host of extraProxyHosts.value) {
    for (const [port, tls] of portTLS) {
      add({ address: host, port, tls, custom: true, family: host.includes(':') ? 'ipv6' : 'hostname' })
    }
  }
  return targets
})

function formatProxyHost(host: string): string {
  return host.includes(':') && !host.startsWith('[') ? `[${host}]` : host
}

function formatProxyAddress(target: ProxyEndpoint): string {
  return `${formatProxyHost(target.address)}:${target.port}`
}

function buildProxyUrl(target: ProxyEndpoint): string {
  const scheme = proxyProtocol.value === 'socks5' ? 'socks5' : target.tls ? 'https' : 'http'
  const username = proxyUsername.value.trim()
  const auth =
    includeProxyCredentials.value && username && proxyPassword.value
      ? `${encodeURIComponent(username)}:${encodeURIComponent(proxyPassword.value)}@`
      : ''
  return `${scheme}://${auth}${formatProxyHost(target.address)}:${target.port}`
}

function validateProxyCredentials(): boolean {
  if (!includeProxyCredentials.value) return true
  if (!proxyUsername.value.trim()) {
    message.warning(t('pages.dashboard.messages.usernameRequired'))
    return false
  }
  if (!proxyPassword.value) {
    message.warning(t('pages.dashboard.messages.passwordRequired'))
    return false
  }
  return true
}

async function copyProxyLink(target: ProxyEndpoint) {
  if (!validateProxyCredentials()) return
  if (await copyText(buildProxyUrl(target))) {
    message.success(t('pages.dashboard.messages.copied'))
    return
  }
  message.error(t('pages.dashboard.messages.copyFailed'))
}

async function copyAllProxyLinks() {
  if (!proxyTargets.value.length) {
    message.info(t('pages.dashboard.messages.noTargets'))
    return
  }
  if (!validateProxyCredentials()) return
  if (await copyText(proxyTargets.value.map((target) => buildProxyUrl(target)).join('\n'))) {
    message.success(t('pages.dashboard.messages.copied'))
    return
  }
  message.error(t('pages.dashboard.messages.copyFailed'))
}

function persistExtraProxyHosts() {
  try {
    localStorage.setItem(EXTRA_PROXY_HOSTS_KEY, JSON.stringify(extraProxyHosts.value))
  } catch {
    // 忽略存储失败，不影响当前页面使用
  }
}

function normalizeProxyHost(value: string): string | null {
  let host = value.trim()
  if (!host) return null
  host = host.replace(/^[a-z][a-z0-9+.-]*:\/\//i, '').split('/')[0]
  if (host.startsWith('[')) {
    const end = host.indexOf(']')
    if (end < 0 || end !== host.length - 1) return null
    host = host.slice(1, -1)
  }
  if (!host || /[\s[\]@]/.test(host)) return null
  const colonCount = (host.match(/:/g) || []).length
  if (colonCount === 1) return null
  return host
}

function addProxyHost() {
  const host = normalizeProxyHost(proxyHostInput.value)
  if (!host) {
    message.warning(t('pages.dashboard.messages.invalidHost'))
    return
  }
  if (extraProxyHosts.value.some((item) => item.toLowerCase() === host.toLowerCase())) {
    message.info(t('pages.dashboard.messages.hostExists'))
    proxyHostInput.value = ''
    return
  }
  extraProxyHosts.value.push(host)
  proxyHostInput.value = ''
  persistExtraProxyHosts()
}

function removeProxyHost(host: string) {
  extraProxyHosts.value = extraProxyHosts.value.filter((item) => item !== host)
  persistExtraProxyHosts()
}

function closeProxyModal() {
  proxyPassword.value = ''
  includeProxyCredentials.value = false
  proxyModalOpen.value = false
}

const upstreams = computed<UpstreamStatus[]>(() => status.value?.upstreams || [])
function checked(u: UpstreamStatus): boolean {
  return !!(u.last_check_at && new Date(u.last_check_at).getTime() > 0)
}
const aliveCount = computed(() => upstreams.value.filter((u) => checked(u) && !u.dead).length)
const deadCount = computed(() => upstreams.value.filter((u) => checked(u) && u.dead).length)
const uncheckedCount = computed(() => upstreams.value.filter((u) => !checked(u)).length)

// 状态接口失败时不能把「读不到」显示成「异常」，否则会误报网关故障
const statusUnavailable = computed(() => !status.value && !loading.value)
const statusColor = computed(() => {
  if (!status.value) return token.value.colorTextSecondary
  return status.value.status === 'up' ? successText.value : errorText.value
})
const readyColor = computed(() => {
  if (!status.value) return token.value.colorTextSecondary
  return status.value.router_ready && status.value.manager_ready ? successText.value : errorText.value
})

fetchData()
useVisiblePolling(() => fetchData(true), 10000)
</script>

<template>
  <a-spin :spinning="loading" data-testid="page-dashboard">
    <div class="page-header">
      <div>
        <a-typography-title :level="4" style="margin: 0;">{{ t('pages.dashboard.title') }}</a-typography-title>
        <a-typography-text type="secondary">
          {{ t('pages.dashboard.subtitle') }}
          <span v-if="lastRefreshed">
            · {{ t('pages.dashboard.lastUpdated', { time: formatTimeOnly(lastRefreshed) }) }}
          </span>
        </a-typography-text>
      </div>
      <a-space wrap>
        <a-tag color="blue">{{ status?.version || 'dev' }}</a-tag>
        <a-button data-testid="dashboard-proxy-link" @click="proxyModalOpen = true">
          {{ t('pages.dashboard.proxyLinks') }}
        </a-button>
        <a-button @click="() => fetchData()" :loading="loading">{{ t('pages.dashboard.refresh') }}</a-button>
      </a-space>
    </div>

    <a-alert
      v-if="statusUnavailable"
      type="warning"
      show-icon
      style="margin-bottom: 16px;"
      :message="t('pages.dashboard.healthUnavailableTitle')"
      :description="t('pages.dashboard.healthUnavailableDesc')"
    >
      <template #action>
        <a-button size="small" :loading="loading" @click="() => fetchData()">
          {{ t('pages.dashboard.retry') }}
        </a-button>
      </template>
    </a-alert>

    <a-row :gutter="16" style="margin-bottom: 16px;">
      <a-col :xs="12" :sm="12" :lg="6">
        <a-card size="small" class="stat-card" style="height: 100%;">
          <a-statistic
            data-testid="stat-status"
            :title="t('pages.dashboard.status')"
            :value="status ? status.status || '-' : '—'"
            :value-style="{ color: statusColor }"
          />
        </a-card>
      </a-col>
      <a-col :xs="12" :sm="12" :lg="6">
        <a-card size="small" class="stat-card" style="height: 100%;">
          <a-statistic
            data-testid="stat-upstreams"
            :title="t('pages.dashboard.availableUpstreams')"
            :value="status ? aliveCount : '—'"
            :suffix="status ? `/ ${upstreams.length || status.upstream_count || 0}` : ''"
          />
          <a-typography-text type="secondary" style="font-size: 12px;">
            {{
              status
                ? t('pages.dashboard.upstreamDetail', { dead: deadCount, unchecked: uncheckedCount })
                : t('pages.dashboard.statusUnavailable')
            }}
          </a-typography-text>
        </a-card>
      </a-col>
      <a-col :xs="12" :sm="12" :lg="6">
        <a-card size="small" class="stat-card" style="height: 100%;">
          <a-statistic
            data-testid="stat-ready"
            :title="t('pages.dashboard.routerReady')"
            :value="
              status
                ? status.router_ready && status.manager_ready
                  ? t('pages.dashboard.ready')
                  : t('pages.dashboard.abnormal')
                : '—'
            "
            :value-style="{ color: readyColor }"
          />
        </a-card>
      </a-col>
      <a-col :xs="12" :sm="12" :lg="6">
        <a-card size="small" class="stat-card" style="height: 100%;">
          <a-statistic :title="t('pages.dashboard.uptime')" :value="formatUptime(status?.uptime_seconds || 0)" />
          <a-typography-text type="secondary" style="font-size: 12px;">
            {{ status?.go_version || '' }}
          </a-typography-text>
        </a-card>
      </a-col>
    </a-row>

    <a-row :gutter="16" style="margin-bottom: 16px;">
      <a-col :xs="24" :lg="8">
        <a-card size="small" :title="t('pages.dashboard.runningInfo')" style="height: 100%;">
          <a-descriptions :column="1" size="small">
            <a-descriptions-item :label="t('pages.dashboard.proxyListen')">
              <a-typography-text code>{{ status?.listen || '-' }}</a-typography-text>
            </a-descriptions-item>
            <a-descriptions-item :label="t('pages.dashboard.managementPort')">
              <a-typography-text code>{{ status?.management || '-' }}</a-typography-text>
            </a-descriptions-item>
            <a-descriptions-item :label="t('pages.dashboard.upstreamCount')">
              {{ status?.upstream_count ?? '-' }}
            </a-descriptions-item>
            <a-descriptions-item :label="t('pages.dashboard.pprof')">
              <a-tag :color="status?.pprof_enabled ? 'green' : 'default'">
                {{ status?.pprof_enabled ? t('common.enabled') : t('common.disabled') }}
              </a-tag>
            </a-descriptions-item>
            <a-descriptions-item :label="t('pages.dashboard.webui')">
              <a-tag :color="status?.webui_enabled === false ? 'default' : 'green'">
                {{ status?.webui_enabled === false ? t('common.disabled') : t('common.enabled') }}
              </a-tag>
            </a-descriptions-item>
            <a-descriptions-item :label="t('pages.dashboard.buildTime')">
              {{ status?.build_time && status.build_time !== 'unknown' ? status.build_time : '—' }}
            </a-descriptions-item>
            <a-descriptions-item :label="t('pages.dashboard.statusTime')">
              {{ formatTime(status?.timestamp) }}
            </a-descriptions-item>
          </a-descriptions>
        </a-card>
      </a-col>

      <a-col :xs="24" :lg="16">
        <a-card
          size="small"
          data-testid="dashboard-traffic-card"
          :title="t('pages.dashboard.trafficOverview')"
          style="height: 100%;"
        >
          <template v-if="traffic && (traffic.enabled || traffic.entries.length > 0)">
            <a-alert
              v-if="!traffic.enabled"
              type="warning"
              show-icon
              style="margin-bottom: 12px;"
              :message="t('pages.dashboard.trafficDisabledTitle')"
              :description="t('pages.dashboard.trafficDisabledDesc')"
            />
            <div class="traffic-summary">
              <span>{{ t('pages.dashboard.records', { count: totals.count }) }}</span>
              <span>{{ t('pages.dashboard.users', { count: totals.users }) }}</span>
              <span v-if="totalBytes > 0">{{ t('pages.dashboard.total', { size: formatBytes(totalBytes) }) }}</span>
            </div>

            <template v-if="totalBytes > 0">
              <div class="traffic-charts">
                <div class="traffic-donut">
                  <div class="chart-caption">{{ t('pages.dashboard.inOutRatio') }}</div>
                  <div class="donut-wrap">
                    <svg
                      viewBox="0 0 120 120"
                      class="donut-chart"
                      role="img"
                      :aria-label="t('pages.dashboard.inOutAria')"
                    >
                      <circle cx="60" cy="60" r="50" fill="none" :stroke="token.colorInfo" stroke-width="12" />
                      <circle
                        cx="60"
                        cy="60"
                        r="50"
                        fill="none"
                        :stroke="token.colorSuccess"
                        stroke-width="12"
                        :stroke-dasharray="`${inArcLength} ${donutCircumference}`"
                        transform="rotate(-90 60 60)"
                      />
                    </svg>
                    <div class="donut-center">
                      <span>{{ t('pages.dashboard.totalTraffic') }}</span>
                      <strong>{{ formatBytes(totalBytes) }}</strong>
                    </div>
                  </div>
                  <div class="donut-legend">
                    <div class="legend-item">
                      <span class="legend-dot" :style="{ backgroundColor: token.colorSuccess }" />
                      <span class="legend-label">{{ t('pages.dashboard.inbound') }}</span>
                      <span class="legend-value">{{ formatBytes(totals.in) }}</span>
                      <span class="legend-percent">{{ formatPercent(inPercent) }}</span>
                    </div>
                    <div class="legend-item">
                      <span class="legend-dot" :style="{ backgroundColor: token.colorInfo }" />
                      <span class="legend-label">{{ t('pages.dashboard.outbound') }}</span>
                      <span class="legend-value">{{ formatBytes(totals.out) }}</span>
                      <span class="legend-percent">{{ formatPercent(outPercent) }}</span>
                    </div>
                  </div>
                </div>

                <div class="traffic-bars">
                  <div class="chart-caption">{{ t('pages.dashboard.top5') }}</div>
                  <div class="bar-list">
                    <div
                      v-for="(item, index) in chartRows"
                      :key="item.placeholder ? `empty-${index}` : item.name"
                      class="bar-item"
                      :class="{ 'bar-item-placeholder': item.placeholder }"
                    >
                      <div class="bar-head">
                        <template v-if="item.placeholder">
                          <span class="bar-name placeholder-name">
                            {{ index === topUpstreams.length ? t('pages.dashboard.noUpstream') : t('pages.dashboard.emptySlot') }}
                          </span>
                          <RouterLink
                            v-if="index === topUpstreams.length"
                            to="/upstreams"
                            class="bar-add"
                          >
                            {{ t('pages.dashboard.addUpstream') }}
                          </RouterLink>
                        </template>
                        <template v-else>
                          <span class="bar-name" :title="item.name === DIRECT_NAME ? DIRECT_LABEL : item.name">
                            {{ item.name === DIRECT_NAME ? DIRECT_LABEL : item.name }}
                          </span>
                          <span class="bar-value">{{ formatBytes(item.total) }} · {{ formatPercent(item.ratio) }}</span>
                        </template>
                      </div>
                      <div class="bar-track" :class="{ 'bar-track-placeholder': item.placeholder }">
                        <div
                          v-if="!item.placeholder"
                          class="bar-fill"
                          :style="{
                            width: `${item.width}%`,
                            background: `linear-gradient(90deg, ${token.colorInfo}, ${token.colorPrimary})`,
                          }"
                        />
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </template>

            <a-typography-text v-else type="secondary" class="charts-empty">
              {{ t('pages.dashboard.noTrafficData') }}
            </a-typography-text>
          </template>
          <a-empty
            v-else
            :description="traffic ? t('pages.dashboard.noTrafficRecord') : t('pages.dashboard.trafficLoadFailed')"
          >
            <RouterLink v-if="traffic" to="/settings?section=quota">
              <a-button type="primary" size="small">{{ t('pages.dashboard.goSettings') }}</a-button>
            </RouterLink>
            <a-button
              v-else
              type="primary"
              size="small"
              data-testid="traffic-retry"
              :loading="loading"
              @click="fetchData()"
            >
              {{ t('pages.dashboard.retry') }}
            </a-button>
          </a-empty>
        </a-card>
      </a-col>
    </a-row>
  </a-spin>

  <a-modal
    :open="proxyModalOpen"
    :title="t('pages.dashboard.proxyModal.title')"
    width="640px"
    :footer="null"
    @cancel="closeProxyModal"
  >
    <div class="proxy-modal">
      <div class="proxy-modal-scroll">
        <div class="proxy-options">
        <div class="proxy-option">
          <span class="proxy-label">{{ t('pages.dashboard.proxyModal.protocol') }}</span>
          <a-select v-model:value="proxyProtocol" :options="proxyProtocolOptions" style="width: 120px;" />
        </div>
        <div class="proxy-option">
          <a-switch v-model:checked="includeProxyCredentials" size="small" />
          <span class="proxy-label">{{ t('pages.dashboard.proxyModal.includeCredentials') }}</span>
        </div>
      </div>

      <div v-if="includeProxyCredentials" class="proxy-auth">
        <a-input
          v-model:value="proxyUsername"
          :placeholder="t('pages.dashboard.proxyModal.username')"
          allow-clear
        />
        <a-input-password
          v-model:value="proxyPassword"
          :placeholder="t('pages.dashboard.proxyModal.password')"
          autocomplete="new-password"
        />
      </div>
      <a-typography-text v-if="includeProxyCredentials" type="secondary" class="proxy-hint">
        {{ t('pages.dashboard.proxyModal.passwordHint') }}
      </a-typography-text>

      <div class="proxy-section">
        <div class="proxy-section-head">
          <span class="proxy-section-title">{{ t('pages.dashboard.proxyModal.available') }}</span>
          <a-typography-text type="secondary" class="proxy-section-desc">
            {{ t('pages.dashboard.proxyModal.availableDesc') }}
          </a-typography-text>
        </div>

        <div v-if="proxyTargets.length" class="proxy-links">
          <div
            v-for="target in proxyTargets"
            :key="`${target.address}:${target.port}:${target.custom ? 'custom' : 'auto'}`"
            class="proxy-link-row"
          >
            <div class="proxy-link-meta">
              <span class="proxy-address">{{ formatProxyAddress(target) }}</span>
              <a-tag v-if="target.custom" color="blue">{{ t('pages.dashboard.proxyModal.custom') }}</a-tag>
              <a-tag v-else-if="target.loopback">{{ t('pages.dashboard.proxyModal.local') }}</a-tag>
              <a-tag v-if="target.interface_name" class="proxy-interface-tag">{{ target.interface_name }}</a-tag>
              <a-tag v-if="target.tls" color="gold">TLS</a-tag>
            </div>
            <div class="proxy-link-line">
              <span class="proxy-url">{{ buildProxyUrl(target) }}</span>
              <a-button type="link" size="small" @click="copyProxyLink(target)">{{ t('common.copy') }}</a-button>
            </div>
          </div>
        </div>
        <a-empty v-else :description="t('pages.dashboard.proxyModal.noTargets')">
          <RouterLink to="/settings?section=basic">
            <a-button type="primary" size="small">{{ t('pages.dashboard.proxyModal.goBasic') }}</a-button>
          </RouterLink>
        </a-empty>
      </div>

      <div class="proxy-section">
        <div class="proxy-section-head">
          <span class="proxy-section-title">{{ t('pages.dashboard.proxyModal.extra') }}</span>
          <a-typography-text type="secondary" class="proxy-section-desc">
            {{ t('pages.dashboard.proxyModal.extraDesc') }}
          </a-typography-text>
        </div>

        <div class="proxy-host-form">
          <a-input
            v-model:value="proxyHostInput"
            :placeholder="t('pages.dashboard.proxyModal.hostPlaceholder')"
            allow-clear
            @keyup.enter="addProxyHost"
          />
          <a-button @click="addProxyHost">{{ t('pages.dashboard.proxyModal.add') }}</a-button>
        </div>

        <div v-if="extraProxyHosts.length" class="proxy-extra-tags">
          <a-tag
            v-for="host in extraProxyHosts"
            :key="host"
            closable
            @close="removeProxyHost(host)"
          >
            {{ host }}
          </a-tag>
        </div>
        <a-typography-text v-else type="secondary" class="proxy-empty-hint">
          {{ t('pages.dashboard.proxyModal.noExtra') }}
        </a-typography-text>
      </div>
      </div>

      <div class="proxy-modal-footer">
        <span class="proxy-footer-hint">
          {{ t('pages.dashboard.proxyModal.footerCount', { count: proxyTargets.length }) }}
        </span>
        <a-space>
          <a-button @click="closeProxyModal">{{ t('common.close') }}</a-button>
          <a-button type="primary" :disabled="!proxyTargets.length" @click="copyAllProxyLinks">
            {{ t('pages.dashboard.proxyModal.copyAll') }}
          </a-button>
        </a-space>
      </div>
    </div>
  </a-modal>
</template>

<style scoped>
.traffic-summary {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 18px;
  margin-bottom: 12px;
  font-size: 12px;
  color: rgba(128, 128, 128, 0.9);
}

.traffic-summary strong {
  font-weight: 600;
  color: rgba(128, 128, 128, 0.95);
  font-variant-numeric: tabular-nums;
}

.traffic-charts {
  display: grid;
  grid-template-columns: minmax(190px, 220px) minmax(0, 1fr);
  align-items: center;
  gap: 24px;
}

.chart-caption {
  margin-bottom: 8px;
  font-size: 12px;
  color: rgba(128, 128, 128, 0.9);
}

.donut-wrap {
  position: relative;
  width: 132px;
  height: 132px;
  margin: 0 auto;
}

.donut-chart {
  display: block;
  width: 100%;
  height: 100%;
}

.donut-center {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  pointer-events: none;
}

.donut-center span {
  font-size: 11px;
  color: rgba(128, 128, 128, 0.9);
}

.donut-center strong {
  font-size: 14px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.donut-legend {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 12px;
}

.legend-item {
  display: grid;
  grid-template-columns: 8px minmax(0, 1fr) auto 42px;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}

.legend-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.legend-label {
  color: rgba(128, 128, 128, 0.9);
}

.legend-value {
  font-variant-numeric: tabular-nums;
}

.legend-percent {
  text-align: right;
  color: rgba(128, 128, 128, 0.9);
  font-variant-numeric: tabular-nums;
}

.bar-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.bar-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 4px;
  font-size: 12px;
}

.bar-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.bar-value {
  flex: 0 0 auto;
  color: rgba(128, 128, 128, 0.9);
  font-variant-numeric: tabular-nums;
}

.bar-item-placeholder .placeholder-name {
  color: rgba(128, 128, 128, 0.55);
}

.bar-add {
  flex: 0 0 auto;
  color: #1677ff;
  text-decoration: none;
}

.bar-add:hover {
  text-decoration: underline;
}

.bar-track {
  height: 8px;
  overflow: hidden;
  border-radius: 999px;
  background: rgba(127, 127, 127, 0.14);
}

.bar-track-placeholder {
  background: transparent;
  border: 1px dashed rgba(127, 127, 127, 0.28);
}

.bar-fill {
  height: 100%;
  border-radius: inherit;
  transition: width 0.3s ease;
}

.charts-empty {
  display: block;
  margin-top: 12px;
  font-size: 12px;
}

.proxy-modal {
  display: flex;
  flex-direction: column;
  max-height: min(560px, calc(100vh - 220px));
}

.proxy-modal-scroll {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  padding-right: 4px;
}

.proxy-options {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px 28px;
}

.proxy-option {
  display: flex;
  align-items: center;
  gap: 8px;
}

.proxy-label {
  font-size: 13px;
  color: rgba(128, 128, 128, 0.95);
}

.proxy-auth {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 12px;
  margin-top: 12px;
}

/* 窄屏统计卡排成两列，值需要压一档字号并允许省略 */
@media (max-width: 768px) {
  .stat-card :deep(.ant-statistic-content) {
    font-size: 20px;
    display: flex;
    align-items: baseline;
    gap: 4px;
    min-width: 0;
    white-space: nowrap;
  }

  /* 「0 条 / 0 个用户」这类后缀在两列宽度里会折行，压一档字号并单行省略 */
  .stat-card :deep(.ant-statistic-content-value),
  .stat-card :deep(.ant-statistic-content-suffix) {
    font-size: 14px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .stat-card :deep(.ant-statistic-content-value) {
    font-size: 20px;
  }

  .traffic-charts {
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
  }

  .donut-wrap {
    width: 112px;
    height: 112px;
  }

  .proxy-auth {
    grid-template-columns: minmax(0, 1fr);
  }

  .proxy-modal {
    max-height: calc(100dvh - 180px);
  }
}

.proxy-hint {
  display: block;
  margin-top: 6px;
  font-size: 12px;
}

.proxy-section {
  margin-top: 18px;
}

.proxy-section-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 8px;
}

.proxy-section-title {
  font-size: 13px;
  font-weight: 600;
}

.proxy-section-desc {
  font-size: 12px;
}

.proxy-links {
  overflow: hidden;
  border: 1px solid rgba(127, 127, 127, 0.18);
  border-radius: 8px;
}

.proxy-link-row {
  padding: 9px 12px;
}

.proxy-link-row + .proxy-link-row {
  border-top: 1px solid rgba(127, 127, 127, 0.14);
}

.proxy-link-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.proxy-link-meta :deep(.ant-tag) {
  margin-inline-end: 0;
  font-size: 11px;
  line-height: 18px;
}

.proxy-address {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  font-weight: 600;
}

.proxy-link-line {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 2px;
}

.proxy-url {
  flex: 1;
  min-width: 0;
  overflow-wrap: anywhere;
  color: rgba(128, 128, 128, 0.95);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  user-select: all;
}

.proxy-host-form {
  display: flex;
  gap: 8px;
}

.proxy-extra-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 10px;
}

.proxy-extra-tags :deep(.ant-tag) {
  margin-inline-end: 0;
}

.proxy-empty-hint {
  display: block;
  margin-top: 6px;
  font-size: 12px;
}

.proxy-modal-footer {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid rgba(127, 127, 127, 0.14);
}

.proxy-footer-hint {
  font-size: 12px;
  color: rgba(128, 128, 128, 0.9);
}

@media (max-width: 768px) {
  .traffic-charts {
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
  }

  .donut-wrap {
    margin-left: 0;
  }

  .proxy-auth {
    grid-template-columns: minmax(0, 1fr);
  }

  .proxy-section-head {
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
  }

  .proxy-modal-footer {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
