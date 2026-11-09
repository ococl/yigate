import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { readStoredLocale, setLocale, type AppLocale } from '@/locales'

const THEME_KEY = 'yigate_theme'

export const useAppStore = defineStore('app', () => {
  const sidebarCollapsed = ref(false)
  const theme = ref<'light' | 'dark'>(
    (localStorage.getItem(THEME_KEY) as 'light' | 'dark') || 'light',
  )
  // 语言状态与 vue-i18n 共用同一份存储键，刷新后保持一致
  const locale = ref<AppLocale>(readStoredLocale())

  const isDark = computed(() => theme.value === 'dark')

  function toggleSidebar() {
    sidebarCollapsed.value = !sidebarCollapsed.value
  }

  function toggleTheme() {
    theme.value = theme.value === 'light' ? 'dark' : 'light'
    localStorage.setItem(THEME_KEY, theme.value)
  }

  function setAppLocale(next: AppLocale) {
    locale.value = next
    setLocale(next)
  }

  return {
    sidebarCollapsed,
    theme,
    isDark,
    locale,
    toggleSidebar,
    toggleTheme,
    setAppLocale,
  }
})
