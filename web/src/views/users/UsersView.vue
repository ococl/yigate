<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { message } from 'antdv-next'
import LoadError from '@/components/LoadError.vue'
import MobileListSkeleton from '@/components/MobileListSkeleton.vue'
import { useIsMobile } from '@/composables/useIsMobile'
import { listUsers, addUser, updateUserPassword, deleteUser, setUserEnabled, batchDeleteUsers } from '@/api/users'
import { parseApiError } from '@/api/types'
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
// 正在切换启用状态的用户名
const toggling = ref<string | null>(null)
// 「回落匿名访问」风险确认弹窗：用户确认后执行 pendingRiskAction
const riskModalVisible = ref(false)
const pendingRiskAction = ref<null | (() => Promise<void>)>(null)
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
  { title: t('pages.users.columns.status'), key: 'status', width: 100 },
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

/** 服务端以 409 + code=last_active_user 表示"该操作会让代理端口失去认证、回落匿名"。 */
function isLastActiveUserError(error: unknown): boolean {
  const apiError = parseApiError(error)
  return apiError?.status === 409 && apiError.code === 'last_active_user'
}

function notifyFailure(error: unknown): void {
  const apiError = parseApiError(error)
  message.error(apiError?.detail || t('common.requestFailed'))
}

/** 需要用户明确接受"回落匿名访问"的风险，确认后执行 pendingRiskAction。 */
function askAnonymousRisk(action: () => Promise<void>): void {
  pendingRiskAction.value = action
  riskModalVisible.value = true
}

async function confirmAnonymousRisk(): Promise<void> {
  const action = pendingRiskAction.value
  riskModalVisible.value = false
  pendingRiskAction.value = null
  if (action) await action()
}

function cancelAnonymousRisk(): void {
  pendingRiskAction.value = null
  riskModalVisible.value = false
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

async function handleDelete(username: string, force = false) {
  if (deleting.value) return
  deleting.value = username
  try {
    await deleteUser(username, force)
    message.success(t('pages.users.messages.deleted'))
    await fetchData()
  } catch (error) {
    // 删除最后一个启用用户：先确认风险，用户确认后带 force 重试
    if (!force && isLastActiveUserError(error)) {
      askAnonymousRisk(() => handleDelete(username, true))
      return
    }
    notifyFailure(error)
  } finally {
    deleting.value = null
  }
}

async function handleToggleEnabled(record: UserInfo, enabled: boolean, force = false) {
  if (toggling.value) return
  toggling.value = record.username
  try {
    await setUserEnabled(record.username, enabled, force)
    message.success(
      enabled ? t('pages.users.messages.enabled', { name: record.username }) : t('pages.users.messages.disabled', { name: record.username }),
    )
    await fetchData()
  } catch (error) {
    // 禁用最后一个启用用户：同样先确认风险
    if (!force && !enabled && isLastActiveUserError(error)) {
      askAnonymousRisk(() => handleToggleEnabled(record, enabled, true))
      return
    }
    notifyFailure(error)
    // 失败时重新拉取，让开关回到服务端真实状态
    await fetchData()
  } finally {
    toggling.value = null
  }
}

function onSelectionChange(keys: (string | number)[]) {
  selectedRowKeys.value = keys.map(String)
}

// 不弹二次确认框逐个删，用户可以一次清掉一批建错的账号
async function handleBulkDelete(force = false) {
  if (bulkDeleting.value || selectedRowKeys.value.length === 0) return
  bulkDeleting.value = true
  const targets = [...selectedRowKeys.value]
  try {
    // 一次请求一次落盘，服务端返回逐项结果
    const res = await batchDeleteUsers(targets, force)
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
  } catch (error) {
    // 选中的用户覆盖了全部启用用户时，同样需要确认匿名回落风险
    if (!force && isLastActiveUserError(error)) {
      selectedRowKeys.value = targets
      askAnonymousRisk(() => handleBulkDelete(true))
      return
    }
    notifyFailure(error)
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
            <a-space>
              <a-typography-text strong>{{ record.username }}</a-typography-text>
              <a-tag :color="record.enabled ? 'green' : 'default'">
                {{ record.enabled ? t('pages.users.status.enabled') : t('pages.users.status.disabled') }}
              </a-tag>
            </a-space>
          </template>
          <template #actions>
            <a-switch
              size="small"
              :checked="record.enabled"
              :loading="toggling === record.username"
              :data-testid="`user-toggle-${record.username}`"
              @change="(checked: unknown) => handleToggleEnabled(record, !!checked)"
            />
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
            @confirm="handleBulkDelete()"
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
      <template v-if="column.key === 'status'">
        <a-switch
          size="small"
          :checked="record.enabled"
          :loading="toggling === record.username"
          :data-testid="`user-toggle-${record.username}`"
          @change="(checked: unknown) => handleToggleEnabled(record, !!checked)"
        />
      </template>
      <template v-else-if="column.key === 'action'">
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

  <a-modal
    v-model:open="riskModalVisible"
    data-testid="users-anonymous-risk"
    :title="t('pages.users.anonymousRisk.title')"
    :ok-text="t('pages.users.anonymousRisk.confirm')"
    :cancel-text="t('common.cancel')"
    ok-type="danger"
    @ok="confirmAnonymousRisk"
    @cancel="cancelAnonymousRisk"
  >
    <a-typography-paragraph>
      {{ t('pages.users.anonymousRisk.description') }}
    </a-typography-paragraph>
    <a-typography-paragraph type="secondary">
      {{ t('pages.users.anonymousRisk.detail') }}
    </a-typography-paragraph>
  </a-modal>
</template>
