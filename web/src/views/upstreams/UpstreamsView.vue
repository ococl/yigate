<script setup lang="ts">
import { computed, ref, watch, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { message } from 'antdv-next'
import LoadError from '@/components/LoadError.vue'
import MobileListSkeleton from '@/components/MobileListSkeleton.vue'
import { useIsMobile } from '@/composables/useIsMobile'
import { useVisiblePolling } from '@/composables/useVisiblePolling'
import {
  listUpstreams,
  addUpstream,
  updateUpstream,
  deleteUpstream,
  batchDeleteUpstreams,
  checkUpstream,
  checkAllUpstreams,
} from '@/api/upstreams'
import { getStatus } from '@/api/system'
import { formatDateTime } from '@/utils/datetime'
import type {
  UpstreamInfo,
  AddUpstreamRequest,
  UpdateUpstreamRequest,
  UpstreamCheckResult,
  UpstreamStatus,
  HealthCheckConfig,
} from '@/types/api'

const { t } = useI18n()
const isMobile = useIsMobile()
const route = useRoute()
const router = useRouter()
const dataSource = ref<UpstreamInfo[]>([])
// 与其它列表页一致：首帧即为加载中，避免「暂无数据」空态闪一帧
const loading = ref(true)
// 首次/刷新失败时展示错误态与重试入口，避免与「暂无数据」混淆
const loadError = ref(false)
// 按名称跟踪进行中的操作：同时巡检或切换多行时，各行的 loading 不能互相覆盖
const checking = ref<string[]>([])
const toggling = ref<string[]>([])
// 正在删除的上游名，避免重复点击触发多次 DELETE
const deleting = ref<string | null>(null)
// 多选批量删除只作用于桌面表格
const selectedRowKeys = ref<string[]>([])
const bulkDeleting = ref(false)
const checkingAll = ref(false)
// 本次会话内手动检查的结果，仅用于补充失败原因文案
const lastChecks = ref<Map<string, UpstreamCheckResult>>(new Map())
// 服务端运行时健康状态（进程内存态），刷新页面后仍可读取
const runtimeStatus = ref<Map<string, UpstreamStatus>>(new Map())

const modalVisible = ref(false)
const isEdit = ref(false)
const editingName = ref('')
// 弹窗标题随语言与编辑状态变化，不再手工维护字符串
const modalTitle = computed(() =>
  isEdit.value ? t('pages.upstreams.form.editTitle') : t('pages.upstreams.form.addTitle'),
)
// 编辑前列表中展示的脱敏 URL，用于判断用户是否真的修改了 URL。
const originalUrl = ref('')
const submitting = ref(false)

function emptyHealthCheck(): HealthCheckConfig {
  return {
    type: '',
    target: '',
    path: '',
    expected_codes: [],
    interval_seconds: 0,
    timeout_seconds: 0,
    failure_threshold: 0,
    success_threshold: 0,
  }
}

const form = ref({
  name: '',
  url: '',
  // 独立认证凭据：不再需要把用户名密码塞进 URL
  username: '',
  password: '',
  enabled: true,
  types: [] as string[],
  priority: 1,
  weight: 1,
  strategy: 'weighted_round_robin',
  health_check: emptyHealthCheck(),
})

// 编辑态下用于判断凭据是否被改动：密码从不回显，留空即表示保持原密码
const originalUsername = ref('')
const originalHasPassword = ref(false)

// 表单内联校验：输入变化后自动清除对应错误
const nameError = ref('')
const urlError = ref('')
const usernameError = ref('')
const passwordError = ref('')
const typesError = ref('')
const targetError = ref('')
const pathError = ref('')

function resetFieldErrors() {
  nameError.value = ''
  urlError.value = ''
  usernameError.value = ''
  passwordError.value = ''
  typesError.value = ''
  targetError.value = ''
  pathError.value = ''
}

watch(
  () => [
    form.value.name,
    form.value.url,
    form.value.username,
    form.value.password,
    form.value.types.join(','),
    form.value.health_check.type,
    form.value.health_check.path,
  ],
  () => resetFieldErrors(),
)

const typeOptions = ['http', 'https', 'socks5', 'socks5h']
const strategyOptions = ['weighted_round_robin', 'least_connections', 'consistent_hash', 'source_ip_hash']
const strategySelectOptions = strategyOptions.map((s) => ({ label: s, value: s }))
const healthCheckTypeOptions = computed(() => [
  { label: t('pages.upstreams.healthTypes.inherit'), value: '' },
  { label: t('pages.upstreams.healthTypes.tcp'), value: 'tcp' },
  { label: t('pages.upstreams.healthTypes.http'), value: 'http' },
  { label: t('pages.upstreams.healthTypes.https'), value: 'https' },
  { label: t('pages.upstreams.healthTypes.tls'), value: 'tls' },
  { label: t('pages.upstreams.healthTypes.socks5'), value: 'socks5' },
  { label: t('pages.upstreams.healthTypes.socks5h'), value: 'socks5h' },
])

const typeHelp = computed(() => t('pages.upstreams.helps.type'))
const priorityHelp = computed(() => t('pages.upstreams.helps.priority'))
const weightHelp = computed(() => t('pages.upstreams.helps.weight'))
const strategyHelp = computed(() => t('pages.upstreams.helps.strategy'))
const healthCheckHelp = computed(() => t('pages.upstreams.helps.healthCheck'))

const columns = computed(() => [
  { title: t('pages.upstreams.columns.enabled'), key: 'enabled', width: 80 },
  { title: t('pages.upstreams.columns.name'), dataIndex: 'name', key: 'name', width: 140 },
  { title: t('pages.upstreams.columns.url'), dataIndex: 'url', key: 'url', ellipsis: true },
  { title: t('pages.upstreams.columns.type'), dataIndex: 'types', key: 'types', width: 150 },
  { title: t('pages.upstreams.columns.priority'), dataIndex: 'priority', key: 'priority', width: 80 },
  { title: t('pages.upstreams.columns.weight'), dataIndex: 'weight', key: 'weight', width: 70 },
  { title: t('pages.upstreams.columns.strategy'), dataIndex: 'strategy', key: 'strategy', width: 170 },
  { title: t('pages.upstreams.columns.healthCheck'), key: 'health_check', width: 120 },
  { title: t('pages.upstreams.columns.status'), key: 'check_status', width: 120 },
  { title: t('pages.upstreams.columns.lastCheck'), key: 'last_check', width: 180 },
  { title: t('pages.upstreams.columns.operations'), key: 'action', width: 190, fixed: 'right' as const },
])

// 从 /api/v1/status 拉取服务端运行时健康状态。
// 状态由网关进程周期性健康检查维护，前端只做读取展示。
async function fetchRuntimeStatus() {
  try {
    const snapshot = await getStatus()
    const next = new Map<string, UpstreamStatus>()
    for (const s of snapshot.upstreams || []) {
      next.set(s.name, s)
    }
    runtimeStatus.value = next
  } catch {
    // 状态读取失败不阻塞列表展示
  }
}

async function fetchData() {
  loading.value = true
  try {
    const [res] = await Promise.all([listUpstreams(), fetchRuntimeStatus()])
    dataSource.value = res.upstreams || []
    loadError.value = false
    ensurePageInRange()
  } catch {
    // 具体错误已由请求拦截器提示，这里只切换列表区域的状态
    loadError.value = true
  } finally {
    loading.value = false
  }
}

function showAddModal() {
  isEdit.value = false
  editingName.value = ''
  originalUrl.value = ''
  resetFieldErrors()
  originalUsername.value = ''
  originalHasPassword.value = false
  form.value = {
    name: '',
    url: '',
    username: '',
    password: '',
    enabled: true,
    types: ['socks5'],
    priority: 1,
    weight: 1,
    strategy: 'weighted_round_robin',
    health_check: emptyHealthCheck(),
  }
  modalVisible.value = true
}

function showEditModal(record: UpstreamInfo) {
  isEdit.value = true
  editingName.value = record.name
  const hc = record.health_check || emptyHealthCheck()
  originalUrl.value = record.url
  originalUsername.value = record.username || ''
  originalHasPassword.value = record.has_password === true
  resetFieldErrors()
  form.value = {
    name: record.name,
    url: record.url,
    username: record.username || '',
    // 密码永不下发，编辑时留空表示沿用原密码
    password: '',
    enabled: record.enabled !== false,
    types: [...(record.types || [])],
    priority: record.priority,
    weight: record.weight,
    strategy: record.strategy || 'weighted_round_robin',
    health_check: {
      type: hc.type || '',
      target: hc.target || '',
      path: hc.path || '',
      expected_codes: [...(hc.expected_codes || [])],
      interval_seconds: hc.interval_seconds || 0,
      timeout_seconds: hc.timeout_seconds || 0,
      failure_threshold: hc.failure_threshold || 0,
      success_threshold: hc.success_threshold || 0,
    },
  }
  modalVisible.value = true
}

function urlUnchanged(): boolean {
  return isEdit.value && form.value.url.trim() === originalUrl.value.trim()
}

function validateForm(): {
  name?: string
  url?: string
  username?: string
  password?: string
  types?: string
  target?: string
  path?: string
} {
  const errors: { name?: string; url?: string; username?: string; password?: string; types?: string; target?: string; path?: string } = {}
  if (!form.value.name.trim()) errors.name = t('pages.upstreams.messages.nameRequired')
  if (!form.value.url.trim()) errors.url = t('pages.upstreams.messages.urlRequired')
  else if (!urlUnchanged() && !/^(https?|socks5h?):\/\/.+/i.test(form.value.url.trim())) {
    errors.url = t('pages.upstreams.messages.urlInvalid')
  }

  // 用户名与密码必须成对提供；编辑态留空密码表示沿用原密码，仅当原本也没有密码时要求填写
  const username = form.value.username.trim()
  const password = form.value.password
  if (username || password) {
    if (!username) errors.username = t('pages.upstreams.messages.usernameRequired')
    if (!password && (!isEdit.value || !originalHasPassword.value)) {
      errors.password = t('pages.upstreams.messages.passwordRequired')
    }
  }
  if (form.value.types.length === 0) errors.types = t('pages.upstreams.messages.typesRequired')
  const target = (form.value.health_check.target || '').trim()
  if (target && !/^[^\s:]+:\d+$/.test(target)) {
    errors.target = t('pages.upstreams.messages.targetInvalid')
  }
  // 深度探测路径可选；填写时必须是以 / 开头的路径。
  const path = form.value.health_check.path.trim()
  if (path && !path.startsWith('/')) {
    errors.path = t('pages.upstreams.messages.pathInvalid')
  }
  return errors
}

async function handleSubmit() {
  resetFieldErrors()
  const errors = validateForm()
  nameError.value = errors.name || ''
  urlError.value = errors.url || ''
  usernameError.value = errors.username || ''
  passwordError.value = errors.password || ''
  typesError.value = errors.types || ''
  targetError.value = errors.target || ''
  pathError.value = errors.path || ''
  if (errors.name || errors.url || errors.username || errors.password || errors.types || errors.target || errors.path) return

  const healthCheck: HealthCheckConfig = {
    type: form.value.health_check.type,
    target: form.value.health_check.target.trim(),
    path: form.value.health_check.path.trim(),
    expected_codes: (form.value.health_check.expected_codes || [])
      .map((v) => Number(v))
      .filter((v) => Number.isInteger(v) && v >= 100 && v <= 599),
    interval_seconds: form.value.health_check.interval_seconds || 0,
    timeout_seconds: form.value.health_check.timeout_seconds || 0,
    failure_threshold: form.value.health_check.failure_threshold || 0,
    success_threshold: form.value.health_check.success_threshold || 0,
  }

  submitting.value = true
  try {
    if (isEdit.value) {
      const data: UpdateUpstreamRequest = {
        name: form.value.name.trim(),
        // 列表中的 URL 已脱敏，未修改时不回传，避免覆盖真实凭证。
        url: urlUnchanged() ? '' : form.value.url.trim(),
        enabled: form.value.enabled,
        types: form.value.types,
        priority: form.value.priority,
        weight: form.value.weight,
        strategy: form.value.strategy,
        health_check: healthCheck,
      }
      const username = form.value.username.trim()
      const password = form.value.password
      // 密码留空表示沿用原密码，因此只在用户实际输入时提交
      if (password) {
        data.username = username
        data.password = password
      } else if (username !== originalUsername.value.trim()) {
        data.username = username
        // 用户名被清空：同时清掉密码，避免留下"无用户名却有密码"的非法组合
        if (!username) data.password = ''
      }
      await updateUpstream(editingName.value, data)
      message.success(t('pages.upstreams.messages.updated'))
      if (editingName.value !== data.name) {
        lastChecks.value.delete(editingName.value)
      }
    } else {
      const data: AddUpstreamRequest = {
        name: form.value.name.trim(),
        url: form.value.url.trim(),
        username: form.value.username.trim() || undefined,
        password: form.value.password || undefined,
        enabled: form.value.enabled,
        types: form.value.types,
        priority: form.value.priority,
        weight: form.value.weight,
        strategy: form.value.strategy,
        health_check: healthCheck,
      }
      await addUpstream(data)
      message.success(t('pages.upstreams.messages.added'))
    }
    modalVisible.value = false
    await fetchData()
  } catch {
    // 错误已由拦截器处理
  } finally {
    submitting.value = false
  }
}

async function handleToggle(record: UpstreamInfo, enabled: boolean) {
  if (toggling.value.includes(record.name)) return
  toggling.value = [...toggling.value, record.name]
  try {
    await updateUpstream(record.name, { enabled })
    message.success(
      t(enabled ? 'pages.upstreams.messages.enabled' : 'pages.upstreams.messages.disabled', {
        name: record.name,
      }),
    )
    await fetchData()
  } catch {
    // 错误已由拦截器处理，重新拉取以还原开关状态
    await fetchData()
  } finally {
    toggling.value = toggling.value.filter((n) => n !== record.name)
  }
}

async function handleDelete(name: string) {
  if (deleting.value) return
  deleting.value = name
  try {
    await deleteUpstream(name)
    message.success(t('pages.upstreams.messages.deleted'))
    lastChecks.value.delete(name)
    await fetchData()
  } catch {
    // 错误已由拦截器处理
  } finally {
    deleting.value = null
  }
}

function onSelectionChange(keys: (string | number)[]) {
  selectedRowKeys.value = keys.map(String)
}

async function handleBulkDelete() {
  if (bulkDeleting.value || selectedRowKeys.value.length === 0) return
  bulkDeleting.value = true
  const targets = [...selectedRowKeys.value]
  try {
    // 一次请求一次落盘，服务端同时清理规则里对上游的引用
    const res = await batchDeleteUpstreams(targets)
    for (const name of res.deleted) lastChecks.value.delete(name)
    const failed = (res.failed ?? []).map((item) => item.target)
    // 失败项留在选中里，方便直接重试
    selectedRowKeys.value = failed
    await fetchData()
    if (failed.length === 0) {
      message.success(t('pages.upstreams.messages.deletedCount', { count: res.deleted.length }))
      return
    }
    message.warning(
      t('pages.upstreams.messages.deletedPartial', {
        deleted: res.deleted.length,
        failed: failed.length,
        names: failed.join(t('pages.upstreams.messages.listSeparator')),
      }),
    )
  } catch {
    // 整体失败（网络/500）已由请求层提示，保留当前选择
  } finally {
    bulkDeleting.value = false
  }
}

async function handleCheck(name: string) {
  if (checking.value.includes(name)) return
  checking.value = [...checking.value, name]
  try {
    const { result } = await checkUpstream(name)
    lastChecks.value.set(result.name, result)
    if (result.alive) {
      message.success(t('pages.upstreams.messages.checkAlive', { name: result.name }))
    } else {
      message.error(
        t('pages.upstreams.messages.checkDead', {
          name: result.name,
          error: result.error || t('pages.upstreams.status.unknownReason'),
        }),
      )
    }
    await fetchData()
  } catch {
    // 错误已由拦截器处理
  } finally {
    checking.value = checking.value.filter((n) => n !== name)
  }
}

async function handleCheckAll() {
  checkingAll.value = true
  try {
    const { results } = await checkAllUpstreams()
    const next = new Map<string, UpstreamCheckResult>()
    let aliveCount = 0
    for (const r of results) {
      next.set(r.name, r)
      if (r.alive) aliveCount++
    }
    lastChecks.value = next
    if (results.length === 0) {
      message.info(t('pages.upstreams.messages.noTargets'))
    } else if (aliveCount === results.length) {
      message.success(t('pages.upstreams.messages.allAlive', { count: aliveCount }))
    } else {
      // 只报数字的话，用户还得自己在表格里找是哪一个坏了
      const failed = results.filter((r) => !r.alive).map((r) => r.name)
      const shown = failed.slice(0, 3).join(t('pages.upstreams.messages.listSeparator'))
      const rest = failed.length > 3 ? t('pages.upstreams.messages.restCount', { count: failed.length }) : ''
      message.warning(
        t('pages.upstreams.messages.partial', {
          alive: aliveCount,
          total: results.length,
          names: shown,
          rest,
        }),
      )
    }
    await fetchData()
  } catch {
    // 错误已由拦截器处理
  } finally {
    checkingAll.value = false
  }
}

function runtimeOf(record: UpstreamInfo) {
  return runtimeStatus.value.get(record.name)
}

function hasChecked(record: UpstreamInfo) {
  const rt = runtimeOf(record)
  if (!rt) return false
  const at = rt.last_check_at ? new Date(rt.last_check_at).getTime() : 0
  return at > 0 || rt.successes + rt.failures > 0
}

function statusText(record: UpstreamInfo) {
  if (record.enabled === false) return t('pages.upstreams.status.disabled')
  const rt = runtimeOf(record)
  if (!rt) return t('pages.upstreams.status.waiting')
  if (!hasChecked(record)) return t('pages.upstreams.status.unchecked')
  if (!rt.dead) return t('pages.upstreams.status.alive')
  const err = lastChecks.value.get(record.name)?.error
  return err ? t('pages.upstreams.status.deadWithError', { error: err }) : t('pages.upstreams.status.dead')
}

function statusColor(record: UpstreamInfo) {
  if (record.enabled === false) return 'default'
  const rt = runtimeOf(record)
  if (!rt || !hasChecked(record)) return 'default'
  return rt.dead ? 'error' : 'success'
}

function statusTooltip(record: UpstreamInfo) {
  if (record.enabled === false) return t('pages.upstreams.status.disabledTooltip')
  const rt = runtimeOf(record)
  if (!rt) return t('pages.upstreams.status.noRuntimeTooltip')
  const parts = [
    t('pages.upstreams.status.successCount', { count: rt.successes }),
    t('pages.upstreams.status.failureCount', { count: rt.failures }),
    t('pages.upstreams.status.consecutiveCount', { count: rt.consecutive_dead }),
  ]
  if (rt.active_conns > 0) parts.push(t('pages.upstreams.status.activeConnections', { count: rt.active_conns }))
  if (rt.dead && rt.dead_since) {
    parts.push(t('pages.upstreams.status.deadSince', { time: formatTime(rt.dead_since) }))
  }
  return parts.join(t('pages.upstreams.status.joiner'))
}

function formatTime(value?: string) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime()) || date.getTime() <= 0) return '—'
  return formatDateTime(date)
}

