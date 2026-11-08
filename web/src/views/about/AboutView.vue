<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { getVersion } from '@/api/system'
import type { VersionInfo } from '@/types/api'
import { formatUptime } from '@/utils/format'

const info = ref<VersionInfo | null>(null)
const error = ref('')

onMounted(async () => {
  try {
    info.value = await getVersion()
  } catch {
    error.value = '无法获取版本信息'
  }
})
</script>

<template>
  <div class="page-header">
    <div>
      <a-typography-title :level="4" style="margin: 0;">关于</a-typography-title>
      <a-typography-text type="secondary">YiGate — 轻量级统一代理网关</a-typography-text>
    </div>
  </div>

  <a-alert v-if="error" type="warning" show-icon :message="error" style="margin-bottom: 16px;" />

  <a-descriptions title="运行版本" :column="2" bordered size="small">
    <a-descriptions-item label="版本">{{ info?.version || '—' }}</a-descriptions-item>
    <a-descriptions-item label="提交">{{ info?.commit || '—' }}</a-descriptions-item>
    <a-descriptions-item label="构建时间">{{ info?.build_time || '—' }}</a-descriptions-item>
    <a-descriptions-item label="Go 版本">{{ info?.go_version || '—' }}</a-descriptions-item>
    <a-descriptions-item label="平台">{{ info?.platform || '—' }}</a-descriptions-item>
    <a-descriptions-item label="运行时长">{{ formatUptime(info?.uptime_seconds || 0) }}</a-descriptions-item>
  </a-descriptions>

  <a-descriptions title="项目信息" :column="1" bordered size="small" style="margin-top: 24px;">
    <a-descriptions-item label="技术栈">Go + Vue 3 + antdv-next</a-descriptions-item>
    <a-descriptions-item label="协议支持">HTTP / HTTPS CONNECT / SOCKS5 / SOCKS5H</a-descriptions-item>
    <a-descriptions-item label="管理能力">上游、路由规则、用户、流量审计与全局设置</a-descriptions-item>
  </a-descriptions>
</template>
