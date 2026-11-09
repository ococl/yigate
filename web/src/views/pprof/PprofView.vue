<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { message } from 'antdv-next'
import { getSettings, updateSettings } from '@/api/settings'

interface PprofLink {
  label: string
  path: string
  description: string
}

const pprofGroups: { title: string; items: PprofLink[] }[] = [
  {
    title: '概览',
    items: [
      {
        label: 'pprof 首页',
        path: '/debug/pprof/',
        description: '查看所有可用的运行时 profile 与当前 goroutine 概览',
      },
    ],
  },
  {
    title: 'CPU 与执行追踪',
    items: [
      {
        label: 'CPU Profile',
        path: '/debug/pprof/profile?seconds=30',
        description: '采集 30 秒 CPU 使用情况，适合定位计算热点',
      },
      {
        label: '执行追踪',
        path: '/debug/pprof/trace?seconds=5',
        description: '采集 5 秒运行时事件，适合分析调度与阻塞',
      },
    ],
  },
  {
    title: '内存',
    items: [
      {
        label: '堆内存',
        path: '/debug/pprof/heap',
        description: '当前堆内存分配快照，定位内存驻留',
      },
      {
        label: '内存分配',
        path: '/debug/pprof/allocs',
        description: '累计内存分配采样，定位频繁分配',
      },
    ],
  },
  {
    title: '并发与阻塞',
    items: [
      {
        label: 'Goroutine',
        path: '/debug/pprof/goroutine',
        description: '全部 goroutine 调用栈，排查泄漏与卡死',
      },
      {
        label: '阻塞分析',
        path: '/debug/pprof/block',
        description: '同步原语导致的阻塞事件',
      },
      {
        label: '互斥锁',
        path: '/debug/pprof/mutex',
        description: '互斥锁竞争情况',
      },
    ],
  },
  {
    title: '运行时',
    items: [
      {
        label: '线程创建',
        path: '/debug/pprof/threadcreate',
        description: '操作系统线程创建情况',
      },
    ],
  },
]

const loaded = ref(false)
const saving = ref(false)
// 开关值来自服务端；读取失败时开关保持禁用，并明确告知原因
const loadError = ref(false)
// 当前生效值来自服务端，表单值保存成功后同步
const enabled = ref(false)

async function fetchSettings() {
  try {
    const settings = await getSettings()
    enabled.value = settings.enable_pprof
    loaded.value = true
    loadError.value = false
  } catch {
    loadError.value = true
  }
}

async function handleToggle(checked: boolean) {
  const previous = enabled.value
  enabled.value = checked
  saving.value = true
  try {
    await updateSettings({ enable_pprof: checked })
    message.success(checked ? 'pprof 调试端点已启用' : 'pprof 调试端点已关闭')
  } catch {
    enabled.value = previous
  } finally {
    saving.value = false
  }
}

onMounted(fetchSettings)
</script>

