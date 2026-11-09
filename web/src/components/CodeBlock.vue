<script setup lang="ts">
import { message } from 'antdv-next'
import { copyText } from '@/utils/clipboard'

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
    message.success('已复制')
    return
  }
  message.error('复制失败，请手动选择内容复制')
}
</script>

<template>
  <div class="code-block">
    <div v-if="title" class="code-block-header">
      <span>{{ title }}</span>
      <a-button v-if="copyable" type="link" size="small" @click="handleCopy">复制</a-button>
    </div>
    <pre><code><slot>{{ code }}</slot></code></pre>
  </div>
</template>
