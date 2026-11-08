<script setup lang="ts">
import { useRouter } from 'vue-router'

const router = useRouter()
</script>

<template>
  <div class="not-found">
    <a-result status="404" title="404" sub-title="页面不存在或已被移除">
      <template #extra>
        <a-space>
          <a-button type="primary" @click="router.push('/dashboard')">返回仪表盘</a-button>
          <a-button @click="router.back()">返回上一页</a-button>
        </a-space>
      </template>
    </a-result>
  </div>
</template>

<style scoped>
.not-found {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 60vh;
}
</style>
