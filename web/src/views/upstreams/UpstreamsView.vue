<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { message } from 'antdv-next'
import { useIsMobile } from '@/composables/useIsMobile'
import {
  listUpstreams,
  addUpstream,
  updateUpstream,
  deleteUpstream,
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
const dataSource = ref<UpstreamInfo[]>([])
const loading = ref(false)
const checking = ref<string | null>(null)
const checkingAll = ref(false)
const toggling = ref<string | null>(null)
// 本次会话内手动检查的结果，仅用于补充失败原因文案
const lastChecks = ref<Map<string, UpstreamCheckResult>>(new Map())
// 服务端运行时健康状态（进程内存态），刷新页面后仍可读取
const runtimeStatus = ref<Map<string, UpstreamStatus>>(new Map())
let statusTimer: number | undefined

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
  } finally {
    loading.value = false
  }
}

function showAddModal() {
  isEdit.value = false
  editingName.value = ''
  modalTitle.value = '新增上游'
  originalUrl.value = ''
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

function validateForm(): string | null {
  if (!form.value.name.trim()) return '请输入名称'
  if (!form.value.url.trim()) return '请输入 URL'
  if (!urlUnchanged() && !/^(https?|socks5h?):\/\/.+/i.test(form.value.url.trim())) {
    return 'URL 需以 http://、https://、socks5:// 或 socks5h:// 开头'
  }
  if (form.value.types.length === 0) return '请选择至少一种类型'
  if (form.value.health_check.type === 'http' || form.value.health_check.type === 'https') {
    if (!form.value.health_check.path.trim()) return 'HTTP 健康检查需要填写探测路径'
  }
  return null
}

async function handleSubmit() {
  const error = validateForm()
  if (error) {
    message.warning(error)
    return
  }

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
  toggling.value = record.name
  try {
    await updateUpstream(record.name, { enabled })
    message.success(enabled ? `已启用上游 ${record.name}` : `已禁用上游 ${record.name}`)
    await fetchData()
  } catch {
    // 错误已由拦截器处理，重新拉取以还原开关状态
    await fetchData()
  } finally {
    toggling.value = null
  }
}

async function handleDelete(name: string) {
  try {
    await deleteUpstream(name)
    message.success('删除成功')
    lastChecks.value.delete(name)
    await fetchData()
  } catch {
    // 错误已由拦截器处理
  }
}

async function handleCheck(name: string) {
  checking.value = name
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
    checking.value = null
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
    message.info(`巡检完成：${aliveCount}/${results.length} 个上游可用`)
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

const healthCheckType = computed(() => form.value.health_check.type)
const showHealthCheckPath = computed(() => ['http', 'https'].includes(healthCheckType.value))

onMounted(() => {
  fetchData()
  // 定期同步服务端健康状态，避免页面长时间停留后展示过期信息
  statusTimer = window.setInterval(fetchRuntimeStatus, 15000)
})

onUnmounted(() => {
  if (statusTimer !== undefined) {
    window.clearInterval(statusTimer)
  }
})
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
        <a-empty v-if="dataSource.length === 0" description="暂无上游，请先新增一个上游代理" />
        <a-card v-for="record in dataSource" :key="record.name" size="small" class="mobile-card">
          <div class="mobile-card-head">
            <a-typography-text strong>{{ record.name }}</a-typography-text>
            <a-switch
              :checked="record.enabled !== false"
              :loading="toggling === record.name"
              size="small"
              @change="(checked: boolean) => handleToggle(record, checked)"
            />
          </div>
          <div class="mobile-card-row">
            <span class="mobile-label">URL</span>
            <span class="mobile-value">{{ record.url }}</span>
          </div>
          <div class="mobile-card-row">
            <span class="mobile-label">类型</span>
            <span class="mobile-value">
              <a-space wrap size="small">
                <a-tag v-for="t in record.types" :key="t" color="blue">{{ t }}</a-tag>
                <span v-if="!record.types || record.types.length === 0" class="muted">—</span>
              </a-space>
            </span>
          </div>
          <div class="mobile-card-row">
            <span class="mobile-label">优先级/权重</span>
            <span class="mobile-value">{{ record.priority ?? 0 }} / {{ record.weight ?? 0 }}</span>
          </div>
          <div class="mobile-card-row">
            <span class="mobile-label">策略</span>
            <span class="mobile-value">{{ record.strategy || '—' }}</span>
          </div>
          <div class="mobile-card-row">
            <span class="mobile-label">健康</span>
            <span class="mobile-value">
              <a-tag>{{ healthCheckText(record) }}</a-tag>
              <a-tag :color="statusColor(record)">{{ statusText(record) }}</a-tag>
            </span>
          </div>
          <div class="mobile-card-row">
            <span class="mobile-label">最后检查</span>
            <span class="mobile-value">{{ lastCheckText(record) }}</span>
          </div>
          <div class="mobile-card-actions">
            <a-button
              size="small"
              :disabled="record.enabled === false"
              :loading="checking === record.name"
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
              <a-button size="small" danger>删除</a-button>
            </a-popconfirm>
          </div>
        </a-card>
      </div>

      <div v-else class="fill-table">
        <a-table
          :data-source="dataSource"
          :columns="columns"
          :loading="loading"
          row-key="name"
          :pagination="{ pageSize: 10, hideOnSinglePage: true }"
          :scroll="{ x: 1280, y: 1 }"
        >
    <template #bodyCell="{ column, record }">
      <template v-if="column.key === 'enabled'">
        <a-tooltip :title="record.enabled === false ? '已禁用：不参与流量调度与健康检查' : '启用中：点击可临时禁用'">
          <a-switch
            :checked="record.enabled !== false"
            :loading="toggling === record.name"
            size="small"
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
            :loading="checking === record.name"
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
            <a-button type="link" size="small" danger>删除</a-button>
          </a-popconfirm>
        </a-space>
      </template>
    </template>
        <template #emptyText>
          <a-empty description="暂无上游，请先新增一个上游代理" />
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
      <a-form-item label="启用" tooltip="关闭后该上游不参与流量调度与健康检查，配置保留，可随时重新启用。">
        <a-switch v-model:checked="form.enabled" checked-children="启用" un-checked-children="禁用" />
      </a-form-item>

      <a-row :gutter="16">
        <a-col :xs="24" :sm="12">
          <a-form-item label="名称" required tooltip="上游唯一标识。编辑时可修改，修改后会同步更新路由规则中对该上游的引用。">
            <a-input v-model:value="form.name" placeholder="如 default、proxy-hk" />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12">
          <a-form-item label="URL" required tooltip="上游代理地址，支持 http://、https://、socks5://、socks5h:// 协议。">
            <a-input v-model:value="form.url" placeholder="如 socks5://127.0.0.1:1080" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-form-item label="类型" :tooltip="typeHelp" required>
        <a-checkbox-group v-model:value="form.types" :options="typeOptions" />
      </a-form-item>

      <a-row :gutter="16">
        <a-col :span="8">
          <a-form-item label="优先级" :tooltip="priorityHelp">
            <a-input-number v-model:value="form.priority" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
        <a-col :span="8">
          <a-form-item label="权重" :tooltip="weightHelp">
            <a-input-number v-model:value="form.weight" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
        <a-col :span="8">
          <a-form-item label="策略" :tooltip="strategyHelp">
            <a-select v-model:value="form.strategy" :options="strategySelectOptions" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-divider style="margin: 4px 0 16px;">健康检查</a-divider>

      <a-row :gutter="16">
        <a-col :xs="24" :sm="12">
          <a-form-item label="探测方式" :tooltip="healthCheckHelp">
            <a-select v-model:value="form.health_check.type" :options="healthCheckTypeOptions" />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12">
          <a-form-item label="探测路径" :tooltip="'仅 http/https 探测方式需要，如 /health。'">
            <a-input
              v-model:value="form.health_check.path"
              :disabled="!showHealthCheckPath"
              placeholder="/health"
            />
          </a-form-item>
        </a-col>
      </a-row>

      <a-row :gutter="16">
        <a-col :xs="24" :sm="12" :md="6">
          <a-form-item label="间隔（秒）" tooltip="0 表示使用全局默认间隔">
            <a-input-number v-model:value="form.health_check.interval_seconds" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12" :md="6">
          <a-form-item label="超时（秒）" tooltip="0 表示使用全局默认超时">
            <a-input-number v-model:value="form.health_check.timeout_seconds" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12" :md="6">
          <a-form-item label="失败阈值" tooltip="连续失败多少次后标记为不可用，0 表示使用全局默认值">
            <a-input-number v-model:value="form.health_check.failure_threshold" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12" :md="6">
          <a-form-item label="恢复阈值" tooltip="连续成功多少次后恢复可用，0 表示使用全局默认值">
            <a-input-number v-model:value="form.health_check.success_threshold" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-form-item label="期望状态码" tooltip="仅 http/https 探测方式使用，留空表示使用默认 200-299">
        <a-select
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
