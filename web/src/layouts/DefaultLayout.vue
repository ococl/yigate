<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useRouter } from 'vue-router'

const appStore = useAppStore()
const authStore = useAuthStore()
const router = useRouter()

const selectedKeys = computed(() => {
  const name = router.currentRoute.value.name as string
  return [name || '']
})

const openKeys = ref<string[]>(['manage', 'system', 'docs'])

function handleLogout() {
  authStore.logout()
  router.push('/login')
}
</script>

<template>
  <a-layout style="min-height: 100vh">
    <a-layout-sider
      v-model:collapsed="appStore.sidebarCollapsed"
      :trigger="null"
      collapsible
      :theme="appStore.isDark ? 'dark' : 'dark'"
    >
      <div class="logo">
        <span v-if="!appStore.sidebarCollapsed" class="logo-text">YiGate</span>
        <span v-else class="logo-text">Y</span>
      </div>
      <a-menu
        :selected-keys="selectedKeys"
        v-model:open-keys="openKeys"
        theme="dark"
        mode="inline"
      >
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
    </a-layout-sider>

    <a-layout>
      <a-layout-header class="header">
        <a-button
          type="text"
          @click="appStore.toggleSidebar"
        >
          <span v-if="appStore.sidebarCollapsed">&#9776;</span>
          <span v-else>&#9776;</span>
        </a-button>

        <div class="header-right">
          <a-button type="text" @click="appStore.toggleTheme">
            {{ appStore.isDark ? '☀️' : '🌙' }}
          </a-button>
          <a-popconfirm title="确定要退出登录吗？" @confirm="handleLogout">
            <a-button type="text">退出</a-button>
          </a-popconfirm>
        </div>
      </a-layout-header>

      <a-layout-content class="content">
        <RouterView />
      </a-layout-content>

      <a-layout-footer style="text-align: center;">
        YiGate &copy; {{ new Date().getFullYear() }}
      </a-layout-footer>
    </a-layout>
  </a-layout>
</template>

<style scoped>
.logo {
  height: 48px;
  display: flex;
  align-items: center;
  justify-content: center;
  margin: 8px 0;
}

.logo-text {
  color: #fff;
  font-size: 20px;
  font-weight: bold;
}

.header {
  background: #fff;
  padding: 0 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.08);
}

.header-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.content {
  margin: 16px;
  padding: 24px;
  min-height: 360px;
  background: #fff;
  border-radius: 8px;
}
</style>