function lastCheckText(record: UpstreamInfo) {
  return formatTime(runtimeOf(record)?.last_check_at)
}

function healthCheckText(record: UpstreamInfo) {
  const hc = record.health_check
  if (!hc || !hc.type) return t('pages.upstreams.status.default')
  return hc.type
}

// 分页写入地址栏，刷新或返回列表时保持页码
const pagination = computed(() => ({
  current: Number(route.query.page) || 1,
  pageSize: Number(route.query.size) || 10,
  pageSizeOptions: ['10', '20', '50'],
  showSizeChanger: true,
  showTotal: (total: number) => t('common.total', { count: total }),
  onChange: handlePageChange,
}))

// 页码越界（删掉最后一页的最后一条、手改 URL）时会看到空表格，回落到最后一页
function ensurePageInRange() {
  const { current, pageSize } = pagination.value
  const maxPage = Math.max(1, Math.ceil(dataSource.value.length / pageSize))
  if (current <= maxPage) return
  void router.replace({ query: { ...route.query, page: String(maxPage) } })
}

function handlePageChange(page: number, size: number) {
  // 每页条数变化时回到第一页，避免停留在越界页码只显示零星几条。
  const targetPage = size === pagination.value.pageSize ? page : 1
  router.replace({ query: { ...route.query, page: String(targetPage), size: String(size) } })
}

