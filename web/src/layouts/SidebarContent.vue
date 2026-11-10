<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { theme } from 'antdv-next'
import { useI18n } from 'vue-i18n'

withDefaults(defineProps<{ collapsed?: boolean }>(), { collapsed: false })

const route = useRoute()
const { t } = useI18n()
const { token } = theme.useToken()
const openKeys = ref<string[]>(['manage', 'system', 'help'])

const selectedKeys = computed(() => {
  const name = route.name as string
  return [name || '']
})
</script>

<template>
  <div class="sidebar-content" :style="{ background: token.colorBgContainer }">
    <div class="logo">
      <span class="logo-text" :style="{ color: token.colorText }">
        {{ collapsed ? 'Y' : 'YiGate' }}
      </span>
    </div>
    <div class="nav-scroll">
      <a-menu :selected-keys="selectedKeys" v-model:open-keys="openKeys" mode="inline" class="nav-menu">
        <a-menu-item key="dashboard">
          <RouterLink to="/dashboard" data-testid="nav-dashboard">{{ t('nav.dashboard') }}</RouterLink>
        </a-menu-item>

        <a-sub-menu key="manage">
          <template #title>{{ t('nav.manage') }}</template>
          <a-menu-item key="upstreams">
            <RouterLink to="/upstreams" data-testid="nav-upstreams">{{ t('nav.upstreams') }}</RouterLink>
          </a-menu-item>
          <a-menu-item key="rules">
            <RouterLink to="/rules" data-testid="nav-rules">{{ t('nav.rules') }}</RouterLink>
          </a-menu-item>
          <a-menu-item key="users">
            <RouterLink to="/users" data-testid="nav-users">{{ t('nav.users') }}</RouterLink>
          </a-menu-item>
          <a-menu-item key="traffic">
            <RouterLink to="/traffic" data-testid="nav-traffic">{{ t('nav.traffic') }}</RouterLink>
          </a-menu-item>
        </a-sub-menu>

        <a-sub-menu key="system">
          <template #title>{{ t('nav.system') }}</template>
          <a-menu-item key="settings">
            <RouterLink to="/settings" data-testid="nav-settings">{{ t('nav.settings') }}</RouterLink>
          </a-menu-item>
          <a-menu-item key="geosite">
            <RouterLink to="/geosite" data-testid="nav-geosite">{{ t('nav.geosite') }}</RouterLink>
          </a-menu-item>
          <a-menu-item key="pprof">
            <RouterLink to="/pprof" data-testid="nav-pprof">{{ t('nav.pprof') }}</RouterLink>
          </a-menu-item>
        </a-sub-menu>

        <a-sub-menu key="help">
          <template #title>{{ t('nav.help') }}</template>
          <a-menu-item key="docs-auth">
            <RouterLink to="/docs/auth" data-testid="nav-docs-auth">{{ t('nav.authDoc') }}</RouterLink>
          </a-menu-item>
          <a-menu-item key="docs-api">
            <RouterLink to="/docs/api" data-testid="nav-docs-api">{{ t('nav.apiDoc') }}</RouterLink>
          </a-menu-item>
          <a-menu-item key="docs-llmx">
            <RouterLink to="/docs/llmx" data-testid="nav-docs-llmx">{{ t('nav.llmx') }}</RouterLink>
          </a-menu-item>
          <a-menu-item key="about">
            <RouterLink to="/about" data-testid="nav-about">{{ t('nav.about') }}</RouterLink>
          </a-menu-item>
        </a-sub-menu>
      </a-menu>
    </div>
  </div>
</template>

<style scoped>
.sidebar-content {
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
</style>
