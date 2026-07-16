<script setup lang="ts">
import { computed } from 'vue'
import { ConfigProvider, App as AntApp, theme } from 'antdv-next'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()

const themeConfig = computed(() => ({
  algorithm: appStore.isDark ? theme.darkAlgorithm : theme.defaultAlgorithm,
}))
</script>

<template>
  <ConfigProvider :theme="themeConfig">
    <AntApp>
      <RouterView />
    </AntApp>
  </ConfigProvider>
</template>
