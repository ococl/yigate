<script setup lang="ts">
import { computed, watch, onMounted } from 'vue'
import { ConfigProvider, App as AntApp, theme } from 'antdv-next'
import zhCN from 'antdv-next/locale/zh_CN'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()

// 侧栏选中项默认是「主色文字 + 浅主色底」，暗色下蓝字压深蓝底只有 2.47:1。
// 这里把选中态文字换成更深/更亮的一档，两个主题都过 4.5:1。
const themeConfig = computed(() => ({
  algorithm: appStore.isDark ? theme.darkAlgorithm : theme.defaultAlgorithm,
  components: {
    Menu: appStore.isDark
      ? { colorPrimary: '#69b1ff', itemSelectedColor: '#69b1ff', itemSelectedBg: '#15325b' }
      : { colorPrimary: '#0958d9', itemSelectedColor: '#0958d9', itemSelectedBg: '#e6f4ff' },
  },
}))

// 在 <body> 上标记暗黑状态，便于全局 CSS 与第三方组件做主题适配
function syncBodyClass() {
  document.body.classList.toggle('app-dark', appStore.isDark)
}

onMounted(syncBodyClass)
watch(() => appStore.isDark, syncBodyClass)
</script>

<template>
  <ConfigProvider :theme="themeConfig" :locale="zhCN">
    <AntApp>
      <RouterView />
    </AntApp>
  </ConfigProvider>
</template>
