<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
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

const { t } = useI18n()
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

const typeOptions = computed(() => [
  { label: t('pages.rules.typeOptions.domain'), value: 'domain' },
  { label: t('pages.rules.typeOptions.domainSuffix'), value: 'domain_suffix' },
  { label: t('pages.rules.typeOptions.domainKeyword'), value: 'domain_keyword' },
  { label: t('pages.rules.typeOptions.domainRegex'), value: 'domain_regex' },
  { label: t('pages.rules.typeOptions.ipCidr'), value: 'ip_cidr' },
  { label: t('pages.rules.typeOptions.geosite'), value: 'geosite' },
  { label: t('pages.rules.typeOptions.match'), value: 'match' },
])
const actionOptions = computed(() => [
  { label: t('pages.rules.actionOptions.proxy'), value: 'proxy' },
  { label: t('pages.rules.actionOptions.direct'), value: 'direct' },
  { label: t('pages.rules.actionOptions.proxyFirst'), value: 'proxy_first' },
  { label: t('pages.rules.actionOptions.directFirst'), value: 'direct_first' },
  { label: t('pages.rules.actionOptions.auto'), value: 'auto' },
  { label: t('pages.rules.actionOptions.deny'), value: 'deny' },
])

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

const columns = computed(() => [
  { title: t('pages.rules.columns.index'), dataIndex: 'index', key: 'index', width: 70 },
  { title: t('pages.rules.columns.enabled'), key: 'enabled', width: 80 },
  { title: t('pages.rules.columns.type'), dataIndex: 'type', key: 'type', width: 150 },
  { title: t('pages.rules.columns.values'), dataIndex: 'values', key: 'values', ellipsis: true },
  { title: t('pages.rules.columns.action'), dataIndex: 'action', key: 'action', width: 100 },
  { title: t('pages.rules.columns.upstreams'), dataIndex: 'upstreams', key: 'upstreams', width: 220 },
  { title: t('pages.rules.columns.operations'), key: 'operations', width: 230, fixed: 'right' as const },
])

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
const viewModeOptions = computed(() => [
  { label: t('pages.rules.viewVisual'), value: 'visual' },
  { label: t('pages.rules.viewText'), value: 'text' },
])
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
      issues.push({ line: lineNo, message: t('pages.rules.issues.emptyAfterBang') })
      return
    }
    const parts = splitEscaped(line, ',')
    const type = normalizeRuleType(parts[0] || '')
    if (!type) {
      issues.push({
        line: lineNo,
        message: t('pages.rules.issues.unknownType', { type: (parts[0] || '').trim() }),
      })
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
      issues.push({ line: lineNo, message: t('pages.rules.issues.missingAction') })
      return
    }
    const action = normalizeRuleAction(parts[actionIndex])
    if (type === 'match' && action === 'deny') {
      issues.push({ line: lineNo, message: t('pages.rules.issues.matchDeny') })
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
      issues.push({ line: lineNo, message: t('pages.rules.issues.needValues', { type }) })
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
      title: t('pages.rules.messages.discardTitle'),
      content: t('pages.rules.messages.discardContent'),
      okText: t('pages.rules.messages.discardOk'),
      cancelText: t('pages.rules.messages.keepEditing'),
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
    message.error(t('pages.rules.messages.fixIssues', { count: textIssues.value.length }))
    return
  }
  const rules = parsedText.value.rules
  const submit = async () => {
    textSaving.value = true
    try {
      await replaceRules(rules)
      message.success(t('pages.rules.messages.saved', { count: rules.length }))
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
      title: t('pages.rules.messages.clearTitle'),
      content: t('pages.rules.messages.clearContent'),
      okText: t('pages.rules.messages.clearOk'),
      okType: 'danger',
      cancelText: t('common.cancel'),
      onOk: submit,
    })
    return
  }
  await submit()
}