// 移动端卡片列表复用同一套分页状态
const mobileItems = computed(() => {
  const { current, pageSize } = pagination.value
  const start = (current - 1) * pageSize
  return dataSource.value.slice(start, start + pageSize)
})

const healthCheckType = computed(() => form.value.health_check.type)
const showHealthCheckPath = computed(() => ['http', 'https'].includes(healthCheckType.value))

onMounted(() => {
  fetchData()
})

// 定期同步服务端健康状态，页面隐藏时暂停
useVisiblePolling(fetchRuntimeStatus, 15000)
</script>

<template>
  <div class="page-shell" data-testid="page-upstreams">
    <div class="page-header">
      <div>
        <a-typography-title :level="4" style="margin: 0;">{{ t('pages.upstreams.title') }}</a-typography-title>
        <a-typography-text type="secondary">
          {{ t('pages.upstreams.subtitle') }}
        </a-typography-text>
      </div>
      <a-space>
        <a-button @click="fetchData" :loading="loading">{{ t('common.refresh') }}</a-button>
        <a-button data-testid="upstreams-check-all" @click="handleCheckAll" :loading="checkingAll">
          {{ t('pages.upstreams.checkAll') }}
        </a-button>
        <a-button type="primary" data-testid="upstreams-add" @click="showAddModal">
          {{ t('pages.upstreams.addUpstream') }}
        </a-button>
      </a-space>
    </div>

    <a-card class="fill-card">
      <div v-if="isMobile" class="mobile-list">
        <LoadError v-if="loadError" :retrying="loading" @retry="fetchData" />
        <MobileListSkeleton v-else-if="loading && dataSource.length === 0" />
        <a-empty v-else-if="dataSource.length === 0" :description="t('pages.upstreams.emptyDescription')">
          <a-button type="primary" data-testid="empty-create" @click="showAddModal">
            {{ t('pages.upstreams.addUpstream') }}
          </a-button>
        </a-empty>
        <MobileCard v-for="record in mobileItems" :key="record.name">
          <template #head>
            <a-typography-text strong>{{ record.name }}</a-typography-text>
          </template>
          <template #extra>
            <a-switch
              :checked="record.enabled !== false"
              :loading="toggling.includes(record.name)"
              size="small"
              :aria-label="t('pages.upstreams.status.toggleAria', { name: record.name })"
              @change="(checked: boolean) => handleToggle(record, checked)"
            />
          </template>
          <MobileCardRow :label="t('pages.upstreams.columns.url')">{{ record.url }}</MobileCardRow>
          <MobileCardRow :label="t('pages.upstreams.columns.type')">
            <a-space wrap size="small">
              <a-tag v-for="type in record.types" :key="type" color="blue">{{ type }}</a-tag>
              <span v-if="!record.types || record.types.length === 0" class="muted">—</span>
            </a-space>
          </MobileCardRow>
          <MobileCardRow :label="`${t('pages.upstreams.columns.priority')} / ${t('pages.upstreams.columns.weight')}`">
            {{ record.priority ?? 0 }} / {{ record.weight ?? 0 }}
          </MobileCardRow>
          <MobileCardRow :label="t('pages.upstreams.columns.strategy')">{{ record.strategy || '—' }}</MobileCardRow>
          <MobileCardRow :label="t('pages.upstreams.columns.healthCheck')">
            <a-tag>{{ healthCheckText(record) }}</a-tag>
            <a-tag :color="statusColor(record)">{{ statusText(record) }}</a-tag>
          </MobileCardRow>
          <MobileCardRow :label="t('pages.upstreams.columns.lastCheck')">{{ lastCheckText(record) }}</MobileCardRow>
          <template #actions>
            <a-button
              size="small"
              data-testid="row-check"
              :disabled="record.enabled === false"
              :loading="checking.includes(record.name)"
              @click="handleCheck(record.name)"
            >
              {{ t('common.check') }}
            </a-button>
            <a-button size="small" data-testid="row-edit" @click="showEditModal(record)">
              {{ t('common.edit') }}
            </a-button>
            <a-popconfirm
              :title="t('pages.upstreams.form.deleteConfirm')"
              :ok-text="t('common.delete')"
              :cancel-text="t('common.cancel')"
              @confirm="handleDelete(record.name)"
            >
              <a-button type="link" size="small" danger data-testid="row-delete" :loading="deleting === record.name">
                {{ t('common.delete') }}
              </a-button>
            </a-popconfirm>
          </template>
        </MobileCard>
        <a-pagination
          v-if="dataSource.length > pagination.pageSize"
          :current="pagination.current"
          :page-size="pagination.pageSize"
          :total="dataSource.length"
          :show-size-changer="false"
          size="small"
          class="mobile-pagination"
          @change="handlePageChange"
        />
      </div>

      <div v-else class="fill-table">
        <div v-if="selectedRowKeys.length > 0" class="bulk-bar">
          <span>{{ t('common.selected', { count: selectedRowKeys.length }) }}</span>
          <a-button size="small" type="text" @click="selectedRowKeys = []">
            {{ t('common.clearSelection') }}
          </a-button>
          <a-popconfirm
            :title="t('pages.upstreams.bulkDeleteConfirm', { count: selectedRowKeys.length })"
            :ok-text="t('common.delete')"
            :cancel-text="t('common.cancel')"
            @confirm="handleBulkDelete"
          >
            <a-button size="small" danger data-testid="upstreams-bulk-delete" :loading="bulkDeleting">
              {{ t('pages.upstreams.bulkDelete') }}
            </a-button>
          </a-popconfirm>
        </div>
        <a-table
          :data-source="dataSource"
          :columns="columns"
          :loading="loading"
          row-key="name"
          :row-selection="{ selectedRowKeys, onChange: onSelectionChange }"
          :pagination="pagination"
          :scroll="{ x: 1280, y: 1 }"
        >
    <template #bodyCell="{ column, record }">
      <template v-if="column.key === 'enabled'">
        <a-tooltip
          :title="
            record.enabled === false
              ? t('pages.upstreams.status.disableTooltip')
              : t('pages.upstreams.status.enableTooltip')
          "
        >
          <a-switch
            :checked="record.enabled !== false"
            :loading="toggling.includes(record.name)"
            size="small"
            :aria-label="t('pages.upstreams.status.toggleAria', { name: record.name })"
            @change="(checked: boolean) => handleToggle(record, checked)"
          />
        </a-tooltip>
      </template>
      <template v-else-if="column.key === 'types'">
        <a-space wrap size="small">
          <a-tag v-for="type in record.types" :key="type" color="blue">{{ type }}</a-tag>
          <span v-if="!record.types || record.types.length === 0" class="muted">—</span>
        </a-space>
      </template>
      <template v-else-if="column.key === 'health_check'">
        <a-tag>{{ healthCheckText(record) }}</a-tag>
      </template>
      <template v-else-if="column.key === 'check_status'">
        <a-tooltip :title="statusTooltip(record)">
          <a-tag :color="statusColor(record)">{{ statusText(record) }}</a-tag>
        </a-tooltip>
      </template>
      <template v-else-if="column.key === 'last_check'">
        <a-typography-text type="secondary">{{ lastCheckText(record) }}</a-typography-text>
      </template>
      <template v-else-if="column.key === 'action'">
        <a-space>
          <a-button
            type="link"
            size="small"
            data-testid="row-check"
            :disabled="record.enabled === false"
            :loading="checking.includes(record.name)"
            @click="handleCheck(record.name)"
          >
            {{ t('common.check') }}
          </a-button>
          <a-button type="link" size="small" data-testid="row-edit" @click="showEditModal(record)">
            {{ t('common.edit') }}
          </a-button>
          <a-popconfirm
            :title="t('pages.upstreams.form.deleteConfirm')"
            :ok-text="t('common.delete')"
            :cancel-text="t('common.cancel')"
            @confirm="handleDelete(record.name)"
          >
            <a-button type="link" size="small" danger data-testid="row-delete" :loading="deleting === record.name">
              {{ t('common.delete') }}
            </a-button>
          </a-popconfirm>
        </a-space>
      </template>
    </template>
        <template #emptyText>
          <LoadError v-if="loadError" :retrying="loading" @retry="fetchData" />
          <a-empty v-else :description="t('pages.upstreams.emptyDescription')">
            <a-button type="primary" data-testid="empty-create" @click="showAddModal">
              {{ t('pages.upstreams.addUpstream') }}
            </a-button>
          </a-empty>
        </template>
        </a-table>
      </div>
    </a-card>
  </div>

  <!-- 新增/编辑弹窗 -->
  <a-modal
    v-model:open="modalVisible"
    :title="modalTitle"
    :confirm-loading="submitting"
    :ok-text="isEdit ? t('pages.upstreams.form.okUpdate') : t('pages.upstreams.form.okAdd')"
    :cancel-text="t('common.cancel')"
    width="720px"
    @ok="handleSubmit"
  >
    <a-form layout="vertical">
      <a-form-item
        html-for="up-enabled"
        :label="t('pages.upstreams.form.enable')"
        :tooltip="t('pages.upstreams.form.enableTooltip')"
      >
        <a-switch
          id="up-enabled"
          v-model:checked="form.enabled"
          :checked-children="t('pages.upstreams.form.enableOn')"
          :un-checked-children="t('pages.upstreams.form.enableOff')"
        />
      </a-form-item>

      <a-row :gutter="16">
        <a-col :xs="24" :sm="12">
          <a-form-item
            html-for="up-name"
            :label="t('pages.upstreams.form.name')"
            required
            :tooltip="t('pages.upstreams.form.nameTooltip')"
            :validate-status="nameError ? 'error' : undefined"
            :help="nameError || undefined"
          >
            <a-input id="up-name" v-model:value="form.name" :placeholder="t('pages.upstreams.form.namePlaceholder')" />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12">
          <a-form-item
            html-for="up-url"
            :label="t('pages.upstreams.form.url')"
            required
            :tooltip="t('pages.upstreams.form.urlTooltip')"
            :validate-status="urlError ? 'error' : undefined"
            :help="urlError || undefined"
          >
            <a-input id="up-url" v-model:value="form.url" :placeholder="t('pages.upstreams.form.urlPlaceholder')" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-row :gutter="16">
        <a-col :xs="24" :sm="12">
          <a-form-item
            html-for="up-username"
            :label="t('pages.upstreams.form.username')"
            :tooltip="t('pages.upstreams.form.usernameTooltip')"
            :validate-status="usernameError ? 'error' : undefined"
            :help="usernameError || undefined"
          >
            <a-input
              id="up-username"
              v-model:value="form.username"
              autocomplete="off"
              :placeholder="t('pages.upstreams.form.usernamePlaceholder')"
            />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12">
          <a-form-item
            html-for="up-password"
            :label="t('pages.upstreams.form.password')"
            :tooltip="t('pages.upstreams.form.passwordTooltip')"
            :validate-status="passwordError ? 'error' : undefined"
            :help="passwordError || undefined"
          >
            <a-input-password
              id="up-password"
              v-model:value="form.password"
              autocomplete="new-password"
              :placeholder="
                isEdit && originalHasPassword
                  ? t('pages.upstreams.form.passwordKeepPlaceholder')
                  : t('pages.upstreams.form.passwordPlaceholder')
              "
            />
          </a-form-item>
        </a-col>
      </a-row>

      <a-form-item
        :label="t('pages.upstreams.form.type')"
        :tooltip="typeHelp"
        required
        :validate-status="typesError ? 'error' : undefined"
        :help="typesError || undefined"
      >
        <a-checkbox-group v-model:value="form.types" :options="typeOptions" />
      </a-form-item>

      <a-row :gutter="16">
        <a-col :span="8">
          <a-form-item
            html-for="up-priority"
            :label="t('pages.upstreams.form.priority')"
            :tooltip="priorityHelp"
          >
            <a-input-number id="up-priority" v-model:value="form.priority" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
        <a-col :span="8">
          <a-form-item html-for="up-weight" :label="t('pages.upstreams.form.weight')" :tooltip="weightHelp">
            <a-input-number id="up-weight" v-model:value="form.weight" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
        <a-col :span="8">
          <a-form-item html-for="up-strategy" :label="t('pages.upstreams.form.strategy')" :tooltip="strategyHelp">
            <a-select id="up-strategy" v-model:value="form.strategy" :options="strategySelectOptions" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-divider style="margin: 4px 0 16px;">{{ t('pages.upstreams.form.healthCheckSection') }}</a-divider>

      <a-row :gutter="16">
        <a-col :xs="24" :sm="12">
          <a-form-item
            html-for="up-type"
            :label="t('pages.upstreams.form.probeType')"
            :tooltip="healthCheckHelp"
          >
            <a-select id="up-type" v-model:value="form.health_check.type" :options="healthCheckTypeOptions" />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12">
          <a-form-item
            html-for="up-target"
            :label="t('pages.upstreams.form.probeTarget')"
            :tooltip="t('pages.upstreams.form.probeTargetTooltip')"
            :validate-status="targetError ? 'error' : undefined"
            :help="targetError || undefined"
          >
            <a-input
              id="up-target"
              v-model:value="form.health_check.target"
              placeholder="www.example.com:443"
            />
          </a-form-item>
        </a-col>
      </a-row>

      <a-row :gutter="16">
        <a-col :xs="24" :sm="12">
          <a-form-item
            html-for="up-path"
            :label="t('pages.upstreams.form.probePath')"
            :tooltip="t('pages.upstreams.form.probePathTooltip')"
            :validate-status="pathError ? 'error' : undefined"
            :help="pathError || undefined"
          >
            <a-input
              id="up-path"
              v-model:value="form.health_check.path"
              :disabled="!showHealthCheckPath"
              placeholder="/health"
            />
          </a-form-item>
        </a-col>
      </a-row>

      <a-row :gutter="16">
        <a-col :xs="24" :sm="12" :md="6">
          <a-form-item
            html-for="up-interval-seconds"
            :label="t('pages.upstreams.form.interval')"
            :tooltip="t('pages.upstreams.form.intervalTooltip')"
          >
            <a-input-number id="up-interval-seconds" v-model:value="form.health_check.interval_seconds" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12" :md="6">
          <a-form-item
            html-for="up-timeout-seconds"
            :label="t('pages.upstreams.form.timeout')"
            :tooltip="t('pages.upstreams.form.timeoutTooltip')"
          >
            <a-input-number id="up-timeout-seconds" v-model:value="form.health_check.timeout_seconds" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12" :md="6">
          <a-form-item
            html-for="up-failure-threshold"
            :label="t('pages.upstreams.form.failureThreshold')"
            :tooltip="t('pages.upstreams.form.failureThresholdTooltip')"
          >
            <a-input-number id="up-failure-threshold" v-model:value="form.health_check.failure_threshold" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12" :md="6">
          <a-form-item
            html-for="up-success-threshold"
            :label="t('pages.upstreams.form.successThreshold')"
            :tooltip="t('pages.upstreams.form.successThresholdTooltip')"
          >
            <a-input-number id="up-success-threshold" v-model:value="form.health_check.success_threshold" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-form-item
        html-for="up-expected-codes"
        :label="t('pages.upstreams.form.expectedCodes')"
        :tooltip="t('pages.upstreams.form.expectedCodesTooltip')"
      >
        <a-select id="up-expected-codes"
          v-model:value="form.health_check.expected_codes"
          mode="tags"
          :disabled="!showHealthCheckPath"
          :placeholder="t('pages.upstreams.form.expectedCodesPlaceholder')"
          style="width: 100%;"
        />
      </a-form-item>
    </a-form>
  </a-modal>
</template>
