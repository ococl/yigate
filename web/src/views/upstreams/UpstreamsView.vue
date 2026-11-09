<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
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
const modalTitle = ref('')
const isEdit = ref(false)
const editingName = ref('')
// 编辑前列表中展示的脱敏 URL，用于判断用户是否真的修改了 URL。
const originalUrl = ref('')
const submitting = ref(false)

function emptyHealthCheck(): HealthCheckConfig {
  return {
    type: '',
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
  enabled: true,
  types: [] as string[],
  priority: 1,
  weight: 1,
  strategy: 'weighted_round_robin',
  health_check: emptyHealthCheck(),
})

// 表单内联校验：输入变化后自动清除对应错误
const nameError = ref('')
const urlError = ref('')
const typesError = ref('')
const pathError = ref('')

function resetFieldErrors() {
  nameError.value = ''
  urlError.value = ''
  typesError.value = ''
  pathError.value = ''
}

watch(
  () => [form.value.name, form.value.url, form.value.types.join(','), form.value.health_check.type, form.value.health_check.path],
  () => resetFieldErrors(),
)

const typeOptions = ['http', 'https', 'socks5', 'socks5h']
const strategyOptions = ['weighted_round_robin', 'least_connections', 'consistent_hash', 'source_ip_hash']
const strategySelectOptions = strategyOptions.map((s) => ({ label: s, value: s }))
const healthCheckTypeOptions = [
  { label: '继承默认（按上游类型推断）', value: '' },
  { label: 'tcp（TCP 连接）', value: 'tcp' },
  { label: 'http（HTTP GET）', value: 'http' },
  { label: 'https（HTTPS GET）', value: 'https' },
  { label: 'tls（TLS 握手）', value: 'tls' },
  { label: 'socks5（SOCKS5 握手）', value: 'socks5' },
  { label: 'socks5h（SOCKS5 远端 DNS 握手）', value: 'socks5h' },
]

const typeHelp = '该上游代理支持的协议类型。请求类型与上游类型匹配时才会被选中，socks5h 表示远端 DNS 解析。'
const priorityHelp = '数值越小越优先。网关会先使用高优先级（小数字）组内的可用上游，只有该组全部不可用时才降级到低优先级组。适合主备场景。'
const weightHelp = '同一优先级组内，按策略分配流量时的权重。数值越大被选中的概率越高；least_connections 策略不参考权重。'
const strategyHelp = '同一优先级组内的选择算法。weighted_round_robin 按权重轮询；least_connections 选活动连接最少；consistent_hash 按目标地址哈希；source_ip_hash 按源地址哈希。'
const healthCheckHelp = '留空的数值表示继承全局默认值。健康检查失败达到阈值后上游会被标记为不可用，恢复成功达到阈值后重新启用。'

