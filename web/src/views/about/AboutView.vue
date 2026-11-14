<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { getVersion } from '@/api/system'
import type { VersionInfo } from '@/types/api'
import { formatUptime } from '@/utils/format'
import { formatDateTime } from '@/utils/datetime'

const { t } = useI18n()

const info = ref<VersionInfo | null>(null)
const error = ref('')

async function loadVersion() {
  error.value = ''
  try {
    info.value = await getVersion()
  } catch {
    error.value = t('pages.about.loadError')
  }
}

onMounted(loadVersion)
</script>

<template>
  <div class="page-header" data-testid="page-about">
    <div>
      <a-typography-title :level="4" style="margin: 0;">{{ t('pages.about.title') }}</a-typography-title>
      <a-typography-text type="secondary">{{ t('pages.about.tagline') }}</a-typography-text>
    </div>
  </div>

  <a-alert v-if="error" type="warning" show-icon :message="error" style="margin-bottom: 16px;">
    <template #action>
      <a-button size="small" data-testid="about-retry" @click="loadVersion">
        {{ t('common.retry') }}
      </a-button>
    </template>
  </a-alert>

  <a-descriptions :title="t('pages.about.versionTitle')" :column="{ xs: 1, sm: 2 }" bordered size="small">
    <a-descriptions-item :label="t('pages.about.fields.version')">{{ info?.version || '—' }}</a-descriptions-item>
    <a-descriptions-item :label="t('pages.about.fields.commit')">{{ info?.commit || '—' }}</a-descriptions-item>
    <a-descriptions-item :label="t('pages.about.fields.buildTime')">
      {{ formatDateTime(info?.build_time) }}
    </a-descriptions-item>
    <a-descriptions-item :label="t('pages.about.fields.goVersion')">{{ info?.go_version || '—' }}</a-descriptions-item>
    <a-descriptions-item :label="t('pages.about.fields.platform')">{{ info?.platform || '—' }}</a-descriptions-item>
    <a-descriptions-item :label="t('pages.about.fields.uptime')">
      {{ formatUptime(info?.uptime_seconds || 0) }}
    </a-descriptions-item>
  </a-descriptions>

  <a-descriptions :title="t('pages.about.projectTitle')" :column="1" bordered size="small" style="margin-top: 24px;">
    <a-descriptions-item :label="t('pages.about.project.stack')">
      {{ t('pages.about.project.stackValue') }}
    </a-descriptions-item>
    <a-descriptions-item :label="t('pages.about.project.protocols')">
      {{ t('pages.about.project.protocolsValue') }}
    </a-descriptions-item>
    <a-descriptions-item :label="t('pages.about.project.capabilities')">
      {{ t('pages.about.project.capabilitiesValue') }}
    </a-descriptions-item>
  </a-descriptions>
</template>
