<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { apiGroups } from '@/data/api-docs'
import type { InputRef } from 'antdv-next'
import { useHotkeys } from '@/composables/useHotkeys'
import { highlightText } from '@/utils/highlight'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const fallbackGroup = apiGroups[0]?.key ?? 'system'
const detailRef = ref<HTMLElement | null>(null)
const searchKeyword = ref('')
const searchInputRef = ref<InputRef | null>(null)

// 按 `/` 直接跳到搜索框
useHotkeys([{ key: '/', handler: () => searchInputRef.value?.focus() }])

function isGroupKey(value: unknown): value is string {
  return typeof value === 'string' && apiGroups.some((group) => group.key === value)
}

// 从路由 query 中解析当前分组，非法或缺失时回退到首个分组
function resolveGroupFromRoute(): string {
  const queryGroup = route.query.group
  const key = Array.isArray(queryGroup) ? queryGroup[0] : queryGroup
  return isGroupKey(key) ? key : fallbackGroup
}

const activeGroup = ref<string>(resolveGroupFromRoute())

// 分组与路由绑定：切换分组写入 ?group=xxx，刷新后可正确定位
function selectGroup(key: string) {
  if (!isGroupKey(key)) return
  activeGroup.value = key
  if (route.query.group === key) return
  router.replace({ query: { ...route.query, group: key } })
}

// 支持直接修改地址栏或浏览器前进/后退
watch(
  () => route.query.group,
  (value) => {
    const key = Array.isArray(value) ? value[0] : value
    if (isGroupKey(key)) {
      activeGroup.value = key
      return
    }
    activeGroup.value = fallbackGroup
    if (key !== undefined) {
      const query = { ...route.query }
      delete query.group
      router.replace({ query })
    }
  },
  { immediate: true },
)

// 分组切换后详情区域回到顶部
watch(activeGroup, () => {
  detailRef.value?.scrollTo({ top: 0 })
})

const methodColor: Record<string, string> = {
  GET: 'green',
  POST: 'blue',
  PUT: 'orange',
  DELETE: 'red',
}

function clearSearch() {
  searchKeyword.value = ''
}

// 结果里把命中的关键词标出来，否则一屏十几条描述，看不出为什么这条被匹配
function highlight(text: string): string {
  return highlightText(text, searchKeyword.value)
}

const filteredGroups = computed(() => {
  if (!searchKeyword.value.trim()) return apiGroups
  const kw = searchKeyword.value.toLowerCase()
  return apiGroups
    .map((group) => ({
      ...group,
      endpoints: group.endpoints.filter(
        (ep) =>
          ep.path.toLowerCase().includes(kw) ||
          ep.summary.toLowerCase().includes(kw) ||
          ep.description.toLowerCase().includes(kw),
      ),
    }))
    .filter((group) => group.endpoints.length > 0)
})

// 搜索过滤后若当前分组不再可见，自动定位到首个匹配分组
watch(filteredGroups, (groups) => {
  if (groups.some((group) => group.key === activeGroup.value)) return
  const first = groups[0]
  if (first) selectGroup(first.key)
})

const totalEndpoints = computed(() =>
  apiGroups.reduce((sum, g) => sum + g.endpoints.length, 0),
)

const paramColumns = computed(() => [
  { title: t('pages.apiDoc.paramColumns.name'), dataIndex: 'name', width: 120 },
  { title: t('pages.apiDoc.paramColumns.type'), dataIndex: 'type', width: 140 },
  { title: t('pages.apiDoc.paramColumns.required'), dataIndex: 'required', width: 60 },
  { title: t('pages.apiDoc.paramColumns.description'), dataIndex: 'description' },
])

const statusColumns = computed(() => [
  { title: t('pages.apiDoc.statusColumns.code'), dataIndex: 'code', width: 80 },
  { title: t('pages.apiDoc.statusColumns.description'), dataIndex: 'description' },
])

</script>

