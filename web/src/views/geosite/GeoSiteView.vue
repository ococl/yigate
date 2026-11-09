<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { message } from 'antdv-next'
import axios from 'axios'
import { generateGeoSiteSyncToken, getGeoSiteInfo, getGeoSiteSyncToken, updateGeoSite } from '@/api/geosite'
import { updateSettings } from '@/api/settings'
import LoadError from '@/components/LoadError.vue'
import { copyText } from '@/utils/clipboard'
import { formatDateTime } from '@/utils/datetime'
import { formatBytes } from '@/utils/format'
import type { GeoSiteInfo, SettingsPatch } from '@/types/api'

const { t } = useI18n()

const info = ref<GeoSiteInfo | null>(null)
// unsupported 标记当前后端没有 GeoSite 管理接口（通常是服务端版本较旧）。
const unsupported = ref(false)
// 状态读取失败时用错误态替代概要区，避免把「读不到」显示成「未找到」
const loadError = ref(false)
const loading = ref(false)
const updating = ref(false)
const savingSource = ref(false)
const savingSync = ref(false)
// rotating 表示正在请求系统重新生成同步令牌。
const rotating = ref(false)

const sourceForm = ref({
  update_url: '',
  update_interval_hours: 24,
  auto_update: false,
  use_upstream: true,
  update_token: '',
})
// restoredDefaults 标记表单来自「恢复默认」，保存时一并清除已设置的更新令牌。
const restoredDefaults = ref(false)

const syncForm = ref({
  enabled: false,
  // token 始终由系统管理，这里只用于掩码展示与复制，不提交修改。
  token: '',
})

const downloadURL = computed(() => {
  if (typeof window === 'undefined') return '/api/v1/geosite/dat'
  return `${window.location.origin}${info.value?.download_path || '/api/v1/geosite/dat'}`
})

const syncSnippet = computed(() => {
  const lines = [
    'routing:',
    '  geosite:',
    '    auto_update: true',
    `    update_url: "${downloadURL.value}"`,
    '    update_interval: 24h',
    `    update_token: "${t('pages.geosite.snippetTokenPlaceholder')}"`,
  ]
  return lines.join('\n')
})

function applyInfo(data: GeoSiteInfo) {
  info.value = data
  sourceForm.value = {
    update_url: data.update_url,
    update_interval_hours: Math.max(1, Math.round(data.update_interval_seconds / 3600) || 24),
    auto_update: data.auto_update,
    // 旧后端不返回该字段，默认按开启处理，与后端默认值一致。
    use_upstream: data.use_upstream ?? true,
    update_token: '',
  }
  syncForm.value = {
    enabled: data.sync_enabled,
    // 令牌未设置时清空，设置时保留已加载的明文，等待 fetchInfo 刷新。
    token: data.sync_token_set ? syncForm.value.token : '',
  }
  restoredDefaults.value = false
}

// loadSyncToken 仅在管理令牌下可用，用于把系统生成的令牌以掩码形式回显。
async function loadSyncToken() {
  try {
    syncForm.value.token = (await getGeoSiteSyncToken({ silent: true })).token
  } catch {
    // 旧后端没有该接口时保持为空，不影响其余功能
  }
}

function errorStatus(error: unknown): number | undefined {
  return axios.isAxiosError(error) ? error.response?.status : undefined
}

async function fetchInfo() {
  loading.value = true
  try {
    const data = await getGeoSiteInfo({ silent: true })
    applyInfo(data)
    unsupported.value = false
    loadError.value = false
    if (data.sync_token_set) {
      await loadSyncToken()
    }
  } catch (error) {
    // 404 说明后端尚未注册 /api/v1/geosite（版本较旧），直接给出可操作的提示，
    // 而不是弹一个含义不明的「请求错误 (404)」。
    if (errorStatus(error) === 404) {
      unsupported.value = true
      loadError.value = false
      info.value = null
      return
    }
    const status = errorStatus(error)
    if (status === 401) {
      // 认证失效由请求拦截器统一跳转登录页
      return
    }
    loadError.value = true
    message.error(
      status
        ? t('pages.geosite.messages.loadFailedStatus', { status })
        : t('pages.geosite.messages.loadFailed'),
    )
  } finally {
    loading.value = false
  }
}

