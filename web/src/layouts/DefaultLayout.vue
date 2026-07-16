<script setup lang="ts">
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()

const menuItems = [
  { key: 'home', label: '首页', path: '/' },
  { key: 'about', label: '关于', path: '/about' },
]
</script>

<template>
  <a-layout style="min-height: 100vh">
    <a-layout-sider v-model:collapsed="appStore.sidebarCollapsed" :trigger="null" collapsible>
      <div class="logo" />
      <a-menu theme="dark" mode="inline" :selected-keys="[$route.name as string]">
        <a-menu-item v-for="item in menuItems" :key="item.key">
          <RouterLink :to="item.path">{{ item.label }}</RouterLink>
        </a-menu-item>
      </a-menu>
    </a-layout-sider>

    <a-layout>
      <a-layout-header style="background: #fff; padding: 0 16px; display: flex; align-items: center;">
        <a-button
          type="text"
          :icon="appStore.sidebarCollapsed ? 'menu-unfold-outlined' : 'menu-fold-outlined'"
          @click="appStore.toggleSidebar"
        />
      </a-layout-header>

      <a-layout-content style="margin: 16px;">
        <div style="background: #fff; padding: 24px; min-height: 360px; border-radius: 8px;">
          <RouterView />
        </div>
      </a-layout-content>

      <a-layout-footer style="text-align: center;">
        YiGate &copy; {{ new Date().getFullYear() }}
      </a-layout-footer>
    </a-layout>
  </a-layout>
</template>

<style scoped>
.logo {
  height: 32px;
  margin: 16px;
  background: rgba(255, 255, 255, 0.3);
  border-radius: 6px;
}
</style>
