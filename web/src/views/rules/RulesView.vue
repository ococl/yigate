<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { message } from 'antdv-next'
import { listRules, addRule, updateRule, moveRule, deleteRule } from '@/api/rules'
import { listUpstreams } from '@/api/upstreams'
import type { RuleInfo, RuleRequest } from '@/types/api'

const dataSource = ref<RuleInfo[]>([])
const upstreamOptions = ref<{ label: string; value: string }[]>([])
const loading = ref(false)
const movingIndex = ref<number | null>(null)
const modalVisible = ref(false)
const submitting = ref(false)
const isEdit = ref(false)
const editingIndex = ref(-1)

const form = ref<RuleRequest>({
  type: 'domain_suffix',
  values: [],
  action: 'proxy',
  upstreams: [],
})
// 文本域使用独立状态，避免每输入一个字符就被重新格式化导致光标跳动
const valuesText = ref('')

const typeOptions = [
  { label: 'domain（精确域名）', value: 'domain' },
  { label: 'domain_suffix（域名后缀）', value: 'domain_suffix' },
  { label: 'domain_keyword（域名关键字）', value: 'domain_keyword' },
  { label: 'domain_regex（域名正则）', value: 'domain_regex' },
  { label: 'ip_cidr（IP 网段）', value: 'ip_cidr' },
  { label: 'geosite（GeoSite 分类）', value: 'geosite' },
  { label: 'match（兜底匹配）', value: 'match' },
]
const actionOptions = [
  { label: 'proxy（走上游）', value: 'proxy' },
  { label: 'direct（直连）', value: 'direct' },
  { label: 'auto（自动判断）', value: 'auto' },
  { label: 'deny（拒绝）', value: 'deny' },
]

const needsValues = computed(() => form.value.type !== 'match')
const needsUpstreams = computed(() => form.value.action === 'proxy')

const columns = [
  { title: '顺序', dataIndex: 'index', key: 'index', width: 70 },
  { title: '类型', dataIndex: 'type', key: 'type', width: 150 },
  { title: '值', dataIndex: 'values', key: 'values', ellipsis: true },
  { title: '动作', dataIndex: 'action', key: 'action', width: 100 },
  { title: '上游', dataIndex: 'upstreams', key: 'upstreams', width: 220 },
  { title: '操作', key: 'operations', width: 230, fixed: 'right' as const },
]

async function fetchData() {
  loading.value = true
  try {
    const [rules, ups] = await Promise.all([listRules(), listUpstreams()])
    dataSource.value = rules.rules || []
    upstreamOptions.value = (ups.upstreams || []).map((u) => ({ label: u.name, value: u.name }))
  } finally {
    loading.value = false
  }
}

function openAddModal() {
  isEdit.value = false
  editingIndex.value = -1
  form.value = { type: 'domain_suffix', values: [], action: 'proxy', upstreams: [] }
  valuesText.value = ''
  modalVisible.value = true
}

function openEditModal(record: RuleInfo) {
  isEdit.value = true
  editingIndex.value = record.index
  form.value = {
    type: record.type,
    values: [...(record.values || [])],
    action: record.action,
    upstreams: [...(record.upstreams || [])],
  }
  valuesText.value = (record.values || []).join('\n')
  modalVisible.value = true
}

function syncValues(value?: string | number) {
  const text = String(value ?? '')
  valuesText.value = text
  form.value.values = text
    .split('\n')
    .map((v) => v.trim())
    .filter(Boolean)
}

