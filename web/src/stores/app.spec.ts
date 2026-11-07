import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAppStore } from './app'

describe('app store', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
  })

  it('toggles the sidebar', () => {
    const store = useAppStore()

    expect(store.sidebarCollapsed).toBe(false)
    store.toggleSidebar()
    expect(store.sidebarCollapsed).toBe(true)
  })

  it('persists theme changes', () => {
    const store = useAppStore()

    expect(store.isDark).toBe(false)
    store.toggleTheme()

    expect(store.theme).toBe('dark')
    expect(store.isDark).toBe(true)
    expect(localStorage.getItem('yigate_theme')).toBe('dark')
  })

  it('restores the saved theme', () => {
    localStorage.setItem('yigate_theme', 'dark')
    setActivePinia(createPinia())

    const store = useAppStore()

    expect(store.theme).toBe('dark')
    expect(store.isDark).toBe(true)
  })
})
