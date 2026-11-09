<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { message } from 'antdv-next'
import LoadError from '@/components/LoadError.vue'
import MobileListSkeleton from '@/components/MobileListSkeleton.vue'
import { useIsMobile } from '@/composables/useIsMobile'
import { listUsers, addUser, updateUserPassword, deleteUser, batchDeleteUsers } from '@/api/users'
import type { UserInfo } from '@/types/api'

const { t } = useI18n()
const isMobile = useIsMobile()
const dataSource = ref<UserInfo[]>([])
const loading = ref(true)
// 首次/刷新失败时展示错误态与重试入口，避免与「暂无数据」混淆
const loadError = ref(false)
const submitting = ref(false)
// 正在删除的用户名，避免重复点击触发多次 DELETE
const deleting = ref<string | null>(null)
// 多选批量删除只作用于桌面表格，移动端仍是单卡操作
const selectedRowKeys = ref<string[]>([])
const bulkDeleting = ref(false)
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

const columns = computed(() => [
  { title: t('pages.users.columns.username'), dataIndex: 'username', key: 'username' },
  { title: t('pages.users.columns.actions'), key: 'action', width: 180, fixed: 'right' as const },
])

// 桌面表格分页受控，删除后能收回越界页码
const PAGE_SIZE = 20
const page = ref(1)
// 移动端卡片与规则页保持一致，本地分页，避免用户多时一次性渲染全部卡片
const MOBILE_PAGE_SIZE = 20
const mobilePage = ref(1)
const mobileItems = computed(() =>
  dataSource.value.slice((mobilePage.value - 1) * MOBILE_PAGE_SIZE, mobilePage.value * MOBILE_PAGE_SIZE),
)
const tablePagination = computed(() => ({
  current: page.value,
  pageSize: PAGE_SIZE,
  hideOnSinglePage: true,
  onChange: (value: number) => {
    page.value = value
  },
}))

function clampPage() {
  const maxPage = Math.max(1, Math.ceil(dataSource.value.length / PAGE_SIZE))
  if (page.value > maxPage) page.value = maxPage
  const maxMobilePage = Math.max(1, Math.ceil(dataSource.value.length / MOBILE_PAGE_SIZE))
  if (mobilePage.value > maxMobilePage) mobilePage.value = maxMobilePage
}

