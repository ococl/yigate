<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'antdv-next'
import { listUsers, addUser, deleteUser } from '@/api/users'
import type { UserInfo } from '@/types/api'

const dataSource = ref<UserInfo[]>([])
const loading = ref(false)
const modalVisible = ref(false)

const form = ref({
  username: '',
  password: '',
})

const columns = [
  { title: '用户名', dataIndex: 'username', key: 'username' },
  { title: '操作', key: 'action', width: 100, fixed: 'right' as const },
]

async function fetchData() {
  loading.value = true
  try {
    const res = await listUsers()
    dataSource.value = res.users || []
  } finally {
    loading.value = false
  }
}

function showAddModal() {
  form.value = { username: '', password: '' }
  modalVisible.value = true
}

async function handleSubmit() {
  if (!form.value.username.trim()) {
    message.warning('请输入用户名')
    return
  }
  if (!form.value.password.trim()) {
    message.warning('请输入密码')
    return
  }
  try {
    await addUser(form.value)
    message.success('添加成功')
    modalVisible.value = false
    await fetchData()
  } catch {
    // 错误已由拦截器处理
  }
}

async function handleDelete(username: string) {
  try {
    await deleteUser(username)
    message.success('删除成功')
    await fetchData()
  } catch {
    // 错误已由拦截器处理
  }
}

onMounted(fetchData)
</script>

<template>
  <div style="display: flex; justify-content: space-between; align-items: center; padding: 0 0 16px 0;">
    <a-typography-title :level="4" style="margin: 0;">用户管理</a-typography-title>
    <a-button type="primary" @click="showAddModal">新增用户</a-button>
  </div>

  <a-table
    :data-source="dataSource"
    :columns="columns"
    :loading="loading"
    row-key="username"
    :pagination="{ pageSize: 20 }"
  >
    <template #bodyCell="{ column, record }">
      <template v-if="column.key === 'action'">
        <a-popconfirm title="确定删除此用户？" @confirm="handleDelete(record.username)">
          <a-button type="link" size="small" danger>删除</a-button>
        </a-popconfirm>
      </template>
    </template>
  </a-table>

  <a-modal v-model:open="modalVisible" title="新增用户" @ok="handleSubmit">
    <a-form layout="vertical">
      <a-form-item label="用户名">
        <a-input v-model:value="form.username" placeholder="用户名" />
      </a-form-item>
      <a-form-item label="密码">
        <a-input-password v-model:value="form.password" placeholder="密码" />
      </a-form-item>
    </a-form>
  </a-modal>
</template>
