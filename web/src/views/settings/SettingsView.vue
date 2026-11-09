<script setup lang="ts">
import { ref, computed, watch, onMounted, nextTick } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { message } from 'antdv-next'
import { reloadConfig } from '@/api/system'
import { getSettings, updateSettings } from '@/api/settings'
import { useAuthStore } from '@/stores/auth'
import { copyText } from '@/utils/clipboard'
import type { SettingsView, SettingsPatch } from '@/types/api'

interface SettingsSection {
  key: string
  title: string
  description: string
  // group 用于左侧二级菜单归类，按数组顺序连续分组
  group: string
}

const defaultSection: SettingsSection = {
  key: 'basic',
  title: '基础设置',
  description: '监听信息、健康检查、连接上限与 SOCKS5 能力',
  group: '基础',
}

const sections: SettingsSection[] = [
  defaultSection,
  {
    key: 'panel',
    title: '管理面板与安全',
    description: '内置 WebUI 开关与管理令牌',
    group: '基础',
  },
  {
    key: 'routing',
    title: '路由策略',
    description: '兜底路由与自动探测策略',
    group: '网络',
  },
  {
    key: 'dns',
    title: 'DNS 解析',
    description: '解析方式、DoH/DoT、超时与 IP 优先级',
    group: '网络',
  },
  {
    key: 'timeout',
    title: '连接与超时',
    description: '拨号、握手、HTTP 交换与隧道空闲超时',
    group: '网络',
  },
  {
    key: 'destination',
    title: '目标访问策略',
    description: '默认动作与 CIDR、域名、端口黑白名单',
    group: '访问与配额',
  },
  {
    key: 'access',
    title: '访问控制',
    description: '客户端白名单、连接限速与单用户并发上限',
    group: '访问与配额',
  },
  {
    key: 'quota',
    title: '流量审计与配额',
    description: '流量审计开关、用户与上游配额',
    group: '访问与配额',
  },
]

// 按 group 折叠为带标题的分组菜单
const groupedSections = computed(() => {
  const groups: { title: string; items: SettingsSection[] }[] = []
  for (const section of sections) {
    const last = groups[groups.length - 1]
    if (last && last.title === section.group) {
      last.items.push(section)
    } else {
      groups.push({ title: section.group, items: [section] })
    }
  }
  return groups
})

// 移动端用下拉框代替左侧分类菜单，避免固定 220px 侧栏挤压表单
const sectionOptions = sections.map((section) => ({
  label: section.title,
  value: section.key,
}))

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()

const settings = ref<SettingsView | null>(null)
const loading = ref(false)
const reloading = ref(false)
const saving = ref(false)
const clearingToken = ref(false)
// 新管理令牌输入：留空表示不修改，避免每次保存都覆盖现有令牌
const tokenInput = ref('')
const detailRef = ref<HTMLElement | null>(null)

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
    geosite_use_upstream: true,
    geosite_update_url: '',
    geosite_update_interval_hours: 24,
    geosite_update_token_set: false,
    geosite_sync_enabled: false,
    geosite_sync_token_set: false,
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
    traffic_retention_days: 30,
    log_level: 'info',
  }
}

// 表单模型：字符串/数字与设置视图保持一致，端口使用逗号分隔文本编辑
const form = ref<SettingsView>(defaultSettings())
const denyPortsText = ref('')
const allowPortsText = ref('')

