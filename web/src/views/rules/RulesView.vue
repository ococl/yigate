<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { message, Modal } from 'antdv-next'
import LoadError from '@/components/LoadError.vue'
import MobileListSkeleton from '@/components/MobileListSkeleton.vue'
import { useIsMobile } from '@/composables/useIsMobile'
import {
  listRules,
  addRule,
  updateRule,
  moveRule,
  deleteRule,
  batchDeleteRules,
  batchToggleRules,
  replaceRules,
} from '@/api/rules'
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
// 多选批量操作只作用于桌面表格；键是规则当前序号
const selectedRowKeys = ref<number[]>([])
const bulkDeleting = ref(false)
// 正在切换启用状态的行（单行开关用），避免连点重复请求
const togglingIndex = ref<number | null>(null)
const bulkToggling = ref(false)
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
  disabled: false,
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
  { label: 'proxy（仅走上游）', value: 'proxy' },
  { label: 'direct（仅直连）', value: 'direct' },
  { label: 'proxy_first（代理优先，失败再直连）', value: 'proxy_first' },
  { label: 'direct_first（直连优先，失败再代理）', value: 'direct_first' },
  { label: 'auto（按全局 auto_mode 判断）', value: 'auto' },
  { label: 'deny（拒绝）', value: 'deny' },
]

const needsValues = computed(() => form.value.type !== 'match')
const needsUpstreams = computed(() => form.value.action !== 'direct' && form.value.action !== 'deny')

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
  { title: '启用', key: 'enabled', width: 80 },
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

// --- 文本视图：与可视化表格共用同一份服务端规则 ---

type RuleViewMode = 'visual' | 'text'
const viewMode = ref<RuleViewMode>('visual')
const viewModeOptions = [
  { label: '可视化', value: 'visual' },
  { label: '文本', value: 'text' },
]
const rulesText = ref('')
// 已保存文本快照，用于判断是否有未保存修改
const savedText = ref('')
const textSaving = ref(false)

// 兼容 Clash 写法（DOMAIN-SUFFIX / IP-CIDR6 / PROXY / REJECT 等），保存时统一转为 yiGate 名称
const RULE_TYPE_ALIASES: Record<string, string> = {
  domain: 'domain',
  domainsuffix: 'domain_suffix',
  domainkeyword: 'domain_keyword',
  domainregex: 'domain_regex',
  ipcidr: 'ip_cidr',
  ipcidr6: 'ip_cidr',
  geosite: 'geosite',
  match: 'match',
}

const RULE_ACTION_ALIASES: Record<string, string> = {
  direct: 'direct',
  proxy: 'proxy',
  auto: 'auto',
  directfirst: 'direct_first',
  proxyfirst: 'proxy_first',
  deny: 'deny',
  reject: 'deny',
  rejectdrop: 'deny',
}

function compactToken(raw: string): string {
  return raw.trim().toLowerCase().replace(/[-_\s]/g, '')
}

function normalizeRuleType(raw: string): string {
  return RULE_TYPE_ALIASES[compactToken(raw)] || ''
}

function normalizeRuleAction(raw: string): string {
  return RULE_ACTION_ALIASES[compactToken(raw)] || ''
}

interface TextRuleIssue {
  line: number
  message: string
}

// splitEscaped 按分隔符切分，只处理 \<sep> 与 \\ 两种转义；
// 其它反斜杠原样保留，避免破坏 domain_regex 里的 \. 写法。
function splitEscaped(text: string, sep: string): string[] {
  const out: string[] = []
  let buf = ''
  for (let i = 0; i < text.length; i += 1) {
    const ch = text[i]
    if (ch === '\\' && i + 1 < text.length) {
      const next = text[i + 1]
      if (next === sep || next === '\\') {
        buf += next
        i += 1
        continue
      }
      buf += ch
      continue
    }
    if (ch === sep) {
      out.push(buf)
      buf = ''
      continue
    }
    buf += ch
  }
  out.push(buf)
  return out
}

function escapeToken(value: string): string {
  return value.replace(/\\/g, '\\\\').replace(/\|/g, '\\|').replace(/,/g, '\\,')
}

