<script setup lang="ts">
import { ref, computed, watch, onMounted, nextTick } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { message } from 'antdv-next'
import { reloadConfig } from '@/api/system'
import { getSettings, updateSettings } from '@/api/settings'
import { useAuthStore } from '@/stores/auth'
import { copyText } from '@/utils/clipboard'
import { useHotkeys } from '@/composables/useHotkeys'
import type { SettingsView, SettingsPatch } from '@/types/api'

interface SettingsSection {
  key: string
  title: string
  description: string
  // group 用于左侧二级菜单归类，按数组顺序连续分组
  group: string
}

const { t } = useI18n()

// 章节标题与分组走翻译，切换语言后菜单与详情头同步更新
const SECTION_DEFS = [
  { key: 'basic', group: 'basic' },
  { key: 'panel', group: 'basic' },
  { key: 'routing', group: 'network' },
  { key: 'dns', group: 'network' },
  { key: 'timeout', group: 'network' },
  { key: 'destination', group: 'access' },
  { key: 'access', group: 'access' },
  { key: 'quota', group: 'access' },
] as const

const defaultSectionKey = 'basic'

const sections = computed<SettingsSection[]>(() =>
  SECTION_DEFS.map((def) => ({
    key: def.key,
    group: t(`pages.settings.groups.${def.group}`),
    title: t(`pages.settings.sections.${def.key}.title`),
    description: t(`pages.settings.sections.${def.key}.description`),
  })),
)

