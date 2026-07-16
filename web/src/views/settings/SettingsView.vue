<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'antdv-next'
import { getStatus, reloadConfig } from '@/api/system'
import type { StatusSnapshot } from '@/types/api'

const status = ref<StatusSnapshot | null>(null)
const loading = ref(false)
const reloading = ref(false)

const pprofLinks = [
  { label: 'pprof 首页', path: '/debug/pprof/' },
  { label: 'CPU Profile', path: '/debug/pprof/profile' },
  { label: '堆内存', path: '/debug/pprof/heap' },
  { label: 'Goroutine', path: '/debug/pprof/goroutine' },
  { label: '内存分配', path: '/debug/pprof/allocs' },
  { label: '阻塞分析', path: '/debug/pprof/block' },
  { label: '互斥锁', path: '/debug/pprof/mutex' },
  { label: '线程创建', path: '/debug/pprof/threadcreate' },
  { label: '执行追踪', path: '/debug/pprof/trace' },
]

async function fetchStatus() {
  loading.value = true
  try {
    status.value = await getStatus()
  } finally {
    loading.value = false
  }
}

async function handleReload() {
  reloading.value = true
  try {
    await reloadConfig()
    message.success('配置热加载成功')
    await fetchStatus()
  } catch {
    // 错误已由拦截器处理
  } finally {
    reloading.value = false
  }
}

onMounted(fetchStatus)
</script>

<template>
  <a-page-header title="系统设置" style="padding: 0 0 16px 0;" />

  <!-- 配置热加载 -->
  <a-card title="配置热加载" size="small" style="margin-bottom: 16px;">
    <a-space>
      <a-popconfirm title="确定要重新加载配置吗？" @confirm="handleReload">
        <a-button type="primary" :loading="reloading">重新加载配置</a-button>
      </a-popconfirm>
      <a-typography-text type="secondary">
        重新读取 YAML 配置文件并原子替换运行时组件
      </a-typography-text>
    </a-space>
  </a-card>

  <!-- 系统状态 -->
  <a-card title="系统状态" size="small" style="margin-bottom: 16px;">
    <a-spin :spinning="loading">
      <a-descriptions :column="2" size="small" bordered>
        <a-descriptions-item label="运行状态">
          <a-tag :color="status?.status === 'up' ? 'green' : 'red'">
            {{ status?.status || '-' }}
          </a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="代理监听">
          <a-typography-text code>{{ status?.listen || '-' }}</a-typography-text>
        </a-descriptions-item>
        <a-descriptions-item label="管理端口">
          <a-typography-text code>{{ status?.management || '-' }}</a-typography-text>
        </a-descriptions-item>
        <a-descriptions-item label="上游数量">
          {{ status?.upstream_count ?? '-' }}
        </a-descriptions-item>
        <a-descriptions-item label="路由就绪">
          <a-tag :color="status?.router_ready ? 'green' : 'red'">
            {{ status?.router_ready ? '是' : '否' }}
          </a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="管理就绪">
          <a-tag :color="status?.manager_ready ? 'green' : 'red'">
            {{ status?.manager_ready ? '是' : '否' }}
          </a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="pprof">
          <a-tag :color="status?.pprof_enabled ? 'green' : 'default'">
            {{ status?.pprof_enabled ? '已启用' : '未启用' }}
          </a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="时间戳">
          {{ status?.timestamp || '-' }}
        </a-descriptions-item>
      </a-descriptions>
    </a-spin>
  </a-card>

  <!-- pprof 链接 -->
  <a-card title="pprof 性能分析" size="small">
    <a-alert type="warning" show-icon style="margin-bottom: 12px;">
      <template #message>仅在 enable_pprof: true 时可用</template>
      <template #description>
        pprof 端点属于敏感入口，不应默认暴露到公网。点击链接将在新窗口打开。
      </template>
    </a-alert>
    <a-list size="small" :data-source="pprofLinks" bordered>
      <template #renderItem="{ item }">
        <a-list-item>
          <a :href="item.path" target="_blank">{{ item.label }}</a>
          <a-typography-text type="secondary" code style="margin-left: 12px; font-size: 12px;">
            {{ item.path }}
          </a-typography-text>
        </a-list-item>
      </template>
    </a-list>
  </a-card>
</template>
