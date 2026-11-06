<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'antdv-next'
import { getStatus, reloadConfig } from '@/api/system'
import { getSettings, updateSettings } from '@/api/settings'
import type { SettingsView, SettingsPatch, StatusSnapshot } from '@/types/api'

const status = ref<StatusSnapshot | null>(null)
const settings = ref<SettingsView | null>(null)
const loading = ref(false)
const reloading = ref(false)
const saving = ref(false)
const activeTab = ref('status')

function defaultSettings(): SettingsView {
  return {
    listen: '',
    management: '',
    management_token_set: false,
    webui_enabled: true,
    check_interval_seconds: 30,
    max_connections: 0,
    enable_pprof: false,
    socks5_allow_bind: false,
    socks5_allow_udp_associate: false,
    routing_final: 'auto',
    auto_mode: 'direct_first',
    auto_direct_timeout_seconds: 1.5,
    auto_cache_ttl_seconds: 3600,
    geosite_path: '',
    geosite_auto_update: false,
    geosite_update_url: '',
    geosite_update_interval_hours: 24,
    socks5h_redirect_enabled: false,
    dns_type: 'system',
    doh_url: '',
    dot_addr: '',
    dns_fallback: true,
    dns_timeout_seconds: 5,
    ip_precedence: 'ipv4_first',
    dial_timeout_seconds: 10,
    handshake_timeout_seconds: 10,
    http_exchange_timeout_seconds: 30,
    tunnel_idle_timeout_seconds: 300,
    protect_private: true,
    default_action: 'allow',
    deny_cidrs: [],
    allow_cidrs: [],
    deny_domains: [],
    allow_domains: [],
    deny_ports: [],
    allow_ports: [],
    allowed_clients: [],
    rate_limit_connections_per_second: 0,
    rate_limit_burst: 0,
    max_conns_per_user: 0,
    traffic_audit_enabled: false,
    per_user_quota: 0,
    per_upstream_quota: 0,
    quota_window_seconds: 3600,
    log_level: 'info',
  }
}

// 表单模型：字符串/数字与设置视图保持一致，端口与期望状态码使用逗号分隔文本编辑
const form = ref<SettingsView>(defaultSettings())
const denyPortsText = ref('')
const allowPortsText = ref('')

const pprofLinks = [
  { label: 'pprof 首页', path: '/debug/pprof/' },
  { label: 'CPU Profile', path: '/debug/pprof/profile' },
  { label: '堆内存', path: '/debug/pprof/heap' },
  { label: 'Goroutine', path: '/debug/pprof/goroutine' },
  { label: '内存分配', path: '/debug/pprof/allocs' },
  { label: '阻塞分析', path: '/debug/pprof/block' },
  { label: '互斥锁', path: '/debug/pprof/mutex' },
  { label: '线程创建', path: '/debug/pprof/threadcreate' },
  { label: '执行追踪', path: '/debug/pprof/trace' },
]

const finalOptions = [
  { label: 'auto（按规则与探测自动决定）', value: 'auto' },
  { label: 'proxy（默认走上游）', value: 'proxy' },
  { label: 'direct（默认直连）', value: 'direct' },
]
const autoModeOptions = [
  { label: 'direct_first（直连优先，失败后走上游）', value: 'direct_first' },
  { label: 'proxy_first（上游优先，失败后直连）', value: 'proxy_first' },
]
const dnsTypeOptions = [
  { label: 'system（系统 DNS）', value: 'system' },
  { label: 'doh（DNS over HTTPS）', value: 'doh' },
  { label: 'dot（DNS over TLS）', value: 'dot' },
]
const ipPrecedenceOptions = [
  { label: 'ipv4_first（IPv4 优先）', value: 'ipv4_first' },
  { label: 'ipv6_first（IPv6 优先）', value: 'ipv6_first' },
  { label: 'system（跟随系统）', value: 'system' },
]
const destinationActionOptions = [
  { label: 'deny（拒绝）', value: 'deny' },
  { label: 'allow（允许）', value: 'allow' },
]
const logLevelOptions = [
  { label: 'debug', value: 'debug' },
  { label: 'info', value: 'info' },
  { label: 'warn', value: 'warn' },
  { label: 'error', value: 'error' },
]

