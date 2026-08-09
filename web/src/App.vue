<script setup lang="ts">
import { computed, watch, onMounted } from 'vue'
import { ConfigProvider, App as AntApp, theme } from 'antdv-next'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()

const themeConfig = computed(() => ({
  algorithm: appStore.isDark ? theme.darkAlgorithm : theme.defaultAlgorithm,
}))

// 在 <body> 上标记暗黑状态，便于全局 CSS 与第三方组件做主题适配
function syncBodyClass() {
  document.body.classList.toggle('app-dark', appStore.isDark)
}

onMounted(syncBodyClass)
watch(() => appStore.isDark, syncBodyClass)
</script>

<template>
  <ConfigProvider :theme="themeConfig">
    <AntApp>
      <RouterView />
    </AntApp>
  </ConfigProvider>
</template>