async function handleSubmit() {
  if (!form.value.type) {
    message.warning('请选择规则类型')
    return
  }
  if (needsValues.value && form.value.values.length === 0) {
    message.warning('请输入至少一个匹配值')
    return
  }
  if (needsUpstreams.value && form.value.upstreams.length === 0) {
    message.warning('proxy 动作需要至少选择一个上游')
    return
  }

  const payload: RuleRequest = {
    type: form.value.type,
    values: needsValues.value ? form.value.values : [],
    action: form.value.action,
    upstreams: needsUpstreams.value ? form.value.upstreams : [],
  }

  submitting.value = true
  try {
    if (isEdit.value) {
      await updateRule(editingIndex.value, payload)
      message.success('更新成功')
    } else {
      await addRule(payload)
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

async function handleMove(index: number, direction: 'up' | 'down') {
  movingIndex.value = index
  try {
    await moveRule(index, direction)
    await fetchData()
  } catch {
    // 错误已由拦截器处理
  } finally {
    movingIndex.value = null
  }
}

async function handleDelete(index: number) {
  try {
    await deleteRule(index)
    message.success('删除成功')
    await fetchData()
  } catch {
    // 错误已由拦截器处理
  }
}

function actionColor(action: string) {
  switch (action) {
    case 'direct':
      return 'green'
    case 'proxy':
      return 'blue'
    case 'deny':
      return 'red'
    default:
      return 'orange'
  }
}

onMounted(fetchData)
</script>

<template>
  <div class="page-header">
    <div>
      <a-typography-title :level="4" style="margin: 0;">路由规则</a-typography-title>
      <a-typography-text type="secondary">
        规则自上而下匹配，命中即停止；顺序会影响最终路由结果。
      </a-typography-text>
    </div>
    <a-space>
      <a-button @click="fetchData" :loading="loading">刷新</a-button>
      <a-button type="primary" @click="openAddModal">新增规则</a-button>
    </a-space>
  </div>

  <a-table
    :data-source="dataSource"
    :columns="columns"
    :loading="loading"
    row-key="index"
    :pagination="{ pageSize: 20, hideOnSinglePage: true }"
  >
    <template #bodyCell="{ column, record }">
      <template v-if="column.key === 'index'">
        {{ record.index + 1 }}
      </template>
      <template v-else-if="column.key === 'values'">
        <a-space wrap size="small">
          <a-tag v-for="v in record.values" :key="v">{{ v }}</a-tag>
          <span v-if="!record.values || record.values.length === 0" class="muted">—</span>
        </a-space>
      </template>
      <template v-else-if="column.key === 'action'">
        <a-tag :color="actionColor(record.action)">{{ record.action }}</a-tag>
      </template>
      <template v-else-if="column.key === 'upstreams'">
        <a-space wrap size="small">
          <a-tag v-for="u in record.upstreams" :key="u" color="purple">{{ u }}</a-tag>
          <span v-if="!record.upstreams || record.upstreams.length === 0" class="muted">—</span>
        </a-space>
      </template>
      <template v-else-if="column.key === 'operations'">
        <a-space>
          <a-tooltip title="上移">
            <a-button
              type="text"
              size="small"
              :disabled="record.index === 0"
              :loading="movingIndex === record.index"
              @click="handleMove(record.index, 'up')"
            >↑</a-button>
          </a-tooltip>
          <a-tooltip title="下移">
            <a-button
              type="text"
              size="small"
              :disabled="record.index === dataSource.length - 1"
              :loading="movingIndex === record.index"
              @click="handleMove(record.index, 'down')"
            >↓</a-button>
          </a-tooltip>
          <a-button type="link" size="small" @click="openEditModal(record)">编辑</a-button>
          <a-popconfirm title="确定删除此规则？" ok-text="删除" cancel-text="取消" @confirm="handleDelete(record.index)">
            <a-button type="link" size="small" danger>删除</a-button>
          </a-popconfirm>
        </a-space>
      </template>
    </template>
    <template #emptyText>
      <a-empty description="暂无路由规则，所有请求将按全局 default_action 处理" />
    </template>
  </a-table>

  <a-modal
    v-model:open="modalVisible"
    :title="isEdit ? `编辑规则 #${editingIndex + 1}` : '新增规则'"
    :confirm-loading="submitting"
    :ok-text="isEdit ? '保存' : '添加'"
    cancel-text="取消"
    width="640px"
    @ok="handleSubmit"
  >
    <a-form layout="vertical">
      <a-row :gutter="16">
        <a-col :span="12">
          <a-form-item label="类型" required>
            <a-select v-model:value="form.type" :options="typeOptions" />
          </a-form-item>
        </a-col>
        <a-col :span="12">
          <a-form-item label="动作" required>
            <a-radio-group v-model:value="form.action" :options="actionOptions" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-form-item v-if="needsValues" label="匹配值（每行一个）" required>
        <a-textarea
          :value="valuesText"
          :rows="5"
          :placeholder="form.type === 'ip_cidr' ? '10.0.0.0/8\n192.168.0.0/16' : 'example.com\nexample.org'"
          @update:value="syncValues"
        />
        <template #extra>
          <span v-if="form.type === 'domain_suffix'">按域名后缀匹配，example.com 会同时匹配 a.example.com。</span>
          <span v-else-if="form.type === 'ip_cidr'">使用 CIDR 格式，例如 10.0.0.0/8。</span>
          <span v-else-if="form.type === 'domain_regex'">使用 Go 正则语法，保存前会校验合法性。</span>
          <span v-else>每行一条，空行会被忽略。</span>
        </template>
      </a-form-item>

      <a-form-item v-else label="匹配值">
        <a-alert type="info" show-icon message="match 类型匹配所有请求，无需填写匹配值，通常放在规则末尾作为兜底。" />
      </a-form-item>

      <a-form-item v-if="needsUpstreams" label="指定上游（可多选）" required>
        <a-select
          v-model:value="form.upstreams"
          mode="multiple"
          :options="upstreamOptions"
          placeholder="不选择时使用全部可用上游"
          allow-clear
        />
        <template #extra>仅对 proxy 动作生效；auto 动作会根据探测结果在直连与全部上游之间选择。</template>
      </a-form-item>
    </a-form>
  </a-modal>
</template>
