<template>
  <div class="mobile-skeleton">
    <a-card v-for="i in count" :key="i" size="small" class="mobile-card">
      <a-skeleton active :title="{ width: '35%' }" :paragraph="{ rows: 2 }" />
    </a-card>
  </div>
</template>

<script setup lang="ts">
// 移动端卡片列表没有表格遮罩，首屏加载时先渲染占位卡片，
// 避免「暂无数据」空态闪现几百毫秒再被数据替换。
withDefaults(defineProps<{ count?: number }>(), { count: 3 })
</script>
