<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { authInfo } from '@/data/api-docs'

interface DocSection {
  key: string
  title: string
}

const sections: DocSection[] = [
  { key: 'overview', title: '认证概览' },
  { key: 'security', title: '安全防护' },
  { key: 'usage', title: '使用示例' },
  { key: 'config', title: '配置说明' },
]

const route = useRoute()
const router = useRouter()

const fallbackSection = sections[0]?.key ?? 'overview'
const detailRef = ref<HTMLElement | null>(null)

function isSectionKey(value: unknown): value is string {
  return typeof value === 'string' && sections.some((section) => section.key === value)
}

// 从路由 query 中解析当前章节，非法或缺失时回退到首个章节
function resolveSectionFromRoute(): string {
  const querySection = route.query.section
  const key = Array.isArray(querySection) ? querySection[0] : querySection
  return isSectionKey(key) ? key : fallbackSection
}

const activeSection = ref<string>(resolveSectionFromRoute())

// 章节与路由绑定：切换章节写入 ?section=xxx，刷新后可正确定位
function selectSection(key: string) {
  if (!isSectionKey(key)) return
  activeSection.value = key
  if (route.query.section === key) return
  router.replace({ query: { ...route.query, section: key } })
}

// 支持直接修改地址栏或浏览器前进/后退
watch(
  () => route.query.section,
  (value) => {
    const key = Array.isArray(value) ? value[0] : value
    if (isSectionKey(key)) {
      activeSection.value = key
      return
    }
    activeSection.value = fallbackSection
    if (key !== undefined) {
      const query = { ...route.query }
      delete query.section
      router.replace({ query })
    }
  },
  { immediate: true },
)

// 切换章节后右侧内容回到顶部
watch(activeSection, () => {
  detailRef.value?.scrollTo({ top: 0 })
})
</script>

<template>
  <div class="auth-doc">
    <div class="auth-doc-title">
      <a-typography-title :level="4" style="margin: 0;">认证机制</a-typography-title>
      <a-typography-text type="secondary">YiGate 管理接口认证说明</a-typography-text>
    </div>

    <div class="auth-doc-body">
      <!-- 左侧章节菜单：固定宽度，保证标题完整显示 -->
      <aside class="auth-doc-aside">
        <a-card size="small" class="section-card">
          <template #title>文档目录</template>
          <a-menu :selected-keys="[activeSection]" mode="inline" style="border: none;">
            <a-menu-item
              v-for="section in sections"
              :key="section.key"
              @click="selectSection(section.key)"
            >
              {{ section.title }}
            </a-menu-item>
          </a-menu>
        </a-card>
      </aside>

      <!-- 右侧章节内容：独立滚动 -->
      <section class="auth-doc-detail">
        <div ref="detailRef" class="auth-doc-detail-scroll">
          <!-- 认证概览 -->
          <a-card v-show="activeSection === 'overview'" title="认证概览" size="small">
            <a-descriptions :column="1" size="small" bordered>
              <a-descriptions-item label="认证方式">
                <a-tag color="blue">{{ authInfo.method }}</a-tag>
              </a-descriptions-item>
              <a-descriptions-item label="请求头">
                <a-typography-text code copyable>{{ authInfo.header }}</a-typography-text>
              </a-descriptions-item>
              <a-descriptions-item label="配置字段">
                <a-typography-text code>{{ authInfo.configField }}</a-typography-text>
              </a-descriptions-item>
              <a-descriptions-item label="说明">{{ authInfo.description }}</a-descriptions-item>
            </a-descriptions>
          </a-card>

          <!-- 安全防护 -->
          <a-card v-show="activeSection === 'security'" title="安全防护" size="small">
            <a-descriptions :column="1" size="small" bordered>
              <a-descriptions-item label="暴力破解防护">
                <a-tag color="orange">{{ authInfo.bruteForceProtection }}</a-tag>
              </a-descriptions-item>
              <a-descriptions-item label="未认证响应状态码">
                <a-tag color="red">HTTP {{ authInfo.unauthorizedResponse.status }}</a-tag>
              </a-descriptions-item>
              <a-descriptions-item label="WWW-Authenticate 响应头">
                <a-typography-text code copyable>
                  {{ authInfo.unauthorizedResponse.headers['WWW-Authenticate'] }}
                </a-typography-text>
              </a-descriptions-item>
              <a-descriptions-item label="未认证响应体">
                <div class="code-block">
                  <pre><code>{{ authInfo.unauthorizedResponse.body }}</code></pre>
                </div>
              </a-descriptions-item>
            </a-descriptions>
          </a-card>

          <!-- 使用示例 -->
          <a-card v-show="activeSection === 'usage'" title="使用示例" size="small">
            <a-typography-title :level="5">携带认证令牌的请求</a-typography-title>
            <div class="code-block">
              <div class="code-block-header">
                <span>cURL</span>
              </div>
              <pre><code>curl -H "Authorization: Bearer &lt;your-token&gt;" \
  http://127.0.0.1:8890/api/v1/status</code></pre>
            </div>

            <a-typography-title :level="5">未携带令牌的请求（token 非空时）</a-typography-title>
            <div class="code-block">
              <div class="code-block-header">
                <span>cURL</span>
              </div>
              <pre><code>curl http://127.0.0.1:8890/api/v1/status