<template>
  <div class="page-shell">
    <!-- 页面标题 -->
    <div class="page-header">
      <div class="page-header-text">
        <a-typography-title :level="4" style="margin: 0;">{{ t('pages.apiDoc.title') }}</a-typography-title>
        <a-typography-text type="secondary">{{ t('pages.apiDoc.subtitle') }}</a-typography-text>
      </div>
      <div>
        <a-tag color="blue">{{ t('pages.apiDoc.endpointCount', { count: totalEndpoints }) }}</a-tag>
        <a-tag color="purple">{{ t('pages.apiDoc.groupCount', { count: apiGroups.length }) }}</a-tag>
      </div>
    </div>

    <!-- 搜索框 -->
    <a-input
      ref="searchInputRef"
      v-model:value="searchKeyword"
      class="api-docs-search"
      :placeholder="t('pages.apiDoc.searchPlaceholder')"
      allow-clear
      :aria-label="t('pages.apiDoc.searchAria')"
    />

    <div class="api-docs-body">
      <!-- 左侧分组导航：固定宽度，保证分组名称完整显示 -->
      <aside class="api-docs-aside">
        <a-card size="small" class="group-card">
          <template #title>{{ t('pages.apiDoc.groupsTitle') }}</template>
          <a-menu
            :selected-keys="[activeGroup]"
            mode="inline"
            style="border: none;"
          >
            <a-menu-item
              v-for="group in filteredGroups"
              :key="group.key"
              @click="selectGroup(group.key)"
            >
              <a-badge
                :count="group.endpoints.length"
                size="small"
                color="blue"
                :offset="[10, 0]"
              >
                {{ group.title }}
              </a-badge>
            </a-menu-item>
          </a-menu>
        </a-card>
      </aside>

      <!-- 右侧端点详情：独立滚动 -->
      <section class="api-docs-detail">
        <div ref="detailRef" class="api-docs-detail-scroll">
          <template v-for="group in filteredGroups" :key="group.key">
            <div v-show="activeGroup === group.key">
              <!-- 分组描述 -->
              <a-alert
                :message="group.title"
                :description="group.description"
                type="info"
                show-icon
                style="margin-bottom: 16px;"
              />

              <!-- 端点列表 -->
              <a-card
                v-for="endpoint in group.endpoints"
                :key="endpoint.id"
                size="small"
                style="margin-bottom: 16px;"
              >
                <!-- 端点标题行 -->
                <template #title>
                  <a-space>
                    <a-tag :color="methodColor[endpoint.method]" style="min-width: 56px; text-align: center; font-weight: bold;">
                      {{ endpoint.method }}
                    </a-tag>
                    <a-typography-text code copyable style="font-size: 14px;">
                      {{ endpoint.path }}
                    </a-typography-text>
                  </a-space>
                </template>

                <!-- 端点摘要 -->
                <a-typography-title :level="5" style="margin-top: 0;">
                  <!-- eslint-disable-next-line vue/no-v-html -- highlight() 已做 HTML 转义 -->
                  <span v-html="highlight(endpoint.summary)" />
                </a-typography-title>
                <a-typography-paragraph type="secondary">
                  <!-- eslint-disable-next-line vue/no-v-html -- highlight() 已做 HTML 转义 -->
                  <span v-html="highlight(endpoint.description)" />
                </a-typography-paragraph>

                <!-- 认证标记 -->
                <a-space style="margin-bottom: 12px;">
                  <a-tag :color="endpoint.auth ? 'red' : 'green'">
                    {{ endpoint.auth ? t('pages.apiDoc.authRequired') : t('pages.apiDoc.authNotRequired') }}
                  </a-tag>
                  <a-typography-text type="secondary" v-if="endpoint.authNote">
                    {{ endpoint.authNote }}
                  </a-typography-text>
                </a-space>

                <!-- 路径参数 -->
                <template v-if="endpoint.params.length > 0">
                  <a-divider title-placement="start" plain style="margin: 8px 0;">
                    <a-typography-text type="secondary">{{ t('pages.apiDoc.paramsTitle') }}</a-typography-text>
                  </a-divider>
                  <a-table
                    :data-source="endpoint.params"
                    :pagination="false"
                    size="small"
                    :columns="paramColumns"
                    :row-key="(r: any) => r.name"
                  >
                    <template #bodyCell="{ column, record }">
                      <template v-if="column.dataIndex === 'required'">
                        {{ record.required ? t('common.yes') : t('common.no') }}
                      </template>
                    </template>
                  </a-table>
                </template>

                <!-- 请求体 -->
                <template v-if="endpoint.requestBody">
                  <a-divider title-placement="start" plain style="margin: 8px 0;">
                    <a-typography-text type="secondary">{{ t('pages.apiDoc.requestBody') }}</a-typography-text>
                  </a-divider>
                  <CodeBlock title="Request Body" :code="endpoint.requestBody!" copyable />
                </template>

                <!-- 响应体 -->
                <a-divider title-placement="start" plain style="margin: 8px 0;">
                  <a-typography-text type="secondary">{{ t('pages.apiDoc.responseBody') }}</a-typography-text>
                </a-divider>
                <CodeBlock title="Response Body" :code="endpoint.responseBody" copyable />

                <!-- 状态码 -->
                <a-divider title-placement="start" plain style="margin: 8px 0;">
                  <a-typography-text type="secondary">{{ t('pages.apiDoc.statusCodes') }}</a-typography-text>
                </a-divider>
                <a-table
                  :data-source="endpoint.statusCodes"
                  :pagination="false"
                  size="small"
                  :columns="statusColumns"
                  :row-key="(r: any) => r.code"
                >
                  <template #bodyCell="{ column, record }">
                    <template v-if="column.dataIndex === 'code'">
                      <a-tag
                        :color="record.code < 300 ? 'green' : record.code < 500 ? 'orange' : 'red'"
                      >
                        {{ record.code }}
                      </a-tag>
                    </template>
                  </template>
                </a-table>

                <!-- 示例 -->
                <a-divider title-placement="start" plain style="margin: 8px 0;">
                  <a-typography-text type="secondary">{{ t('pages.apiDoc.requestExample') }}</a-typography-text>
                </a-divider>
                <CodeBlock
                  title="cURL"
                  :code="endpoint.example.request || ''"
                  :copyable="!!endpoint.example.request"
                />

                <a-divider title-placement="start" plain style="margin: 8px 0;">
                  <a-typography-text type="secondary">{{ t('pages.apiDoc.responseExample') }}</a-typography-text>
                </a-divider>
                <CodeBlock title="Response" :code="endpoint.example.response" copyable />
              </a-card>
            </div>
          </template>

          <!-- 无搜索结果 -->
          <a-empty
            v-if="filteredGroups.length === 0"
            :description="
              searchKeyword
                ? t('pages.apiDoc.emptyWithKeyword', { keyword: searchKeyword })
                : t('pages.apiDoc.empty')
            "
            style="margin-top: 60px;"
          >
            <a-button v-if="searchKeyword" size="small" @click="clearSearch">
              {{ t('pages.apiDoc.clearSearch') }}
            </a-button>
          </a-empty>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.api-docs-search {
  flex: 0 0 auto;
  margin-bottom: 16px;
}