function formatTextNow() {
  if (textIssues.value.length > 0) {
    message.warning(t('pages.rules.messages.formatFirst'))
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
  if (!form.value.type) fieldErrors.type = t('pages.rules.messages.typeRequired')
  if (needsValues.value && form.value.values.length === 0) {
    fieldErrors.values = t('pages.rules.messages.valuesRequired')
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
      message.success(t('pages.rules.messages.updated'))
    } else {
      await addRule(payload)
      message.success(t('pages.rules.messages.added'))
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
    message.success(t('pages.rules.messages.deleted'))
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
      message.success(t('pages.rules.messages.deletedCount', { count: res.deleted.length }))
      return
    }
    const names = failed
      .map((item) => t('pages.rules.messages.itemLabel', { index: Number(item.target) + 1 }))
      .join(t('pages.rules.messages.listSeparator'))
    message.warning(
      t('pages.rules.messages.deletedPartial', {
        deleted: res.deleted.length,
        failed: failed.length,
        names,
      }),
    )
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
    message.success(enabled ? t('pages.rules.messages.enabled') : t('pages.rules.messages.disabled'))
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
  const label = disabled
    ? t('pages.rules.messages.toggleActionDisable')
    : t('pages.rules.messages.toggleActionEnable')
  try {
    const res = await batchToggleRules(targets, disabled)
    selectedRowKeys.value = []
    await fetchData()
    const failed = res.failed ?? []
    if (failed.length === 0) {
      message.success(t('pages.rules.messages.toggledCount', { action: label, count: res.updated.length }))
      return
    }
    const names = failed
      .map((item) => t('pages.rules.messages.itemLabel', { index: Number(item.target) + 1 }))
      .join(t('pages.rules.messages.listSeparator'))
    message.warning(
      t('pages.rules.messages.toggledPartial', {
        action: label,
        count: res.updated.length,
        failed: failed.length,
        names,
      }),
    )
  } catch {
    // 整体失败已由请求层提示，保留当前选择
  } finally {
    bulkToggling.value = false
  }
}

onMounted(fetchData)
</script>

<template>
  <div class="page-shell" data-testid="page-rules">
    <div class="page-header">
      <div>
        <a-typography-title :level="4" style="margin: 0;">{{ t('pages.rules.title') }}</a-typography-title>
        <a-typography-text type="secondary">
          {{ t('pages.rules.subtitle') }}
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
        <a-button @click="fetchData" :loading="loading">{{ t('common.refresh') }}</a-button>
        <a-button v-if="viewMode === 'visual'" type="primary" data-testid="rules-add" @click="openAddModal">
          {{ t('pages.rules.addRule') }}
        </a-button>
      </a-space>
    </div>

    <a-card class="fill-card">
      <div v-if="viewMode === 'text'" class="text-editor">
        <a-alert
          type="info"
          show-icon
          :message="t('pages.rules.text.message')"
          :description="t('pages.rules.text.description')"
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
          :message="t('pages.rules.text.issues', { count: textIssues.length })"
        >
          <template #description>
            <ul class="text-issues">
              <li v-for="issue in textIssues" :key="`${issue.line}-${issue.message}`">
                {{ t('pages.rules.text.issueLine', { line: issue.line, message: issue.message }) }}
              </li>
            </ul>
          </template>
        </a-alert>
        <div class="text-actions">
          <a-space wrap>
            <a-button @click="loadRulesIntoText" :disabled="textSaving">
              {{ t('pages.rules.text.reset') }}
            </a-button>
            <a-button @click="formatTextNow" :disabled="textSaving">
              {{ t('pages.rules.text.format') }}
            </a-button>
            <a-button
              type="primary"
              :loading="textSaving"
              :disabled="textIssues.length > 0"
              @click="saveRulesText"
            >
              {{ t('pages.rules.text.save') }}
            </a-button>
          </a-space>
          <span class="muted">
            {{ t('pages.rules.text.summary', { count: parsedText.rules.length })
            }}{{ disabledCount > 0 ? t('pages.rules.text.disabledSuffix', { count: disabledCount }) : ''
            }}{{ textDirty ? t('pages.rules.text.dirtySuffix') : '' }}
          </span>
        </div>
      </div>

      <div v-else-if="isMobile" class="mobile-list">
        <LoadError v-if="loadError" :retrying="loading" @retry="fetchData" />
        <MobileListSkeleton v-else-if="loading && dataSource.length === 0" />
        <a-empty v-else-if="dataSource.length === 0" :description="t('pages.rules.emptyDescription')">
          <a-button type="primary" data-testid="empty-create" @click="openAddModal">
            {{ t('pages.rules.addRule') }}
          </a-button>
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
              <a-tag v-if="record.disabled" color="default">{{ t('pages.rules.disabledTag') }}</a-tag>
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
          <MobileCardRow :label="t('pages.rules.columns.values')">
              <a-space wrap size="small">
                <a-tag v-for="v in record.values" :key="v">{{ v }}</a-tag>
                <span v-if="!record.values || record.values.length === 0" class="muted">—</span>
              </a-space>
          </MobileCardRow>
          <MobileCardRow :label="t('pages.rules.columns.upstreams')">
              <a-space wrap size="small">
                <a-tag v-for="u in record.upstreams" :key="u" color="purple">{{ u }}</a-tag>
                <span v-if="!record.upstreams || record.upstreams.length === 0" class="muted">—</span>
              </a-space>
          </MobileCardRow>
          <template #actions>
            <a-button
              size="small"
              data-testid="row-move-up"
              :disabled="record.index === 0 || (movingIndex !== null && movingIndex !== record.index)"
              :loading="movingIndex === record.index"
              @click="handleMove(record.index, 'up')"
            >
              {{ t('pages.rules.moveUp') }}
            </a-button>
            <a-button
              size="small"
              data-testid="row-move-down"
              :disabled="
                record.index === dataSource.length - 1 || (movingIndex !== null && movingIndex !== record.index)
              "
              :loading="movingIndex === record.index"
              @click="handleMove(record.index, 'down')"
            >
              {{ t('pages.rules.moveDown') }}
            </a-button>
            <a-button size="small" @click="openEditModal(record)">{{ t('common.edit') }}</a-button>
            <a-popconfirm
              :title="t('pages.rules.deleteConfirm')"
              :ok-text="t('common.delete')"
              :cancel-text="t('common.cancel')"
              @confirm="handleDelete(record.index)"
            >
              <a-button type="link" size="small" danger :loading="deleting === record.index">
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
          <a-button size="small" :loading="bulkToggling" @click="handleBulkToggle(false)">
            {{ t('pages.rules.bulkEnable') }}
          </a-button>
          <a-button size="small" :loading="bulkToggling" @click="handleBulkToggle(true)">
            {{ t('pages.rules.bulkDisable') }}
          </a-button>
          <a-popconfirm
            :title="t('pages.rules.bulkDeleteConfirm', { count: selectedRowKeys.length })"
            :ok-text="t('common.delete')"
            :cancel-text="t('common.cancel')"
            @confirm="handleBulkDelete"
          >
            <a-button size="small" danger :loading="bulkDeleting">{{ t('pages.rules.bulkDelete') }}</a-button>
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
          <a-tooltip :title="t('pages.rules.moveUp')">
            <a-button
              type="link"
              size="small"
              data-testid="row-move-up"
              :disabled="record.index === 0 || (movingIndex !== null && movingIndex !== record.index)"
              :loading="movingIndex === record.index"
              :aria-label="t('pages.rules.moveUp')"
              @click="handleMove(record.index, 'up')"
            >↑</a-button>
          </a-tooltip>
          <a-tooltip :title="t('pages.rules.moveDown')">
            <a-button
              type="link"
              size="small"
              data-testid="row-move-down"
              :disabled="
                record.index === dataSource.length - 1 || (movingIndex !== null && movingIndex !== record.index)
              "
              :loading="movingIndex === record.index"
              :aria-label="t('pages.rules.moveDown')"
              @click="handleMove(record.index, 'down')"
            >↓</a-button>
          </a-tooltip>
          <a-button type="link" size="small" data-testid="row-edit" @click="openEditModal(record)">
            {{ t('common.edit') }}
          </a-button>
          <a-popconfirm
            :title="t('pages.rules.deleteConfirmShort')"
            :ok-text="t('common.delete')"
            :cancel-text="t('common.cancel')"
            @confirm="handleDelete(record.index)"
          >
            <a-button type="link" size="small" danger data-testid="row-delete" :loading="deleting === record.index">
              {{ t('common.delete') }}
            </a-button>
          </a-popconfirm>
        </a-space>
      </template>
    </template>
        <template #emptyText>
          <LoadError v-if="loadError" :retrying="loading" @retry="fetchData" />
          <a-empty v-else :description="t('pages.rules.emptyDescription')">
            <a-button type="primary" data-testid="empty-create" @click="openAddModal">
              {{ t('pages.rules.addRule') }}
            </a-button>
          </a-empty>
        </template>
        </a-table>
      </div>
    </a-card>
  </div>

  <a-modal
    v-model:open="modalVisible"
    :title="
      isEdit
        ? t('pages.rules.editTitle', { index: editingIndex + 1 })
        : t('pages.rules.addTitle')
    "
    :confirm-loading="submitting"
    :ok-text="isEdit ? t('pages.rules.saveOk') : t('pages.rules.addOk')"
    :cancel-text="t('common.cancel')"
    width="640px"
    @ok="handleSubmit"
  >
    <a-form layout="vertical">
      <a-row :gutter="16">
        <a-col :xs="24" :sm="12">
          <a-form-item
            html-for="rule-type"
            :label="t('pages.rules.form.type')"
            required
            :validate-status="fieldErrors.type ? 'error' : undefined"
            :help="fieldErrors.type"
          >
            <a-select id="rule-type" v-model:value="form.type" :options="typeOptions" />
          </a-form-item>
        </a-col>
        <a-col :xs="24" :sm="12">
          <a-form-item :label="t('pages.rules.form.action')" required>
            <a-select v-model:value="form.action" :options="actionOptions" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-form-item
        v-if="needsValues"
        html-for="rule-values"
        :label="t('pages.rules.form.valuesLabel')"
        required
        :validate-status="fieldErrors.values ? 'error' : undefined"
        :help="fieldErrors.values"
      >
        <a-textarea
          id="rule-values"
          :value="valuesText"
          :rows="5"
          :placeholder="
            form.type === 'ip_cidr'
              ? t('pages.rules.form.cidrPlaceholder')
              : t('pages.rules.form.valuesPlaceholder')
          "
          @update:value="syncValues"
        />
        <template #extra>
          <span v-if="form.type === 'domain_suffix'">{{ t('pages.rules.form.hintDomainSuffix') }}</span>
          <span v-else-if="form.type === 'ip_cidr'">{{ t('pages.rules.form.hintIpCidr') }}</span>
          <span v-else-if="form.type === 'domain_regex'">{{ t('pages.rules.form.hintDomainRegex') }}</span>
          <span v-else-if="form.type === 'geosite'">{{ t('pages.rules.form.hintGeosite') }}</span>
          <span v-else>{{ t('pages.rules.form.hintDefault') }}</span>
        </template>
      </a-form-item>

      <a-form-item v-else :label="t('pages.rules.form.valuesNoneLabel')">
        <a-alert type="info" show-icon :message="t('pages.rules.form.valuesNoneHint')" />
      </a-form-item>

      <a-form-item
        html-for="rule-upstreams"
        v-if="needsUpstreams"
        :label="t('pages.rules.form.upstreamsLabel')"
      >
        <a-select id="rule-upstreams"
          v-model:value="form.upstreams"
          mode="multiple"
          :options="upstreamOptions"
          :placeholder="t('pages.rules.form.upstreamsPlaceholder')"
          allow-clear
        />
        <template #extra>
          {{ t('pages.rules.form.upstreamsHint') }}
        </template>
      </a-form-item>

      <a-form-item :label="t('pages.rules.form.statusLabel')">
        <a-space>
          <a-switch
            :checked="form.disabled !== true"
            @change="(checked) => (form.disabled = checked !== true)"
          />
          <span class="muted">
            {{ form.disabled ? t('pages.rules.form.statusDisabled') : t('pages.rules.form.statusEnabled') }}
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