# 响应:
# HTTP/1.1 401 Unauthorized
# WWW-Authenticate: Bearer realm="yigate-management"
# {"status":"unauthorized"}</code></pre>
            </div>
          </a-card>

          <!-- 配置说明 -->
          <a-card v-show="activeSection === 'config'" title="配置说明" size="small">
            <a-alert type="warning" show-icon style="margin-bottom: 12px;">
              <template #message>强制认证场景</template>
              <template #description>
                管理端口绑定非回环地址时，<a-typography-text code>management_auth_token</a-typography-text>
                必须配置。启用 pprof 且管理端口非回环时同样必填。
              </template>
            </a-alert>

            <a-typography-title :level="5">YAML 配置示例</a-typography-title>
            <div class="code-block">
              <div class="code-block-header">
                <span>config.yml</span>
              </div>
              <pre><code>management: 127.0.0.1:8890
management_auth_token: "${YIGATE_MANAGEMENT_TOKEN}"</code></pre>
            </div>

            <a-typography-paragraph type="secondary" style="margin-top: 12px;">
              配置中的 <a-typography-text code>${YIGATE_MANAGEMENT_TOKEN}</a-typography-text>
              会被环境变量展开。环境变量前缀为 <a-typography-text code>YIGATE_</a-typography-text>。
              缺失或空值会导致启动失败。
            </a-typography-paragraph>

            <a-typography-title :level="5">令牌轮换</a-typography-title>
            <a-typography-paragraph type="secondary">
              令牌可在「系统设置 → 管理面板与安全」中修改，或通过
              <a-typography-text code>PUT /api/v1/config/settings</a-typography-text> 传入
              <a-typography-text code>management_auth_token</a-typography-text>。修改后立即生效且无需重启，
              旧令牌当场失效，当前会话会被强制重新登录。清除令牌（传空字符串）仅在管理端口绑定回环地址时允许。
            </a-typography-paragraph>
          </a-card>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
/* 页面撑满工作区高度：标题固定，右侧内容独立滚动 */
.auth-doc {
  display: flex;
  flex-direction: column;
  height: 100%;
  width: 100%;
}

.auth-doc-title {
  flex: 0 0 auto;
  display: flex;
  align-items: baseline;
  gap: 12px;
  padding: 0 0 16px 0;
}

.auth-doc-body {
  display: flex;
  flex: 1 1 auto;
  gap: 16px;
  min-height: 0;
}

/* 左侧目录固定宽度，避免菜单文字被截断 */
.auth-doc-aside {
  flex: 0 0 220px;
  width: 220px;
  min-width: 220px;
  height: 100%;
  min-height: 0;
}

.auth-doc-detail {
  flex: 1 1 auto;
  min-width: 0;
  height: 100%;
  min-height: 0;
}

/* 左侧目录卡片固定，章节过多时卡片内部滚动 */
.section-card {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.section-card :deep(.ant-card-body) {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
}

.auth-doc-detail-scroll {
  height: 100%;
  overflow-x: hidden;
  overflow-y: auto;
  overscroll-behavior: contain;
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
