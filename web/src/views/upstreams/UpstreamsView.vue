<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'antdv-next'
import { listUpstreams, addUpstream, updateUpstream, deleteUpstream } from '@/api/upstreams'
import type { UpstreamInfo, AddUpstreamRequest, UpdateUpstreamRequest } from '@/types/api'

const dataSource = ref<UpstreamInfo[]>([])
const loading = ref(false)

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

const columns = [
  { title: '名称', dataIndex: 'name', key: 'name' },
  { title: 'URL', dataIndex: 'url', key: 'url', ellipsis: true },
  { title: '类型', dataIndex: 'types', key: 'types' },
  { title: '优先级', dataIndex: 'priority', key: 'priority', width: 80 },
  { title: '权重', dataIndex: 'weight', key: 'weight', width: 80 },
  { title: '策略', dataIndex: 'strategy', key: 'strategy' },
  { title: '健康检查', dataIndex: 'health_check_type', key: 'health_check_type', width: 100 },
  { title: '操作', key: 'action', width: 150, fixed: 'right' as const },
]

async function fetchData() {
  loading.value = true
  try {
    const res = await listUpstreams()
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

  try {
    if (isEdit.value) {
      const data: UpdateUpstreamRequest = {
        url: form.value.url,
        types: form.value.types,
        priority: form.value.priority,
        weight: form.value.weight,
        strategy: form.value.strategy,
      }
      await updateUpstream(editingName.value, data)
      message.success('更新成功')
    } else {
      if (!form.value.name.trim()) {
        message.warning('请输入名称')
        return
      }
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
    await fetchData()
  } catch {
    // 错误已由拦截器处理
  }
}

onMounted(fetchData)
</script>

<template>
  <a-page-header title="上游管理" style="padding: 0 0 16px 0;">
    <template #extra>
      <a-button type="primary" @click="showAddModal">新增上游</a-button>
    </template>
  </a-page-header>

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
      <template v-else-if="column.key === 'action'">
        <a-space>
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
      <a-form-item label="名称" v-if="!isEdit">
        <a-input v-model:value="form.name" placeholder="上游名称" />
      </a-form-item>
      <a-form-item label="名称" v-else>
        <a-input :value="form.name" disabled />
      </a-form-item>

      <a-form-item label="URL">
        <a-input v-model:value="form.url" placeholder="如 socks5://127.0.0.1:1080" />
      </a-form-item>

      <a-form-item label="类型">
        <a-checkbox-group v-model:value="form.types" :options="typeOptions" />
      </a-form-item>

      <a-row :gutter="16">
        <a-col :span="12">
          <a-form-item label="优先级">
            <a-input-number v-model:value="form.priority" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
        <a-col :span="12">
          <a-form-item label="权重">
            <a-input-number v-model:value="form.weight" :min="0" style="width: 100%;" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-form-item label="策略">
        <a-select v-model:value="form.strategy" :options="strategyOptions.map(s => ({ label: s, value: s }))" />
      </a-form-item>
    </a-form>
  </a-modal>
</template>