async function handleUpdate() {
  // 更新地址是必要条件，先做前置校验，避免落到服务端 4xx/5xx。
  if (!info.value?.update_url) {
    message.warning(t('pages.geosite.messages.needUrl'))
    return
  }
  updating.value = true
  try {
    const result = await updateGeoSite()
    applyInfo(result.info)
    if (result.changed) {
      message.success(
        t('pages.geosite.messages.updated', { version: result.info.version || '—' }),
      )
    } else {
      message.info(t('pages.geosite.messages.upToDate'))
    }
  } catch {
    // 错误提示由请求拦截器统一处理
  } finally {
    updating.value = false
  }
}

function buildSourcePatch(): SettingsPatch | null {
  const form = sourceForm.value
  if (form.auto_update && form.update_interval_hours <= 0) {
    message.warning(t('pages.geosite.messages.autoIntervalRequired'))
    return null
  }
  const patch: SettingsPatch = {
    geosite_update_url: form.update_url.trim(),
    geosite_update_interval_hours: form.update_interval_hours,
    geosite_auto_update: form.auto_update,
    geosite_use_upstream: form.use_upstream,
  }
  const token = form.update_token.trim()
  if (token) {
    patch.geosite_update_token = token
  } else if (restoredDefaults.value) {
    // 空字符串表示清除已设置的更新令牌，与「恢复默认」语义一致。
    patch.geosite_update_token = ''
  }
  return patch
}

// handleRestoreDefaults 把「更新来源」表单恢复为系统默认值。
// 只填充表单，仍需点击「保存」才会写回配置，避免意外开启自动更新。
function handleRestoreDefaults() {
  const seconds = info.value?.default_update_interval_seconds || 24 * 3600
  sourceForm.value = {
    update_url: info.value?.default_update_url || info.value?.update_url || '',
    update_interval_hours: Math.max(1, Math.round(seconds / 3600)),
    auto_update: true,
    use_upstream: true,
    update_token: '',
  }
  restoredDefaults.value = true
  message.info(t('pages.geosite.messages.defaultsFilled'))
}

async function handleSaveSource() {
  const patch = buildSourcePatch()
  if (!patch) return
  savingSource.value = true
  try {
    await updateSettings(patch)
    message.success(t('pages.geosite.messages.sourceSaved'))
    await fetchInfo()
  } catch {
    // 错误已统一提示
  } finally {
    savingSource.value = false
  }
}

async function handleSaveSync() {
  const patch: SettingsPatch = {
    geosite_sync_enabled: syncForm.value.enabled,
  }
  savingSync.value = true
  try {
    await updateSettings(patch)
    await fetchInfo()
    message.success(
      t(
        syncForm.value.enabled ? 'pages.geosite.messages.syncEnabled' : 'pages.geosite.messages.syncDisabled',
      ),
    )
  } catch {
    // 错误已统一提示
  } finally {
    savingSync.value = false
  }
}

async function handleRotateToken() {
  // 刷新 info 会重置表单，先记下用户当前的勾选状态。
  const enabled = syncForm.value.enabled
  rotating.value = true
  try {
    const result = await generateGeoSiteSyncToken()
    applyInfo(result.info)
    syncForm.value.token = result.token
    syncForm.value.enabled = enabled
    message.success(t('pages.geosite.messages.tokenRotated'))
  } catch {
    // 错误已统一提示
  } finally {
    rotating.value = false
  }
}

// 勾选「允许下游同步」且尚无可用的同步令牌时，立即由系统生成，无需等保存。
async function handleSyncToggle(checked: boolean) {
  if (!checked || syncForm.value.token || rotating.value) {
    return
  }
  await handleRotateToken()
}

async function copyValue(text: string, tip: string) {
  if (await copyText(text)) {
    message.success(tip)
    return
  }
  message.warning(t('pages.geosite.messages.copyFailed'))
}

onMounted(fetchInfo)
</script>