const columns = [
  { title: '启用', key: 'enabled', width: 80 },
  { title: '名称', dataIndex: 'name', key: 'name', width: 140 },
  { title: 'URL', dataIndex: 'url', key: 'url', ellipsis: true },
  { title: '类型', dataIndex: 'types', key: 'types', width: 150 },
  { title: '优先级', dataIndex: 'priority', key: 'priority', width: 80 },
  { title: '权重', dataIndex: 'weight', key: 'weight', width: 70 },
  { title: '策略', dataIndex: 'strategy', key: 'strategy', width: 170 },
  { title: '健康检查', key: 'health_check', width: 120 },
  { title: '健康状态', key: 'check_status', width: 120 },
  { title: '最后检查', key: 'last_check', width: 180 },
  { title: '操作', key: 'action', width: 190, fixed: 'right' as const },
]

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
  modalTitle.value = '新增上游'
  originalUrl.value = ''
  resetFieldErrors()
  form.value = {
    name: '',
    url: '',
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
  modalTitle.value = '编辑上游'
  const hc = record.health_check || emptyHealthCheck()
  originalUrl.value = record.url
  resetFieldErrors()
  form.value = {
    name: record.name,
    url: record.url,
    enabled: record.enabled !== false,
    types: [...(record.types || [])],
    priority: record.priority,
    weight: record.weight,
    strategy: record.strategy || 'weighted_round_robin',
    health_check: {
      type: hc.type || '',
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

function validateForm(): { name?: string; url?: string; types?: string; path?: string } {
  const errors: { name?: string; url?: string; types?: string; path?: string } = {}
  if (!form.value.name.trim()) errors.name = '请输入名称'
  if (!form.value.url.trim()) errors.url = '请输入 URL'
  else if (!urlUnchanged() && !/^(https?|socks5h?):\/\/.+/i.test(form.value.url.trim())) {
    errors.url = 'URL 需以 http://、https://、socks5:// 或 socks5h:// 开头'
  }
  if (form.value.types.length === 0) errors.types = '请选择至少一种类型'
  if (form.value.health_check.type === 'http' || form.value.health_check.type === 'https') {
    if (!form.value.health_check.path.trim()) {
      errors.path = 'HTTP 健康检查需要填写探测路径'
    }
  }
  return errors
}

async function handleSubmit() {
  resetFieldErrors()
  const errors = validateForm()
  nameError.value = errors.name || ''
  urlError.value = errors.url || ''
  typesError.value = errors.types || ''
  pathError.value = errors.path || ''
  if (errors.name || errors.url || errors.types || errors.path) return

  const healthCheck: HealthCheckConfig = {
    type: form.value.health_check.type,
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
      await updateUpstream(editingName.value, data)
      message.success('更新成功')
      if (editingName.value !== data.name) {
        lastChecks.value.delete(editingName.value)
      }
    } else {
      const data: AddUpstreamRequest = {
        name: form.value.name.trim(),
        url: form.value.url.trim(),
        enabled: form.value.enabled,
        types: form.value.types,
        priority: form.value.priority,
        weight: form.value.weight,
        strategy: form.value.strategy,
        health_check: healthCheck,
      }
      await addUpstream(data)
      message.success('添加成功')
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
    message.success(enabled ? `已启用上游 ${record.name}` : `已禁用上游 ${record.name}`)
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
    message.success('删除成功')
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
      message.success(`已删除 ${res.deleted.length} 个上游`)
      return
    }
    message.warning(`已删除 ${res.deleted.length} 个，失败 ${failed.length} 个：${failed.join('、')}`)
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
      message.success(`上游 ${result.name} 可用`)
    } else {
      message.error(`上游 ${result.name} 不可用：${result.error || '未知原因'}`)
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
      message.info('没有可巡检的上游')
    } else if (aliveCount === results.length) {
      message.success(`巡检完成：${aliveCount} 个上游全部可用`)
    } else {
      // 只报数字的话，用户还得自己在表格里找是哪一个坏了
      const failed = results.filter((r) => !r.alive).map((r) => r.name)
      const shown = failed.slice(0, 3).join('、')
      const rest = failed.length > 3 ? ` 等 ${failed.length} 个` : ''
      message.warning(`巡检完成：${aliveCount}/${results.length} 个可用；不可用：${shown}${rest}`)
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
  if (record.enabled === false) return '已禁用'
  const rt = runtimeOf(record)
  if (!rt) return '等待检测'
  if (!hasChecked(record)) return '未检测'
  if (!rt.dead) return '可用'
  const err = lastChecks.value.get(record.name)?.error
  return err ? `不可用：${err}` : '不可用'
}

function statusColor(record: UpstreamInfo) {
  if (record.enabled === false) return 'default'
  const rt = runtimeOf(record)
  if (!rt || !hasChecked(record)) return 'default'
  return rt.dead ? 'error' : 'success'
}

function statusTooltip(record: UpstreamInfo) {
  if (record.enabled === false) return '上游已禁用，不参与流量调度与健康检查'
  const rt = runtimeOf(record)
  if (!rt) return '尚未获取到运行时状态'
  const parts = [`成功 ${rt.successes} 次`, `失败 ${rt.failures} 次`, `连续失败 ${rt.consecutive_dead} 次`]
  if (rt.active_conns > 0) parts.push(`活动连接 ${rt.active_conns}`)
  if (rt.dead && rt.dead_since) parts.push(`自 ${formatTime(rt.dead_since)} 起不可用`)
  return parts.join('，')
}

function formatTime(value?: string) {
  if (!value) return '—'
  const t = new Date(value)
  if (Number.isNaN(t.getTime()) || t.getTime() <= 0) return '—'
  return formatDateTime(t)
}

function lastCheckText(record: UpstreamInfo) {
  return formatTime(runtimeOf(record)?.last_check_at)
}

function healthCheckText(record: UpstreamInfo) {
  const hc = record.health_check
  if (!hc || !hc.type) return '默认'
  return hc.type
}

// 分页写入地址栏，刷新或返回列表时保持页码
const pagination = computed(() => ({
  current: Number(route.query.page) || 1,
  pageSize: Number(route.query.size) || 10,
  pageSizeOptions: ['10', '20', '50'],
  showSizeChanger: true,
  showTotal: (total: number) => `共 ${total} 条`,
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
  <div class="page-shell">
    <div class="page-header">
      <div>
        <a-typography-title :level="4" style="margin: 0;">上游管理</a-typography-title>
        <a-typography-text type="secondary">
          配置上游代理、负载策略与健康检查；修改会立即生效。
        </a-typography-text>
      </div>
      <a-space>
        <a-button @click="fetchData" :loading="loading">刷新</a-button>
        <a-button @click="handleCheckAll" :loading="checkingAll">一键巡检</a-button>
        <a-button type="primary" @click="showAddModal">新增上游</a-button>
      </a-space>
    </div>

    <a-card class="fill-card">
      <div v-if="isMobile" class="mobile-list">
        <LoadError v-if="loadError" :retrying="loading" @retry="fetchData" />
        <MobileListSkeleton v-else-if="loading && dataSource.length === 0" />
        <a-empty v-else-if="dataSource.length === 0" description="暂无上游，请先新增一个上游代理">
          <a-button type="primary" @click="showAddModal">新增上游</a-button>
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
              :aria-label="`启用或禁用上游 ${record.name}`"
              @change="(checked: boolean) => handleToggle(record, checked)"
            />
          </template>
          <MobileCardRow label="URL">{{ record.url }}</MobileCardRow>
          <MobileCardRow label="类型">
            <a-space wrap size="small">
              <a-tag v-for="t in record.types" :key="t" color="blue">{{ t }}</a-tag>
              <span v-if="!record.types || record.types.length === 0" class="muted">—</span>
            </a-space>
          </MobileCardRow>
          <MobileCardRow label="优先级/权重">{{ record.priority ?? 0 }} / {{ record.weight ?? 0 }}</MobileCardRow>
          <MobileCardRow label="策略">{{ record.strategy || '—' }}</MobileCardRow>
          <MobileCardRow label="健康">
            <a-tag>{{ healthCheckText(record) }}</a-tag>
            <a-tag :color="statusColor(record)">{{ statusText(record) }}</a-tag>
          </MobileCardRow>
          <MobileCardRow label="最后检查">{{ lastCheckText(record) }}</MobileCardRow>
          <template #actions>
            <a-button
              size="small"
              :disabled="record.enabled === false"
              :loading="checking.includes(record.name)"
              @click="handleCheck(record.name)"
            >
              检查
            </a-button>
            <a-button size="small" @click="showEditModal(record)">编辑</a-button>
            <a-popconfirm
              title="删除后相关路由规则对该上游的引用也会被移除，确定删除？"
              ok-text="删除"
              cancel-text="取消"
              @confirm="handleDelete(record.name)"
            >
              <a-button type="link" size="small" danger :loading="deleting === record.name">删除</a-button>
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
          <span>已选 {{ selectedRowKeys.length }} 项</span>
          <a-button size="small" type="text" @click="selectedRowKeys = []">取消选择</a-button>
          <a-popconfirm
            :title="`确定删除选中的 ${selectedRowKeys.length} 个上游？此操作不可撤销。`"
            ok-text="删除"
            cancel-text="取消"
            @confirm="handleBulkDelete"
          >
            <a-button size="small" danger :loading="bulkDeleting">批量删除</a-button>
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
        <a-tooltip :title="record.enabled === false ? '已禁用：不参与流量调度与健康检查' : '启用中：点击可临时禁用'">
          <a-switch
            :checked="record.enabled !== false"
            :loading="toggling.includes(record.name)"
            size="small"
            :aria-label="`启用或禁用上游 ${record.name}`"
            @change="(checked: boolean) => handleToggle(record, checked)"
          />
        </a-tooltip>
      </template>
      <template v-else-if="column.key === 'types'">
        <a-space wrap size="small">
          <a-tag v-for="t in record.types" :key="t" color="blue">{{ t }}</a-tag>
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
            :disabled="record.enabled === false"
            :loading="checking.includes(record.name)"
            @click="handleCheck(record.name)"
          >
            检查
          </a-button>
          <a-button type="link" size="small" @click="showEditModal(record)">编辑</a-button>
          <a-popconfirm
            title="删除后相关路由规则对该上游的引用也会被移除，确定删除？"
            ok-text="删除"
            cancel-text="取消"
            @confirm="handleDelete(record.name)"
          >
            <a-button type="link" size="small" danger :loading="deleting === record.name">删除</a-button>
          </a-popconfirm>
        </a-space>
      </template>
    </template>
        <template #emptyText>
          <LoadError v-if="loadError" :retrying="loading" @retry="fetchData" />
          <a-empty v-else description="暂无上游，请先新增一个上游代理">
            <a-button type="primary" @click="showAddModal">新增上游</a-button>
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
    :ok-text="isEdit ? '更新' : '添加'"
    cancel-text="取消"
    width="720px"
    @ok="handleSubmit"
  >
    <a-form layout="vertical">
      <a-form-item html-for="up-enabled" label="启用" tooltip="关闭后该上游不参与流量调度与健康检查，配置保留，可随时重新启用。">
        <a-switch id="up-enabled" v-model:checked="form.enabled" checked-children="启用" un-checked-children="禁用" />
      </a-form-item>

      <a-row :gutter="16">
        <a-col :xs="24" :sm="12">
          <a-form-item
            html-for="up-name"
            label="名称"
            required
            tooltip="上游唯一标识。编辑时可修改，修改后会同步更新路由规则中对该上游的引用。"
            :validate-status="nameError ? 'error' : undefined"
            :help="nameError || undefined"
          >
            <a-input id="up-name" v-model:value="form.name" placeholder="如 default、proxy-hk" />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12">
          <a-form-item
            html-for="up-url"
            label="URL"
            required
            tooltip="上游代理地址，支持 http://、https://、socks5://、socks5h:// 协议。"
            :validate-status="urlError ? 'error' : undefined"
            :help="urlError || undefined"
          >
            <a-input id="up-url" v-model:value="form.url" placeholder="如 socks5://127.0.0.1:1080" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-form-item
        label="类型"
        :tooltip="typeHelp"
        required
        :validate-status="typesError ? 'error' : undefined"
        :help="typesError || undefined"
      >
        <a-checkbox-group v-model:value="form.types" :options="typeOptions" />
      </a-form-item>

      <a-row :gutter="16">
        <a-col :span="8">
          <a-form-item html-for="up-priority" label="优先级" :tooltip="priorityHelp">
            <a-input-number id="up-priority" v-model:value="form.priority" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
        <a-col :span="8">
          <a-form-item html-for="up-weight" label="权重" :tooltip="weightHelp">
            <a-input-number id="up-weight" v-model:value="form.weight" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
        <a-col :span="8">
          <a-form-item html-for="up-strategy" label="策略" :tooltip="strategyHelp">
            <a-select id="up-strategy" v-model:value="form.strategy" :options="strategySelectOptions" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-divider style="margin: 4px 0 16px;">健康检查</a-divider>

      <a-row :gutter="16">
        <a-col :xs="24" :sm="12">
          <a-form-item html-for="up-type" label="探测方式" :tooltip="healthCheckHelp">
            <a-select id="up-type" v-model:value="form.health_check.type" :options="healthCheckTypeOptions" />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12">
          <a-form-item
            html-for="up-path"
            label="探测路径"
            :tooltip="'仅 http/https 探测方式需要，如 /health。'"
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
          <a-form-item html-for="up-interval-seconds" label="间隔（秒）" tooltip="0 表示使用全局默认间隔">
            <a-input-number id="up-interval-seconds" v-model:value="form.health_check.interval_seconds" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12" :md="6">
          <a-form-item html-for="up-timeout-seconds" label="超时（秒）" tooltip="0 表示使用全局默认超时">
            <a-input-number id="up-timeout-seconds" v-model:value="form.health_check.timeout_seconds" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12" :md="6">
          <a-form-item html-for="up-failure-threshold" label="失败阈值" tooltip="连续失败多少次后标记为不可用，0 表示使用全局默认值">
            <a-input-number id="up-failure-threshold" v-model:value="form.health_check.failure_threshold" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12" :md="6">
          <a-form-item html-for="up-success-threshold" label="恢复阈值" tooltip="连续成功多少次后恢复可用，0 表示使用全局默认值">
            <a-input-number id="up-success-threshold" v-model:value="form.health_check.success_threshold" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-form-item html-for="up-expected-codes" label="期望状态码" tooltip="仅 http/https 探测方式使用，留空表示使用默认 200-299">
        <a-select id="up-expected-codes"
          v-model:value="form.health_check.expected_codes"
          mode="tags"
          :disabled="!showHealthCheckPath"
          placeholder="例如 200,204"
          style="width: 100%;"
        />
      </a-form-item>
    </a-form>
  </a-modal>
</template>