<template>
  <div class="page-shell pprof-view">
    <div class="page-header">
      <div>
        <a-typography-title :level="4" style="margin: 0;">pprof 性能分析</a-typography-title>
        <a-typography-text type="secondary">
          通过管理端口访问 Go 运行时性能采集端点，用于排查 CPU、内存与阻塞问题。
        </a-typography-text>
      </div>
      <a-tag :color="enabled ? 'green' : 'default'">{{ enabled ? '已启用' : '未启用' }}</a-tag>
    </div>

    <div class="pprof-body">
      <!-- 左侧：开关、安全提示与使用方式 -->
      <aside class="pprof-aside">
        <a-card size="small" title="采集开关" class="aside-card">
          <a-form layout="vertical">
            <a-form-item label="pprof 调试端点" tooltip="启用后可在管理端口访问 /debug/pprof/">
              <div class="switch-row">
                <a-switch
                  :checked="enabled"
                  :loading="saving"
                  :disabled="!loaded"
                  checked-children="启用"
                  un-checked-children="关闭"
                  @change="(value: boolean) => handleToggle(value)"
                />
                <a-typography-text v-if="loadError" type="danger" class="switch-hint">
                  读取开关状态失败
                  <a-button type="link" size="small" style="padding: 0 4px;" @click="fetchSettings">重试</a-button>
                </a-typography-text>
                <a-typography-text v-else type="secondary" class="switch-hint">保存后立即生效</a-typography-text>
              </div>
            </a-form-item>
          </a-form>

          <a-alert
            type="warning"
            show-icon
            message="pprof 属于敏感调试入口"
            description="仅在管理端口绑定回环地址或受信网络时启用，请勿直接暴露到公网。采集期间会带来额外性能开销。"
          />

          <div class="usage">
            <a-typography-text strong>使用方式</a-typography-text>
            <a-typography-paragraph type="secondary" class="usage-desc">
              以本机管理端口 8890 为例，采集结果下载后可用
              <a-typography-text code>go tool pprof</a-typography-text> 分析。
            </a-typography-paragraph>
            <div class="code-block">
              <div class="code-block-header"><span>采集 CPU Profile（30 秒）</span></div>
              <pre><code>curl -o cpu.pprof "http://127.0.0.1:8890/debug/pprof/profile?seconds=30"</code></pre>
            </div>
            <div class="code-block">
              <div class="code-block-header"><span>本地分析</span></div>
              <pre><code>go tool pprof -http=:8081 cpu.pprof</code></pre>
            </div>
            <a-typography-paragraph type="secondary" class="usage-desc">
              若管理令牌已启用，请在请求中携带
              <a-typography-text code>Authorization: Bearer &lt;token&gt;</a-typography-text>；
              浏览器直接打开链接时需先通过管理端认证。
            </a-typography-paragraph>
          </div>
        </a-card>
      </aside>

      <!-- 右侧：按类别列出全部端点 -->
      <section class="pprof-detail">
        <div class="pprof-detail-scroll">
          <a-alert
            v-if="loaded && !enabled"
            type="info"
            show-icon
            style="margin-bottom: 16px;"
            message="pprof 尚未启用"
            description="开启左侧开关后，下方入口才可访问。"
          />
          <a-card
            v-for="group in pprofGroups"
            :key="group.title"
            size="small"
            :title="group.title"
            class="group-card"
          >
            <div class="endpoint-list">
              <div v-for="item in group.items" :key="item.path" class="endpoint-item">
                <div class="endpoint-main">
                  <a :href="item.path" target="_blank" rel="noopener noreferrer">{{ item.label }}</a>
                  <a-typography-text type="secondary" class="endpoint-desc">
                    {{ item.description }}
                  </a-typography-text>
                </div>
                <div class="endpoint-actions">
                  <a-typography-text code class="endpoint-path">{{ item.path }}</a-typography-text>
                  <a :href="item.path" target="_blank" rel="noopener noreferrer">打开</a>
                </div>
              </div>
            </div>
          </a-card>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.pprof-view {
  width: 100%;
}

.pprof-body {
  flex: 1 1 auto;
  display: flex;
  gap: 16px;
  min-height: 0;
}

/* 左侧固定宽度，内容超出时卡片内部滚动 */
.pprof-aside {
  flex: 0 0 360px;
  width: 360px;
  min-width: 360px;
  height: 100%;
  min-height: 0;
}

.aside-card {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.aside-card :deep(.ant-card-body) {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
}

.switch-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.switch-hint {
  font-size: 12px;
}

.usage {
  margin-top: 16px;
}

.usage-desc {
  font-size: 12px;
  margin: 8px 0;
}

.code-block {
  margin-bottom: 12px;
  border-radius: 6px;
  overflow: hidden;
  background: rgba(127, 127, 127, 0.08);
}

.code-block-header {
  padding: 4px 12px;
  font-size: 12px;
  color: rgba(128, 128, 128, 0.9);
  border-bottom: 1px solid rgba(128, 128, 128, 0.18);
}

.code-block pre {
  margin: 0;
  padding: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.6;
  overflow-x: auto;
}

.pprof-detail {
  flex: 1 1 auto;
  min-width: 0;
  height: 100%;
  min-height: 0;
}

.pprof-detail-scroll {
  height: 100%;
  overflow-x: hidden;
  overflow-y: auto;
  padding-right: 4px;
}

.group-card {
  margin-bottom: 16px;
}

.endpoint-list {
  display: flex;
  flex-direction: column;
}

.endpoint-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 0;
}

.endpoint-item + .endpoint-item {
  border-top: 1px dashed rgba(128, 128, 128, 0.18);
}

.endpoint-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.endpoint-desc {
  font-size: 12px;
}

.endpoint-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 0 0 auto;
}

.endpoint-path {
  font-size: 12px;
}

@media (max-width: 992px) {
  .pprof-body {
    flex-direction: column;
  }

  .pprof-aside {
    flex: 0 0 auto;
    width: 100%;
    min-width: 0;
    height: auto;
  }

  .aside-card :deep(.ant-card-body) {
    overflow-y: visible;
  }

  .pprof-detail {
    height: auto;
  }

  .pprof-detail-scroll {
    height: auto;
    overflow-y: visible;
  }
}
</style>
