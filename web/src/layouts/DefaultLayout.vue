<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { theme } from 'antdv-next'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useRouter } from 'vue-router'
import SidebarContent from './SidebarContent.vue'

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
            :aria-label="isMobile ? '打开导航菜单' : '收起或展开侧栏'"
            @click="toggleNavigation"
          >
            ☰
          </a-button>
          <span v-if="isMobile" class="mobile-brand">YiGate</span>
        </div>

        <div class="header-right">
          <a-tooltip :title="appStore.isDark ? '切换到浅色模式' : '切换到深色模式'">
            <a-button
              type="text"
              :aria-label="appStore.isDark ? '切换到浅色模式' : '切换到深色模式'"
              @click="appStore.toggleTheme"
            >
              {{ appStore.isDark ? '☀️' : '🌙' }}
            </a-button>
          </a-tooltip>
          <a-popconfirm title="确定要退出登录吗？" ok-text="退出" cancel-text="取消" @confirm="handleLogout">
            <a-button type="text">退出</a-button>
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