/** 规则序列化为文本：类型,匹配值1|匹配值2,动作[,上游1|上游2]；行首 ! 表示禁用 */
function formatRulesText(rules: RuleRequest[]): string {
  return rules
    .map((rule) => {
      const values = (rule.values || []).map(escapeToken).join('|') || '-'
      const upstreams = (rule.upstreams || []).map(escapeToken).join('|')
      const fields = [rule.type, values, rule.action]
      if (upstreams) fields.push(upstreams)
      return (rule.disabled ? '!' : '') + fields.join(',')
    })
    .join('\n')
}

/**
 * 解析文本。动作从行尾向前识别，所以匹配值里可以直接写逗号（例如正则量词 {1,3}），
 * 多个匹配值之间用 | 分隔。
 */
function parseRulesText(text: string): { rules: RuleRequest[]; issues: TextRuleIssue[] } {
  const rules: RuleRequest[] = []
  const issues: TextRuleIssue[] = []
  text.split(/\r?\n/).forEach((raw, index) => {
    const lineNo = index + 1
    let line = raw.trim()
    if (!line || line.startsWith('#')) return
    // 行首 ! 表示禁用该条规则；与整行 # 注释区分开，禁用规则会保留在配置里
    const disabled = line.startsWith('!')
    if (disabled) line = line.slice(1).trim()
    if (!line) {
      issues.push({ line: lineNo, message: '! 后面缺少规则内容' })
      return
    }
    const parts = splitEscaped(line, ',')
    const type = normalizeRuleType(parts[0] || '')
    if (!type) {
      issues.push({ line: lineNo, message: `未知类型 "${(parts[0] || '').trim()}"` })
      return
    }
    let actionIndex = -1
    for (let i = parts.length - 1; i >= 2; i -= 1) {
      if (normalizeRuleAction(parts[i])) {
        actionIndex = i
        break
      }
    }
    if (actionIndex < 0) {
      // 兼容 Clash 的 MATCH,PROXY 两段写法
      const shorthand = type === 'match' && parts.length === 2 ? normalizeRuleAction(parts[1]) : ''
      if (shorthand) {
        rules.push({ type, values: [], action: shorthand, upstreams: [], disabled })
        return
      }
      issues.push({ line: lineNo, message: '缺少动作（如 direct / proxy / auto / deny）' })
      return
    }
    const action = normalizeRuleAction(parts[actionIndex])
    if (type === 'match' && action === 'deny') {
      issues.push({ line: lineNo, message: 'match 类型不能使用 deny 动作' })
      return
    }
    const rawValues = parts.slice(1, actionIndex).join(',')
    const values =
      rawValues === '' || rawValues === '-'
        ? []
        : splitEscaped(rawValues, '|')
            .map((v) => v.trim())
            .filter((v) => v !== '' && v !== '-')
    if (type !== 'match' && values.length === 0) {
      issues.push({ line: lineNo, message: `类型 ${type} 至少需要一个匹配值` })
      return
    }
    const rawUpstreams = parts.slice(actionIndex + 1).join(',')
    const upstreams = rawUpstreams
      ? splitEscaped(rawUpstreams, '|')
          .map((v) => v.trim())
          .filter(Boolean)
      : []
    rules.push({ type, values: type === 'match' ? [] : values, action, upstreams, disabled })
  })
  return { rules, issues }
}

const parsedText = computed(() => parseRulesText(rulesText.value))
const textIssues = computed(() => parsedText.value.issues)
const textDirty = computed(() => rulesText.value !== savedText.value)
const disabledCount = computed(() => parsedText.value.rules.filter((rule) => rule.disabled).length)

function loadRulesIntoText() {
  rulesText.value = formatRulesText(dataSource.value)
  savedText.value = rulesText.value
}

function onViewModeChange(next: string | number) {
  const mode = next as RuleViewMode
  if (mode === viewMode.value) return
  if (mode === 'visual' && textDirty.value) {
    Modal.confirm({
      title: '放弃文本修改？',
      content: '文本视图中有未保存的修改，切换到可视化视图会丢弃这些修改。',
      okText: '放弃修改',
      cancelText: '继续编辑',
      onOk: () => {
        viewMode.value = 'visual'
      },
    })
    return
  }
  viewMode.value = mode
  if (mode === 'text') loadRulesIntoText()
}

