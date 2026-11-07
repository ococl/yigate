<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { message } from 'antdv-next'
import { getGeoSiteInfo, updateGeoSite } from '@/api/geosite'
import { updateSettings } from '@/api/settings'
import type { GeoSiteInfo, SettingsPatch } from '@/types/api'

const info = ref<GeoSiteInfo | null>(null)
const loading = ref(false)
const updating = ref(false)
const savingSource = ref(false)
const savingSync = ref(false)

const sourceForm = ref({
  path: '',
  update_url: '',
  update_interval_hours: 24,
  auto_update: false,
  update_token: '',
})

const syncForm = ref({
  enabled: false,
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
    '    path: "../data/geosite.dat"',
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
    path: data.path,
    update_url: data.update_url,
    update_interval_hours: Math.max(1, Math.round(data.update_interval_seconds / 3600) || 24),
    auto_update: data.auto_update,
    update_token: '',
  }
  syncForm.value = {
    enabled: data.sync_enabled,
    token: '',
  }
}

async function fetchInfo() {
  loading.value = true
  try {
    applyInfo(await getGeoSiteInfo())
  } finally {
    loading.value = false
  }
}

async function handleUpdate() {
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
    geosite_path: form.path.trim(),
    geosite_update_url: form.update_url.trim(),
    geosite_update_interval_hours: form.update_interval_hours,
    geosite_auto_update: form.auto_update,
  }
  const token = form.update_token.trim()
  if (token) {
    patch.geosite_update_token = token
  }
  return patch
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
  const form = syncForm.value
  if (form.enabled && !form.token.trim() && !info.value?.sync_token_set) {
    message.warning('开启下游同步前请设置同步令牌')
    return
  }
  const patch: SettingsPatch = {
    geosite_sync_enabled: form.enabled,
  }
  const token = form.token.trim()
  if (token) {
    patch.geosite_sync_token = token
  }
  savingSync.value = true
  try {
    await updateSettings(patch)
    message.success('下游同步设置已保存')
    await fetchInfo()
  } catch {
    // 错误已统一提示
  } finally {
    savingSync.value = false
  }
}

function formatBytes(value: number): string {
  if (!value || value <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB']
  let size = value
  let unit = 0
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024
    unit++
  }
  return `${size.toFixed(unit === 0 ? 0 : 2)} ${units[unit]}`
}

function formatTime(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString('zh-CN', { hour12: false })
}