.api-docs-body {
  display: flex;
  flex: 1 1 auto;
  gap: 16px;
  min-height: 0;
}

/* 左侧分组固定宽度，避免分组名称被截断 */
.api-docs-aside {
  flex: 0 0 220px;
  width: 220px;
  min-width: 220px;
  height: 100%;
  min-height: 0;
}

.api-docs-detail {
  flex: 1 1 auto;
  min-width: 0;
  height: 100%;
  min-height: 0;
}

.api-docs-detail-scroll {
  height: 100%;
  overflow-x: hidden;
  overflow-y: auto;
  overscroll-behavior: contain;
}

/* 左侧分组卡片固定，分组过多时卡片内部滚动 */
.group-card {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.group-card :deep(.ant-card-body) {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
}

.api-docs-detail mark {
  padding: 0 2px;
  border-radius: 2px;
  background: #fff1b8;
  color: inherit;
}

body.app-dark .api-docs-detail mark {
  background: #5c4813;
  color: #fff;
}

@media (max-width: 768px) {
  .api-docs-search {
    margin-bottom: 12px;
  }

  .api-docs-body {
    flex-direction: column;
    gap: 12px;
  }

  .api-docs-aside {
    flex: 0 0 auto;
    width: 100%;
    min-width: 0;
    height: auto;
    max-height: 168px;
  }

  .group-card {
    height: auto;
    max-height: 168px;
  }

  .api-docs-detail {
    height: auto;
  }

  .api-docs-detail-scroll {
    height: auto;
    overflow-y: visible;
  }
}
</style>
