<script setup lang="ts">
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
    title="数据加载失败"
    :sub-title="description || '请检查后端服务是否运行，然后重试。'"
  >
    <template #extra>
      <a-button type="primary" :loading="retrying" @click="emit('retry')">重试</a-button>
    </template>
  </a-result>
</template>

<style scoped>
.load-error {
  padding: 24px 0;
}
</style>
