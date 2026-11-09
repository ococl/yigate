<script setup lang="ts">
import { ref, computed } from 'vue'
import { message, theme } from 'antdv-next'
import { useVisiblePolling } from '@/composables/useVisiblePolling'
import { useAppStore } from '@/stores/app'
import { getHealth, getStatus, getTraffic } from '@/api/system'
import { formatDateTime, formatTimeOnly } from '@/utils/datetime'
import { formatBytes, formatUptime } from '@/utils/format'
import { copyText } from '@/utils/clipboard'
import type { HealthStatus, ProxyEndpoint, StatusSnapshot, TrafficSnapshot, UpstreamStatus } from '@/types/api'

const health = ref<HealthStatus | null>(null)
const status = ref<StatusSnapshot | null>(null)
const traffic = ref<TrafficSnapshot | null>(null)
const loading = ref(false)
const lastRefreshed = ref<Date | null>(null)

const { token } = theme.useToken()
const appStore = useAppStore()
// antd 的 colorSuccess 是 #52c41a，白底上只有 2.27:1；状态值是大字号，至少要 3:1
const successText = computed(() => (appStore.isDark ? '#49aa19' : '#237804'))
const errorText = computed(() => (appStore.isDark ? '#ff7875' : '#cf1322'))

// silent=true 用于定时刷新：避免每次自动刷新都整页 loading 抖动
async function fetchData(silent = false) {
  if (!silent) loading.value = true
  try {
    const [h, s, t] = await Promise.all([
      getHealth({ silent }).catch(() => null),
      getStatus({ silent }).catch(() => null),
      getTraffic({ silent }).catch(() => null),
    ])
    health.value = h
    status.value = s
    traffic.value = t
    // 全部接口都失败时保留上一次成功时间，避免把刷新失败显示成「刚刚更新」
    if (h || s || t) lastRefreshed.value = new Date()
  } finally {
    if (!silent) loading.value = false
  }
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
  grouped.set('直连', 0)
  // 纳入所有已配置上游，即使当前没有流量也占一个位置
  for (const upstream of status.value?.upstreams || []) {
    grouped.set(upstream.name, 0)
  }
  for (const entry of traffic.value?.entries || []) {
    const total = entry.bytes_in + entry.bytes_out
    if (total <= 0) continue
    const name = entry.upstream || '直连'
    grouped.set(name, (grouped.get(name) || 0) + total)
  }
  const list = [...grouped.entries()]
    .map(([name, total]) => ({ name, total }))
    .sort((a, b) => b.total - a.total || a.name.localeCompare(b.name))

  // 始终保留一个“直连”名额：其余上游只取前 4 名参与比较。
  const direct = list.find((item) => item.name === '直连') || { name: '直连', total: 0 }
  const selected = list.filter((item) => item.name !== '直连').slice(0, 4)
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
    message.warning('请输入代理用户名')
    return false
  }
  if (!proxyPassword.value) {
    message.warning('请输入代理密码')
    return false
  }
  return true
}

async function copyProxyLink(target: ProxyEndpoint) {
  if (!validateProxyCredentials()) return
  if (await copyText(buildProxyUrl(target))) {
    message.success('已复制')
    return
  }
  message.error('复制失败，请手动选择链接复制')
}

