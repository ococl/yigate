<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { message } from 'antdv-next'
import {
  listUpstreams,
  addUpstream,
  updateUpstream,
  deleteUpstream,
  checkUpstream,
  checkAllUpstreams,
} from '@/api/upstreams'
import { getStatus } from '@/api/system'
import type {
  UpstreamInfo,
  AddUpstreamRequest,
  UpdateUpstreamRequest,
  UpstreamCheckResult,
  UpstreamStatus,
} from '@/types/api'

const dataSource = ref<UpstreamInfo[]>([])
const loading = ref(false)
const checking = ref(false)
const checkingAll = ref(false)
// 本次会话内手动检查的结果，仅用于补充失败原因文案
const lastChecks = ref<Map<string, UpstreamCheckResult>>(new Map())
// 服务端运行时健康状态（进程内存态），刷新页面后仍可读取
const runtimeStatus = ref<Map<string, UpstreamStatus>>(new Map())
let statusTimer: number | undefined

const modalVisible = ref(false)
const modalTitle = ref('')
const isEdit = ref(false)
const editingName = ref('')

const form = ref({
  name: '',
  url: '',
  types: [] as string[],
  priority: 1,
  weight: 1,
  strategy: 'round_robin',
})

const typeOptions = ['http', 'https', 'socks5', 'socks5h']
const strategyOptions = ['round_robin', 'weighted_round_robin', 'least_connections', 'consistent_hash', 'source_ip_hash']

const typeHelp = '该上游代理支持的协议类型。请求类型与上游类型匹配时才会被选中，socks5h 表示远端 DNS 解析。'
const priorityHelp = '数值越小越优先。网关会先使用高优先级（小数字）组内的可用上游，只有该组全部不可用时才降级到低优先级组。适合主备场景。'
const weightHelp = '同一优先级组内，按策略分配流量时的权重。数值越大被选中的概率越高；least_connections 策略不参考权重。'
const strategyHelp = '同一优先级组内的选择算法。weighted_round_robin 按权重轮询；least_connections 选活动连接最少；consistent_hash 按目标地址哈希；source_ip_hash 按源地址哈希。'

const columns = [
  { title: '名称', dataIndex: 'name', key: 'name' },
  { title: 'URL', dataIndex: 'url', key: 'url', ellipsis: true },
  { title: '类型', dataIndex: 'types', key: 'types' },
  { title: '优先级', dataIndex: 'priority', key: 'priority', width: 80 },
  { title: '权重', dataIndex: 'weight', key: 'weight', width: 80 },
  { title: '策略', dataIndex: 'strategy', key: 'strategy' },
  { title: '健康检查', dataIndex: 'health_check_type', key: 'health_check_type', width: 110 },
  { title: '健康状态', key: 'check_status', width: 120 },
  { title: '最后检查', key: 'last_check', width: 180 },
  { title: '操作', key: 'action', width: 200, fixed: 'right' as const },
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
  modalTitle.value = '新增上游'
  form.value = {
    name: '',
    url: '',
    types: ['socks5'],
    priority: 1,
    weight: 1,
    strategy: 'round_robin',
  }
  modalVisible.value = true
}

function showEditModal(record: UpstreamInfo) {
  isEdit.value = true
  editingName.value = record.name
  modalTitle.value = '编辑上游'
  form.value = {
    name: record.name,
    url: record.url,
    types: [...record.types],
    priority: record.priority,
    weight: record.weight,
    strategy: record.strategy,
  }
  modalVisible.value = true
}