async function fetchData() {
  loading.value = true
  try {
    const res = await listUsers()
    dataSource.value = res.users || []
    loadError.value = false
    clampPage()
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
  if (!form.value.username.trim()) fieldErrors.username = t('pages.users.messages.usernameRequired')
  if (!form.value.password.trim()) fieldErrors.password = t('pages.users.messages.passwordRequired')
  else if (form.value.password.length < 4) fieldErrors.password = t('pages.users.messages.passwordTooShort')
  if (Object.keys(fieldErrors).length > 0) return
  submitting.value = true
  try {
    await addUser({ username: form.value.username.trim(), password: form.value.password })
    message.success(t('pages.users.messages.added'))
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
  if (resetPassword.value.length < 4) fieldErrors.reset = t('pages.users.messages.passwordTooShort')
  if (Object.keys(fieldErrors).length > 0) return
  submitting.value = true
  try {
    await updateUserPassword(resetTarget.value, resetPassword.value)
    message.success(t('pages.users.messages.passwordUpdated', { name: resetTarget.value }))
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
    message.success(t('pages.users.messages.deleted'))
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

// 不弹二次确认框逐个删，用户可以一次清掉一批建错的账号
async function handleBulkDelete() {
  if (bulkDeleting.value || selectedRowKeys.value.length === 0) return
  bulkDeleting.value = true
  const targets = [...selectedRowKeys.value]
  try {
    // 一次请求一次落盘，服务端返回逐项结果
    const res = await batchDeleteUsers(targets)
    const failed = (res.failed ?? []).map((item) => item.target)
    // 失败项留在选中里：用户可以直接再点一次，不用在列表里重新找
    selectedRowKeys.value = failed
    await fetchData()
    if (failed.length === 0) {
      message.success(t('pages.users.messages.deletedCount', { count: res.deleted.length }))
      return
    }
    message.warning(
      t('pages.users.messages.deletedPartial', {
        deleted: res.deleted.length,
        failed: failed.length,
        names: failed.join(t('pages.users.messages.listSeparator')),
      }),
    )
  } catch {
    // 整体失败（网络/500）已由请求层提示，保留当前选择
  } finally {
    bulkDeleting.value = false
  }
}

onMounted(fetchData)
</script>

<template>
  <div class="page-shell" data-testid="page-users">
    <div class="page-header">
      <div>
        <a-typography-title :level="4" style="margin: 0;">{{ t('pages.users.title') }}</a-typography-title>
        <a-typography-text type="secondary">
          {{ t('pages.users.subtitle') }}
        </a-typography-text>
      </div>
      <a-space>
        <a-button @click="fetchData" :loading="loading">{{ t('common.refresh') }}</a-button>
        <a-button type="primary" data-testid="users-add" @click="showAddModal">{{ t('pages.users.addUser') }}</a-button>
      </a-space>
    </div>

    <a-card class="fill-card">
      <a-alert
        class="fill-card-toolbar"
        type="info"
        show-icon
        style="margin-bottom: 16px;"
        :message="t('pages.users.authNotice.message')"
        :description="t('pages.users.authNotice.description')"
      />

      <div v-if="isMobile" class="mobile-list">
        <LoadError v-if="loadError" :retrying="loading" @retry="fetchData" />
        <MobileListSkeleton v-else-if="loading && dataSource.length === 0" />
        <a-empty v-else-if="dataSource.length === 0" :description="t('pages.users.emptyDescription')">
          <a-button type="primary" data-testid="empty-create" @click="showAddModal">
            {{ t('pages.users.addUser') }}
          </a-button>
        </a-empty>
        <MobileCard v-for="record in mobileItems" :key="record.username">
          <template #head>
            <a-typography-text strong>{{ record.username }}</a-typography-text>
          </template>
          <template #actions>
            <a-button size="small" @click="showResetModal(record.username)">
              {{ t('pages.users.changePassword') }}
            </a-button>
            <a-popconfirm
              :title="t('pages.users.deleteConfirm')"
              :ok-text="t('common.delete')"
              :cancel-text="t('common.cancel')"
              @confirm="handleDelete(record.username)"
            >
              <a-button type="link" size="small" danger data-testid="row-delete" :loading="deleting === record.username">
                {{ t('common.delete') }}
              </a-button>
            </a-popconfirm>
          </template>
        </MobileCard>
        <a-pagination
          v-if="dataSource.length > MOBILE_PAGE_SIZE"
          v-model:current="mobilePage"
          :page-size="MOBILE_PAGE_SIZE"
          :total="dataSource.length"
          :show-size-changer="false"
          size="small"
          class="mobile-pagination"
        />
      </div>

      <div v-else class="fill-table">
        <div v-if="selectedRowKeys.length > 0" class="bulk-bar">
          <span>{{ t('common.selected', { count: selectedRowKeys.length }) }}</span>
          <a-button size="small" type="text" @click="selectedRowKeys = []">
            {{ t('common.clearSelection') }}
          </a-button>
          <a-popconfirm
            :title="t('pages.users.bulkDeleteConfirm', { count: selectedRowKeys.length })"
            :ok-text="t('common.delete')"
            :cancel-text="t('common.cancel')"
            @confirm="handleBulkDelete"
          >
            <a-button size="small" danger data-testid="users-bulk-delete" :loading="bulkDeleting">
              {{ t('pages.users.bulkDelete') }}
            </a-button>
          </a-popconfirm>
        </div>
        <a-table
          :data-source="dataSource"
          :columns="columns"
          :loading="loading"
          row-key="username"
          :row-selection="{ selectedRowKeys, onChange: onSelectionChange }"
          :pagination="tablePagination"
          :scroll="{ x: 480, y: 1 }"
        >
    <template #bodyCell="{ column, record }">
      <template v-if="column.key === 'action'">
        <a-space>
          <a-button type="link" size="small" @click="showResetModal(record.username)">
            {{ t('pages.users.changePassword') }}
          </a-button>
          <a-popconfirm
            :title="t('pages.users.deleteConfirm')"
            :ok-text="t('common.delete')"
            :cancel-text="t('common.cancel')"
            @confirm="handleDelete(record.username)"
          >
            <a-button type="link" size="small" danger data-testid="row-delete" :loading="deleting === record.username">
              {{ t('common.delete') }}
            </a-button>
          </a-popconfirm>
        </a-space>
      </template>
    </template>
        <template #emptyText>
          <LoadError v-if="loadError" :retrying="loading" @retry="fetchData" />
          <a-empty v-else :description="t('pages.users.emptyDescription')">
            <a-button type="primary" data-testid="empty-create" @click="showAddModal">
              {{ t('pages.users.addUser') }}
            </a-button>
          </a-empty>
        </template>
        </a-table>
      </div>
    </a-card>
  </div>

  <a-modal
    v-model:open="modalVisible"
    :title="t('pages.users.form.addTitle')"
    :confirm-loading="submitting"
    :ok-text="t('pages.users.form.okAdd')"
    :cancel-text="t('common.cancel')"
    @ok="handleSubmit"
  >
    <a-form layout="vertical">
      <a-form-item
        html-for="user-username"
        :label="t('pages.users.form.username')"
        required
        :validate-status="fieldErrors.username ? 'error' : undefined"
        :help="fieldErrors.username"
      >
        <a-input
          id="user-username"
          v-model:value="form.username"
          :placeholder="t('pages.users.form.usernamePlaceholder')"
        />
      </a-form-item>
      <a-form-item
        html-for="user-password"
        :label="t('pages.users.form.password')"
        required
        :validate-status="fieldErrors.password ? 'error' : undefined"
        :help="fieldErrors.password"
      >
        <a-input-password
          id="user-password"
          v-model:value="form.password"
          :placeholder="t('pages.users.form.passwordPlaceholder')"
          autocomplete="new-password"
        />
      </a-form-item>
    </a-form>
  </a-modal>

  <a-modal
    v-model:open="passwordModalVisible"
    :title="t('pages.users.form.resetTitle', { name: resetTarget })"
    :confirm-loading="submitting"
    :ok-text="t('pages.users.form.okSave')"
    :cancel-text="t('common.cancel')"
    @ok="handleResetPassword"
  >
    <a-form layout="vertical">
      <a-form-item
        html-for="user-reset-password"
        :label="t('pages.users.form.newPassword')"
        required
        :validate-status="fieldErrors.reset ? 'error' : undefined"
        :help="fieldErrors.reset"
      >
        <a-input-password
          id="user-reset-password"
          v-model:value="resetPassword"
          :placeholder="t('pages.users.form.newPasswordPlaceholder')"
          autocomplete="new-password"
        />
      </a-form-item>
    </a-form>
  </a-modal>
</template>
