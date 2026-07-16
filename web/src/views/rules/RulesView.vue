<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'antdv-next'
import { listRules, addRule, deleteRule } from '@/api/rules'
import type { RuleInfo, AddRuleRequest } from '@/types/api'

const dataSource = ref<RuleInfo[]>([])
const loading = ref(false)
const modalVisible = ref(false)

const form = ref<AddRuleRequest>({
  type: 'domain',
  values: [],
  action: 'proxy',
  upstreams: [],
})

const typeOptions = ['domain', 'domain_suffix', 'domain_keyword', 'domain_regex', 'ip_cidr', 'geosite', 'match']
const actionOptions = ['direct', 'proxy', 'auto']

const columns = [
  { title: '索引', dataIndex: 'index', key: 'index', width: 60 },
  { title: '类型', dataIndex: 'type', key: 'type', width: 120 },
  { title: '值', dataIndex: 'values', key: 'values', ellipsis: true },
  { title: '动作', dataIndex: 'action', key: 'action', width: 80 },
  { title: '上游', dataIndex: 'upstreams', key: 'upstreams' },
  { title: '操作', key: 'action', width: 80, fixed: 'right' as const },
]

async function fetchData() {
  loading.value = true
  try {
    const res = await listRules()
    dataSource.value = res.rules || []
  } finally {
    loading.value = false
  }
}

function showAddModal() {
  form.value = {
    type: 'domain',
    values: [],
    action: 'proxy',
    upstreams: [],
  }
  modalVisible.value = true
}

async function handleSubmit() {
  if (form.value.values.length === 0) {
    message.warning('请输入至少一个值')
    return
  }
  try {
    await addRule(form.value)
    message.success('添加成功')
    modalVisible.value = false
    await fetchData()
  } catch {
    // 错误已由拦截器处理
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

onMounted(fetchData)
</script>

<template>
  <a-page-header title="路由规则" style="padding: 0 0 16px 0;">
    <template #extra>
      <a-button type="primary" @click="showAddModal">新增规则</a-button>
    </template>
  </a-page-header>

  <a-table
    :data-source="dataSource"
    :columns="columns"
    :loading="loading"
    row-key="index"
    :pagination="{ pageSize: 20 }"
  >
    <template #bodyCell="{ column, record }">
      <template v-if="column.key === 'values'">
        <a-tag v-for="v in record.values" :key="v">{{ v }}</a-tag>
      </template>
      <template v-else-if="column.key === 'action'">
        <a-tag :color="record.action === 'direct' ? 'green' : record.action === 'proxy' ? 'blue' : 'orange'">
          {{ record.action }}
        </a-tag>
      </template>
      <template v-else-if="column.key === 'upstreams'">
        <a-tag v-for="u in record.upstreams" :key="u" color="purple">{{ u }}</a-tag>
        <span v-if="!record.upstreams || record.upstreams.length === 0">-</span>
      </template>
      <template v-else-if="column.key === 'action'">
        <a-popconfirm title="确定删除此规则？" @confirm="handleDelete(record.index)">
          <a-button type="link" size="small" danger>删除</a-button>
        </a-popconfirm>
      </template>
    </template>
  </a-table>

  <a-modal v-model:open="modalVisible" title="新增规则" @ok="handleSubmit">
    <a-form layout="vertical">
      <a-form-item label="类型">
        <a-select v-model:value="form.type" :options="typeOptions.map(t => ({ label: t, value: t }))" />
      </a-form-item>

      <a-form-item label="值（每行一个）">
        <a-textarea
          :value="form.values.join('\n')"
          @input="(e: any) => form.values = e.target.value.split('\n').filter((v: string) => v.trim())"
          :rows="4"
          placeholder="example.com&#10;*.example.com"
        />
      </a-form-item>

      <a-form-item label="动作">
        <a-radio-group v-model:value="form.action" :options="actionOptions.map(a => ({ label: a, value: a }))" />
      </a-form-item>

      <a-form-item label="上游（逗号分隔）">
        <a-input
          :value="form.upstreams.join(',')"
          @input="(e: any) => form.upstreams = e.target.value.split(',').map((s: string) => s.trim()).filter(Boolean)"
          placeholder="us-east-1,us-west-1"
        />
      </a-form-item>
    </a-form>
  </a-modal>
</template>