// 按 group 折叠为带标题的分组菜单
const groupedSections = computed(() => {
  const groups: { title: string; items: SettingsSection[] }[] = []
  for (const section of sections.value) {
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
const sectionOptions = computed(() =>
  sections.value.map((section) => ({
    label: section.title,
    value: section.key,
  })),
)

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()

const settings = ref<SettingsView | null>(null)
const loading = ref(false)
// 设置读取失败：此时表单里只是默认值，绝不能当成真实配置保存
const loadError = ref(false)
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

const finalOptions = computed(() => [
  { label: t('pages.settings.options.final.auto'), value: 'auto' },
  { label: t('pages.settings.options.final.proxy'), value: 'proxy' },
  { label: t('pages.settings.options.final.direct'), value: 'direct' },
  { label: t('pages.settings.options.final.deny'), value: 'deny' },
])
const autoModeOptions = computed(() => [
  { label: t('pages.settings.options.autoMode.directFirst'), value: 'direct_first' },
  { label: t('pages.settings.options.autoMode.proxyFirst'), value: 'proxy_first' },
])
const dnsTypeOptions = computed(() => [
  { label: t('pages.settings.options.dnsType.system'), value: 'system' },
  { label: t('pages.settings.options.dnsType.doh'), value: 'doh' },
  { label: t('pages.settings.options.dnsType.dot'), value: 'dot' },
])
const ipPrecedenceOptions = computed(() => [
  { label: t('pages.settings.options.ipPrecedence.ipv4First'), value: 'ipv4_first' },
  { label: t('pages.settings.options.ipPrecedence.ipv6First'), value: 'ipv6_first' },
  { label: t('pages.settings.options.ipPrecedence.system'), value: 'system' },
])
const destinationActionOptions = computed(() => [
  { label: t('pages.settings.options.destinationAction.deny'), value: 'deny' },
  { label: t('pages.settings.options.destinationAction.allow'), value: 'allow' },
])
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
  return typeof value === 'string' && sections.value.some((section) => section.key === value)
}

function resolveSectionFromRoute(): string {
  const querySection = route.query.section
  const key = Array.isArray(querySection) ? querySection[0] : querySection
  return isSectionKey(key) ? key : defaultSectionKey
}

const activeSection = ref<string>(resolveSectionFromRoute())
const currentSection = computed(
  () => sections.value.find((section) => section.key === activeSection.value) ?? sections.value[0],
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
    activeSection.value = defaultSectionKey
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

// 字段级校验：保存时把错误挂到对应字段，同时切换到该字段所在章节，
// 避免顶部 toast 一闪而过、用户还要自己找是哪个字段填错了。
interface SettingsFieldError {
  key: string
  section: string
  message: string
}

const settingsErrors = ref<Record<string, string>>({})

function resetSettingsErrors() {
  settingsErrors.value = {}
}

watch(
  () => [
    form.value.dns_type,
    form.value.doh_url,
    form.value.dot_addr,
    denyPortsText.value,
    allowPortsText.value,
    tokenInput.value,
  ],
  () => resetSettingsErrors(),
)

function validateSettings(): SettingsFieldError[] {
  const f = form.value
  const errors: SettingsFieldError[] = []

  if (f.dns_type === 'doh') {
    const url = f.doh_url.trim()
    if (!url) {
      errors.push({ key: 'doh_url', section: 'dns', message: t('pages.settings.validation.dohRequired') })
    } else if (!/^https?:\/\/\S+$/i.test(url)) {
      errors.push({ key: 'doh_url', section: 'dns', message: t('pages.settings.validation.dohInvalid') })
    }
  }
  if (f.dns_type === 'dot') {
    const addr = f.dot_addr.trim()
    if (!addr) {
      errors.push({ key: 'dot_addr', section: 'dns', message: t('pages.settings.validation.dotRequired') })
    } else if (!/^[^\s:]+:\d{1,5}$/.test(addr)) {
      errors.push({ key: 'dot_addr', section: 'dns', message: t('pages.settings.validation.dotInvalid') })
    }
  }

  const deny = parsePorts(denyPortsText.value)
  if (deny.invalid.length > 0) {
    errors.push({
      key: 'deny_ports',
      section: 'destination',
      message: t('pages.settings.validation.portsInvalid', { ports: deny.invalid.join(', ') }),
    })
  }
  const allow = parsePorts(allowPortsText.value)
  if (allow.invalid.length > 0) {
    errors.push({
      key: 'allow_ports',
      section: 'destination',
      message: t('pages.settings.validation.portsInvalid', { ports: allow.invalid.join(', ') }),
    })
  }

  const token = tokenInput.value.trim()
  if (token && token.length < 8) {
    errors.push({ key: 'token', section: 'panel', message: t('pages.settings.validation.tokenTooShort') })
  }

  // GeoSite 自动更新间隔由 GeoSite 页面维护，没有对应输入框，保留仅提示
  if (f.geosite_auto_update && f.geosite_update_interval_hours <= 0) {
    errors.push({
      key: 'geosite_interval',
      section: 'routing',
      message: t('pages.settings.validation.geositeInterval'),
    })
  }

  return errors
}

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
    loadError.value = false
  } catch {
    // 保留已有内容（首次加载时为空），只标记失败，避免默认值被误认成真实配置
    loadError.value = true
  } finally {
    loading.value = false
  }
}

async function handleReload() {
  reloading.value = true
  try {
    await reloadConfig()
    message.success(t('pages.settings.messages.reloaded'))
    await fetchAll()
  } catch {
    // 错误已由拦截器处理
  } finally {
    reloading.value = false
  }
}

// Ctrl/Cmd+S 直接保存：光标停在输入框里时也要生效，否则改完按保存会没反应
useHotkeys([
  {
    key: 's',
    ctrlOrCmd: true,
    allowInInput: true,
    handler: () => {
      if (saving.value) return
      void handleSave()
    },
  },
])

function resetForm() {
  if (settings.value) applySettingsToForm(settings.value)
  message.info(t('pages.settings.messages.reset'))
}

async function handleSave() {
  if (!form.value) return
  if (loadError.value || !settings.value) {
    message.warning(t('pages.settings.messages.notLoaded'))
    return
  }
  const f = form.value

  resetSettingsErrors()
  const errors = validateSettings()
  for (const err of errors) settingsErrors.value[err.key] = err.message
  if (errors.length > 0) {
    // 跳到第一个出错字段所在章节，并保留一条汇总提示
    selectSection(errors[0].section)
    const summary =
      errors.length === 1
        ? errors[0].message
        : t('pages.settings.messages.errorSummary', {
            message: errors[0].message,
            count: errors.length,
          })
    message.warning(summary)
    return
  }
  const newToken = tokenInput.value.trim()

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
    deny_ports: parsePorts(denyPortsText.value).ports,
    allow_ports: parsePorts(allowPortsText.value).ports,
    allowed_clients: f.allowed_clients || [],
    rate_limit_connections_per_second: Math.round(f.rate_limit_connections_per_second),
    rate_limit_burst: Math.round(f.rate_limit_burst),
    max_conns_per_user: Math.round(f.max_conns_per_user),
    traffic_audit_enabled: f.traffic_audit_enabled,
    per_user_quota: Math.round(f.per_user_quota),
    per_upstream_quota: Math.round(f.per_upstream_quota),
    quota_window_seconds: f.quota_window_seconds,
    // 0 在运行时表示「用默认 30 天」；此前这里强转成 1，会让流量明细只保留一天
    traffic_retention_days: Math.max(0, Math.round(f.traffic_retention_days ?? 30)),
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
      message.success(t('pages.settings.messages.savedTokenRotated'))
      tokenInput.value = ''
      markSaved()
      authStore.logout()
      router.push('/login')
      return
    }
    message.success(t('pages.settings.messages.saved'))
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
    message.success(t('pages.settings.messages.tokenGeneratedCopied'))
    return
  }
  message.success(t('pages.settings.messages.tokenGenerated'))
}