<template>
  <div class="page-shell geosite-view">
    <div class="page-header">
      <div>
        <a-typography-title :level="4" style="margin: 0;">{{ t('pages.geosite.title') }}</a-typography-title>
        <a-typography-text type="secondary">
          {{ t('pages.geosite.subtitle') }}
        </a-typography-text>
      </div>
      <a-space>
        <a-button :loading="loading" @click="fetchInfo">{{ t('common.refresh') }}</a-button>
        <a-popconfirm
          :title="t('pages.geosite.updateConfirm')"
          :ok-text="t('pages.geosite.updateNow')"
          :cancel-text="t('common.cancel')"
          @confirm="handleUpdate"
        >
          <a-button type="primary" :loading="updating" :disabled="unsupported">
            {{ t('pages.geosite.updateNow') }}
          </a-button>
        </a-popconfirm>
      </a-space>
    </div>

    <a-alert
      v-if="unsupported"
      type="error"
      show-icon
      class="geosite-alert"
      :message="t('pages.geosite.unsupported.title')"
      :description="t('pages.geosite.unsupported.description')"
    />

    <LoadError
      v-if="loadError && !unsupported"
      :description="t('pages.geosite.loadError')"
      :retrying="loading"
      @retry="fetchInfo"
    />

    <div v-else-if="!unsupported" class="geosite-body">
      <!-- 版本概要：横向铺开，避免占用左右分栏空间 -->
      <a-card size="small" class="summary-card">
        <a-descriptions :column="{ xs: 1, sm: 2, lg: 4 }" size="small" :colon="false">
          <a-descriptions-item :label="t('pages.geosite.summary.status')">
            <a-tag :color="info?.exists ? 'green' : 'red'">
              {{ info?.exists ? t('pages.geosite.summary.loaded') : t('pages.geosite.summary.missing') }}
            </a-tag>
          </a-descriptions-item>
          <a-descriptions-item :label="t('pages.geosite.summary.version')">
            <span class="mono">{{ info?.version || '—' }}</span>
          </a-descriptions-item>
          <a-descriptions-item :label="t('pages.geosite.summary.size')">
            {{ formatBytes(info?.size || 0, '—') }}
          </a-descriptions-item>
          <a-descriptions-item :label="t('pages.geosite.summary.updatedAt')">
            {{ formatDateTime(info?.updated_at) }}
          </a-descriptions-item>
          <a-descriptions-item :label="t('pages.geosite.summary.lastChecked')">
            {{ formatDateTime(info?.last_checked_at) }}
          </a-descriptions-item>
          <a-descriptions-item :label="t('pages.geosite.summary.autoUpdate')">
            {{ info?.auto_update ? t('pages.geosite.summary.enabled') : t('pages.geosite.summary.disabled') }}
          </a-descriptions-item>
          <a-descriptions-item :label="t('pages.geosite.summary.source')" :span="2">
            <span class="mono ellipsis" :title="info?.source_url || ''">{{ info?.source_url || '—' }}</span>
          </a-descriptions-item>
        </a-descriptions>
        <div class="summary-foot">
          <a-typography-text type="secondary" class="mono ellipsis" :title="info?.path || ''">
            {{ t('pages.geosite.summary.dataPath') }}{{ info?.path || '—' }}
          </a-typography-text>
          <a-button
            size="small"
            type="link"
            :disabled="!info?.sha256"
            @click="copyValue(info?.sha256 || '', t('pages.geosite.messages.shaCopied'))"
          >
            {{ t('pages.geosite.summary.copySha') }}
          </a-button>
        </div>
      </a-card>

      <a-alert
        v-if="info && !info.exists"
        type="warning"
        show-icon
        class="geosite-alert"
        :message="t('pages.geosite.alerts.missingTitle')"
        :description="t('pages.geosite.alerts.missingDescription')"
      />
      <a-alert
        v-if="info?.last_error"
        type="error"
        show-icon
        class="geosite-alert"
        :message="t('pages.geosite.alerts.lastErrorTitle')"
        :description="info.last_error"
      />

      <!-- 左右分栏：左侧更新来源，右侧下游同步 -->
      <div class="geosite-columns">
        <section class="geosite-column">
          <a-card size="small" :title="t('pages.geosite.source.title')" class="column-card">
            <template #extra>
              <a-tag :color="info?.auto_update ? 'green' : 'default'">
                {{ info?.auto_update ? t('pages.geosite.source.autoTag') : t('pages.geosite.source.manualTag') }}
              </a-tag>
            </template>
            <div class="column-scroll">
              <a-form layout="vertical">
                <a-form-item
                  html-for="geo-update-url"
                  :label="t('pages.geosite.source.updateUrl')"
                  :tooltip="t('pages.geosite.source.updateUrlTooltip')"
                >
                  <a-input id="geo-update-url"
                    v-model:value="sourceForm.update_url"
                    :placeholder="t('pages.geosite.source.updateUrlPlaceholder')"
                    :aria-label="t('pages.geosite.source.updateUrlAria')"
                  />
                </a-form-item>
                <a-form-item
                  html-for="geo-update-token"
                  :label="t('pages.geosite.source.token')"
                  :tooltip="t('pages.geosite.source.tokenTooltip')"
                >
                  <a-input-password id="geo-update-token"
                    v-model:value="sourceForm.update_token"
                    :placeholder="
                      restoredDefaults && info?.update_token_set
                        ? t('pages.geosite.source.tokenPlaceholderClear')
                        : info?.update_token_set
                          ? t('pages.geosite.source.tokenPlaceholderSet')
                          : t('pages.geosite.source.tokenPlaceholderEmpty')
                    "
                    autocomplete="new-password"
                    :aria-label="t('pages.geosite.source.tokenAria')"
                  />
                </a-form-item>

                <div class="field-row">
                  <div class="field-block">
                    <a-form-item html-for="geo-auto-update" :label="t('pages.geosite.source.autoUpdate')">
                      <a-switch
                        id="geo-auto-update"
                        v-model:checked="sourceForm.auto_update"
                        :aria-label="t('pages.geosite.source.autoUpdateAria')"
                      />
                    </a-form-item>
                  </div>
                  <div class="field-block">
                    <a-form-item html-for="geo-update-interval-hours" :label="t('pages.geosite.source.interval')">
                      <a-input-number id="geo-update-interval-hours"
                        v-model:value="sourceForm.update_interval_hours"
                        :min="1"
                        :max="8760"
                        style="width: 100%;"
                        :aria-label="t('pages.geosite.source.intervalAria')"
                      />
                    </a-form-item>
                  </div>
                </div>

                <a-form-item html-for="geo-use-upstream"
                  :label="t('pages.geosite.source.useUpstream')"
                  :tooltip="t('pages.geosite.source.useUpstreamTooltip')"
                >
                  <a-switch
                    id="geo-use-upstream"
                    v-model:checked="sourceForm.use_upstream"
                    :aria-label="t('pages.geosite.source.useUpstreamAria')"
                  />
                  <a-typography-text type="secondary" class="foot-hint" style="margin-left: 8px;">
                    {{
                      sourceForm.use_upstream
                        ? t('pages.geosite.source.useUpstreamOn')
                        : t('pages.geosite.source.useUpstreamOff')
                    }}
                  </a-typography-text>
                </a-form-item>
              </a-form>
            </div>
            <div class="column-foot">
              <a-typography-text type="secondary" class="foot-hint">
                {{ t('pages.geosite.source.hotReloadHint') }}
              </a-typography-text>
              <a-space>
                <a-button :disabled="loading" @click="fetchInfo">{{ t('pages.geosite.source.restore') }}</a-button>
                <a-button @click="handleRestoreDefaults">{{ t('pages.geosite.source.restoreDefaults') }}</a-button>
                <a-button type="primary" :loading="savingSource" @click="handleSaveSource">
                  {{ t('pages.geosite.source.save') }}
                </a-button>
              </a-space>
            </div>
          </a-card>
        </section>

        <section class="geosite-column">
          <a-card size="small" :title="t('pages.geosite.sync.title')" class="column-card">
            <template #extra>
              <a-tag :color="info?.sync_enabled ? 'green' : 'default'">
                {{ info?.sync_enabled ? t('pages.geosite.sync.on') : t('pages.geosite.sync.off') }}
              </a-tag>
            </template>
            <div class="column-scroll">
              <a-alert
                type="info"
                show-icon
                class="sync-tip"
                :message="t('pages.geosite.sync.tipTitle')"
                :description="t('pages.geosite.sync.tipDescription')"
              />

              <a-form layout="vertical">
                <a-form-item html-for="geo-enabled" :label="t('pages.geosite.sync.allowLabel')">
                  <a-switch id="geo-enabled"
                    v-model:checked="syncForm.enabled"
                    :aria-label="t('pages.geosite.sync.allowAria')"
                    @change="handleSyncToggle"
                  />
                </a-form-item>
                <a-form-item
                  :label="t('pages.geosite.sync.tokenLabel')"
                  :tooltip="t('pages.geosite.sync.tokenTooltip')"
                >
                  <div class="token-row">
                    <a-input-password
                      :value="syncForm.token"
                      readonly
                      :placeholder="
                        loading && info?.sync_token_set
                          ? t('pages.geosite.sync.tokenLoading')
                          : t('pages.geosite.sync.tokenEmpty')
                      "
                      :aria-label="t('pages.geosite.sync.tokenAria')"
                    />
                    <a-button
                      :disabled="!syncForm.token"
                      @click="copyValue(syncForm.token, t('pages.geosite.messages.syncTokenCopied'))"
                    >
                      {{ t('pages.geosite.sync.copy') }}
                    </a-button>
                    <a-button :loading="rotating" @click="handleRotateToken">
                      {{ t('pages.geosite.sync.regenerate') }}
                    </a-button>
                  </div>
                </a-form-item>
              </a-form>

              <a-descriptions :column="1" size="small" bordered class="sync-endpoint">
                <a-descriptions-item :label="t('pages.geosite.sync.endpointDownload')">
                  <div class="copy-line">
                    <span class="mono ellipsis" :title="downloadURL">{{ downloadURL }}</span>
                    <a-button
                      type="link"
                      size="small"
                      @click="copyValue(downloadURL, t('pages.geosite.messages.downloadUrlCopied'))"
                    >
                      {{ t('pages.geosite.sync.copy') }}
                    </a-button>
                  </div>
                </a-descriptions-item>
                <a-descriptions-item :label="t('pages.geosite.sync.endpointVersion')">
                  <span class="mono">{{ info?.version || '—' }}</span>
                </a-descriptions-item>
              </a-descriptions>

              <div class="snippet-title">
                <a-typography-text strong>{{ t('pages.geosite.sync.snippetTitle') }}</a-typography-text>
                <a-button
                  type="link"
                  size="small"
                  @click="copyValue(syncSnippet, t('pages.geosite.messages.snippetCopied'))"
                >
                  {{ t('pages.geosite.sync.copy') }}
                </a-button>
              </div>
              <pre class="snippet">{{ syncSnippet }}</pre>
            </div>
            <div class="column-foot">
              <a-typography-text type="secondary" class="foot-hint">
                {{ t('pages.geosite.sync.footHint') }}
              </a-typography-text>
              <a-button type="primary" :loading="savingSync" @click="handleSaveSync">
                {{ t('pages.geosite.sync.save') }}
              </a-button>
            </div>
          </a-card>
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
.geosite-view {
  width: 100%;
}