async function copyText(text: string, tip: string) {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      message.success(tip)
      return
    }
    throw new Error('clipboard unavailable')
  } catch {
    message.warning('当前浏览器不支持自动复制，请手动选择复制')
  }
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
          <a-button type="primary" :loading="updating">立即更新</a-button>
        </a-popconfirm>
      </a-space>
    </div>

    <div class="geosite-body">
      <a-alert
        v-if="info && !info.exists"
        type="warning"
        show-icon
        style="margin-bottom: 16px;"
        message="未找到 GeoSite 数据文件"
        description="引用 geosite 类型规则时匹配会被跳过。可配置更新地址后点击「立即更新」，或手动放置数据文件。"
      />
      <a-alert
        v-if="info?.last_error"
        type="error"
        show-icon
        style="margin-bottom: 16px;"
        message="最近一次更新失败"
        :description="info.last_error"
      />

      <a-row :gutter="16">
        <a-col :xs="24" :xl="14">
          <a-card size="small" title="版本信息" class="geosite-card">
            <a-descriptions :column="2" bordered size="small">
              <a-descriptions-item label="状态">
                <a-tag :color="info?.exists ? 'green' : 'red'">
                  {{ info?.exists ? '已加载' : '未找到' }}
                </a-tag>
              </a-descriptions-item>
              <a-descriptions-item label="版本">
                <span class="mono">{{ info?.version || '—' }}</span>
              </a-descriptions-item>
              <a-descriptions-item label="文件大小">{{ formatBytes(info?.size || 0) }}</a-descriptions-item>
              <a-descriptions-item label="数据更新时间">{{ formatTime(info?.updated_at) }}</a-descriptions-item>
              <a-descriptions-item label="上次检查">{{ formatTime(info?.last_checked_at) }}</a-descriptions-item>
              <a-descriptions-item label="自动更新">{{ info?.auto_update ? '已启用' : '已关闭' }}</a-descriptions-item>
              <a-descriptions-item label="数据来源" :span="2">
                <span class="mono break-all">{{ info?.source_url || '—' }}</span>
              </a-descriptions-item>
              <a-descriptions-item label="本地路径" :span="2">
                <span class="mono break-all">{{ info?.path || '—' }}</span>
              </a-descriptions-item>
            </a-descriptions>
            <div class="geosite-foot">
              <a-button size="small" :disabled="!info?.sha256" @click="copyText(info?.sha256 || '', '完整校验值已复制')">
                复制完整 SHA-256
              </a-button>
            </div>
          </a-card>
        </a-col>

        <a-col :xs="24" :xl="10">
          <a-card size="small" title="更新来源" class="geosite-card">
            <a-form layout="vertical">
              <a-form-item label="数据路径" tooltip="相对路径基于配置文件所在目录解析">
                <a-input v-model:value="sourceForm.path" placeholder="../data/geosite.dat" />
              </a-form-item>
              <a-form-item label="更新地址" tooltip="可填写官方 dlc.dat 下载地址，或其他 YiGate 节点的 /api/v1/geosite/dat">
                <a-input v-model:value="sourceForm.update_url" placeholder="https://..." />
              </a-form-item>
              <a-form-item label="更新令牌" tooltip="从启用了同步的 YiGate 节点拉取时，填写对方的同步令牌">
                <a-input-password
                  v-model:value="sourceForm.update_token"
                  :placeholder="info?.update_token_set ? '已设置，留空保持不变' : '可选，Bearer 令牌'"
                  autocomplete="new-password"
                />
              </a-form-item>
              <a-row :gutter="12">
                <a-col :span="12">
                  <a-form-item label="自动更新">
                    <a-switch v-model:checked="sourceForm.auto_update" />
                  </a-form-item>
                </a-col>
                <a-col :span="12">
                  <a-form-item label="间隔（小时）">
                    <a-input-number
                      v-model:value="sourceForm.update_interval_hours"
                      :min="1"
                      :max="8760"
                      style="width: 100%;"
                    />
                  </a-form-item>
                </a-col>
              </a-row>
              <a-space>
                <a-button type="primary" :loading="savingSource" @click="handleSaveSource">保存更新配置</a-button>
                <a-button :disabled="loading" @click="fetchInfo">还原</a-button>
              </a-space>
            </a-form>
          </a-card>
        </a-col>
      </a-row>

      <a-card size="small" title="下游节点同步" class="geosite-card geosite-sync">
        <a-alert
          type="info"
          show-icon
          style="margin-bottom: 16px;"
          message="只读同步机制"
          description="开启后，其他节点可使用下方只读令牌下载 /api/v1/geosite/dat。响应带 ETag 与 X-GeoSite-Version，下游据此做条件请求：数据未变化时返回 304，不重复传输。该令牌不能修改配置或触发更新。"
        />

        <a-row :gutter="16">
          <a-col :xs="24" :md="12">
            <a-form layout="vertical">
              <a-form-item label="允许下游同步">
                <a-switch v-model:checked="syncForm.enabled" />
                <a-typography-text type="secondary" style="margin-left: 8px;">
                  关闭后该端点仅接受管理令牌
                </a-typography-text>
              </a-form-item>
              <a-form-item label="同步令牌" tooltip="至少 8 位，仅授予读取 GeoSite 数据的权限">
                <a-input-password
                  v-model:value="syncForm.token"
                  :placeholder="info?.sync_token_set ? '已设置，留空保持不变' : '至少 8 位'"
                  autocomplete="new-password"
                />
              </a-form-item>
              <a-button type="primary" :loading="savingSync" @click="handleSaveSync">保存同步设置</a-button>
            </a-form>
          </a-col>
          <a-col :xs="24" :md="12">
            <a-descriptions :column="1" size="small" bordered>
              <a-descriptions-item label="下载地址">
                <span class="mono break-all">{{ downloadURL }}</span>
                <a-button type="link" size="small" @click="copyText(downloadURL, '下载地址已复制')">复制</a-button>
              </a-descriptions-item>
              <a-descriptions-item label="当前版本">
                <span class="mono">{{ info?.version || '—' }}</span>
              </a-descriptions-item>
              <a-descriptions-item label="同步状态">
                <a-tag :color="info?.sync_enabled ? 'green' : 'default'">
                  {{ info?.sync_enabled ? '已开启' : '未开启' }}
                </a-tag>
              </a-descriptions-item>
            </a-descriptions>
          </a-col>
        </a-row>

        <div class="snippet-title">
          <a-typography-text strong>下游接入配置</a-typography-text>
          <a-button type="link" size="small" @click="copyText(syncSnippet, '下游配置片段已复制')">复制</a-button>
        </div>
        <pre class="snippet">{{ syncSnippet }}</pre>
      </a-card>
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
  overflow-y: auto;
  padding-right: 4px;
}

.geosite-card {
  height: 100%;
  margin-bottom: 16px;
}

.geosite-foot {
  margin-top: 12px;
}

.geosite-sync {
  height: auto;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
}

.break-all {
  word-break: break-all;
}

.snippet-title {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 16px 0 8px;
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
</style>