// 清除管理令牌：仅回环管理地址允许（非回环时后端会拒绝）
async function handleClearToken() {
  clearingToken.value = true
  try {
    await updateSettings({ management_auth_token: '' })
    message.success(t('pages.settings.messages.tokenCleared'))
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
  <div class="page-shell">
    <div class="page-header">
      <div class="page-header-text">
        <a-typography-title :level="4" style="margin: 0;">{{ t('pages.settings.title') }}</a-typography-title>
        <a-typography-text type="secondary">
          {{ t('pages.settings.subtitle') }}
        </a-typography-text>
        <div class="settings-section-field">
          <label class="settings-section-label" for="settings-section-select">
            {{ t('pages.settings.sectionLabel') }}
          </label>
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
        <a-button @click="fetchAll" :loading="loading">{{ t('common.refresh') }}</a-button>
        <a-popconfirm
          :title="t('pages.settings.actions.reloadConfirm')"
          :ok-text="t('pages.settings.actions.reloadOk')"
          :cancel-text="t('common.cancel')"
          @confirm="handleReload"
        >
          <a-button :loading="reloading">{{ t('pages.settings.actions.reload') }}</a-button>
        </a-popconfirm>
        <a-button @click="resetForm">{{ t('pages.settings.actions.reset') }}</a-button>
        <a-button
          type="primary"
          :loading="saving"
          :disabled="!isDirty || loadError || !settings"
          :title="t('pages.settings.actions.saveTitle')"
          @click="handleSave"
        >
          {{ t('pages.settings.actions.save') }}
        </a-button>
      </a-space>
    </div>

    <a-alert
      v-if="loadError"
      type="error"
      show-icon
      class="settings-load-error"
      :message="
        settings ? t('pages.settings.loadError.refreshTitle') : t('pages.settings.loadError.loadTitle')
      "
      :description="
        settings
          ? t('pages.settings.loadError.refreshDescription')
          : t('pages.settings.loadError.loadDescription')
      "
    >
      <template #action>
        <a-button size="small" :loading="loading" @click="fetchAll">
          {{ t('pages.settings.loadError.retry') }}
        </a-button>
      </template>
    </a-alert>

    <div v-if="!loadError || settings" class="settings-body">
      <!-- 左侧设置分类 -->
      <aside class="settings-aside">
        <a-card size="small" class="settings-nav-card">
          <template #title>{{ t('pages.settings.navTitle') }}</template>
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
                <a-card size="small" :title="t('pages.settings.current.title')" style="margin-bottom: 16px;">
                  <a-descriptions size="small" bordered :column="{ xs: 1, sm: 2, lg: 3 }">
                    <a-descriptions-item :label="t('pages.settings.current.listen')">
                      <a-typography-text code>{{ settings?.listen || '—' }}</a-typography-text>
                    </a-descriptions-item>
                    <a-descriptions-item :label="t('pages.settings.current.management')">
                      <a-typography-text code>
                        {{ settings?.management || t('pages.settings.current.notConfigured') }}
                      </a-typography-text>
                    </a-descriptions-item>
                    <a-descriptions-item :label="t('pages.settings.current.token')">
                      <a-tag :color="settings?.management_token_set ? 'green' : 'orange'">
                        {{
                          settings?.management_token_set
                            ? t('pages.settings.current.set')
                            : t('pages.settings.current.unset')
                        }}
                      </a-tag>
                    </a-descriptions-item>
                  </a-descriptions>
                  <a-alert
                    v-if="multiListener"
                    type="info"
                    show-icon
                    style="margin-top: 12px;"
                    :message="t('pages.settings.current.multiTitle')"
                    :description="
                      t('pages.settings.current.multiDescription', {
                        count: listenList.length,
                        ports: listenList.join(', '),
                      })
                    "
                  />
                </a-card>

                <a-card size="small" :title="t('pages.settings.cards.check')" style="margin-bottom: 16px;">
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="8">
                      <a-form-item
                        html-for="stg-check-interval-seconds"
                        :label="t('pages.settings.fields.checkInterval')"
                        :tooltip="t('pages.settings.fields.checkIntervalTooltip')"
                      >
                        <a-input-number id="stg-check-interval-seconds"
                          v-model:value="form.check_interval_seconds"
                          :min="0"
                          style="width: 100%;"
                          :aria-label="t('pages.settings.fields.checkInterval')"
                        />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="8">
                      <a-form-item
                        html-for="stg-max-connections"
                        :label="t('pages.settings.fields.maxConnections')"
                        :tooltip="t('pages.settings.fields.maxConnectionsTooltip')"
                      >
                        <a-input-number id="stg-max-connections"
                          v-model:value="form.max_connections"
                          :min="0"
                          style="width: 100%;"
                          :aria-label="t('pages.settings.fields.maxConnections')"
                        />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="8">
                      <a-form-item html-for="stg-log-level" :label="t('pages.settings.fields.logLevel')">
                        <a-select
                          id="stg-log-level"
                          v-model:value="form.log_level"
                          :options="logLevelOptions"
                          :aria-label="t('pages.settings.fields.logLevel')"
                        />
                      </a-form-item>
                    </a-col>
                  </a-row>
                </a-card>

                <a-card size="small" :title="t('pages.settings.cards.socks5')">
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="12">
                      <a-form-item html-for="stg-socks5-allow-bind" label="SOCKS5 BIND">
                        <a-switch id="stg-socks5-allow-bind"
                          v-model:checked="form.socks5_allow_bind"
                          :checked-children="t('pages.settings.switches.allow')"
                          :un-checked-children="t('pages.settings.switches.deny')"
                          aria-label="SOCKS5 BIND"
                        />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="12">
                      <a-form-item html-for="stg-socks5-allow-udp-associate" label="SOCKS5 UDP ASSOCIATE">
                        <a-switch id="stg-socks5-allow-udp-associate"
                          v-model:checked="form.socks5_allow_udp_associate"
                          :checked-children="t('pages.settings.switches.allow')"
                          :un-checked-children="t('pages.settings.switches.deny')"
                          aria-label="SOCKS5 UDP ASSOCIATE"
                        />
                      </a-form-item>
                    </a-col>
                  </a-row>
                </a-card>
              </div>

              <!-- 管理面板与安全 -->
              <div v-show="activeSection === 'panel'">
                <a-card size="small" :title="t('pages.settings.cards.panel')" style="margin-bottom: 16px;">
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="8">
                      <a-form-item
                        html-for="stg-webui-enabled"
                        :label="t('pages.settings.fields.webui')"
                        :tooltip="t('pages.settings.fields.webuiTooltip')"
                      >
                        <a-switch id="stg-webui-enabled"
                          v-model:checked="form.webui_enabled"
                          :checked-children="t('pages.settings.switches.enable')"
                          :un-checked-children="t('pages.settings.switches.disable')"
                          :aria-label="t('pages.settings.fields.webui')"
                        />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="16">
                      <a-form-item html-for="stg-token-input"
                        :label="t('pages.settings.fields.token')"
                        :tooltip="
                          t('pages.settings.fields.tokenTooltip', {
                            state: settings?.management_token_set
                              ? t('pages.settings.current.set')
                              : t('pages.settings.current.unset'),
                          })
                        "
                        :validate-status="settingsErrors.token ? 'error' : undefined"
                        :help="settingsErrors.token"
                      >
                        <div class="token-input-row">
                          <a-input-password id="stg-token-input"
                            v-model:value="tokenInput"
                            :placeholder="t('pages.settings.fields.tokenPlaceholder')"
                            autocomplete="new-password"
                          />
                          <a-button @click="handleGenerateToken">
                            {{ t('pages.settings.fields.tokenGenerate') }}
                          </a-button>
                        </div>
                      </a-form-item>
                    </a-col>
                  </a-row>
                  <a-space wrap>
                    <a-popconfirm
                      v-if="settings?.management_token_set"
                      :title="t('pages.settings.fields.tokenClearConfirm')"
                      :ok-text="t('pages.settings.fields.tokenClearOk')"
                      :cancel-text="t('common.cancel')"
                      @confirm="handleClearToken"
                    >
                      <a-button danger :loading="clearingToken">
                        {{ t('pages.settings.fields.tokenClear') }}
                      </a-button>
                    </a-popconfirm>
                    <a-typography-text type="secondary" style="font-size: 12px;">
                      {{ t('pages.settings.fields.tokenHint') }}
                    </a-typography-text>
                  </a-space>
                </a-card>

                <a-alert
                  type="warning"
                  show-icon
                  :message="t('pages.settings.fields.securityTitle')"
                  :description="t('pages.settings.fields.securityDescription')"
                />
              </div>

              <!-- 路由与 GeoSite -->
              <div v-show="activeSection === 'routing'">
                <a-card size="small" :title="t('pages.settings.cards.routing')" style="margin-bottom: 16px;">
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="8">
                      <a-form-item
                        html-for="stg-routing-final"
                        :label="t('pages.settings.fields.routingFinal')"
                        :tooltip="t('pages.settings.fields.routingFinalTooltip')"
                      >
                        <a-select id="stg-routing-final" v-model:value="form.routing_final" :options="finalOptions" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="8">
                      <a-form-item
                        html-for="stg-auto-mode"
                        :label="t('pages.settings.fields.autoMode')"
                        :tooltip="t('pages.settings.fields.autoModeTooltip')"
                      >
                        <a-select id="stg-auto-mode" v-model:value="form.auto_mode" :options="autoModeOptions" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="4">
                      <a-form-item
                        html-for="stg-auto-direct-timeout-seconds"
                        :label="t('pages.settings.fields.autoDirectTimeout')"
                      >
                        <a-input-number id="stg-auto-direct-timeout-seconds"
                          v-model:value="form.auto_direct_timeout_seconds"
                          :min="0.1"
                          :step="0.1"
                          style="width: 100%;"
                        />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="4">
                      <a-form-item
                        html-for="stg-auto-cache-ttl-seconds"
                        :label="t('pages.settings.fields.autoCacheTtl')"
                      >
                        <a-input-number id="stg-auto-cache-ttl-seconds" v-model:value="form.auto_cache_ttl_seconds" :min="0" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                  </a-row>
                  <a-form-item
                    html-for="stg-socks5h-redirect-enabled"
                    :label="t('pages.settings.fields.socks5hRedirect')"
                  >
                    <a-switch
                      id="stg-socks5h-redirect-enabled"
                      v-model:checked="form.socks5h_redirect_enabled"
                      :aria-label="t('pages.settings.fields.socks5hRedirect')"
                    />
                    <a-typography-text type="secondary" style="margin-left: 8px;">
                      {{ t('pages.settings.fields.socks5hRedirectHint') }}
                    </a-typography-text>
                  </a-form-item>
                </a-card>
              </div>

              <!-- DNS 解析 -->
              <div v-show="activeSection === 'dns'">
                <a-card size="small" :title="t('pages.settings.cards.dns')" style="margin-bottom: 16px;">
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="6">
                      <a-form-item html-for="stg-dns-type" :label="t('pages.settings.fields.dnsType')">
                        <a-select id="stg-dns-type" v-model:value="form.dns_type" :options="dnsTypeOptions" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="8">
                      <a-form-item
                        html-for="stg-doh-url"
                        :label="t('pages.settings.fields.dohUrl')"
                        :tooltip="t('pages.settings.fields.dohUrlTooltip')"
                        :validate-status="settingsErrors.doh_url ? 'error' : undefined"
                        :help="settingsErrors.doh_url"
                      >
                        <a-input id="stg-doh-url"
                          v-model:value="form.doh_url"
                          :disabled="form.dns_type !== 'doh'"
                          :placeholder="t('pages.settings.fields.dohUrlPlaceholder')"
                        />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="5">
                      <a-form-item
                        html-for="stg-dot-addr"
                        :label="t('pages.settings.fields.dotAddr')"
                        :tooltip="t('pages.settings.fields.dotAddrTooltip')"
                        :validate-status="settingsErrors.dot_addr ? 'error' : undefined"
                        :help="settingsErrors.dot_addr"
                      >
                        <a-input
                          id="stg-dot-addr"
                          v-model:value="form.dot_addr"
                          :disabled="form.dns_type !== 'dot'"
                          placeholder="1.1.1.1:853"
                        />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="5">
                      <a-form-item html-for="stg-ip-precedence" :label="t('pages.settings.fields.ipPrecedence')">
                        <a-select id="stg-ip-precedence" v-model:value="form.ip_precedence" :options="ipPrecedenceOptions" />
                      </a-form-item>
                    </a-col>
                  </a-row>
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="6">
                      <a-form-item html-for="stg-dns-timeout-seconds" :label="t('pages.settings.fields.dnsTimeout')">
                        <a-input-number id="stg-dns-timeout-seconds" v-model:value="form.dns_timeout_seconds" :min="0.1" :step="0.1" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="6">
                      <a-form-item html-for="stg-dns-fallback" :label="t('pages.settings.fields.dnsFallback')">
                        <a-switch
                          id="stg-dns-fallback"
                          v-model:checked="form.dns_fallback"
                          :aria-label="t('pages.settings.fields.dnsFallback')"
                        />
                      </a-form-item>
                    </a-col>
                  </a-row>
                </a-card>
              </div>

              <!-- 连接与超时 -->
              <div v-show="activeSection === 'timeout'">
                <a-card size="small" :title="t('pages.settings.cards.timeout')">
                  <a-alert
                    type="info"
                    show-icon
                    style="margin-bottom: 12px;"
                    :message="t('pages.settings.fields.timeoutNotice')"
                  />
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="6">
                      <a-form-item html-for="stg-dial-timeout-seconds" :label="t('pages.settings.fields.dialTimeout')">
                        <a-input-number id="stg-dial-timeout-seconds" v-model:value="form.dial_timeout_seconds" :min="0" :step="0.1" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="6">
                      <a-form-item html-for="stg-handshake-timeout-seconds" :label="t('pages.settings.fields.handshakeTimeout')">
                        <a-input-number id="stg-handshake-timeout-seconds" v-model:value="form.handshake_timeout_seconds" :min="0" :step="0.1" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="6">
                      <a-form-item html-for="stg-http-exchange-timeout-seconds" :label="t('pages.settings.fields.httpExchangeTimeout')">
                        <a-input-number id="stg-http-exchange-timeout-seconds"
                          v-model:value="form.http_exchange_timeout_seconds"
                          :min="0"
                          :step="0.1"
                          style="width: 100%;"
                        />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="6">
                      <a-form-item html-for="stg-tunnel-idle-timeout-seconds" :label="t('pages.settings.fields.tunnelIdleTimeout')">
                        <a-input-number id="stg-tunnel-idle-timeout-seconds" v-model:value="form.tunnel_idle_timeout_seconds" :min="0" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                  </a-row>
                </a-card>
              </div>

              <!-- 目标访问策略 -->
              <div v-show="activeSection === 'destination'">
                <a-card size="small" :title="t('pages.settings.cards.defaultAction')">
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="6">
                      <a-form-item html-for="stg-default-action" :label="t('pages.settings.fields.defaultAction')">
                        <a-select id="stg-default-action" v-model:value="form.default_action" :options="destinationActionOptions" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="6">
                      <a-form-item
                        html-for="stg-protect-private"
                        :label="t('pages.settings.fields.protectPrivate')"
                        :tooltip="t('pages.settings.fields.protectPrivateTooltip')"
                      >
                        <a-switch
                          id="stg-protect-private"
                          v-model:checked="form.protect_private"
                          :aria-label="t('pages.settings.fields.protectPrivate')"
                        />
                      </a-form-item>
                    </a-col>
                  </a-row>
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="12">
                      <a-form-item html-for="stg-deny-cidrs" :label="t('pages.settings.fields.denyCidrs')">
                        <a-select id="stg-deny-cidrs" v-model:value="form.deny_cidrs" mode="tags" placeholder="10.0.0.0/8" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="12">
                      <a-form-item html-for="stg-allow-cidrs" :label="t('pages.settings.fields.allowCidrs')">
                        <a-select id="stg-allow-cidrs" v-model:value="form.allow_cidrs" mode="tags" placeholder="192.168.0.0/16" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                  </a-row>
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="12">
                      <a-form-item html-for="stg-deny-domains" :label="t('pages.settings.fields.denyDomains')">
                        <a-select id="stg-deny-domains" v-model:value="form.deny_domains" mode="tags" placeholder="example.com" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="12">
                      <a-form-item html-for="stg-allow-domains" :label="t('pages.settings.fields.allowDomains')">
                        <a-select id="stg-allow-domains" v-model:value="form.allow_domains" mode="tags" placeholder="*.internal" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                  </a-row>
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="12">
                      <a-form-item
                      html-for="stg-deny-ports-text"
                      :label="t('pages.settings.fields.denyPorts')"
                      :tooltip="t('pages.settings.fields.denyPortsTooltip')"
                      :validate-status="settingsErrors.deny_ports ? 'error' : undefined"
                      :help="settingsErrors.deny_ports"
                    >
                        <a-input id="stg-deny-ports-text" v-model:value="denyPortsText" placeholder="25, 135, 445" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="12">
                      <a-form-item
                      html-for="stg-allow-ports-text"
                      :label="t('pages.settings.fields.allowPorts')"
                      :validate-status="settingsErrors.allow_ports ? 'error' : undefined"
                      :help="settingsErrors.allow_ports"
                    >
                        <a-input id="stg-allow-ports-text" v-model:value="allowPortsText" placeholder="80, 443" />
                      </a-form-item>
                    </a-col>
                  </a-row>
                </a-card>
              </div>

              <!-- 访问控制 -->
              <div v-show="activeSection === 'access'">
                <a-card size="small" :title="t('pages.settings.cards.access')">
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="12">
                      <a-form-item
                        html-for="stg-allowed-clients"
                        :label="t('pages.settings.fields.allowedClients')"
                        :tooltip="t('pages.settings.fields.allowedClientsTooltip')"
                      >
                        <a-select id="stg-allowed-clients" v-model:value="form.allowed_clients" mode="tags" placeholder="127.0.0.1, 10.0.0.0/8" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="4">
                      <a-form-item
                        html-for="stg-rate-limit-connections-per-second"
                        :label="t('pages.settings.fields.rateLimit')"
                        :tooltip="t('pages.settings.fields.rateLimitTooltip')"
                      >
                        <a-input-number id="stg-rate-limit-connections-per-second" v-model:value="form.rate_limit_connections_per_second" :min="0" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="4">
                      <a-form-item html-for="stg-rate-limit-burst" :label="t('pages.settings.fields.burst')">
                        <a-input-number id="stg-rate-limit-burst" v-model:value="form.rate_limit_burst" :min="0" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="4">
                      <a-form-item
                        html-for="stg-max-conns-per-user"
                        :label="t('pages.settings.fields.maxConnsPerUser')"
                        :tooltip="t('pages.settings.fields.maxConnsPerUserTooltip')"
                      >
                        <a-input-number id="stg-max-conns-per-user" v-model:value="form.max_conns_per_user" :min="0" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                  </a-row>
                </a-card>
              </div>

              <!-- 流量审计与配额 -->
              <div v-show="activeSection === 'quota'">
                <a-card size="small" :title="t('pages.settings.cards.quota')">
                  <a-row :gutter="16">
                    <a-col :xs="24" :md="4">
                      <a-form-item
                        html-for="stg-traffic-audit-enabled"
                        :label="t('pages.settings.fields.auditEnabled')"
                      >
                        <a-switch
                          id="stg-traffic-audit-enabled"
                          v-model:checked="form.traffic_audit_enabled"
                          :checked-children="t('pages.settings.switches.enable')"
                          :un-checked-children="t('pages.settings.switches.disable')"
                          :aria-label="t('pages.settings.fields.auditEnabled')"
                        />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="5">
                      <a-form-item
                        html-for="stg-per-user-quota"
                        :label="t('pages.settings.fields.perUserQuota')"
                        :tooltip="t('pages.settings.fields.perUserQuotaTooltip')"
                      >
                        <a-input-number id="stg-per-user-quota" v-model:value="form.per_user_quota" :min="0" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="5">
                      <a-form-item
                        html-for="stg-per-upstream-quota"
                        :label="t('pages.settings.fields.perUpstreamQuota')"
                        :tooltip="t('pages.settings.fields.perUpstreamQuotaTooltip')"
                      >
                        <a-input-number id="stg-per-upstream-quota" v-model:value="form.per_upstream_quota" :min="0" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="5">
                      <a-form-item
                        html-for="stg-quota-window-seconds"
                        :label="t('pages.settings.fields.quotaWindow')"
                        :tooltip="t('pages.settings.fields.quotaWindowTooltip')"
                      >
                        <a-input-number id="stg-quota-window-seconds" v-model:value="form.quota_window_seconds" :min="0" style="width: 100%;" />
                      </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="5">
                      <a-form-item
                        html-for="stg-traffic-retention-days"
                        :label="t('pages.settings.fields.retentionDays')"
                        :tooltip="t('pages.settings.fields.retentionDaysTooltip')"
                      >
                        <a-input-number id="stg-traffic-retention-days"
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
    :title="t('pages.settings.leave.title')"
    :ok-text="t('pages.settings.leave.ok')"
    :cancel-text="t('pages.settings.leave.cancel')"
    :mask-closable="false"
    @ok="resolveLeave(true)"
    @cancel="resolveLeave(false)"
  >
    <p>{{ t('pages.settings.leave.body') }}</p>
  </a-modal>
</template>

<style scoped>
.token-input-row {
  display: flex;
  gap: 8px;
}

.token-input-row :deep(.ant-input-affix-wrapper) {
  flex: 1 1 auto;
}

.settings-load-error {
  flex: 0 0 auto;
  margin-bottom: 16px;
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
