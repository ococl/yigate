<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { message } from 'antdv-next'
import LoadError from '@/components/LoadError.vue'
import { useIsMobile } from '@/composables/useIsMobile'
import { listUsers, addUser, updateUserPassword, deleteUser } from '@/api/users'
import type { UserInfo } from '@/types/api'

const isMobile = useIsMobile()
const dataSource = ref<UserInfo[]>([])
const loading = ref(false)
// 首次/刷新失败时展示错误态与重试入口，避免与「暂无数据」混淆
const loadError = ref(false)
const submitting = ref(false)
// 正在删除的用户名，避免重复点击触发多次 DELETE
const deleting = ref<string | null>(null)
const modalVisible = ref(false)
const passwordModalVisible = ref(false)
const resetTarget = ref('')
const resetPassword = ref('')

// 表单内联校验：字段名 -> 错误文案，输入变化后自动清除
const fieldErrors = reactive<Record<string, string>>({})

function resetFieldErrors() {
  for (const key of Object.keys(fieldErrors)) delete fieldErrors[key]
}

const form = ref({
  username: '',
  password: '',
})

watch([() => form.value.username, () => form.value.password, resetPassword], () => resetFieldErrors())

const columns = [
  { title: '用户名', dataIndex: 'username', key: 'username' },
  { title: '操作', key: 'action', width: 180, fixed: 'right' as const },
]

async function fetchData() {
  loading.value = true
  try {
    const res = await listUsers()
    dataSource.value = res.users || []
    loadError.value = false
  } catch {
    // 具体错误已由请求拦截器提示，这里只切换列表区域的状态
    loadError.value = true
  } finally {
    loading.value = false
  }
}

function showAddModal() {
  form.value = { username: '', password: '' }
  modalVisible.value = true
}

async function handleSubmit() {
  resetFieldErrors()
  if (!form.value.username.trim()) fieldErrors.username = '请输入用户名'
  if (!form.value.password.trim()) fieldErrors.password = '请输入密码'
  else if (form.value.password.length < 4) fieldErrors.password = '密码至少 4 位'
  if (Object.keys(fieldErrors).length > 0) return
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
  resetFieldErrors()
  passwordModalVisible.value = true
}

async function handleResetPassword() {
  resetFieldErrors()
  if (resetPassword.value.length < 4) fieldErrors.reset = '密码至少 4 位'
  if (Object.keys(fieldErrors).length > 0) return
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
  if (deleting.value) return
  deleting.value = username
  try {
    await deleteUser(username)
    message.success('删除成功')
    await fetchData()
  } catch {
    // 错误已由拦截器处理
  } finally {
    deleting.value = null
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

      <div v-if="isMobile" class="mobile-list">
        <LoadError v-if="loadError" :retrying="loading" @retry="fetchData" />
        <a-empty v-else-if="dataSource.length === 0" description="暂无用户，未配置用户时表示不启用代理认证" />
        <a-card v-for="record in dataSource" :key="record.username" size="small" class="mobile-card">
          <div class="mobile-card-head">
            <a-typography-text strong>{{ record.username }}</a-typography-text>
          </div>
          <div class="mobile-card-actions">
            <a-button size="small" @click="showResetModal(record.username)">修改密码</a-button>
            <a-popconfirm
              title="删除后该用户将无法继续通过代理认证，确定删除？"
              ok-text="删除"
              cancel-text="取消"
              @confirm="handleDelete(record.username)"
            >
              <a-button size="small" danger :loading="deleting === record.username">删除</a-button>
            </a-popconfirm>
          </div>
        </a-card>
      </div>

      <div v-else class="fill-table">
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
            <a-button type="link" size="small" danger :loading="deleting === record.username">删除</a-button>
          </a-popconfirm>
        </a-space>
      </template>
    </template>
        <template #emptyText>
          <LoadError v-if="loadError" :retrying="loading" @retry="fetchData" />
          <a-empty v-else description="暂无用户，未配置用户时表示不启用代理认证" />
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
      <a-form-item
        html-for="user-username"
        label="用户名"
        required
        :validate-status="fieldErrors.username ? 'error' : undefined"
        :help="fieldErrors.username"
      >
        <a-input id="user-username" v-model:value="form.username" placeholder="用户名" />
      </a-form-item>
      <a-form-item
        html-for="user-password"
        label="密码"
        required
        :validate-status="fieldErrors.password ? 'error' : undefined"
        :help="fieldErrors.password"
      >
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
      <a-form-item
        html-for="user-reset-password"
        label="新密码"
        required
        :validate-status="fieldErrors.reset ? 'error' : undefined"
        :help="fieldErrors.reset"
      >
        <a-input-password id="user-reset-password" v-model:value="resetPassword" placeholder="至少 4 位" autocomplete="new-password" />
      </a-form-item>
    </a-form>
  </a-modal>
</template>
