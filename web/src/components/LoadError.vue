<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

withDefaults(
  defineProps<{
    /** 失败原因提示，留空时使用通用文案 */
    description?: string
    /** 重试请求进行中 */
    retrying?: boolean
  }>(),
  { description: '', retrying: false },
)

const emit = defineEmits<{ retry: [] }>()
</script>

<template>
  <a-result
    class="load-error"
    status="warning"
    :title="t('common.loadFailed')"
    :sub-title="description || t('common.loadFailedHint')"
  >
    <template #extra>
      <a-button type="primary" data-testid="load-error-retry" :loading="retrying" @click="emit('retry')">
        {{ t('common.retry') }}
      </a-button>
    </template>
  </a-result>
</template>

<style scoped>
.load-error {
  padding: 24px 0;
}
</style>
