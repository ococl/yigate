<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { authInfo } from '@/data/api-docs'

interface DocSection {
  key: string
  title: string
}

const { t } = useI18n()

// 章节标题走翻译，切换语言后目录与卡片标题同步更新
const SECTION_KEYS = ['overview', 'security', 'usage', 'config'] as const

const sections = computed<DocSection[]>(() =>
  SECTION_KEYS.map((key) => ({ key, title: t(`pages.authDoc.sections.${key}`) })),
)

const route = useRoute()
const router = useRouter()

const fallbackSection = SECTION_KEYS[0]
const detailRef = ref<HTMLElement | null>(null)

function isSectionKey(value: unknown): value is string {
  return typeof value === 'string' && sections.value.some((section) => section.key === value)
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
  <div class="page-shell" data-testid="page-auth-doc">
    <div class="page-header">
      <div class="page-header-text">
        <a-typography-title :level="4" style="margin: 0;">{{ t('pages.authDoc.title') }}</a-typography-title>
        <a-typography-text type="secondary">{{ t('pages.authDoc.subtitle') }}</a-typography-text>
      </div>
    </div>

    <div class="auth-doc-body">
      <!-- 左侧章节菜单：固定宽度，保证标题完整显示 -->
      <aside class="auth-doc-aside">
        <a-card size="small" class="section-card">
          <template #title>{{ t('pages.authDoc.navTitle') }}</template>
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
          <a-card v-show="activeSection === 'overview'" :title="t('pages.authDoc.sections.overview')" size="small">
            <a-descriptions :column="1" size="small" bordered>
              <a-descriptions-item :label="t('pages.authDoc.overview.method')">
                <a-tag color="blue">{{ authInfo.method }}</a-tag>
              </a-descriptions-item>
              <a-descriptions-item :label="t('pages.authDoc.overview.header')">
                <a-typography-text code copyable>{{ authInfo.header }}</a-typography-text>
              </a-descriptions-item>
              <a-descriptions-item :label="t('pages.authDoc.overview.configField')">
                <a-typography-text code>{{ authInfo.configField }}</a-typography-text>
              </a-descriptions-item>
              <a-descriptions-item :label="t('pages.authDoc.overview.description')">{{ authInfo.description }}</a-descriptions-item>
            </a-descriptions>
          </a-card>

          <!-- 安全防护 -->
          <a-card v-show="activeSection === 'security'" :title="t('pages.authDoc.sections.security')" size="small">
            <a-descriptions :column="1" size="small" bordered>
              <a-descriptions-item :label="t('pages.authDoc.security.bruteForce')">
                <a-tag color="orange">{{ authInfo.bruteForceProtection }}</a-tag>
              </a-descriptions-item>
              <a-descriptions-item :label="t('pages.authDoc.security.unauthorizedStatus')">
                <a-tag color="red">HTTP {{ authInfo.unauthorizedResponse.status }}</a-tag>
              </a-descriptions-item>
              <a-descriptions-item :label="t('pages.authDoc.security.authenticateHeader')">
                <a-typography-text code copyable>
                  {{ authInfo.unauthorizedResponse.headers['WWW-Authenticate'] }}
                </a-typography-text>
              </a-descriptions-item>
              <a-descriptions-item :label="t('pages.authDoc.security.unauthorizedBody')">
                <CodeBlock :code="authInfo.unauthorizedResponse.body" />
              </a-descriptions-item>
            </a-descriptions>
          </a-card>

          <!-- 使用示例 -->
          <a-card v-show="activeSection === 'usage'" :title="t('pages.authDoc.sections.usage')" size="small">
            <a-typography-title :level="5">{{ t('pages.authDoc.usage.withToken') }}</a-typography-title>
            <CodeBlock title="cURL">curl -H "Authorization: Bearer &lt;your-token&gt;" \
  http://127.0.0.1:8890/api/v1/status</CodeBlock>

            <a-typography-title :level="5">{{ t('pages.authDoc.usage.withoutToken') }}</a-typography-title>
            <CodeBlock title="cURL">curl http://127.0.0.1:8890/api/v1/status

{{ t('pages.authDoc.usage.responseComment') }}
# HTTP/1.1 401 Unauthorized
# WWW-Authenticate: Bearer realm="yigate-management"
# {"status":"unauthorized"}</CodeBlock>
          </a-card>

          <!-- 配置说明 -->
          <a-card v-show="activeSection === 'config'" :title="t('pages.authDoc.sections.config')" size="small">
            <a-alert type="warning" show-icon style="margin-bottom: 12px;">
              <template #message>{{ t('pages.authDoc.config.forceTitle') }}</template>
              <template #description>
                {{ t('pages.authDoc.config.forceBefore') }}<a-typography-text code>management_auth_token</a-typography-text>{{
                  t('pages.authDoc.config.forceMid')
                }}
              </template>
            </a-alert>

            <a-typography-title :level="5">{{ t('pages.authDoc.config.yamlTitle') }}</a-typography-title>
            <CodeBlock title="config.yml">management: 127.0.0.1:8890
management_auth_token: "${YIGATE_MANAGEMENT_TOKEN}"</CodeBlock>

            <a-typography-paragraph type="secondary" style="margin-top: 12px;">
              {{ t('pages.authDoc.config.envBefore') }}<a-typography-text code>${YIGATE_MANAGEMENT_TOKEN}</a-typography-text>{{
                t('pages.authDoc.config.envMid')
              }}<a-typography-text code>YIGATE_</a-typography-text>{{ t('pages.authDoc.config.envAfter') }}
            </a-typography-paragraph>

            <a-typography-title :level="5">{{ t('pages.authDoc.config.rotateTitle') }}</a-typography-title>
            <a-typography-paragraph type="secondary">
              {{ t('pages.authDoc.config.rotateBefore') }}<a-typography-text code>PUT /api/v1/config/settings</a-typography-text>{{
                t('pages.authDoc.config.rotateMid')
              }}<a-typography-text code>management_auth_token</a-typography-text>{{ t('pages.authDoc.config.rotateAfter') }}
            </a-typography-paragraph>
          </a-card>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
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

@media (max-width: 768px) {
  .auth-doc-body {
    flex-direction: column;
    gap: 12px;
  }

  .auth-doc-aside {
    flex: 0 0 auto;
    width: 100%;
    min-width: 0;
    height: auto;
    max-height: 168px;
  }

  .section-card {
    height: auto;
    max-height: 168px;
  }

  .auth-doc-detail {
    height: auto;
  }

  .auth-doc-detail-scroll {
    height: auto;
    overflow-y: visible;
  }
}


.auth-doc-detail-scroll {
  height: 100%;
  overflow-x: hidden;
  overflow-y: auto;
  overscroll-behavior: contain;
}

</style>
