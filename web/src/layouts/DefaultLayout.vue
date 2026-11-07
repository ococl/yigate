<script setup lang="ts">
import { computed } from 'vue'
import { theme } from 'antdv-next'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useRouter } from 'vue-router'

const appStore = useAppStore()
const authStore = useAuthStore()
const router = useRouter()
const { token } = theme.useToken()

const selectedKeys = computed(() => {
  const name = router.currentRoute.value.name as string
  return [name || '']
})

const openKeys = ref<string[]>(['manage', 'system', 'docs'])

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

function handleLogout() {
  authStore.logout()
  router.push('/login')
}
</script>

<template>
  <a-layout class="app-layout">
    <a-layout-sider
      v-model:collapsed="appStore.sidebarCollapsed"
      :trigger="null"
      collapsible
      :width="216"
      class="app-sider"
      :style="siderStyle"
    >
      <div class="logo">
        <span class="logo-text" :style="{ color: token.colorText }">
          {{ appStore.sidebarCollapsed ? 'Y' : 'YiGate' }}
        </span>
        <span v-if="!appStore.sidebarCollapsed" class="logo-sub" :style="{ color: token.colorTextTertiary }">
          管理后台
        </span>
      </div>
      <div class="nav-scroll">
        <a-menu :selected-keys="selectedKeys" v-model:open-keys="openKeys" mode="inline" class="nav-menu">
          <a-menu-item key="dashboard">
            <RouterLink to="/dashboard">仪表盘</RouterLink>
          </a-menu-item>

          <a-sub-menu key="manage">
            <template #title>管理</template>
            <a-menu-item key="upstreams">
              <RouterLink to="/upstreams">上游管理</RouterLink>
            </a-menu-item>
            <a-menu-item key="rules">
              <RouterLink to="/rules">路由规则</RouterLink>
            </a-menu-item>
            <a-menu-item key="users">
              <RouterLink to="/users">用户管理</RouterLink>
            </a-menu-item>
            <a-menu-item key="traffic">
              <RouterLink to="/traffic">流量审计</RouterLink>
            </a-menu-item>
          </a-sub-menu>

          <a-sub-menu key="system">
            <template #title>系统</template>
            <a-menu-item key="settings">
              <RouterLink to="/settings">系统设置</RouterLink>
            </a-menu-item>
          </a-sub-menu>

          <a-sub-menu key="docs">
            <template #title>文档</template>
            <a-menu-item key="docs-auth">
              <RouterLink to="/docs/auth">认证机制</RouterLink>
            </a-menu-item>
            <a-menu-item key="docs-api">
              <RouterLink to="/docs/api">API 文档</RouterLink>
            </a-menu-item>
          </a-sub-menu>

          <a-menu-item key="about">
            <RouterLink to="/about">关于</RouterLink>
          </a-menu-item>
        </a-menu>
      </div>
    </a-layout-sider>

    <a-layout class="main-layout">
      <a-layout-header class="header" :style="headerStyle">
        <a-button type="text" @click="appStore.toggleSidebar">☰</a-button>

        <div class="header-right">
          <a-tooltip :title="appStore.isDark ? '切换到浅色模式' : '切换到深色模式'">
            <a-button type="text" @click="appStore.toggleTheme">
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

.app-sider :deep(.ant-layout-sider-children) {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.logo {
  flex: 0 0 56px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  border-bottom: 1px solid rgba(128, 128, 128, 0.18);
}

.logo-text {
  font-size: 20px;
  font-weight: 700;
  letter-spacing: 0.5px;
}

.logo-sub {
  font-size: 12px;
}

/* 菜单区域独立滚动，logo 固定在顶部 */
.nav-scroll {
  flex: 1 1 auto;
  min-height: 0;
  padding: 8px 0;
  overflow-x: hidden;
  overflow-y: auto;
  overscroll-behavior: contain;
}

.nav-menu {
  border-inline-end: none !important;
  background: transparent;
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

</style>
