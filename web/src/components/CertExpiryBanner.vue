<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getStatus } from '@/api/system'
import { useVisiblePolling } from '@/composables/useVisiblePolling'
import { formatDateTime } from '@/utils/datetime'
import type { CertificateStatus } from '@/types/api'

/** 与服务端告警窗口保持一致：30 天内到期就提示。 */
const WARN_DAYS = 30
const MAX_ITEMS = 3

// 服务端的 kind 是稳定的英文标识（日志里也用同一套），这里只做展示映射。
const KIND_KEYS: Record<string, string> = {
  'listener certificate': 'listener',
  'TLCP signing certificate': 'gmSign',
  'TLCP encryption certificate': 'gmEnc',
  'client CA': 'clientCa',
  'upstream CA': 'upstreamCa',
}

const { t } = useI18n()
const certificates = ref<CertificateStatus[]>([])
const dismissed = ref(false)

async function refresh() {
  try {
    const status = await getStatus({ silent: true })
    certificates.value = status.certificates || []
  } catch {
    // 横幅只是附加信息：接口不可用时保留上一次结果，不额外弹错。
  }
}

useVisiblePolling(refresh, 60_000, true)

const urgent = computed(() =>
  certificates.value
    .filter((item) => item.expired || item.days_left <= WARN_DAYS)
    .sort((a, b) => a.days_left - b.days_left),
)
const hasExpired = computed(() => urgent.value.some((item) => item.expired))
const visible = computed(() => !dismissed.value && urgent.value.length > 0)
const shown = computed(() => urgent.value.slice(0, MAX_ITEMS))
const restCount = computed(() => Math.max(0, urgent.value.length - MAX_ITEMS))

function kindLabel(kind: string): string {
  const key = KIND_KEYS[kind]
  return key ? t(`layout.certKinds.${key}`) : kind
}

function expiryLabel(item: CertificateStatus): string {
  return item.expired
    ? t('layout.certExpiryExpiredAt', { date: formatDateTime(item.not_after) })
    : t('layout.certExpiryDaysLeft', { days: item.days_left })
}
</script>

<template>
  <a-alert
    v-if="visible"
    class="cert-expiry-banner"
    data-testid="cert-expiry-banner"
    :type="hasExpired ? 'error' : 'warning'"
    show-icon
    closable
    :message="hasExpired ? t('layout.certExpiryExpiredTitle') : t('layout.certExpiryTitle')"
    @close="dismissed = true"
  >
    <template #description>
      <ul class="cert-expiry-list">
        <li v-for="item in shown" :key="item.file">
          <span class="cert-expiry-file">{{ item.file }}</span>
          <span class="cert-expiry-meta"> · {{ kindLabel(item.kind) }} · {{ expiryLabel(item) }}</span>
        </li>
      </ul>
      <div v-if="restCount > 0" class="cert-expiry-more">
        {{ t('layout.certExpiryMore', { count: restCount }) }}
      </div>
    </template>
  </a-alert>
</template>

<style scoped>
.cert-expiry-banner {
  margin-bottom: 16px;
}

.cert-expiry-list {
  margin: 4px 0 0;
  padding-left: 18px;
}

.cert-expiry-file {
  font-family: var(--yigate-font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  word-break: break-all;
}

.cert-expiry-meta {
  color: inherit;
  opacity: 0.85;
}

.cert-expiry-more {
  margin-top: 4px;
}
</style>
