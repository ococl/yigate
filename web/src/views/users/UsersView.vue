<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'antdv-next'
import { listUsers, addUser, updateUserPassword, deleteUser } from '@/api/users'
import type { UserInfo } from '@/types/api'

const dataSource = ref<UserInfo[]>([])
const loading = ref(false)
const submitting = ref(false)
const modalVisible = ref(false)
const passwordModalVisible = ref(false)
const resetTarget = ref('')
const resetPassword = ref('')

const form = ref({
  username: '',
  password: '',
})

const columns = [
  { title: '用户名', dataIndex: 'username', key: 'username' },
  { title: '操作', key: 'action', width: 180, fixed: 'right' as const },
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
  if (form.value.password.length < 4) {
    message.warning('密码至少 4 位')
    return
  }
  submitting.value = true
  try {
    await addUser({ username: form.value.username.trim(), password: form.value.password })
    message.success('添加成功')
    modalVisible.value = false
    await fetchData()
  } catch {
    // 错误已由拦截器处理
  } finally {
    submitting.value = false
  }
}

function showResetModal(username: string) {
  resetTarget.value = username
  resetPassword.value = ''
  passwordModalVisible.value = true
}

async function handleResetPassword() {
  if (resetPassword.value.length < 4) {
    message.warning('密码至少 4 位')
    return
  }
  submitting.value = true
  try {
    await updateUserPassword(resetTarget.value, resetPassword.value)
    message.success(`用户 ${resetTarget.value} 的密码已更新`)
    passwordModalVisible.value = false
  } catch {
    // 错误已由拦截器处理
  } finally {
    submitting.value = false
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
  <div class="page-shell">
    <div class="page-header">
      <div>
        <a-typography-title :level="4" style="margin: 0;">用户管理</a-typography-title>
        <a-typography-text type="secondary">
          代理认证用户；修改密码后，新连接立即使用新凭证。
        </a-typography-text>
      </div>
      <a-space>
        <a-button @click="fetchData" :loading="loading">刷新</a-button>
        <a-button type="primary" @click="showAddModal">新增用户</a-button>
      </a-space>
    </div>

    <a-card class="fill-card">
      <a-alert
        class="fill-card-toolbar"
        type="info"
        show-icon
        style="margin-bottom: 16px;"
        message="未配置用户时不启用代理认证；新增任意用户后，所有代理连接都必须提供用户名密码。"
        description="多监听器模式下，新增/删除用户会同步到所有监听器。"
      />

      <div class="fill-table">
        <a-table
          :data-source="dataSource"
          :columns="columns"
          :loading="loading"
          row-key="username"
          :pagination="{ pageSize: 20, hideOnSinglePage: true }"
          :scroll="{ x: 480, y: 1 }"
        >
    <template #bodyCell="{ column, record }">
      <template v-if="column.key === 'action'">
        <a-space>
          <a-button type="link" size="small" @click="showResetModal(record.username)">修改密码</a-button>
          <a-popconfirm
            title="删除后该用户将无法继续通过代理认证，确定删除？"
            ok-text="删除"
            cancel-text="取消"
            @confirm="handleDelete(record.username)"
          >
            <a-button type="link" size="small" danger>删除</a-button>
          </a-popconfirm>
        </a-space>
      </template>
    </template>
        <template #emptyText>
          <a-empty description="暂无用户，未配置用户时表示不启用代理认证" />
        </template>
        </a-table>
      </div>
    </a-card>
  </div>

  <a-modal
    v-model:open="modalVisible"
    title="新增用户"
    :confirm-loading="submitting"
    ok-text="添加"
    cancel-text="取消"
    @ok="handleSubmit"
  >
    <a-form layout="vertical">
      <a-form-item html-for="user-username" label="用户名" required>
        <a-input id="user-username" v-model:value="form.username" placeholder="用户名" />
      </a-form-item>
      <a-form-item html-for="user-password" label="密码" required>
        <a-input-password id="user-password" v-model:value="form.password" placeholder="密码" autocomplete="new-password" />
      </a-form-item>
    </a-form>
  </a-modal>

  <a-modal
    v-model:open="passwordModalVisible"
    :title="`修改密码：${resetTarget}`"
    :confirm-loading="submitting"
    ok-text="保存"
    cancel-text="取消"
    @ok="handleResetPassword"
  >
    <a-form layout="vertical">
      <a-form-item html-for="user-reset-password" label="新密码" required>
        <a-input-password id="user-reset-password" v-model:value="resetPassword" placeholder="至少 4 位" autocomplete="new-password" />
      </a-form-item>
    </a-form>
  </a-modal>
</template>
