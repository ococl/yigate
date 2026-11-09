<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { message } from 'antdv-next'
import { getSettings, updateSettings } from '@/api/settings'

interface PprofLink {
  label: string
  path: string
  description: string
}

const { t } = useI18n()

// 端点与分组走翻译，切换语言后标题与说明同步更新
const PPROF_GROUPS = [
  { title: 'overview', items: ['index'] },
  { title: 'cpu', items: ['profile', 'trace'] },
  { title: 'memory', items: ['heap', 'allocs'] },
  { title: 'concurrency', items: ['goroutine', 'block', 'mutex'] },
  { title: 'runtime', items: ['threadcreate'] },
] as const

const PPROF_PATHS: Record<string, string> = {
  index: '/debug/pprof/',
  profile: '/debug/pprof/profile?seconds=30',
  trace: '/debug/pprof/trace?seconds=5',
  heap: '/debug/pprof/heap',
  allocs: '/debug/pprof/allocs',
  goroutine: '/debug/pprof/goroutine',
  block: '/debug/pprof/block',
  mutex: '/debug/pprof/mutex',
  threadcreate: '/debug/pprof/threadcreate',
}

const pprofGroups = computed<{ title: string; items: PprofLink[] }[]>(() =>
  PPROF_GROUPS.map((group) => ({
    title: t(`pages.pprof.groups.${group.title}`),
    items: group.items.map((key) => ({
      label: t(`pages.pprof.endpoints.${key}.label`),
      path: PPROF_PATHS[key],
      description: t(`pages.pprof.endpoints.${key}.description`),
    })),
  })),
)

const loaded = ref(false)
const saving = ref(false)
// 开关值来自服务端；读取失败时开关保持禁用，并明确告知原因
const loadError = ref(false)
// 当前生效值来自服务端，表单值保存成功后同步
const enabled = ref(false)

async function fetchSettings() {
  try {
    const settings = await getSettings()
    enabled.value = settings.enable_pprof
    loaded.value = true
    loadError.value = false
  } catch {
    loadError.value = true
  }
}

async function handleToggle(checked: boolean) {
  const previous = enabled.value
  enabled.value = checked
  saving.value = true
  try {
    await updateSettings({ enable_pprof: checked })
    message.success(t(checked ? 'pages.pprof.messages.enabled' : 'pages.pprof.messages.disabled'))
  } catch {
    enabled.value = previous
  } finally {
    saving.value = false
  }
}

onMounted(fetchSettings)
</script>