async function saveRulesText() {
  if (textIssues.value.length > 0) {
    message.error(`还有 ${textIssues.value.length} 处格式问题，请先修正`)
    return
  }
  const rules = parsedText.value.rules
  const submit = async () => {
    textSaving.value = true
    try {
      await replaceRules(rules)
      message.success(`已保存 ${rules.length} 条规则`)
      await fetchData()
      loadRulesIntoText()
    } catch {
      // 错误已由请求拦截器提示，保留文本内容供用户继续修改
    } finally {
      textSaving.value = false
    }
  }
  if (rules.length === 0) {
    Modal.confirm({
      title: '清空全部路由规则？',
      content: '保存后会删除所有路由规则，未命中的请求将按全局 final 动作处理。',
      okText: '清空',
      okType: 'danger',
      cancelText: '取消',
      onOk: submit,
    })
    return
  }
  await submit()
}

function formatTextNow() {
  if (textIssues.value.length > 0) {
    message.warning('请先修正格式问题再格式化')
    return
  }
  rulesText.value = formatRulesText(parsedText.value.rules)
}

async function fetchData() {
  loading.value = true
  try {
    const [rules, ups] = await Promise.all([listRules(), listUpstreams()])
    dataSource.value = rules.rules || []
    clampPages()
    upstreamOptions.value = (ups.upstreams || []).map((u) => ({ label: u.name, value: u.name }))
    loadError.value = false
    // 文本视图下刷新时同步最新规则；有未保存修改则保留用户输入
    if (viewMode.value === 'text' && !textDirty.value) loadRulesIntoText()
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
  form.value = { type: 'domain_suffix', values: [], action: 'proxy', upstreams: [], disabled: false }
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
    disabled: record.disabled === true,
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
    disabled: form.value.disabled === true,
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
  const classes: string[] = []
  if (highlightIndex.value === record.index) classes.push('row-highlight')
  if (record.disabled) classes.push('row-disabled')
  return classes.join(' ')
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
    case 'direct_first':
      return 'green'
    case 'proxy':
    case 'proxy_first':
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
  const targets = [...selectedRowKeys.value]
  try {
    // 索引由服务端按删除前的位置解释，并合并成一次落盘，这里不必自己倒序
    const res = await batchDeleteRules(targets)
    // 索引已经变了，失败项无法按原索引保留，统一清空选择
    selectedRowKeys.value = []
    await fetchData()
    const failed = res.failed ?? []
    if (failed.length === 0) {
      message.success(`已删除 ${res.deleted.length} 条规则`)
      return
    }
    const names = failed.map((item) => `第 ${Number(item.target) + 1} 条`).join('、')
    message.warning(`已删除 ${res.deleted.length} 条，失败 ${failed.length} 条：${names}`)
  } catch {
    // 整体失败（网络/500）已由请求层提示，保留当前选择
  } finally {
    bulkDeleting.value = false
  }
}

/** 单行启用/禁用：整条规则回写，避免只发改动字段时清空匹配值与上游。 */
async function handleToggle(record: RuleInfo, enabled: boolean) {
  if (togglingIndex.value !== null) return
  togglingIndex.value = record.index
  try {
    await updateRule(record.index, {
      type: record.type,
      values: [...(record.values || [])],
      action: record.action,
      upstreams: [...(record.upstreams || [])],
      disabled: !enabled,
    })
    message.success(enabled ? '已启用' : '已禁用')
    await fetchData()
  } catch {
    // 错误已由拦截器处理
  } finally {
    togglingIndex.value = null
  }
}

async function handleBulkToggle(disabled: boolean) {
  if (bulkToggling.value || selectedRowKeys.value.length === 0) return
  bulkToggling.value = true
  const targets = [...selectedRowKeys.value]
  const label = disabled ? '禁用' : '启用'
  try {
    const res = await batchToggleRules(targets, disabled)
    selectedRowKeys.value = []
    await fetchData()
    const failed = res.failed ?? []
    if (failed.length === 0) {
      message.success(`已${label} ${res.updated.length} 条规则`)
      return
    }
    const names = failed.map((item) => `第 ${Number(item.target) + 1} 条`).join('、')
    message.warning(`已${label} ${res.updated.length} 条，失败 ${failed.length} 条：${names}`)
  } catch {
    // 整体失败已由请求层提示，保留当前选择
  } finally {
    bulkToggling.value = false
  }
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
      <a-space wrap>
        <a-radio-group
          :value="viewMode"
          size="small"
          option-type="button"
          :options="viewModeOptions"
          @update:value="onViewModeChange"
        />
        <a-button @click="fetchData" :loading="loading">刷新</a-button>
        <a-button v-if="viewMode === 'visual'" type="primary" @click="openAddModal">新增规则</a-button>
      </a-space>
    </div>

    <a-card class="fill-card">
      <div v-if="viewMode === 'text'" class="text-editor">
        <a-alert
          type="info"
          show-icon
          message="每行一条规则：类型,匹配值1|匹配值2,动作[,上游1|上游2]"
          description="动作为 direct / proxy / direct_first / proxy_first / auto / deny；行首 ! 表示禁用（保留规则但不参与匹配）；整行以 # 开头为注释；兼容 Clash 写法（DOMAIN-SUFFIX / PROXY / REJECT）；匹配值含逗号或竖线时用反斜杠转义。"
        />
        <a-textarea
          v-model:value="rulesText"
          class="rules-textarea"
          :rows="18"
          spellcheck="false"
          placeholder="geosite,category-ads-all,deny&#10;!domain_suffix,ads.example.com,deny&#10;geosite,cn,direct&#10;geosite,geolocation-!cn,proxy&#10;match,-,proxy"
        />
        <a-alert
          v-if="textIssues.length > 0"
          type="error"
          show-icon
          :message="`${textIssues.length} 处格式问题`"
        >
          <template #description>
            <ul class="text-issues">
              <li v-for="issue in textIssues" :key="`${issue.line}-${issue.message}`">
                第 {{ issue.line }} 行：{{ issue.message }}
              </li>
            </ul>
          </template>
        </a-alert>
        <div class="text-actions">
          <a-space wrap>
            <a-button @click="loadRulesIntoText" :disabled="textSaving">重置</a-button>
            <a-button @click="formatTextNow" :disabled="textSaving">格式化</a-button>
            <a-button
              type="primary"
              :loading="textSaving"
              :disabled="textIssues.length > 0"
              @click="saveRulesText"
            >
              保存规则
            </a-button>
          </a-space>
          <span class="muted">
            {{ parsedText.rules.length }} 条规则{{ disabledCount > 0 ? ` · ${disabledCount} 条已禁用` : ''
            }}{{ textDirty ? ' · 有未保存修改' : '' }}
          </span>
        </div>
      </div>

      <div v-else-if="isMobile" class="mobile-list">
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
            <a-space size="small">
              <a-tag v-if="record.disabled" color="default">已禁用</a-tag>
              <a-tag :color="actionColor(record.action)">{{ record.action }}</a-tag>
              <a-switch
                size="small"
                :checked="!record.disabled"
                :loading="togglingIndex === record.index"
                :disabled="togglingIndex !== null && togglingIndex !== record.index"
                @change="(checked) => handleToggle(record, checked === true)"
              />
            </a-space>
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
          <a-button size="small" :loading="bulkToggling" @click="handleBulkToggle(false)">批量启用</a-button>
          <a-button size="small" :loading="bulkToggling" @click="handleBulkToggle(true)">批量禁用</a-button>
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
      <template v-else-if="column.key === 'enabled'">
        <a-switch
          size="small"
          :checked="!record.disabled"
          :loading="togglingIndex === record.index"
          :disabled="togglingIndex !== null && togglingIndex !== record.index"
          @change="(checked) => handleToggle(record, checked === true)"
        />
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
            <a-select v-model:value="form.action" :options="actionOptions" />
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
        <template #extra>
          proxy、proxy_first、auto 会使用这里指定的上游；不选则使用全部可用上游。direct_first 仅在回退到代理时使用。
        </template>
      </a-form-item>

      <a-form-item label="规则状态">
        <a-space>
          <a-switch
            :checked="form.disabled !== true"
            @change="(checked) => (form.disabled = checked !== true)"
          />
          <span class="muted">
            {{ form.disabled ? '已禁用：保存后保留规则但不参与匹配' : '启用中：参与路由匹配' }}
          </span>
        </a-space>
      </a-form-item>
    </a-form>
  </a-modal>
</template>

<style scoped>
.text-editor {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.rules-textarea {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace;
  font-size: 13px;
  line-height: 1.6;
  tab-size: 2;
}

.text-issues {
  margin: 0;
  padding-left: 18px;
}

.text-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

/* 禁用行整体弱化，但仍可编辑与重新启用 */
:deep(.row-disabled) > td {
  opacity: 0.55;
}
</style>
