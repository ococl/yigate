<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { message } from 'antdv-next'
import LoadError from '@/components/LoadError.vue'
import MobileListSkeleton from '@/components/MobileListSkeleton.vue'
import { useIsMobile } from '@/composables/useIsMobile'
import { listRules, addRule, updateRule, moveRule, deleteRule } from '@/api/rules'
import { listUpstreams } from '@/api/upstreams'
import type { RuleInfo, RuleRequest } from '@/types/api'

const isMobile = useIsMobile()
const dataSource = ref<RuleInfo[]>([])
const upstreamOptions = ref<{ label: string; value: string }[]>([])
const loading = ref(true)
// 首次/刷新失败时展示错误态与重试入口，避免与「暂无数据」混淆
const loadError = ref(false)
const movingIndex = ref<number | null>(null)
// 移动后目标行的新位置，用于滚动定位与短暂高亮
const highlightIndex = ref<number | null>(null)
let highlightTimer: ReturnType<typeof setTimeout> | undefined
// 正在删除的规则序号，避免重复点击触发多次 DELETE
const deleting = ref<number | null>(null)
// 多选批量删除只作用于桌面表格；键是规则当前序号
const selectedRowKeys = ref<number[]>([])
const bulkDeleting = ref(false)
const modalVisible = ref(false)
const submitting = ref(false)
const isEdit = ref(false)
const editingIndex = ref(-1)

// 表单内联校验：字段名 -> 错误文案，输入变化后自动清除
const fieldErrors = reactive<Record<string, string>>({})

function resetFieldErrors() {
  for (const key of Object.keys(fieldErrors)) delete fieldErrors[key]
}

const form = ref<RuleRequest>({
  type: 'domain_suffix',
  values: [],
  action: 'proxy',
  upstreams: [],
})
// 文本域使用独立状态，避免每输入一个字符就被重新格式化导致光标跳动
const valuesText = ref('')
watch(valuesText, () => resetFieldErrors())

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

// 移动端卡片列表本地分页，与桌面表格每页条数保持一致
const MOBILE_PAGE_SIZE = 20
const mobilePage = ref(1)
// 桌面表格同样受控，删除/重排后能把越界页码收回来
const DESKTOP_PAGE_SIZE = 20
const desktopPage = ref(1)
const desktopPagination = computed(() => ({
  current: desktopPage.value,
  pageSize: DESKTOP_PAGE_SIZE,
  hideOnSinglePage: true,
  onChange: (page: number) => {
    desktopPage.value = page
  },
}))
const mobileItems = computed(() =>
  dataSource.value.slice((mobilePage.value - 1) * MOBILE_PAGE_SIZE, mobilePage.value * MOBILE_PAGE_SIZE),
)

const columns = [
  { title: '顺序', dataIndex: 'index', key: 'index', width: 70 },
  { title: '类型', dataIndex: 'type', key: 'type', width: 150 },
  { title: '值', dataIndex: 'values', key: 'values', ellipsis: true },
  { title: '动作', dataIndex: 'action', key: 'action', width: 100 },
  { title: '上游', dataIndex: 'upstreams', key: 'upstreams', width: 220 },
  { title: '操作', key: 'operations', width: 230, fixed: 'right' as const },
]

// 删除/重排后页码可能越界，会停在空页；越界时回落到最后一页，其余情况保留当前页
function clampPages() {
  const total = dataSource.value.length
  const desktopMax = Math.max(1, Math.ceil(total / DESKTOP_PAGE_SIZE))
  if (desktopPage.value > desktopMax) desktopPage.value = desktopMax
  const mobileMax = Math.max(1, Math.ceil(total / MOBILE_PAGE_SIZE))
  if (mobilePage.value > mobileMax) mobilePage.value = mobileMax
}