<template>
  <div class="page-shell pprof-view">
    <div class="page-header">
      <div>
        <a-typography-title :level="4" style="margin: 0;">{{ t('pages.pprof.title') }}</a-typography-title>
        <a-typography-text type="secondary">
          {{ t('pages.pprof.subtitle') }}
        </a-typography-text>
      </div>
      <a-tag :color="enabled ? 'green' : 'default'">
        {{ enabled ? t('pages.pprof.enabledTag') : t('pages.pprof.disabledTag') }}
      </a-tag>
    </div>

    <div class="pprof-body">
      <!-- 左侧：开关、安全提示与使用方式 -->
      <aside class="pprof-aside">
        <a-card size="small" :title="t('pages.pprof.aside.cardTitle')" class="aside-card">
          <a-form layout="vertical">
            <a-form-item
              :label="t('pages.pprof.aside.switchLabel')"
              :tooltip="t('pages.pprof.aside.switchTooltip')"
            >
              <div class="switch-row">
                <a-switch
                  :checked="enabled"
                  :loading="saving"
                  :disabled="!loaded"
                  :checked-children="t('pages.pprof.aside.switchOn')"
                  :un-checked-children="t('pages.pprof.aside.switchOff')"
                  @change="(value: boolean) => handleToggle(value)"
                />
                <a-typography-text v-if="loadError" type="danger" class="switch-hint">
                  {{ t('pages.pprof.aside.loadError') }}
                  <a-button type="link" size="small" style="padding: 0 4px;" @click="fetchSettings">
                    {{ t('common.retry') }}
                  </a-button>
                </a-typography-text>
                <a-typography-text v-else type="secondary" class="switch-hint">
                  {{ t('pages.pprof.aside.saveHint') }}
                </a-typography-text>
              </div>
            </a-form-item>
          </a-form>

          <a-alert
            type="warning"
            show-icon
            :message="t('pages.pprof.aside.warningTitle')"
            :description="t('pages.pprof.aside.warningDescription')"
          />

          <div class="usage">
            <a-typography-text strong>{{ t('pages.pprof.aside.usageTitle') }}</a-typography-text>
            <a-typography-paragraph type="secondary" class="usage-desc">
              {{ t('pages.pprof.aside.usageDesc') }}
              <a-typography-text code>go tool pprof</a-typography-text>
              {{ t('pages.pprof.aside.usageDescTail') }}
            </a-typography-paragraph>
            <div class="code-block">
              <div class="code-block-header">
                <span>{{ t('pages.pprof.aside.cpuTitle') }}</span>
              </div>
              <pre><code>curl -o cpu.pprof "http://127.0.0.1:8890/debug/pprof/profile?seconds=30"</code></pre>
            </div>
            <div class="code-block">
              <div class="code-block-header">
                <span>{{ t('pages.pprof.aside.analyzeTitle') }}</span>
              </div>
              <pre><code>go tool pprof -http=:8081 cpu.pprof</code></pre>
            </div>
            <a-typography-paragraph type="secondary" class="usage-desc">
              {{ t('pages.pprof.aside.tokenDesc') }}
              <a-typography-text code>Authorization: Bearer &lt;token&gt;</a-typography-text>{{ t('pages.pprof.aside.tokenDescTail') }}
            </a-typography-paragraph>
          </div>
        </a-card>
      </aside>

      <!-- 右侧：按类别列出全部端点 -->
      <section class="pprof-detail">
        <div class="pprof-detail-scroll">
          <a-alert
            v-if="loaded && !enabled"
            type="info"
            show-icon
            style="margin-bottom: 16px;"
            :message="t('pages.pprof.notEnabled.title')"
            :description="t('pages.pprof.notEnabled.description')"
          />
          <a-card
            v-for="group in pprofGroups"
            :key="group.title"
            size="small"
            :title="group.title"
            class="group-card"
          >
            <div class="endpoint-list">
              <div v-for="item in group.items" :key="item.path" class="endpoint-item">
                <div class="endpoint-main">
                  <a :href="item.path" target="_blank" rel="noopener noreferrer">{{ item.label }}</a>
                  <a-typography-text type="secondary" class="endpoint-desc">
                    {{ item.description }}
                  </a-typography-text>
                </div>
                <div class="endpoint-actions">
                  <a-typography-text code class="endpoint-path">{{ item.path }}</a-typography-text>
                  <a :href="item.path" target="_blank" rel="noopener noreferrer">{{ t('pages.pprof.open') }}</a>
                </div>
              </div>
            </div>
          </a-card>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.pprof-view {
  width: 100%;
}

.pprof-body {
  flex: 1 1 auto;
  display: flex;
  gap: 16px;
  min-height: 0;
}

/* 左侧固定宽度，内容超出时卡片内部滚动 */
.pprof-aside {
  flex: 0 0 360px;
  width: 360px;
  min-width: 360px;
  height: 100%;
  min-height: 0;
}

.aside-card {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.aside-card :deep(.ant-card-body) {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
}

.switch-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.switch-hint {
  font-size: 12px;
}

.usage {
  margin-top: 16px;
}

.usage-desc {
  font-size: 12px;
  margin: 8px 0;
}

.code-block {
  margin-bottom: 12px;
  border-radius: 6px;
  overflow: hidden;
  background: rgba(127, 127, 127, 0.08);
}

.code-block-header {
  padding: 4px 12px;
  font-size: 12px;
  color: rgba(128, 128, 128, 0.9);
  border-bottom: 1px solid rgba(128, 128, 128, 0.18);
}

.code-block pre {
  margin: 0;
  padding: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.6;
  overflow-x: auto;
}

.pprof-detail {
  flex: 1 1 auto;
  min-width: 0;
  height: 100%;
  min-height: 0;
}

.pprof-detail-scroll {
  height: 100%;
  overflow-x: hidden;
  overflow-y: auto;
  padding-right: 4px;
}

.group-card {
  margin-bottom: 16px;
}

.endpoint-list {
  display: flex;
  flex-direction: column;
}

.endpoint-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 0;
}

.endpoint-item + .endpoint-item {
  border-top: 1px dashed rgba(128, 128, 128, 0.18);
}

.endpoint-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.endpoint-desc {
  font-size: 12px;
}

.endpoint-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 0 0 auto;
}

.endpoint-path {
  font-size: 12px;
}

@media (max-width: 992px) {
  .pprof-body {
    flex-direction: column;
  }

  .pprof-aside {
    flex: 0 0 auto;
    width: 100%;
    min-width: 0;
    height: auto;
  }

  .aside-card :deep(.ant-card-body) {
    overflow-y: visible;
  }

  .pprof-detail {
    height: auto;
  }

  .pprof-detail-scroll {
    height: auto;
    overflow-y: visible;
  }
}
</style>