async function handleSubmit() {
  if (!form.value.url.trim()) {
    message.warning('请输入 URL')
    return
  }
  if (form.value.types.length === 0) {
    message.warning('请选择至少一种类型')
    return
  }
  if (!form.value.name.trim()) {
    message.warning('请输入名称')
    return
  }

  try {
    if (isEdit.value) {
      const data: UpdateUpstreamRequest = {
        name: form.value.name.trim(),
        url: form.value.url,
        types: form.value.types,
        priority: form.value.priority,
        weight: form.value.weight,
        strategy: form.value.strategy,
      }
      await updateUpstream(editingName.value, data)
      message.success('更新成功')
      // 若改名，清除旧名称的临时检查结果
      if (editingName.value !== data.name) {
        lastChecks.value.delete(editingName.value)
      }
    } else {
      const data: AddUpstreamRequest = {
        name: form.value.name,
        url: form.value.url,
        types: form.value.types,
        priority: form.value.priority,
        weight: form.value.weight,
        strategy: form.value.strategy,
      }
      await addUpstream(data)
      message.success('添加成功')
    }
    modalVisible.value = false
    await fetchData()
  } catch {
    // 错误已由拦截器处理
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
  checking.value = true
  try {
    const { result } = await checkUpstream(name)
    lastChecks.value.set(result.name, result)
    if (result.alive) {
      message.success(`上游 ${result.name} 可用`)
    } else {
      message.error(`上游 ${result.name} 不可用：${result.error || 'unknown'}`)
    }
    await fetchData()
  } catch {
    // 错误已由拦截器处理
  } finally {
    checking.value = false
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

function statusText(record: UpstreamInfo) {
  const rt = runtimeStatus.value.get(record.name)
  if (!rt) return '未知'
  if (!rt.dead) return '可用'
  const err = lastChecks.value.get(record.name)?.error
  return err ? `不可用：${err}` : '不可用'
}

function statusColor(record: UpstreamInfo) {
  const rt = runtimeStatus.value.get(record.name)
  if (!rt) return 'default'
  return rt.dead ? 'error' : 'success'
}

function lastCheckText(record: UpstreamInfo) {
  const at = runtimeStatus.value.get(record.name)?.last_check_at
  if (!at) return '—'
  const t = new Date(at)
  if (Number.isNaN(t.getTime()) || t.getTime() <= 0) return '—'
  return t.toLocaleString()
}

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
  <div style="display: flex; justify-content: space-between; align-items: center; padding: 0 0 16px 0;">
    <a-typography-title :level="4" style="margin: 0;">上游管理</a-typography-title>
    <a-space>
      <a-button @click="fetchData" :loading="loading">刷新</a-button>
      <a-button @click="handleCheckAll" :loading="checkingAll">一键巡检</a-button>
      <a-button type="primary" @click="showAddModal">新增上游</a-button>
    </a-space>
  </div>

  <a-table
    :data-source="dataSource"
    :columns="columns"
    :loading="loading"
    row-key="name"
    :pagination="{ pageSize: 10 }"
  >
    <template #bodyCell="{ column, record }">
      <template v-if="column.key === 'types'">
        <a-tag v-for="t in record.types" :key="t" color="blue">{{ t }}</a-tag>
      </template>
      <template v-else-if="column.key === 'check_status'">
        <a-tag :color="statusColor(record)">{{ statusText(record) }}</a-tag>
      </template>
      <template v-else-if="column.key === 'last_check'">
        <a-typography-text type="secondary">{{ lastCheckText(record) }}</a-typography-text>
      </template>
      <template v-else-if="column.key === 'action'">
        <a-space>
          <a-button type="link" size="small" :loading="checking" @click="handleCheck(record.name)">检查</a-button>
          <a-button type="link" size="small" @click="showEditModal(record)">编辑</a-button>
          <a-popconfirm title="确定删除此上游？" @confirm="handleDelete(record.name)">
            <a-button type="link" size="small" danger>删除</a-button>
          </a-popconfirm>
        </a-space>
      </template>
    </template>
  </a-table>

  <!-- 新增/编辑弹窗 -->
  <a-modal
    v-model:open="modalVisible"
    :title="modalTitle"
    @ok="handleSubmit"
    :ok-text="isEdit ? '更新' : '添加'"
  >
    <a-form layout="vertical">
      <a-form-item label="名称" :tooltip="'上游唯一标识。编辑时可修改，修改后会同步更新路由规则中对该上游的引用。'">
        <a-input v-model:value="form.name" placeholder="如 default、proxy-hk" />
      </a-form-item>

      <a-form-item label="URL" :tooltip="'上游代理地址，支持 http://、https://、socks5://、socks5h:// 协议。'">
        <a-input v-model:value="form.url" placeholder="如 socks5://127.0.0.1:1080" />
      </a-form-item>

      <a-form-item label="类型" :tooltip="typeHelp">
        <a-checkbox-group v-model:value="form.types" :options="typeOptions" />
      </a-form-item>

      <a-row :gutter="16">
        <a-col :span="12">
          <a-form-item label="优先级" :tooltip="priorityHelp">
            <a-input-number v-model:value="form.priority" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
        <a-col :span="12">
          <a-form-item label="权重" :tooltip="weightHelp">
            <a-input-number v-model:value="form.weight" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-form-item label="策略" :tooltip="strategyHelp">
        <a-select v-model:value="form.strategy" :options="strategyOptions.map(s => ({ label: s, value: s }))" />
      </a-form-item>
    </a-form>
  </a-modal>
</template>