async function fetchData() {
  loading.value = true
  try {
    const [rules, ups] = await Promise.all([listRules(), listUpstreams()])
    dataSource.value = rules.rules || []
    clampPages()
    upstreamOptions.value = (ups.upstreams || []).map((u) => ({ label: u.name, value: u.name }))
    loadError.value = false
  } catch {
    // 具体错误已由请求拦截器提示，这里只切换列表区域的状态
    loadError.value = true
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
  resetFieldErrors()
  if (!form.value.type) fieldErrors.type = '请选择规则类型'
  if (needsValues.value && form.value.values.length === 0) {
    fieldErrors.values = '请输入至少一个匹配值'
  }
  if (Object.keys(fieldErrors).length > 0) return
  // proxy 动作未指定上游时表示使用全部可用上游，无需强制选择

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

// 移动后行序变化，长列表里用户看不出刚才那行去了哪；翻到对应页并高亮一小会儿
function revealMovedRule(newIndex: number) {
  const desktopTarget = Math.floor(newIndex / DESKTOP_PAGE_SIZE) + 1
  if (desktopPage.value !== desktopTarget) desktopPage.value = desktopTarget
  const mobileTarget = Math.floor(newIndex / MOBILE_PAGE_SIZE) + 1
  if (mobilePage.value !== mobileTarget) mobilePage.value = mobileTarget

  highlightIndex.value = newIndex
  window.clearTimeout(highlightTimer)
  highlightTimer = window.setTimeout(() => {
    highlightIndex.value = null
  }, 2000)

  void nextTick(() => {
    // 等表格在切页后完成一次渲染，再定位
    window.requestAnimationFrame(() => {
      const localIndex = newIndex % (isMobile.value ? MOBILE_PAGE_SIZE : DESKTOP_PAGE_SIZE)
      const selector = isMobile.value ? '.mobile-card' : '.ant-table-tbody tr.ant-table-row'
      document.querySelectorAll(selector)[localIndex]?.scrollIntoView({ block: 'center', behavior: 'smooth' })
    })
  })
}

function rowClassName(record: RuleInfo): string {
  return highlightIndex.value === record.index ? 'row-highlight' : ''
}

async function handleMove(index: number, direction: 'up' | 'down') {
  // 移动期间禁止并发：第二次点击的序号基于旧顺序，落库后会错位
  if (movingIndex.value !== null) return
  movingIndex.value = index
  const targetIndex = direction === 'up' ? index - 1 : index + 1
  try {
    await moveRule(index, direction)
    await fetchData()
    revealMovedRule(targetIndex)
  } catch {
    // 错误已由拦截器处理
  } finally {
    movingIndex.value = null
  }
}

async function handleDelete(index: number) {
  if (deleting.value !== null) return
  deleting.value = index
  try {
    await deleteRule(index)
    message.success('删除成功')
    await fetchData()
  } catch {
    // 错误已由拦截器处理
  } finally {
    deleting.value = null
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

function onSelectionChange(keys: (string | number)[]) {
  selectedRowKeys.value = keys.map(Number)
}

async function handleBulkDelete() {
  if (bulkDeleting.value || selectedRowKeys.value.length === 0) return
  bulkDeleting.value = true
  // 删除会让后面的序号前移，必须从大到小删，否则会删错行
  const targets = [...selectedRowKeys.value].sort((a, b) => b - a)
  const failed: number[] = []
  for (const index of targets) {
    try {
      await deleteRule(index)
    } catch {
      failed.push(index)
    }
  }
  bulkDeleting.value = false
  selectedRowKeys.value = []
  await fetchData()
  if (failed.length === 0) {
    message.success(`已删除 ${targets.length} 条规则`)
    return
  }
  const names = failed.map((index) => `第 ${index + 1} 条`).join('、')
  message.warning(`已删除 ${targets.length - failed.length} 条，失败 ${failed.length} 条：${names}`)
}

onMounted(fetchData)
</script>

<template>
  <div class="page-shell">
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

    <a-card class="fill-card">
      <div v-if="isMobile" class="mobile-list">
        <LoadError v-if="loadError" :retrying="loading" @retry="fetchData" />
        <MobileListSkeleton v-else-if="loading && dataSource.length === 0" />
        <a-empty v-else-if="dataSource.length === 0" description="暂无路由规则，所有请求将按全局 default_action 处理">
          <a-button type="primary" @click="openAddModal">新增规则</a-button>
        </a-empty>
        <MobileCard
          v-for="record in mobileItems"
          :key="record.index"
          :highlight="highlightIndex === record.index"
        >
          <template #head>
            <a-space size="small">
              <a-tag>{{ record.index + 1 }}</a-tag>
              <a-typography-text strong>{{ record.type }}</a-typography-text>
            </a-space>
          </template>
          <template #extra>
            <a-tag :color="actionColor(record.action)">{{ record.action }}</a-tag>
          </template>
          <MobileCardRow label="匹配值">
              <a-space wrap size="small">
                <a-tag v-for="v in record.values" :key="v">{{ v }}</a-tag>
                <span v-if="!record.values || record.values.length === 0" class="muted">—</span>
              </a-space>
          </MobileCardRow>
          <MobileCardRow label="上游">
              <a-space wrap size="small">
                <a-tag v-for="u in record.upstreams" :key="u" color="purple">{{ u }}</a-tag>
                <span v-if="!record.upstreams || record.upstreams.length === 0" class="muted">—</span>
              </a-space>
          </MobileCardRow>
          <template #actions>
            <a-button
              size="small"
              :disabled="record.index === 0 || (movingIndex !== null && movingIndex !== record.index)"
              :loading="movingIndex === record.index"
              @click="handleMove(record.index, 'up')"
            >
              上移
            </a-button>
            <a-button
              size="small"
              :disabled="
                record.index === dataSource.length - 1 || (movingIndex !== null && movingIndex !== record.index)
              "
              :loading="movingIndex === record.index"
              @click="handleMove(record.index, 'down')"
            >
              下移
            </a-button>
            <a-button size="small" @click="openEditModal(record)">编辑</a-button>
            <a-popconfirm
              title="确定删除这条规则？"
              ok-text="删除"
              cancel-text="取消"
              @confirm="handleDelete(record.index)"
            >
              <a-button type="link" size="small" danger :loading="deleting === record.index">删除</a-button>
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
          <span>已选 {{ selectedRowKeys.length }} 项</span>
          <a-button size="small" type="text" @click="selectedRowKeys = []">取消选择</a-button>
          <a-popconfirm
            :title="`确定删除选中的 ${selectedRowKeys.length} 条规则？此操作不可撤销。`"
            ok-text="删除"
            cancel-text="取消"
            @confirm="handleBulkDelete"
          >
            <a-button size="small" danger :loading="bulkDeleting">批量删除</a-button>
          </a-popconfirm>
        </div>
        <a-table
          :data-source="dataSource"
          :columns="columns"
          :loading="loading"
          row-key="index"
          :row-selection="{ selectedRowKeys, onChange: onSelectionChange }"
          :row-class-name="rowClassName"
          :pagination="desktopPagination"
          :scroll="{ x: 900, y: 1 }"
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
              type="link"
              size="small"
              :disabled="record.index === 0 || (movingIndex !== null && movingIndex !== record.index)"
              :loading="movingIndex === record.index"
              aria-label="上移"
              @click="handleMove(record.index, 'up')"
            >↑</a-button>
          </a-tooltip>
          <a-tooltip title="下移">
            <a-button
              type="link"
              size="small"
              :disabled="
                record.index === dataSource.length - 1 || (movingIndex !== null && movingIndex !== record.index)
              "
              :loading="movingIndex === record.index"
              aria-label="下移"
              @click="handleMove(record.index, 'down')"
            >↓</a-button>
          </a-tooltip>
          <a-button type="link" size="small" @click="openEditModal(record)">编辑</a-button>
          <a-popconfirm title="确定删除此规则？" ok-text="删除" cancel-text="取消" @confirm="handleDelete(record.index)">
            <a-button type="link" size="small" danger :loading="deleting === record.index">删除</a-button>
          </a-popconfirm>
        </a-space>
      </template>
    </template>
        <template #emptyText>
          <LoadError v-if="loadError" :retrying="loading" @retry="fetchData" />
          <a-empty v-else description="暂无路由规则，所有请求将按全局 default_action 处理">
            <a-button type="primary" @click="openAddModal">新增规则</a-button>
          </a-empty>
        </template>
        </a-table>
      </div>
    </a-card>
  </div>

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
        <a-col :xs="24" :sm="12">
          <a-form-item
            html-for="rule-type"
            label="类型"
            required
            :validate-status="fieldErrors.type ? 'error' : undefined"
            :help="fieldErrors.type"
          >
            <a-select id="rule-type" v-model:value="form.type" :options="typeOptions" />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12">
          <a-form-item label="动作" required>
            <a-radio-group v-model:value="form.action" :options="actionOptions" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-form-item
        v-if="needsValues"
        html-for="rule-values"
        label="匹配值（每行一个）"
        required
        :validate-status="fieldErrors.values ? 'error' : undefined"
        :help="fieldErrors.values"
      >
        <a-textarea
          id="rule-values"
          :value="valuesText"
          :rows="5"
          :placeholder="form.type === 'ip_cidr' ? '10.0.0.0/8\n192.168.0.0/16' : 'example.com\nexample.org'"
          @update:value="syncValues"
        />
        <template #extra>
          <span v-if="form.type === 'domain_suffix'">按域名后缀匹配，example.com 会同时匹配 a.example.com。</span>
          <span v-else-if="form.type === 'ip_cidr'">使用 CIDR 格式，例如 10.0.0.0/8，非法网段会在保存时报错。</span>
          <span v-else-if="form.type === 'domain_regex'">使用 Go 正则语法，保存前会校验合法性。</span>
          <span v-else-if="form.type === 'geosite'">填写 geosite 分类名，例如 cn、geolocation-!cn；需要先配置并加载 geosite.dat。</span>
          <span v-else>每行一条，空行会被忽略。</span>
        </template>
      </a-form-item>

      <a-form-item v-else label="匹配值">
        <a-alert type="info" show-icon message="match 类型匹配所有请求，无需填写匹配值，通常放在规则末尾作为兜底。" />
      </a-form-item>

      <a-form-item html-for="rule-upstreams" v-if="needsUpstreams" label="指定上游（可多选）">
        <a-select id="rule-upstreams"
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
