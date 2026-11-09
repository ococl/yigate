<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { message } from 'antdv-next'
import LoadError from '@/components/LoadError.vue'
import { copyText } from '@/utils/clipboard'

const { t } = useI18n()

// 内容是 public/llmx.txt，随构建一起发布：AI 直接 GET 这个路径就能拿到同一份说明
const docUrl = `${import.meta.env.BASE_URL}llmx.txt`

const content = ref('')
const loading = ref(true)
const failed = ref(false)

async function fetchDoc() {
  loading.value = true
  failed.value = false
  try {
    const resp = await fetch(docUrl)
    if (!resp.ok) throw new Error(String(resp.status))
    content.value = await resp.text()
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
}

onMounted(fetchDoc)

async function copyAll() {
  const copied = await copyText(content.value)
  if (copied) {
    message.success(t('pages.llmx.copied'))
    return
  }
  message.error(t('pages.llmx.copyFailed'))
}
</script>

<template>
  <div class="page-shell" data-testid="page-llmx">
    <div class="page-header">
      <div class="page-header-text">
        <a-typography-title :level="4" style="margin: 0;">{{ t('pages.llmx.title') }}</a-typography-title>
        <a-typography-text type="secondary">
          {{ t('pages.llmx.subtitle') }}
        </a-typography-text>
      </div>
      <a-space wrap>
        <a-button :loading="loading" @click="fetchDoc">{{ t('common.refresh') }}</a-button>
        <a-button type="primary" :disabled="!content" @click="copyAll">{{ t('pages.llmx.copyAll') }}</a-button>
        <a-button :href="docUrl" target="_blank" rel="noopener">{{ t('pages.llmx.openRaw') }}</a-button>
      </a-space>
    </div>

    <a-card class="fill-card">
      <LoadError v-if="failed" :retrying="loading" @retry="fetchDoc" />
      <a-skeleton v-else-if="loading" active :paragraph="{ rows: 16 }" />
      <pre v-else class="llmx-text">{{ content }}</pre>
    </a-card>
  </div>
</template>

<style scoped>
/* 纯文本按原样排版：保留缩进与换行，长行折行但不出现横向滚动条 */
.llmx-text {
  flex: 1 1 auto;
  min-height: 0;
  margin: 0;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: var(--ant-font-family-code, ui-monospace, SFMono-Regular, Menlo, monospace);
  font-size: 13px;
  line-height: 1.8;
  color: var(--ant-color-text);
}
</style>