const finalOptions = [
  { label: 'auto（按规则与探测自动决定）', value: 'auto' },
  { label: 'proxy（默认走上游）', value: 'proxy' },
  { label: 'direct（默认直连）', value: 'direct' },
  { label: 'deny（拒绝未命中流量）', value: 'deny' },
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

// 实际生效的监听地址：多 listener 模式下由后端以逗号拼接返回
const listenList = computed(() =>
  (settings.value?.listen || '')
    .split(',')
    .map((v) => v.trim())
    .filter(Boolean),
)
const multiListener = computed(() => listenList.value.length > 1)

function isSectionKey(value: unknown): value is string {
  return typeof value === 'string' && sections.some((section) => section.key === value)
}

function resolveSectionFromRoute(): string {
  const querySection = route.query.section
  const key = Array.isArray(querySection) ? querySection[0] : querySection
  return isSectionKey(key) ? key : defaultSection.key
}

const activeSection = ref<string>(resolveSectionFromRoute())
const currentSection = computed(
  () => sections.find((section) => section.key === activeSection.value) ?? defaultSection,
)

// 章节与路由绑定：切换章节写入 ?section=xxx，刷新后可正确定位
function selectSection(key: string) {
  if (!isSectionKey(key)) return
  activeSection.value = key
  if (route.query.section === key) return
  router.replace({ query: { ...route.query, section: key } })
}

// 支持直接修改地址栏或浏览器前进/后退
watch(
  () => route.query.section,
  (value) => {
    const key = Array.isArray(value) ? value[0] : value
    if (isSectionKey(key)) {
      activeSection.value = key
      return
    }
    activeSection.value = defaultSection.key
    if (key !== undefined) {
      const query = { ...route.query }
      delete query.section
      router.replace({ query })
    }
  },
  { immediate: true },
)

// 切换章节后右侧内容回到顶部
watch(activeSection, () => {
  detailRef.value?.scrollTo({ top: 0 })
})

// 端口输入允许 1-65535 的整数，非法片段单独提示，避免后端校验失败后才发现
function parsePorts(text: string): { ports: number[]; invalid: string[] } {
  const ports: number[] = []
  const invalid: string[] = []
  for (const raw of text.split(/[,\s]+/)) {
    const v = raw.trim()
    if (!v) continue
    const n = Number(v)
    if (!Number.isInteger(n) || n < 1 || n > 65535) {
      invalid.push(v)
      continue
    }
    ports.push(n)
  }
  return { ports, invalid }
}

function formSnapshot() {
  return JSON.stringify({
    form: form.value,
    deny: denyPortsText.value,
    allow: allowPortsText.value,
    token: tokenInput.value.trim(),
  })
}

// 保存基线，用于判断表单是否有未保存修改
const savedSnapshot = ref('')

function applySettingsToForm(view: SettingsView) {
  settings.value = view
  form.value = { ...view }
  denyPortsText.value = (view.deny_ports || []).join(', ')
  allowPortsText.value = (view.allow_ports || []).join(', ')
  tokenInput.value = ''
  // 等表单控件完成挂载与初值规范化后再记录基线，避免把控件归一化误判为修改
  void nextTick(() => {
    savedSnapshot.value = formSnapshot()
  })
}

const isDirty = computed(
  () => savedSnapshot.value !== '' && formSnapshot() !== savedSnapshot.value,
)

function markSaved() {
  savedSnapshot.value = formSnapshot()
}

// 离开设置页前提示未保存修改，避免误切换页面丢失编辑内容
const leaveConfirmOpen = ref(false)
let leaveResolve: ((value: boolean) => void) | null = null

onBeforeRouteLeave(() => {
  if (!isDirty.value) return true
  leaveConfirmOpen.value = true
  return new Promise<boolean>((resolve) => {
    leaveResolve = resolve
  })
})

function resolveLeave(leave: boolean) {
  leaveConfirmOpen.value = false
  leaveResolve?.(leave)
  leaveResolve = null
}

async function fetchAll() {
  loading.value = true
  try {
    applySettingsToForm(await getSettings())
  } finally {
    loading.value = false
  }
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
  const denyParsed = parsePorts(denyPortsText.value)
  const allowParsed = parsePorts(allowPortsText.value)
  const invalidPorts = [...denyParsed.invalid, ...allowParsed.invalid]
  if (invalidPorts.length > 0) {
    message.warning(`端口需为 1-65535 的整数，请修正：${invalidPorts.join(', ')}`)
    return
  }
  const newToken = tokenInput.value.trim()
  if (newToken && newToken.length < 8) {
    message.warning('管理令牌至少 8 位，过短的令牌容易被暴力破解')
    return
  }

  const patch: SettingsPatch = {
    check_interval_seconds: Math.round(f.check_interval_seconds),
    max_connections: Math.round(f.max_connections),
    webui_enabled: f.webui_enabled,
    // pprof 开关由独立页面管理，此处不提交，避免覆盖另一页面的修改
    socks5_allow_bind: f.socks5_allow_bind,
    socks5_allow_udp_associate: f.socks5_allow_udp_associate,
    routing_final: f.routing_final,
    auto_mode: f.auto_mode,
    auto_direct_timeout_seconds: f.auto_direct_timeout_seconds,
    auto_cache_ttl_seconds: f.auto_cache_ttl_seconds,
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
    deny_ports: denyParsed.ports,
    allow_ports: allowParsed.ports,
    allowed_clients: f.allowed_clients || [],
    rate_limit_connections_per_second: Math.round(f.rate_limit_connections_per_second),
    rate_limit_burst: Math.round(f.rate_limit_burst),
    max_conns_per_user: Math.round(f.max_conns_per_user),
    traffic_audit_enabled: f.traffic_audit_enabled,
    per_user_quota: Math.round(f.per_user_quota),
    per_upstream_quota: Math.round(f.per_upstream_quota),
    quota_window_seconds: f.quota_window_seconds,
    traffic_retention_days: Math.max(1, Math.round(f.traffic_retention_days || 30)),
    log_level: f.log_level,
  }

  if (newToken) {
    patch.management_auth_token = newToken
  }

  saving.value = true
  try {
    await updateSettings(patch)
    if (newToken) {
      // 令牌已轮换：当前会话的旧令牌立即失效，清理本地状态并回到登录页
      message.success('设置已保存，管理令牌已更新，请使用新令牌重新登录')
      tokenInput.value = ''
      markSaved()
      authStore.logout()
      router.push('/login')
      return
    }
    message.success('设置已保存并生效')
    applySettingsToForm(await getSettings())
  } catch {
    // 错误已由拦截器处理
  } finally {
    saving.value = false
  }
}

// 生成随机管理令牌：由系统生成，保存后需用新令牌重新登录。
async function handleGenerateToken() {
  const bytes = new Uint8Array(32)
  crypto.getRandomValues(bytes)
  tokenInput.value = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
  if (await copyText(tokenInput.value)) {
    message.success('已生成并复制新令牌，请妥善保存后点击保存')
    return
  }
  message.success('已生成新令牌，请点击输入框右侧眼睛图标查看并复制后保存')
}

// 清除管理令牌：仅回环管理地址允许（非回环时后端会拒绝）
async function handleClearToken() {
  clearingToken.value = true
  try {
    await updateSettings({ management_auth_token: '' })
    message.success('管理令牌已清除，管理端口现在无需认证')
    applySettingsToForm(await getSettings())
  } catch {
    // 错误已由拦截器处理
  } finally {
    clearingToken.value = false
  }
}

onMounted(fetchAll)
</script>

<template>
  <div class="settings">
    <div class="settings-header">
      <div class="settings-header-text">
        <a-typography-title :level="4" style="margin: 0;">系统设置</a-typography-title>
        <a-typography-text type="secondary">
          修改后立即写入配置文件并热加载；WebUI、pprof 与管理令牌即时生效，监听地址等启动参数需重启。
        </a-typography-text>
        <div class="settings-section-field">
          <label class="settings-section-label" for="settings-section-select">设置分类</label>
          <a-select
            id="settings-section-select"
            class="settings-section-select"
            :value="activeSection"
            :options="sectionOptions"
            @change="(value) => selectSection(String(value))"
          />
        </div>
      </div>
      <a-space wrap class="settings-actions">
        <a-button @click="fetchAll" :loading="loading">刷新</a-button>
        <a-popconfirm
          title="重新读取 YAML 配置文件？未保存的表单修改会丢失。"
          ok-text="重载"
          cancel-text="取消"
          @confirm="handleReload"
        >
          <a-button :loading="reloading">重新加载配置</a-button>
        </a-popconfirm>
        <a-button @click="resetForm">还原</a-button>
        <a-button type="primary" :loading="saving" :disabled="!isDirty" @click="handleSave">保存设置</a-button>
      </a-space>
    </div>

    <div class="settings-body">
      <!-- 左侧设置分类 -->
      <aside class="settings-aside">
        <a-card size="small" class="settings-nav-card">
          <template #title>设置分类</template>
          <a-menu :selected-keys="[activeSection]" mode="inline" style="border: none;">
            <a-menu-item-group
              v-for="group in groupedSections"
              :key="group.title"
              :title="group.title"
            >
              <a-menu-item
                v-for="section in group.items"
                :key="section.key"
                @click="selectSection(section.key)"
              >
                {{ section.title }}
              </a-menu-item>
            </a-menu-item-group>
          </a-menu>
        </a-card>
      </aside>

      <!-- 右侧设置内容：独立滚动 -->
      <section class="settings-detail">
        <div class="settings-detail-header">
          <a-typography-title :level="5" style="margin: 0;">
            {{ currentSection.title }}
          </a-typography-title>
          <a-typography-text type="secondary">{{ currentSection.description }}</a-typography-text>
        </div>

        <div ref="detailRef" class="settings-detail-scroll">
          <a-spin :spinning="loading">
            <a-form layout="vertical">
              <!-- 基础设置 -->
              <div v-show="activeSection === 'basic'">
                <a-card size="small" title="当前生效配置" style="margin-bottom: 16px;">
                  <a-descriptions size="small" bordered :column="{ xs: 1, sm: 2, lg: 3 }">
                    <a-descriptions-item label="生效监听地址">
                      <a-typography-text code>{{ settings?.listen || '—' }}</a-typography-text>
                    </a-descriptions-item>
                    <a-descriptions-item label="管理端口">
                      <a-typography-text code>{{ settings?.management || '未配置' }}</a-typography-text>
                    </a-descriptions-item>
                    <a-descriptions-item label="管理令牌">
                      <a-tag :color="settings?.management_token_set ? 'green' : 'orange'">
                        {{ settings?.management_token_set ? '已设置' : '未设置' }}
                      </a-tag>
                    </a-descriptions-item>
                  </a-descriptions>
                  <a-alert
                    v-if="multiListener"
                    type="info"
                    show-icon
                    style="margin-top: 12px;"
                    message="检测到多个监听端口"
                    :description="`共 ${listenList.length} 个端口（${listenList.join(', ')}）。SOCKS5 命令、访问控制与最大连接数的修改会同步写入所有端口；如需按端口差异化配置，请直接编辑 YAML 中的 listeners。`"
                  />
                </a-card>

                <a-card size="small" title="检查与连接" style="margin-bottom: 16px;">
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="8">
                      <a-form-item label="健康检查间隔（秒）" tooltip="上游健康检查的全局默认间隔；0 表示停止周期性检查">
                        <a-input-number
                          v-model:value="form.check_interval_seconds"
                          :min="0"
                          style="width: 100%;"
                          aria-label="健康检查间隔（秒）"
                        />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="8">
                      <a-form-item label="最大连接数（兜底）" tooltip="listener 未单独设置时的并发上限；0 表示不限制">
                        <a-input-number
                          v-model:value="form.max_connections"
                          :min="0"
                          style="width: 100%;"
                          aria-label="最大连接数（兜底）"
                        />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="8">
                      <a-form-item label="日志级别">
                        <a-select v-model:value="form.log_level" :options="logLevelOptions" aria-label="日志级别" />
                      </a-form-item>
                    </a-col>
                  </a-row>
                </a-card>

                <a-card size="small" title="SOCKS5 能力">
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="12">
                      <a-form-item label="SOCKS5 BIND">
                        <a-switch
                          v-model:checked="form.socks5_allow_bind"
                          checked-children="允许"
                          un-checked-children="禁止"
                          aria-label="SOCKS5 BIND"
                        />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="12">
                      <a-form-item label="SOCKS5 UDP ASSOCIATE">
                        <a-switch
                          v-model:checked="form.socks5_allow_udp_associate"
                          checked-children="允许"
                          un-checked-children="禁止"
                          aria-label="SOCKS5 UDP ASSOCIATE"
                        />
                      </a-form-item>
                    </a-col>
                  </a-row>
                </a-card>
              </div>

              <!-- 管理面板与安全 -->
              <div v-show="activeSection === 'panel'">
                <a-card size="small" title="管理面板" style="margin-bottom: 16px;">
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="8">
                      <a-form-item label="内置 WebUI" tooltip="关闭后管理端口仅提供 API，不再返回前端页面">
                        <a-switch
                          v-model:checked="form.webui_enabled"
                          checked-children="启用"
                          un-checked-children="关闭"
                          aria-label="内置 WebUI"
                        />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="16">
                      <a-form-item
                        label="管理令牌"
                        :tooltip="`对应 management_auth_token；当前${settings?.management_token_set ? '已设置' : '未设置'}。留空表示保持不变。`"
                      >
                        <div class="token-input-row">
                          <a-input-password
                            v-model:value="tokenInput"
                            placeholder="留空则不修改，修改后需重新登录"
                            autocomplete="new-password"
                          />
                          <a-button @click="handleGenerateToken">生成</a-button>
                        </div>
                      </a-form-item>
                    </a-col>
                  </a-row>
                  <a-space wrap>
                    <a-popconfirm
                      v-if="settings?.management_token_set"
                      title="清除后管理端口将不再要求认证，仅建议在回环地址下使用，确定清除？"
                      ok-text="清除"
                      cancel-text="取消"
                      @confirm="handleClearToken"
                    >
                      <a-button danger :loading="clearingToken">清除管理令牌</a-button>
                    </a-popconfirm>
                    <a-typography-text type="secondary" style="font-size: 12px;">
                      WebUI 开关与令牌修改保存后立即生效；轮换令牌会强制当前会话重新登录。
                    </a-typography-text>
                  </a-space>
                </a-card>

                <a-alert
                  type="warning"
                  show-icon
                  message="管理面板安全提示"
                  description="管理端口绑定非回环地址时必须配置管理令牌。可直接点击「生成」由系统创建强随机令牌；令牌修改后当前会话会立即失效。"
                />
              </div>

              <!-- 路由与 GeoSite -->
              <div v-show="activeSection === 'routing'">
                <a-card size="small" title="路由策略" style="margin-bottom: 16px;">
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
                        <a-input-number
                          v-model:value="form.auto_direct_timeout_seconds"
                          :min="0.1"
                          :step="0.1"
                          style="width: 100%;"
                        />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="4">
                      <a-form-item label="自动结果缓存（秒）">
                        <a-input-number v-model:value="form.auto_cache_ttl_seconds" :min="0" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                  </a-row>
                  <a-form-item label="socks5h 重定向">
                    <a-switch v-model:checked="form.socks5h_redirect_enabled" aria-label="socks5h 重定向" />
                    <a-typography-text type="secondary" style="margin-left: 8px;">
                      启用后，目标为 socks5h:// 的请求会重定向到指定上游
                    </a-typography-text>
                  </a-form-item>
                </a-card>
              </div>

              <!-- DNS 解析 -->
              <div v-show="activeSection === 'dns'">
                <a-card size="small" title="DNS 解析" style="margin-bottom: 16px;">
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="6">
                      <a-form-item label="解析方式">
                        <a-select v-model:value="form.dns_type" :options="dnsTypeOptions" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="8">
                      <a-form-item label="DoH URL" tooltip="仅 doh 模式生效">
                        <a-input
                          v-model:value="form.doh_url"
                          :disabled="form.dns_type !== 'doh'"
                          placeholder="https://dns.example/dns-query"
                        />
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
                        <a-switch v-model:checked="form.dns_fallback" aria-label="失败回退系统 DNS" />
                      </a-form-item>
                    </a-col>
                  </a-row>
                </a-card>
              </div>

              <!-- 连接与超时 -->
              <div v-show="activeSection === 'timeout'">
                <a-card size="small" title="超时参数">
                  <a-alert
                    type="info"
                    show-icon
                    style="margin-bottom: 12px;"
                    message="填写 0 表示使用内置默认值（拨号/握手 10s、HTTP 交换 60s、隧道空闲 5min）；数值单位为秒。"
                  />
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="6">
                      <a-form-item label="拨号超时（秒）">
                        <a-input-number v-model:value="form.dial_timeout_seconds" :min="0" :step="0.1" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="6">
                      <a-form-item label="握手超时（秒）">
                        <a-input-number v-model:value="form.handshake_timeout_seconds" :min="0" :step="0.1" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="6">
                      <a-form-item label="HTTP 交换超时（秒）">
                        <a-input-number
                          v-model:value="form.http_exchange_timeout_seconds"
                          :min="0"
                          :step="0.1"
                          style="width: 100%;"
                        />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="6">
                      <a-form-item label="隧道空闲超时（秒）">
                        <a-input-number v-model:value="form.tunnel_idle_timeout_seconds" :min="0" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                  </a-row>
                </a-card>
              </div>

              <!-- 目标访问策略 -->
              <div v-show="activeSection === 'destination'">
                <a-card size="small" title="默认动作">
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="6">
                      <a-form-item label="默认动作">
                        <a-select v-model:value="form.default_action" :options="destinationActionOptions" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="6">
                      <a-form-item label="保护私网目标" tooltip="启用后默认拒绝访问内网地址">
                        <a-switch v-model:checked="form.protect_private" aria-label="保护私网目标" />
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
              </div>

              <!-- 访问控制 -->
              <div v-show="activeSection === 'access'">
                <a-card size="small" title="客户端与限速">
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
              </div>

              <!-- 流量审计与配额 -->
              <div v-show="activeSection === 'quota'">
                <a-card size="small" title="流量审计与配额">
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="4">
                      <a-form-item label="启用流量审计">
                        <a-switch v-model:checked="form.traffic_audit_enabled" checked-children="启用" un-checked-children="关闭" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="5">
                      <a-form-item label="单用户配额（字节）" tooltip="0 表示不限制">
                        <a-input-number v-model:value="form.per_user_quota" :min="0" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="5">
                      <a-form-item label="单上游配额（字节）" tooltip="0 表示不限制">
                        <a-input-number v-model:value="form.per_upstream_quota" :min="0" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="5">
                      <a-form-item label="配额窗口（秒）" tooltip="0 表示使用默认 1 小时">
                        <a-input-number v-model:value="form.quota_window_seconds" :min="0" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="5">
                      <a-form-item label="流量保留天数" tooltip="默认保留 30 天，0 表示使用默认值">
                        <a-input-number
                          v-model:value="form.traffic_retention_days"
                          :min="0"
                          :max="3650"
                          style="width: 100%;"
                        />
                      </a-form-item>
                    </a-col>
                  </a-row>
                </a-card>
              </div>

            </a-form>
          </a-spin>
        </div>
      </section>
    </div>
  </div>

  <a-modal
    v-model:open="leaveConfirmOpen"
    title="有未保存的设置"
    ok-text="离开"
    cancel-text="继续编辑"
    :mask-closable="false"
    @ok="resolveLeave(true)"
    @cancel="resolveLeave(false)"
  >
    <p>离开后本次修改将丢失，确定离开吗？</p>
  </a-modal>
</template>

<style scoped>
/* 页面撑满工作区高度：标题操作区固定，右侧内容独立滚动 */
.settings {
  display: flex;
  flex-direction: column;
  height: 100%;
  width: 100%;
}

.token-input-row {
  display: flex;
  gap: 8px;
}

.token-input-row :deep(.ant-input-affix-wrapper) {
  flex: 1 1 auto;
}

.settings-header {
  flex: 0 0 auto;
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  flex-wrap: wrap;
  padding: 0 0 16px 0;
}

.settings-header-text {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.settings-body {
  display: flex;
  flex: 1 1 auto;
  gap: 16px;
  min-height: 0;
}

/* 左侧分类固定宽度，避免菜单文字被截断 */
.settings-aside {
  flex: 0 0 220px;
  width: 220px;
  min-width: 220px;
  height: 100%;
  min-height: 0;
}

.settings-nav-card {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.settings-nav-card :deep(.ant-card-body) {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
}

.settings-detail {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
}

.settings-detail-header {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding-bottom: 12px;
}

.settings-detail-scroll {
  flex: 1 1 auto;
  min-height: 0;
  overflow-x: hidden;
  overflow-y: auto;
  overscroll-behavior: contain;
}

.settings-section-field {
  display: none;
}

.settings-section-label {
  display: block;
  margin-bottom: 4px;
  font-size: 12px;
  color: rgba(128, 128, 128, 0.9);
}

@media (max-width: 768px) {
  .settings-header {
    gap: 12px;
    padding-bottom: 12px;
  }

  .settings-actions {
    width: 100%;
  }

  .settings-actions :deep(.ant-space-item) {
    flex: 1 1 auto;
  }

  .settings-actions :deep(.ant-btn) {
    width: 100%;
  }

  .settings-section-field {
    display: block;
    width: 100%;
    margin-top: 8px;
  }

  .settings-section-select {
    width: 100%;
  }

  .settings-aside {
    display: none;
  }

  .settings-body {
    display: block;
  }

  .settings-detail {
    height: 100%;
  }
}
</style>
