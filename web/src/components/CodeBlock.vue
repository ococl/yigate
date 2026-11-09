<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { message } from 'antdv-next'
import { copyText } from '@/utils/clipboard'

const { t } = useI18n()

const props = withDefaults(
  defineProps<{
    // 静态示例可以直接写在默认插槽里，不必再传一份字符串
    code?: string
    title?: string
    copyable?: boolean
  }>(),
  { code: '', title: '', copyable: false },
)

async function handleCopy() {
  if (await copyText(props.code)) {
    message.success(t('common.copied'))
    return
  }
  message.error(t('common.copyFailed'))
}
</script>

<template>
  <div class="code-block">
    <div v-if="title" class="code-block-header">
      <span>{{ title }}</span>
      <a-button v-if="copyable" type="link" size="small" @click="handleCopy">{{ t('common.copy') }}</a-button>
    </div>
    <pre><code><slot>{{ code }}</slot></code></pre>
  </div>
</template>
