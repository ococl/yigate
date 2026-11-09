<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { message } from 'antdv-next'
import axios from 'axios'
import { generateGeoSiteSyncToken, getGeoSiteInfo, getGeoSiteSyncToken, updateGeoSite } from '@/api/geosite'
import { updateSettings } from '@/api/settings'
import LoadError from '@/components/LoadError.vue'
import { copyText } from '@/utils/clipboard'
import { formatDateTime } from '@/utils/datetime'
import { formatBytes } from '@/utils/format'
import type { GeoSiteInfo, SettingsPatch } from '@/types/api'

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
    '    update_token: "<在此填写本节点的同步令牌>"',
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
    message.error(status ? `加载 GeoSite 状态失败（${status}）` : '加载 GeoSite 状态失败，请检查后端服务')
  } finally {
    loading.value = false
  }
}

async function handleUpdate() {
  // 更新地址是必要条件，先做前置校验，避免落到服务端 4xx/5xx。
  if (!info.value?.update_url) {
    message.warning('请先在「更新来源」中填写更新地址并保存，再执行更新')
    return
  }
  updating.value = true
  try {
    const result = await updateGeoSite()
    applyInfo(result.info)
    if (result.changed) {
      message.success(`GeoSite 数据已更新至版本 ${result.info.version || '—'}`)
    } else {
      message.info('已是最新版本，无需更新')
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
    message.warning('启用自动更新时，更新间隔必须大于 0')
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
  message.info('已填入默认值，点击「保存」后生效')
}

async function handleSaveSource() {
  const patch = buildSourcePatch()
  if (!patch) return
  savingSource.value = true
  try {
    await updateSettings(patch)
    message.success('更新配置已保存并生效')
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
    message.success(syncForm.value.enabled ? '下游同步已开启' : '下游同步已关闭')
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
    message.success('已生成新的同步令牌，可直接复制')
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
  message.warning('复制失败，请手动选择复制')
}

onMounted(fetchInfo)
</script>

<template>
  <div class="page-shell geosite-view">
    <div class="page-header">
      <div>
        <a-typography-title :level="4" style="margin: 0;">GeoSite 数据</a-typography-title>
        <a-typography-text type="secondary">
          域名库版本、更新来源，以及供下游节点同步的只读数据端点。
        </a-typography-text>
      </div>
      <a-space>
        <a-button :loading="loading" @click="fetchInfo">刷新</a-button>
        <a-popconfirm
          title="立即从更新地址拉取最新 GeoSite 数据？"
          ok-text="更新"
          cancel-text="取消"
          @confirm="handleUpdate"
        >
          <a-button type="primary" :loading="updating" :disabled="unsupported">立即更新</a-button>
        </a-popconfirm>
      </a-space>
    </div>

    <a-alert
      v-if="unsupported"
      type="error"
      show-icon
      class="geosite-alert"
      message="当前后端未提供 GeoSite 管理接口"
      description="请求 /api/v1/geosite 返回 404，说明所连接服务端的版本较旧。请重新构建并重启后端服务（make build），或把开发服务器代理的 VITE_API_TARGET 指向已升级的实例。"
    />

    <LoadError
      v-if="loadError && !unsupported"
      description="无法读取 GeoSite 数据状态，请检查后端服务是否运行。"
      :retrying="loading"
      @retry="fetchInfo"
    />

    <div v-else-if="!unsupported" class="geosite-body">
      <!-- 版本概要：横向铺开，避免占用左右分栏空间 -->
      <a-card size="small" class="summary-card">
        <a-descriptions :column="{ xs: 1, sm: 2, lg: 4 }" size="small" :colon="false">
          <a-descriptions-item label="状态">
            <a-tag :color="info?.exists ? 'green' : 'red'">
              {{ info?.exists ? '已加载' : '未找到' }}
            </a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="版本">
            <span class="mono">{{ info?.version || '—' }}</span>
          </a-descriptions-item>
          <a-descriptions-item label="文件大小">{{ formatBytes(info?.size || 0, '—') }}</a-descriptions-item>
          <a-descriptions-item label="数据更新时间">{{ formatDateTime(info?.updated_at) }}</a-descriptions-item>
          <a-descriptions-item label="上次检查">{{ formatDateTime(info?.last_checked_at) }}</a-descriptions-item>
          <a-descriptions-item label="自动更新">{{ info?.auto_update ? '已启用' : '已关闭' }}</a-descriptions-item>
          <a-descriptions-item label="来源" :span="2">
            <span class="mono ellipsis" :title="info?.source_url || ''">{{ info?.source_url || '—' }}</span>
          </a-descriptions-item>
        </a-descriptions>
        <div class="summary-foot">
          <a-typography-text type="secondary" class="mono ellipsis" :title="info?.path || ''">
            数据路径：{{ info?.path || '—' }}
          </a-typography-text>
          <a-button
            size="small"
            type="link"
            :disabled="!info?.sha256"
            @click="copyValue(info?.sha256 || '', '完整校验值已复制')"
          >
            复制完整 SHA-256
          </a-button>
        </div>
      </a-card>

      <a-alert
        v-if="info && !info.exists"
        type="warning"
        show-icon
        class="geosite-alert"
        message="未找到 GeoSite 数据文件"
        description="引用 geosite 类型规则时匹配会被跳过。可配置更新地址后点击「立即更新」，或手动放置数据文件。"
      />
      <a-alert
        v-if="info?.last_error"
        type="error"
        show-icon
        class="geosite-alert"
        message="最近一次更新失败"
        :description="info.last_error"
      />

      <!-- 左右分栏：左侧更新来源，右侧下游同步 -->
      <div class="geosite-columns">
        <section class="geosite-column">
          <a-card size="small" title="更新来源" class="column-card">
            <template #extra>
              <a-tag :color="info?.auto_update ? 'green' : 'default'">
                {{ info?.auto_update ? '自动更新' : '手动更新' }}
              </a-tag>
            </template>
            <div class="column-scroll">
              <a-form layout="vertical">
                <a-form-item html-for="geo-update-url" label="更新地址" tooltip="可填写官方 dlc.dat 下载地址，或其他 YiGate 节点的 /api/v1/geosite/dat">
                  <a-input id="geo-update-url"
                    v-model:value="sourceForm.update_url"
                    placeholder="https://..."
                    aria-label="GeoSite 更新地址"
                  />
                </a-form-item>
                <a-form-item html-for="geo-update-token" label="更新令牌" tooltip="从启用了同步的 YiGate 节点拉取时，填写对方的同步令牌">
                  <a-input-password id="geo-update-token"
                    v-model:value="sourceForm.update_token"
                    :placeholder="
                      restoredDefaults && info?.update_token_set
                        ? '保存后将清除已设置的令牌'
                        : info?.update_token_set
                          ? '已设置，留空保持不变'
                          : '可选，Bearer 令牌'
                    "
                    autocomplete="new-password"
                    aria-label="GeoSite 更新令牌"
                  />
                </a-form-item>

                <div class="field-row">
                  <div class="field-block">
                    <a-form-item html-for="geo-auto-update" label="自动更新">
                      <a-switch id="geo-auto-update" v-model:checked="sourceForm.auto_update" aria-label="自动更新" />
                    </a-form-item>
                  </div>
                  <div class="field-block">
                    <a-form-item html-for="geo-update-interval-hours" label="间隔（小时）">
                      <a-input-number id="geo-update-interval-hours"
                        v-model:value="sourceForm.update_interval_hours"
                        :min="1"
                        :max="8760"
                        style="width: 100%;"
                        aria-label="自动更新间隔（小时）"
                      />
                    </a-form-item>
                  </div>
                </div>

                <a-form-item html-for="geo-use-upstream"
                  label="通过上游代理下载"
                  tooltip="优先使用已配置且可用的上游代理，全部失败后回退直连；网络正常时可关闭"
                >
                  <a-switch id="geo-use-upstream" v-model:checked="sourceForm.use_upstream" aria-label="通过上游代理下载" />
                  <a-typography-text type="secondary" class="foot-hint" style="margin-left: 8px;">
                    {{ sourceForm.use_upstream ? '优先走上游' : '始终直连' }}
                  </a-typography-text>
                </a-form-item>
              </a-form>
            </div>
            <div class="column-foot">
              <a-typography-text type="secondary" class="foot-hint">保存后立即热加载</a-typography-text>
              <a-space>
                <a-button :disabled="loading" @click="fetchInfo">还原</a-button>
                <a-button @click="handleRestoreDefaults">恢复默认</a-button>
                <a-button type="primary" :loading="savingSource" @click="handleSaveSource">保存</a-button>
              </a-space>
            </div>
          </a-card>
        </section>

        <section class="geosite-column">
          <a-card size="small" title="下游节点同步" class="column-card">
            <template #extra>
              <a-tag :color="info?.sync_enabled ? 'green' : 'default'">
                {{ info?.sync_enabled ? '已开启' : '未开启' }}
              </a-tag>
            </template>
            <div class="column-scroll">
              <a-alert
                type="info"
                show-icon
                class="sync-tip"
                message="只读同步"
                description="下游使用同步令牌下载数据；响应带 ETag 与 X-GeoSite-Version，数据未变时返回 304，不重复传输。该令牌不能修改配置或触发更新。"
              />

              <a-form layout="vertical">
                <a-form-item html-for="geo-enabled" label="允许下游同步">
                  <a-switch id="geo-enabled"
                    v-model:checked="syncForm.enabled"
                    aria-label="允许下游同步"
                    @change="handleSyncToggle"
                  />
                </a-form-item>
                <a-form-item
                  label="同步令牌"
                  tooltip="由系统随机生成，仅授予读取 GeoSite 数据的权限；默认掩码显示，可复制后填入下游配置"
                >
                  <div class="token-row">
                    <a-input-password
                      :value="syncForm.token"
                      readonly
                      :placeholder="loading && info?.sync_token_set ? '正在加载…' : '尚未生成'"
                      aria-label="GeoSite 同步令牌"
                    />
                    <a-button
                      :disabled="!syncForm.token"
                      @click="copyValue(syncForm.token, '同步令牌已复制')"
                    >
                      复制
                    </a-button>
                    <a-button :loading="rotating" @click="handleRotateToken">
                      重新生成
                    </a-button>
                  </div>
                </a-form-item>
              </a-form>

              <a-descriptions :column="1" size="small" bordered class="sync-endpoint">
                <a-descriptions-item label="下载地址">
                  <div class="copy-line">
                    <span class="mono ellipsis" :title="downloadURL">{{ downloadURL }}</span>
                    <a-button type="link" size="small" @click="copyValue(downloadURL, '下载地址已复制')">复制</a-button>
                  </div>
                </a-descriptions-item>
                <a-descriptions-item label="当前版本">
                  <span class="mono">{{ info?.version || '—' }}</span>
                </a-descriptions-item>
              </a-descriptions>

              <div class="snippet-title">
                <a-typography-text strong>下游接入配置</a-typography-text>
                <a-button type="link" size="small" @click="copyValue(syncSnippet, '下游配置片段已复制')">复制</a-button>
              </div>
              <pre class="snippet">{{ syncSnippet }}</pre>
            </div>
            <div class="column-foot">
              <a-typography-text type="secondary" class="foot-hint">关闭后端点仅接受管理令牌</a-typography-text>
              <a-button type="primary" :loading="savingSync" @click="handleSaveSync">保存</a-button>
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
