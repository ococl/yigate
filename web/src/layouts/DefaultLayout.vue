<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { theme } from 'antdv-next'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { LOCALE_OPTIONS } from '@/locales'
import SidebarContent from './SidebarContent.vue'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const router = useRouter()
const { token } = theme.useToken()

const isMobile = ref(false)
const mobileNavOpen = ref(false)
let mediaQuery: MediaQueryList | null = null

function syncViewport(event: MediaQueryList | MediaQueryListEvent) {
  isMobile.value = event.matches
  if (!event.matches) mobileNavOpen.value = false
}

onMounted(() => {
  mediaQuery = window.matchMedia('(max-width: 768px)')
  syncViewport(mediaQuery)
  mediaQuery.addEventListener('change', syncViewport)
})

onUnmounted(() => {
  mediaQuery?.removeEventListener('change', syncViewport)
})

// 移动端点击菜单跳转后自动收起抽屉
watch(
  () => router.currentRoute.value.fullPath,
  () => {
    mobileNavOpen.value = false
  },
)

const siderStyle = computed(() => ({
  background: token.value.colorBgContainer,
  borderRight: `1px solid ${token.value.colorBorderSecondary}`,
}))

const headerStyle = computed(() => ({
  background: token.value.colorBgContainer,
  borderBottom: `1px solid ${token.value.colorBorderSecondary}`,
  color: token.value.colorText,
}))

const contentStyle = computed(() => ({
  background: token.value.colorBgContainer,
  color: token.value.colorText,
}))

function toggleNavigation() {
  if (isMobile.value) {
    mobileNavOpen.value = !mobileNavOpen.value
    return
  }
  appStore.toggleSidebar()
}

function handleLogout() {
  authStore.logout()
  router.push('/login')
}

function handleLocaleSelect(info: { key: string | number }) {
  const next = LOCALE_OPTIONS.find((item) => item.value === String(info.key))
  if (next) appStore.setAppLocale(next.value)
}

// 语言下拉用 menu 数据结构传给 Dropdown，选中态跟随当前语言
const localeMenu = computed(() => ({
  items: LOCALE_OPTIONS.map((option) => ({ key: option.value, label: option.label })),
  selectedKeys: [appStore.locale],
  onClick: handleLocaleSelect,
}))
</script>

<template>
  <a-layout class="app-layout">
    <a-layout-sider
      v-if="!isMobile"
      v-model:collapsed="appStore.sidebarCollapsed"
      :trigger="null"
      collapsible
      :width="216"
      class="app-sider"
      :style="siderStyle"
    >
      <SidebarContent :collapsed="appStore.sidebarCollapsed" />
    </a-layout-sider>

    <a-drawer
      v-else
      :open="mobileNavOpen"
      placement="left"
      :width="216"
      :closable="false"
      :body-style="{ padding: 0, height: '100%' }"
      root-class-name="mobile-nav-drawer"
      @close="mobileNavOpen = false"
    >
      <SidebarContent />
    </a-drawer>

    <a-layout class="main-layout">
      <a-layout-header class="header" :style="headerStyle">
        <div class="header-left">
          <a-button
            type="text"
            data-testid="nav-toggle"
            :aria-label="isMobile ? t('layout.openNav') : t('layout.toggleSidebar')"
            @click="toggleNavigation"
          >
            ☰
          </a-button>
          <span v-if="isMobile" class="mobile-brand">YiGate</span>
        </div>

        <div class="header-right">
          <a-dropdown placement="bottomRight" :menu="localeMenu">
            <a-button type="text" :aria-label="t('layout.language')">
              {{ appStore.locale === 'zh-CN' ? '中' : 'EN' }}
            </a-button>
          </a-dropdown>
          <a-tooltip :title="appStore.isDark ? t('layout.toLight') : t('layout.toDark')">
            <a-button
              type="text"
              :aria-label="appStore.isDark ? t('layout.toLight') : t('layout.toDark')"
              @click="appStore.toggleTheme"
            >
              <span class="theme-icon" aria-hidden="true">
                <svg v-if="appStore.isDark" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                  <circle cx="12" cy="12" r="4" />
                  <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41" />
                </svg>
                <svg v-else viewBox="0 0 24 24" fill="currentColor">
                  <path d="M21 12.79A9 9 0 1 1 11.21 3a7 7 0 0 0 9.79 9.79z" />
                </svg>
              </span>
            </a-button>
          </a-tooltip>
          <a-popconfirm
            :title="t('layout.logoutConfirm')"
            :ok-text="t('layout.logoutOk')"
            :cancel-text="t('common.cancel')"
            @confirm="handleLogout"
          >
            <a-button type="text">{{ t('layout.logout') }}</a-button>
          </a-popconfirm>
        </div>
      </a-layout-header>

      <a-layout-content class="content" :style="contentStyle">
        <RouterView />
      </a-layout-content>
    </a-layout>
  </a-layout>
</template>

<style scoped>
/* 根布局撑满视口并禁止整体滚动，滚动交给侧栏与工作区各自处理 */
.app-layout {
  height: 100vh;
  height: 100dvh;
  overflow: hidden;
}

.app-sider {
  height: 100%;
}

.main-layout {
  height: 100%;
  min-width: 0;
  overflow: hidden;
}

/* 页头锁定，不随工作区滚动 */
.header {
  flex: 0 0 56px;
  padding: 0 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 56px;
  line-height: 56px;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
}

.mobile-brand {
  font-size: 17px;
  font-weight: 700;
  letter-spacing: 0.3px;
  white-space: nowrap;
}

.theme-icon {
  display: inline-flex;
  width: 16px;
  height: 16px;
}

.theme-icon svg {
  width: 100%;
  height: 100%;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* 工作区独立滚动 */
.content {
  flex: 1 1 auto;
  min-height: 0;
  margin: 16px;
  padding: 24px;
  border-radius: 8px;
  overflow-x: auto;
  overflow-y: auto;
  overscroll-behavior: contain;
}

@media (max-width: 768px) {
  .header {
    padding: 0 8px;
  }

  .header-right {
    gap: 4px;
  }

  .content {
    margin: 8px;
    padding: 12px;
    border-radius: 6px;
  }
}
</style>