async function copyAllProxyLinks() {
  if (!proxyTargets.value.length) {
    message.info('暂无可复制的代理地址')
    return
  }
  if (!validateProxyCredentials()) return
  if (await copyText(proxyTargets.value.map((target) => buildProxyUrl(target)).join('\n'))) {
    message.success('已复制')
    return
  }
  message.error('复制失败，请手动选择链接复制')
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
    message.warning('请输入有效的 IP 或主机名，无需填写端口')
    return
  }
  if (extraProxyHosts.value.some((item) => item.toLowerCase() === host.toLowerCase())) {
    message.info('该地址已存在')
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

// 健康接口失败时不能把「读不到」显示成「异常」，否则会误报网关故障
const healthUnavailable = computed(() => !health.value && !loading.value)
const statusColor = computed(() => {
  if (!health.value) return token.value.colorTextSecondary
  return health.value.status === 'up' ? successText.value : errorText.value
})
const readyColor = computed(() => {
  if (!health.value) return token.value.colorTextSecondary
  return health.value.router_ready && health.value.manager_ready ? successText.value : errorText.value
})

fetchData()
useVisiblePolling(() => fetchData(true), 10000)
</script>

<template>
  <a-spin :spinning="loading">
    <div class="page-header">
      <div>
        <a-typography-title :level="4" style="margin: 0;">仪表盘</a-typography-title>
        <a-typography-text type="secondary">
          网关实时状态概览 · 每 10 秒自动刷新
          <span v-if="lastRefreshed"> · 最近更新 {{ formatTimeOnly(lastRefreshed) }}</span>
        </a-typography-text>
      </div>
      <a-space wrap>
        <a-tag color="blue">{{ status?.version || 'dev' }}</a-tag>
        <a-button @click="proxyModalOpen = true">代理链接</a-button>
        <a-button @click="() => fetchData()" :loading="loading">刷新</a-button>
      </a-space>
    </div>

    <a-alert
      v-if="healthUnavailable"
      type="warning"
      show-icon
      style="margin-bottom: 16px;"
      message="无法获取网关运行状态"
      description="健康检查接口请求失败，下方指标不代表真实运行状态。请确认后端服务是否正常，然后重试。"
    >
      <template #action>
        <a-button size="small" :loading="loading" @click="() => fetchData()">重试</a-button>
      </template>
    </a-alert>

    <a-row :gutter="16" style="margin-bottom: 16px;">
      <a-col :xs="24" :sm="12" :lg="6">
        <a-card size="small" style="height: 100%;">
          <a-statistic title="运行状态" :value="health ? health.status || '-' : '—'" :value-style="{ color: statusColor }" />
        </a-card>
      </a-col>
      <a-col :xs="24" :sm="12" :lg="6">
        <a-card size="small" style="height: 100%;">
          <a-statistic
            title="可用上游"
            :value="status ? aliveCount : '—'"
            :suffix="status ? `/ ${upstreams.length || health?.upstream_count || 0}` : ''"
          />
          <a-typography-text type="secondary" style="font-size: 12px;">
            {{ status ? `不可用 ${deadCount} · 未检测 ${uncheckedCount}` : '状态接口不可用' }}
          </a-typography-text>
        </a-card>
      </a-col>
      <a-col :xs="24" :sm="12" :lg="6">
        <a-card size="small" style="height: 100%;">
          <a-statistic
            title="路由 / 管理就绪"
            :value="health ? (health.router_ready && health.manager_ready ? '就绪' : '异常') : '—'"
            :value-style="{ color: readyColor }"
          />
        </a-card>
      </a-col>
      <a-col :xs="24" :sm="12" :lg="6">
        <a-card size="small" style="height: 100%;">
          <a-statistic title="运行时长" :value="formatUptime(status?.uptime_seconds || 0)" />
          <a-typography-text type="secondary" style="font-size: 12px;">
            {{ status?.go_version || '' }}
          </a-typography-text>
        </a-card>
      </a-col>
    </a-row>

    <a-row :gutter="16" style="margin-bottom: 16px;">
      <a-col :xs="24" :lg="8">
        <a-card size="small" title="运行信息" style="height: 100%;">
          <a-descriptions :column="1" size="small">
            <a-descriptions-item label="代理监听">
              <a-typography-text code>{{ status?.listen || '-' }}</a-typography-text>
            </a-descriptions-item>
            <a-descriptions-item label="管理端口">
              <a-typography-text code>{{ status?.management || '-' }}</a-typography-text>
            </a-descriptions-item>
            <a-descriptions-item label="上游数量">
              {{ status?.upstream_count ?? '-' }}
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
            <a-descriptions-item label="状态时间">
              {{ formatTime(status?.timestamp) }}
            </a-descriptions-item>
          </a-descriptions>
        </a-card>
      </a-col>

      <a-col :xs="24" :lg="16">
        <a-card size="small" title="流量概览" style="height: 100%;">
          <template v-if="traffic && (traffic.enabled || traffic.entries.length > 0)">
            <a-alert
              v-if="!traffic.enabled"
              type="warning"
              show-icon
              style="margin-bottom: 12px;"
              message="流量审计已关闭"
              description="以下为已持久化的历史记录；重新开启审计后才会记录新流量。"
            />
            <div class="traffic-summary">
              <span>共 <strong>{{ totals.count }}</strong> 条记录</span>
              <span>覆盖 <strong>{{ totals.users }}</strong> 个用户</span>
              <span v-if="totalBytes > 0">合计 <strong>{{ formatBytes(totalBytes) }}</strong></span>
            </div>

            <template v-if="totalBytes > 0">
              <div class="traffic-charts">
                <div class="traffic-donut">
                  <div class="chart-caption">流入 / 流出占比</div>
                  <div class="donut-wrap">
                    <svg viewBox="0 0 120 120" class="donut-chart" role="img" aria-label="流入与流出流量占比">
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
                      <span>总流量</span>
                      <strong>{{ formatBytes(totalBytes) }}</strong>
                    </div>
                  </div>
                  <div class="donut-legend">
                    <div class="legend-item">
                      <span class="legend-dot" :style="{ backgroundColor: token.colorSuccess }" />
                      <span class="legend-label">流入</span>
                      <span class="legend-value">{{ formatBytes(totals.in) }}</span>
                      <span class="legend-percent">{{ formatPercent(inPercent) }}</span>
                    </div>
                    <div class="legend-item">
                      <span class="legend-dot" :style="{ backgroundColor: token.colorInfo }" />
                      <span class="legend-label">流出</span>
                      <span class="legend-value">{{ formatBytes(totals.out) }}</span>
                      <span class="legend-percent">{{ formatPercent(outPercent) }}</span>
                    </div>
                  </div>
                </div>

                <div class="traffic-bars">
                  <div class="chart-caption">流量 Top 5（含直连）</div>
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
                            {{ index === topUpstreams.length ? '未配置上游' : '空位' }}
                          </span>
                          <RouterLink
                            v-if="index === topUpstreams.length"
                            to="/upstreams"
                            class="bar-add"
                          >
                            添加上游
                          </RouterLink>
                        </template>
                        <template v-else>
                          <span class="bar-name" :title="item.name">{{ item.name }}</span>
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

            <a-typography-text v-else type="secondary" class="charts-empty">暂无流量数据</a-typography-text>
          </template>
          <a-empty v-else :description="traffic ? '暂无流量记录' : '流量数据加载失败'">
            <RouterLink v-if="traffic" to="/settings?section=quota">
              <a-button type="primary" size="small">前往系统设置查看流量审计</a-button>
            </RouterLink>
            <a-button v-else type="primary" size="small" :loading="loading" @click="fetchData()">重试</a-button>
          </a-empty>
        </a-card>
      </a-col>
    </a-row>
  </a-spin>

  <a-modal
    :open="proxyModalOpen"
    title="代理链接"
    width="640px"
    :footer="null"
    @cancel="closeProxyModal"
  >
    <div class="proxy-modal">
      <div class="proxy-modal-scroll">
        <div class="proxy-options">
        <div class="proxy-option">
          <span class="proxy-label">协议</span>
          <a-select v-model:value="proxyProtocol" :options="proxyProtocolOptions" style="width: 120px;" />
        </div>
        <div class="proxy-option">
          <a-switch v-model:checked="includeProxyCredentials" size="small" />
          <span class="proxy-label">复制时包含账号密码</span>
        </div>
      </div>

      <div v-if="includeProxyCredentials" class="proxy-auth">
        <a-input v-model:value="proxyUsername" placeholder="代理用户名" allow-clear />
        <a-input-password
          v-model:value="proxyPassword"
          placeholder="代理密码"
          autocomplete="new-password"
        />
      </div>
      <a-typography-text v-if="includeProxyCredentials" type="secondary" class="proxy-hint">
        服务端不返回代理密码，请手动输入一次；密码只保留在当前页面，关闭弹窗后清除。
      </a-typography-text>

      <div class="proxy-section">
        <div class="proxy-section-head">
          <span class="proxy-section-title">可用地址</span>
          <a-typography-text type="secondary" class="proxy-section-desc">
            已自动探测本机所有可用网卡地址，可逐条或全部复制
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
              <a-tag v-if="target.custom" color="blue">自定义</a-tag>
              <a-tag v-else-if="target.loopback">本机</a-tag>
              <a-tag v-if="target.interface_name" class="proxy-interface-tag">{{ target.interface_name }}</a-tag>
              <a-tag v-if="target.tls" color="gold">TLS</a-tag>
            </div>
            <div class="proxy-link-line">
              <span class="proxy-url">{{ buildProxyUrl(target) }}</span>
              <a-button type="link" size="small" @click="copyProxyLink(target)">复制</a-button>
            </div>
          </div>
        </div>
        <a-empty v-else description="未探测到可用地址，请先在系统设置中检查代理监听地址">
          <RouterLink to="/settings?section=basic">
            <a-button type="primary" size="small">前往基础设置</a-button>
          </RouterLink>
        </a-empty>
      </div>

      <div class="proxy-section">
        <div class="proxy-section-head">
          <span class="proxy-section-title">额外地址</span>
          <a-typography-text type="secondary" class="proxy-section-desc">
            可添加常用域名或公网 IP，端口沿用代理监听端口
          </a-typography-text>
        </div>

        <div class="proxy-host-form">
          <a-input
            v-model:value="proxyHostInput"
            placeholder="例如 home.example.com 或 203.0.113.10"
            allow-clear
            @keyup.enter="addProxyHost"
          />
          <a-button @click="addProxyHost">添加</a-button>
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
          暂无额外地址
        </a-typography-text>
      </div>
      </div>

      <div class="proxy-modal-footer">
        <span class="proxy-footer-hint">共 {{ proxyTargets.length }} 个地址</span>
        <a-space>
          <a-button @click="closeProxyModal">关闭</a-button>
          <a-button type="primary" :disabled="!proxyTargets.length" @click="copyAllProxyLinks">
            复制全部链接
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

@media (max-width: 768px) {
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