function formatUptime(seconds: number): string {
  if (!seconds || seconds < 0) return '—'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d} 天 ${h} 小时`
  if (h > 0) return `${h} 小时 ${m} 分钟`
  return `${m} 分钟`
}

function parsePorts(text: string): number[] {
  return text
    .split(/[,\s]+/)
    .map((v) => Number(v.trim()))
    .filter((v) => Number.isInteger(v) && v > 0 && v <= 65535)
}

function applySettingsToForm(view: SettingsView) {
  settings.value = view
  form.value = { ...view }
  denyPortsText.value = (view.deny_ports || []).join(', ')
  allowPortsText.value = (view.allow_ports || []).join(', ')
}

async function fetchAll() {
  loading.value = true
  try {
    const [snapshot, view] = await Promise.all([getStatus(), getSettings()])
    status.value = snapshot
    applySettingsToForm(view)
  } finally {
    loading.value = false
  }
}

async function fetchStatus() {
  status.value = await getStatus()
}

async function handleReload() {
  reloading.value = true
  try {
    await reloadConfig()
    message.success('配置热加载成功')
    await fetchAll()
  } catch {
    // 错误已由拦截器处理
  } finally {
    reloading.value = false
  }
}

function resetForm() {
  if (settings.value) applySettingsToForm(settings.value)
  message.info('已还原为上次加载的设置')
}

async function handleSave() {
  if (!form.value) return
  const f = form.value
  if (f.dns_type === 'doh' && !f.doh_url.trim()) {
    message.warning('DoH 模式需要填写 DoH URL')
    return
  }
  if (f.dns_type === 'dot' && !f.dot_addr.trim()) {
    message.warning('DoT 模式需要填写 DoT 地址')
    return
  }
  if (f.geosite_auto_update && f.geosite_update_interval_hours <= 0) {
    message.warning('启用 GeoSite 自动更新时，更新间隔必须大于 0')
    return
  }

  const patch: SettingsPatch = {
    check_interval_seconds: Math.round(f.check_interval_seconds),
    max_connections: Math.round(f.max_connections),
    enable_pprof: f.enable_pprof,
    socks5_allow_bind: f.socks5_allow_bind,
    socks5_allow_udp_associate: f.socks5_allow_udp_associate,
    routing_final: f.routing_final,
    auto_mode: f.auto_mode,
    auto_direct_timeout_seconds: f.auto_direct_timeout_seconds,
    auto_cache_ttl_seconds: f.auto_cache_ttl_seconds,
    geosite_path: f.geosite_path,
    geosite_auto_update: f.geosite_auto_update,
    geosite_update_url: f.geosite_update_url,
    geosite_update_interval_hours: f.geosite_update_interval_hours,
    socks5h_redirect_enabled: f.socks5h_redirect_enabled,
    dns_type: f.dns_type,
    doh_url: f.doh_url,
    dot_addr: f.dot_addr,
    dns_fallback: f.dns_fallback,
    dns_timeout_seconds: f.dns_timeout_seconds,
    ip_precedence: f.ip_precedence,
    dial_timeout_seconds: f.dial_timeout_seconds,
    handshake_timeout_seconds: f.handshake_timeout_seconds,
    http_exchange_timeout_seconds: f.http_exchange_timeout_seconds,
    tunnel_idle_timeout_seconds: f.tunnel_idle_timeout_seconds,
    protect_private: f.protect_private,
    default_action: f.default_action,
    deny_cidrs: f.deny_cidrs || [],
    allow_cidrs: f.allow_cidrs || [],
    deny_domains: f.deny_domains || [],
    allow_domains: f.allow_domains || [],
    deny_ports: parsePorts(denyPortsText.value),
    allow_ports: parsePorts(allowPortsText.value),
    allowed_clients: f.allowed_clients || [],
    rate_limit_connections_per_second: Math.round(f.rate_limit_connections_per_second),
    rate_limit_burst: Math.round(f.rate_limit_burst),
    max_conns_per_user: Math.round(f.max_conns_per_user),
    traffic_audit_enabled: f.traffic_audit_enabled,
    per_user_quota: Math.round(f.per_user_quota),
    per_upstream_quota: Math.round(f.per_upstream_quota),
    quota_window_seconds: f.quota_window_seconds,
    log_level: f.log_level,
  }

  saving.value = true
  try {
    await updateSettings(patch)
    message.success('设置已保存并生效')
    const view = await getSettings()
    applySettingsToForm(view)
    await fetchStatus()
  } catch {
    // 错误已由拦截器处理
  } finally {
    saving.value = false
  }
}

onMounted(fetchAll)
</script>

<template>
  <div class="page-header">
    <div>
      <a-typography-title :level="4" style="margin: 0;">系统设置</a-typography-title>
      <a-typography-text type="secondary">
        修改后立即写入配置文件并热加载；监听地址等启动参数需重启生效。
      </a-typography-text>
    </div>
    <a-space>
      <a-button @click="fetchAll" :loading="loading">刷新</a-button>
      <a-popconfirm title="重新读取 YAML 配置文件？未保存的表单修改会丢失。" ok-text="重载" cancel-text="取消" @confirm="handleReload">
        <a-button :loading="reloading">重新加载配置</a-button>
      </a-popconfirm>
    </a-space>
  </div>

  <a-tabs v-model:activeKey="activeTab">
    <a-tab-pane key="status" tab="运行状态">
      <a-spin :spinning="loading">
        <a-descriptions :column="3" size="small" bordered>
          <a-descriptions-item label="运行状态">
            <a-tag :color="status?.status === 'up' ? 'green' : 'red'">{{ status?.status || '-' }}</a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="代理监听">
            <a-typography-text code>{{ status?.listen || '-' }}</a-typography-text>
          </a-descriptions-item>
          <a-descriptions-item label="管理端口">
            <a-typography-text code>{{ status?.management || '-' }}</a-typography-text>
          </a-descriptions-item>
          <a-descriptions-item label="上游数量">{{ status?.upstream_count ?? '-' }}</a-descriptions-item>
          <a-descriptions-item label="路由就绪">
            <a-tag :color="status?.router_ready ? 'green' : 'red'">{{ status?.router_ready ? '是' : '否' }}</a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="管理就绪">
            <a-tag :color="status?.manager_ready ? 'green' : 'red'">{{ status?.manager_ready ? '是' : '否' }}</a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="版本">{{ status?.version || '-' }}</a-descriptions-item>
          <a-descriptions-item label="运行时长">{{ formatUptime(status?.uptime_seconds || 0) }}</a-descriptions-item>
          <a-descriptions-item label="时间戳">{{ status?.timestamp || '-' }}</a-descriptions-item>
        </a-descriptions>
      </a-spin>
    </a-tab-pane>

    <a-tab-pane key="general" tab="全局设置">
      <a-spin :spinning="loading">
        <a-form layout="vertical">
          <a-card size="small" title="基础" style="margin-bottom: 16px;">
            <a-row :gutter="16">
              <a-col :xs="24" :md="8">
                <a-form-item label="健康检查间隔（秒）" tooltip="上游健康检查的全局默认间隔">
                  <a-input-number v-model:value="form.check_interval_seconds" :min="1" style="width: 100%;" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="8">
                <a-form-item label="最大连接数" tooltip="0 表示不限制">
                  <a-input-number v-model:value="form.max_connections" :min="0" style="width: 100%;" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="8">
                <a-form-item label="日志级别">
                  <a-select v-model:value="form.log_level" :options="logLevelOptions" />
                </a-form-item>
              </a-col>
            </a-row>
            <a-row :gutter="16">
              <a-col :xs="24" :md="8">
                <a-form-item label="pprof 调试端口" tooltip="启用后可在管理端口访问 /debug/pprof/">
                  <a-switch v-model:checked="form.enable_pprof" checked-children="启用" un-checked-children="关闭" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="8">
                <a-form-item label="SOCKS5 BIND">
                  <a-switch v-model:checked="form.socks5_allow_bind" checked-children="允许" un-checked-children="禁止" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="8">
                <a-form-item label="SOCKS5 UDP ASSOCIATE">
                  <a-switch
                    v-model:checked="form.socks5_allow_udp_associate"
                    checked-children="允许"
                    un-checked-children="禁止"
                  />
                </a-form-item>
              </a-col>
            </a-row>
          </a-card>

          <a-card size="small" title="路由" style="margin-bottom: 16px;">
            <a-row :gutter="16">
              <a-col :xs="24" :md="8">
                <a-form-item label="兜底动作" tooltip="所有规则都未命中时使用的动作">
                  <a-select v-model:value="form.routing_final" :options="finalOptions" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="8">
                <a-form-item label="自动模式" tooltip="final=auto 时的判断方式">
                  <a-select v-model:value="form.auto_mode" :options="autoModeOptions" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="4">
                <a-form-item label="直连探测超时（秒）">
                  <a-input-number v-model:value="form.auto_direct_timeout_seconds" :min="0.1" :step="0.1" style="width: 100%;" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="4">
                <a-form-item label="自动结果缓存（秒）">
                  <a-input-number v-model:value="form.auto_cache_ttl_seconds" :min="0" style="width: 100%;" />
                </a-form-item>
              </a-col>
            </a-row>
            <a-row :gutter="16">
              <a-col :xs="24" :md="10">
                <a-form-item label="GeoSite 数据路径">
                  <a-input v-model:value="form.geosite_path" placeholder="../data/geosite.dat" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="8">
                <a-form-item label="GeoSite 更新地址">
                  <a-input v-model:value="form.geosite_update_url" placeholder="https://..." />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="3">
                <a-form-item label="自动更新">
                  <a-switch v-model:checked="form.geosite_auto_update" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="3">
                <a-form-item label="更新间隔（小时）">
                  <a-input-number v-model:value="form.geosite_update_interval_hours" :min="1" style="width: 100%;" />
                </a-form-item>
              </a-col>
            </a-row>
            <a-form-item label="socks5h 重定向">
              <a-switch v-model:checked="form.socks5h_redirect_enabled" />
              <a-typography-text type="secondary" style="margin-left: 8px;">
                启用后，目标为 socks5h:// 的请求会重定向到指定上游
              </a-typography-text>
            </a-form-item>
          </a-card>

          <a-card size="small" title="DNS" style="margin-bottom: 16px;">
            <a-row :gutter="16">
              <a-col :xs="24" :md="6">
                <a-form-item label="解析方式">
                  <a-select v-model:value="form.dns_type" :options="dnsTypeOptions" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="8">
                <a-form-item label="DoH URL" :tooltip="'仅 doh 模式生效'">
                  <a-input v-model:value="form.doh_url" :disabled="form.dns_type !== 'doh'" placeholder="https://dns.example/dns-query" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="5">
                <a-form-item label="DoT 地址" tooltip="仅 dot 模式生效，格式 host:port">
                  <a-input v-model:value="form.dot_addr" :disabled="form.dns_type !== 'dot'" placeholder="1.1.1.1:853" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="5">
                <a-form-item label="IP 优先级">
                  <a-select v-model:value="form.ip_precedence" :options="ipPrecedenceOptions" />
                </a-form-item>
              </a-col>
            </a-row>
            <a-row :gutter="16">
              <a-col :xs="24" :md="6">
                <a-form-item label="解析超时（秒）">
                  <a-input-number v-model:value="form.dns_timeout_seconds" :min="0.1" :step="0.1" style="width: 100%;" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="6">
                <a-form-item label="失败回退系统 DNS">
                  <a-switch v-model:checked="form.dns_fallback" />
                </a-form-item>
              </a-col>
            </a-row>
          </a-card>

          <a-card size="small" title="超时" style="margin-bottom: 16px;">
            <a-row :gutter="16">
              <a-col :xs="24" :md="6">
                <a-form-item label="拨号超时（秒）">
                  <a-input-number v-model:value="form.dial_timeout_seconds" :min="0.1" :step="0.1" style="width: 100%;" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="6">
                <a-form-item label="握手超时（秒）">
                  <a-input-number v-model:value="form.handshake_timeout_seconds" :min="0.1" :step="0.1" style="width: 100%;" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="6">
                <a-form-item label="HTTP 交换超时（秒）">
                  <a-input-number
                    v-model:value="form.http_exchange_timeout_seconds"
                    :min="0.1"
                    :step="0.1"
                    style="width: 100%;"
                  />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="6">
                <a-form-item label="隧道空闲超时（秒）">
                  <a-input-number
                    v-model:value="form.tunnel_idle_timeout_seconds"
                    :min="0"
                    style="width: 100%;"
                  />
                </a-form-item>
              </a-col>
            </a-row>
          </a-card>

          <a-card size="small" title="目标访问策略" style="margin-bottom: 16px;">
            <a-row :gutter="16">
              <a-col :xs="24" :md="6">
                <a-form-item label="默认动作">
                  <a-select v-model:value="form.default_action" :options="destinationActionOptions" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="6">
                <a-form-item label="保护私网目标" tooltip="启用后默认拒绝访问内网地址">
                  <a-switch v-model:checked="form.protect_private" />
                </a-form-item>
              </a-col>
            </a-row>
            <a-row :gutter="16">
              <a-col :xs="24" :md="12">
                <a-form-item label="拒绝 CIDR">
                  <a-select v-model:value="form.deny_cidrs" mode="tags" placeholder="10.0.0.0/8" style="width: 100%;" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="12">
                <a-form-item label="允许 CIDR">
                  <a-select v-model:value="form.allow_cidrs" mode="tags" placeholder="192.168.0.0/16" style="width: 100%;" />
                </a-form-item>
              </a-col>
            </a-row>
            <a-row :gutter="16">
              <a-col :xs="24" :md="12">
                <a-form-item label="拒绝域名">
                  <a-select v-model:value="form.deny_domains" mode="tags" placeholder="example.com" style="width: 100%;" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="12">
                <a-form-item label="允许域名">
                  <a-select v-model:value="form.allow_domains" mode="tags" placeholder="*.internal" style="width: 100%;" />
                </a-form-item>
              </a-col>
            </a-row>
            <a-row :gutter="16">
              <a-col :xs="24" :md="12">
                <a-form-item label="拒绝端口" tooltip="逗号或空格分隔，如 25, 135, 445">
                  <a-input v-model:value="denyPortsText" placeholder="25, 135, 445" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="12">
                <a-form-item label="允许端口">
                  <a-input v-model:value="allowPortsText" placeholder="80, 443" />
                </a-form-item>
              </a-col>
            </a-row>
          </a-card>

          <a-card size="small" title="访问控制" style="margin-bottom: 16px;">
            <a-row :gutter="16">
              <a-col :xs="24" :md="12">
                <a-form-item label="客户端白名单" tooltip="留空表示不限制；支持单个 IP 或 CIDR">
                  <a-select v-model:value="form.allowed_clients" mode="tags" placeholder="127.0.0.1, 10.0.0.0/8" style="width: 100%;" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="4">
                <a-form-item label="每秒连接上限" tooltip="0 表示不限制">
                  <a-input-number v-model:value="form.rate_limit_connections_per_second" :min="0" style="width: 100%;" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="4">
                <a-form-item label="突发容量">
                  <a-input-number v-model:value="form.rate_limit_burst" :min="0" style="width: 100%;" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="4">
                <a-form-item label="单用户最大连接" tooltip="0 表示不限制">
                  <a-input-number v-model:value="form.max_conns_per_user" :min="0" style="width: 100%;" />
                </a-form-item>
              </a-col>
            </a-row>
          </a-card>

          <a-card size="small" title="流量审计" style="margin-bottom: 16px;">
            <a-row :gutter="16">
              <a-col :xs="24" :md="6">
                <a-form-item label="启用流量审计">
                  <a-switch v-model:checked="form.traffic_audit_enabled" checked-children="启用" un-checked-children="关闭" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="6">
                <a-form-item label="单用户配额（字节）" tooltip="0 表示不限制">
                  <a-input-number v-model:value="form.per_user_quota" :min="0" style="width: 100%;" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="6">
                <a-form-item label="单上游配额（字节）" tooltip="0 表示不限制">
                  <a-input-number v-model:value="form.per_upstream_quota" :min="0" style="width: 100%;" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="6">
                <a-form-item label="配额窗口（秒）" tooltip="0 表示使用默认 1 小时">
                  <a-input-number v-model:value="form.quota_window_seconds" :min="0" style="width: 100%;" />
                </a-form-item>
              </a-col>
            </a-row>
          </a-card>

          <a-space>
            <a-button type="primary" :loading="saving" @click="handleSave">保存设置</a-button>
            <a-button @click="resetForm">还原</a-button>
          </a-space>
        </a-form>
      </a-spin>
    </a-tab-pane>

    <a-tab-pane key="pprof" tab="pprof 性能分析">
      <a-alert type="warning" show-icon style="margin-bottom: 12px;">
        <template #message>仅在 enable_pprof 开启时可用</template>
        <template #description>
          pprof 端点属于敏感入口，不应默认暴露到公网。点击链接将在新窗口打开。
        </template>
      </a-alert>
      <a-list size="small" :data-source="pprofLinks" bordered>
        <template #renderItem="{ item }">
          <a-list-item>
            <a :href="item.path" target="_blank" rel="noopener noreferrer">{{ item.label }}</a>
            <a-typography-text type="secondary" code style="margin-left: 12px; font-size: 12px;">
              {{ item.path }}
            </a-typography-text>
          </a-list-item>
        </template>
      </a-list>
    </a-tab-pane>
  </a-tabs>
</template>