.geosite-body {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 16px;
  overflow-y: auto;
  padding-right: 4px;
}

.summary-card :deep(.ant-card-body) {
  padding: 12px 16px;
}

.summary-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px dashed rgba(128, 128, 128, 0.2);
}

.geosite-alert {
  margin: 0;
}

/* 左右分栏：两列等高、各自独立滚动，窄屏自动堆叠 */
.geosite-columns {
  display: flex;
  gap: 16px;
  flex: 1 1 auto;
  min-height: 0;
}

.geosite-column {
  flex: 1 1 0;
  min-width: 0;
  display: flex;
}

.column-card {
  display: flex;
  flex-direction: column;
  width: 100%;
  min-height: 0;
}

.column-card :deep(.ant-card-body) {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-height: 0;
  padding: 16px;
}

.column-scroll {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  padding-right: 4px;
}

.column-foot {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid rgba(128, 128, 128, 0.18);
}

.foot-hint {
  font-size: 12px;
}

.field-row {
  display: flex;
  gap: 16px;
  align-items: flex-start;
}

.field-block {
  flex: 1 1 0;
  min-width: 0;
}

.sync-tip {
  margin-bottom: 12px;
}

.sync-endpoint {
  margin-bottom: 12px;
}

.token-row {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.token-row :deep(.ant-input-affix-wrapper) {
  flex: 1 1 auto;
  min-width: 0;
}

.copy-line {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
}

.copy-line .mono {
  flex: 1 1 auto;
  min-width: 0;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
}

.ellipsis {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.snippet-title {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0 0 8px;
}

.snippet {
  margin: 0;
  padding: 12px;
  border-radius: 6px;
  background: rgba(127, 127, 127, 0.08);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.6;
  overflow-x: auto;
}

@media (max-width: 992px) {
  .geosite-columns {
    flex-direction: column;
  }

  .geosite-body {
    overflow-y: auto;
  }

  .column-scroll {
    overflow-y: visible;
  }

  .field-row {
    flex-direction: column;
    gap: 12px;
  }

  .summary-foot,
  .column-foot {
    flex-wrap: wrap;
  }
}
</style>
