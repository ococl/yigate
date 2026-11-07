<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { apiGroups } from '@/data/api-docs'

const route = useRoute()
const router = useRouter()

const fallbackGroup = apiGroups[0]?.key ?? 'system'
const detailRef = ref<HTMLElement | null>(null)
const searchKeyword = ref('')

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

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text).then(() => {
    // 使用 antdv-next message 提示
    window.dispatchEvent(new CustomEvent('copy-success'))
  })
}
</script>

<template>
  <div class="api-docs">
    <!-- 页面标题 -->
    <div class="api-docs-title">
      <div style="display: flex; align-items: baseline; gap: 12px;">
        <a-typography-title :level="4" style="margin: 0;">API 文档</a-typography-title>
        <a-typography-text type="secondary">YiGate 管理接口完整说明</a-typography-text>
      </div>
      <div>
        <a-tag color="blue">{{ totalEndpoints }} 个端点</a-tag>
        <a-tag color="purple">{{ apiGroups.length }} 个分组</a-tag>
      </div>
    </div>

    <!-- 搜索框 -->
    <a-input-search
      v-model:value="searchKeyword"
      class="api-docs-search"
      placeholder="搜索端点路径、名称或描述..."
      allow-clear
    />

    <div class="api-docs-body">
      <!-- 左侧分组导航：固定宽度，保证分组名称完整显示 -->
      <aside class="api-docs-aside">
        <a-card size="small" class="group-card">
          <template #title>接口分组</template>
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
                :number-style="{ backgroundColor: '#1677ff' }"
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
                  {{ endpoint.summary }}
                </a-typography-title>
                <a-typography-paragraph type="secondary">
                  {{ endpoint.description }}
                </a-typography-paragraph>

                <!-- 认证标记 -->
                <a-space style="margin-bottom: 12px;">
                  <a-tag :color="endpoint.auth ? 'red' : 'green'">
                    {{ endpoint.auth ? '需要认证' : '无需认证' }}
                  </a-tag>
                  <a-typography-text type="secondary" v-if="endpoint.authNote">
                    {{ endpoint.authNote }}
                  </a-typography-text>
                </a-space>

                <!-- 路径参数 -->
                <template v-if="endpoint.params.length > 0">
                  <a-divider title-placement="start" plain style="margin: 8px 0;">
                    <a-typography-text type="secondary">路径参数</a-typography-text>
                  </a-divider>
                  <a-table
                    :data-source="endpoint.params"
                    :pagination="false"
                    size="small"
                    :columns="[
                      { title: '参数名', dataIndex: 'name', width: 120 },
                      { title: '类型', dataIndex: 'type', width: 140 },
                      { title: '必填', dataIndex: 'required', width: 60 },
                      { title: '说明', dataIndex: 'description' },
                    ]"
                    :row-key="(r: any) => r.name"
                  >
                    <template #bodyCell="{ column, record }">
                      <template v-if="column.dataIndex === 'required'">
                        {{ record.required ? '是' : '否' }}
                      </template>
                    </template>
                  </a-table>
                </template>

                <!-- 请求体 -->
                <template v-if="endpoint.requestBody">
                  <a-divider title-placement="start" plain style="margin: 8px 0;">
                    <a-typography-text type="secondary">请求体 (JSON)</a-typography-text>
                  </a-divider>
                  <div class="code-block">
                    <div class="code-block-header">
                      <span>Request Body</span>
                      <a-button type="text" size="small" @click="copyToClipboard(endpoint.requestBody!)">
                        复制
                      </a-button>
                    </div>
                    <pre><code>{{ endpoint.requestBody }}</code></pre>
                  </div>
                </template>

                <!-- 响应体 -->
                <a-divider title-placement="start" plain style="margin: 8px 0;">
                  <a-typography-text type="secondary">响应体 (JSON)</a-typography-text>
                </a-divider>
                <div class="code-block">
                  <div class="code-block-header">
                    <span>Response Body</span>
                    <a-button type="text" size="small" @click="copyToClipboard(endpoint.responseBody)">
                      复制
                    </a-button>
                  </div>
                  <pre><code>{{ endpoint.responseBody }}</code></pre>
                </div>

                <!-- 状态码 -->
                <a-divider title-placement="start" plain style="margin: 8px 0;">
                  <a-typography-text type="secondary">状态码</a-typography-text>
                </a-divider>
                <a-table
                  :data-source="endpoint.statusCodes"
                  :pagination="false"
                  size="small"
                  :columns="[
                    { title: '状态码', dataIndex: 'code', width: 80 },
                    { title: '说明', dataIndex: 'description' },
                  ]"
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
                  <a-typography-text type="secondary">请求示例</a-typography-text>
                </a-divider>
                <div class="code-block">
                  <div class="code-block-header">
                    <span>cURL</span>
                    <a-button
                      type="text"
                      size="small"
                      v-if="endpoint.example.request"
                      @click="copyToClipboard(endpoint.example.request)"
                    >
                      复制
                    </a-button>
                  </div>
                  <pre><code>{{ endpoint.example.request }}</code></pre>
                </div>

                <a-divider title-placement="start" plain style="margin: 8px 0;">
                  <a-typography-text type="secondary">响应示例</a-typography-text>
                </a-divider>
                <div class="code-block">
                  <div class="code-block-header">
                    <span>Response</span>
                    <a-button type="text" size="small" @click="copyToClipboard(endpoint.example.response)">
                      复制
                    </a-button>
                  </div>
                  <pre><code>{{ endpoint.example.response }}</code></pre>
                </div>
              </a-card>
            </div>
          </template>

          <!-- 无搜索结果 -->
          <a-empty
            v-if="filteredGroups.length === 0"
            description="未找到匹配的端点"
            style="margin-top: 60px;"
          />
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
/* 页面撑满工作区高度：标题/搜索固定，详情区域独立滚动 */
.api-docs {
  display: flex;
  flex-direction: column;
  height: 100%;
  max-width: 1200px;
}

.api-docs-title {
  flex: 0 0 auto;
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0 0 16px 0;
}

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

.code-block {
  background: var(--ant-color-fill-quaternary, #f5f5f5);
  border: 1px solid var(--ant-color-border-secondary, #e8e8e8);
  border-radius: 6px;
  overflow: hidden;
  margin: 8px 0;
}

.code-block-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 4px 12px;
  background: var(--ant-color-fill-tertiary, #fafafa);
  border-bottom: 1px solid var(--ant-color-border-secondary, #e8e8e8);
  font-size: 12px;
  color: var(--ant-color-text-secondary, #666);
}

.code-block pre {
  margin: 0;
  padding: 12px 16px;
  overflow-x: auto;
  font-size: 13px;
  line-height: 1.6;
}

.code-block code {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
}
</style>
