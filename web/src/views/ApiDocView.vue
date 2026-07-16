<script setup lang="ts">
import { ref, computed } from 'vue'
import { apiGroups, authInfo, type ApiEndpoint } from '@/data/api-docs'

const activeGroup = ref<string>('system')
const searchKeyword = ref('')

const methodColor: Record<string, string> = {
  GET: 'green',
  POST: 'blue',
  PUT: 'orange',
  DELETE: 'red',
}

const filteredGroups = computed(() => {
  if (!searchKeyword.value.trim()) return apiGroups
  const kw = searchKeyword.value.toLowerCase()
  return apiGroups
    .map((group) => ({
      ...group,
      endpoints: group.endpoints.filter(
        (ep) =>
          ep.path.toLowerCase().includes(kw) ||
          ep.summary.toLowerCase().includes(kw) ||
          ep.description.toLowerCase().includes(kw),
      ),
    }))
    .filter((group) => group.endpoints.length > 0)
})

const totalEndpoints = computed(() =>
  apiGroups.reduce((sum, g) => sum + g.endpoints.length, 0),
)

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text).then(() => {
    // 使用 antdv-next message 提示
    window.dispatchEvent(new CustomEvent('copy-success'))
  })
}
</script>

<template>
  <div class="api-docs">
    <!-- 页面标题 -->
    <a-page-header
      title="API 文档"
      sub-title="YiGate 管理接口完整说明"
      style="padding: 0 0 16px 0;"
    >
      <template #extra>
        <a-tag color="blue">{{ totalEndpoints }} 个端点</a-tag>
        <a-tag color="purple">{{ apiGroups.length }} 个分组</a-tag>
      </template>
    </a-page-header>

    <!-- 认证说明 -->
    <a-card title="认证机制" size="small" style="margin-bottom: 16px;">
      <a-descriptions :column="1" size="small" bordered>
        <a-descriptions-item label="认证方式">
          <a-tag color="blue">{{ authInfo.method }}</a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="请求头">
          <a-typography-text code copyable>{{ authInfo.header }}</a-typography-text>
        </a-descriptions-item>
        <a-descriptions-item label="配置字段">
          <a-typography-text code>{{ authInfo.configField }}</a-typography-text>
        </a-descriptions-item>
        <a-descriptions-item label="说明">{{ authInfo.description }}</a-descriptions-item>
        <a-descriptions-item label="暴力破解防护">
          <a-tag color="orange">{{ authInfo.bruteForceProtection }}</a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="未认证响应">
          <a-typography-text type="danger">
            HTTP {{ authInfo.unauthorizedResponse.status }} — {{ authInfo.unauthorizedResponse.body }}
          </a-typography-text>
        </a-descriptions-item>
      </a-descriptions>
    </a-card>

    <!-- 搜索框 -->
    <a-input-search
      v-model:value="searchKeyword"
      placeholder="搜索端点路径、名称或描述..."
      style="margin-bottom: 16px;"
      allow-clear
    />

    <a-row :gutter="16">
      <!-- 左侧分组导航 -->
      <a-col :span="5">
        <a-card size="small" style="position: sticky; top: 16px;">
          <template #title>接口分组</template>
          <a-menu
            :selected-keys="[activeGroup]"
            mode="inline"
            style="border: none;"
          >
            <a-menu-item
              v-for="group in filteredGroups"
              :key="group.key"
              @click="activeGroup = group.key"
            >
              <a-badge
                :count="group.endpoints.length"
                :number-style="{ backgroundColor: '#1677ff' }"
                :offset="[10, 0]"
              >
                {{ group.title }}
              </a-badge>
            </a-menu-item>
          </a-menu>
        </a-card>
      </a-col>

      <!-- 右侧端点详情 -->
      <a-col :span="19">
        <template v-for="group in filteredGroups" :key="group.key">
          <div v-show="activeGroup === group.key">
            <!-- 分组描述 -->
            <a-alert
              :message="group.title"
              :description="group.description"
              type="info"
              show-icon
              style="margin-bottom: 16px;"
            />

            <!-- 端点列表 -->
            <a-card
              v-for="endpoint in group.endpoints"
              :key="endpoint.id"
              size="small"
              style="margin-bottom: 16px;"
            >
              <!-- 端点标题行 -->
              <template #title>
                <a-space>
                  <a-tag :color="methodColor[endpoint.method]" style="min-width: 56px; text-align: center; font-weight: bold;">
                    {{ endpoint.method }}
                  </a-tag>
                  <a-typography-text code copyable style="font-size: 14px;">
                    {{ endpoint.path }}
                  </a-typography-text>
                </a-space>
              </template>

              <!-- 端点摘要 -->
              <a-typography-title :level="5" style="margin-top: 0;">
                {{ endpoint.summary }}
              </a-typography-title>
              <a-typography-paragraph type="secondary">
                {{ endpoint.description }}
              </a-typography-paragraph>

              <!-- 认证标记 -->
              <a-space style="margin-bottom: 12px;">
                <a-tag :color="endpoint.auth ? 'red' : 'green'">
                  {{ endpoint.auth ? '需要认证' : '无需认证' }}
                </a-tag>
                <a-typography-text type="secondary" v-if="endpoint.authNote">
                  {{ endpoint.authNote }}
                </a-typography-text>
              </a-space>

              <!-- 路径参数 -->
              <template v-if="endpoint.params.length > 0">
                <a-divider orientation="left" plain style="margin: 8px 0;">
                  <a-typography-text type="secondary">路径参数</a-typography-text>
                </a-divider>
                <a-table
                  :data-source="endpoint.params"
                  :pagination="false"
                  size="small"
                  :columns="[
                    { title: '参数名', dataIndex: 'name', width: 120 },
                    { title: '类型', dataIndex: 'type', width: 140 },
                    { title: '必填', dataIndex: 'required', width: 60, customRender: ({ text }: { text: boolean }) => text ? '是' : '否' },
                    { title: '说明', dataIndex: 'description' },
                  ]"
                  :row-key="(r: any) => r.name"
                />
              </template>

              <!-- 请求体 -->
              <template v-if="endpoint.requestBody">
                <a-divider orientation="left" plain style="margin: 8px 0;">
                  <a-typography-text type="secondary">请求体 (JSON)</a-typography-text>
                </a-divider>
                <div class="code-block">
                  <div class="code-block-header">
                    <span>Request Body</span>
                    <a-button type="text" size="small" @click="copyToClipboard(endpoint.requestBody!)">
                      复制
                    </a-button>
                  </div>
                  <pre><code>{{ endpoint.requestBody }}</code></pre>
                </div>
              </template>

              <!-- 响应体 -->
              <a-divider orientation="left" plain style="margin: 8px 0;">
                <a-typography-text type="secondary">响应体 (JSON)</a-typography-text>
              </a-divider>
              <div class="code-block">
                <div class="code-block-header">
                  <span>Response Body</span>
                  <a-button type="text" size="small" @click="copyToClipboard(endpoint.responseBody)">
                    复制
                  </a-button>
                </div>
                <pre><code>{{ endpoint.responseBody }}</code></pre>
              </div>

              <!-- 状态码 -->
              <a-divider orientation="left" plain style="margin: 8px 0;">
                <a-typography-text type="secondary">状态码</a-typography-text>
              </a-divider>
              <a-table
                :data-source="endpoint.statusCodes"
                :pagination="false"
                size="small"
                :columns="[
                  { title: '状态码', dataIndex: 'code', width: 80 },
                  { title: '说明', dataIndex: 'description' },
                ]"
                :row-key="(r: any) => r.code"
              >
                <template #bodyCell="{ column, record }">
                  <template v-if="column.dataIndex === 'code'">
                    <a-tag
                      :color="record.code < 300 ? 'green' : record.code < 500 ? 'orange' : 'red'"
                    >
                      {{ record.code }}
                    </a-tag>
                  </template>
                </template>
              </a-table>

              <!-- 示例 -->
              <a-divider orientation="left" plain style="margin: 8px 0;">
                <a-typography-text type="secondary">请求示例</a-typography-text>
              </a-divider>
              <div class="code-block">
                <div class="code-block-header">
                  <span>cURL</span>
                  <a-button
                    type="text"
                    size="small"
                    v-if="endpoint.example.request"
                    @click="copyToClipboard(endpoint.example.request)"
                  >
                    复制
                  </a-button>
                </div>
                <pre><code>{{ endpoint.example.request }}</code></pre>
              </div>

              <a-divider orientation="left" plain style="margin: 8px 0;">
                <a-typography-text type="secondary">响应示例</a-typography-text>
              </a-divider>
              <div class="code-block">
                <div class="code-block-header">
                  <span>Response</span>
                  <a-button type="text" size="small" @click="copyToClipboard(endpoint.example.response)">
                    复制
                  </a-button>
                </div>
                <pre><code>{{ endpoint.example.response }}</code></pre>
              </div>
            </a-card>
          </div>
        </template>

        <!-- 无搜索结果 -->
        <a-empty
          v-if="filteredGroups.length === 0"
          description="未找到匹配的端点"
          style="margin-top: 60px;"
        />
      </a-col>
    </a-row>
  </div>
</template>

<style scoped>
.api-docs {
  max-width: 1200px;
}

.code-block {
  background: #f5f5f5;
  border: 1px solid #e8e8e8;
  border-radius: 6px;
  overflow: hidden;
  margin: 8px 0;
}

.code-block-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 4px 12px;
  background: #fafafa;
  border-bottom: 1px solid #e8e8e8;
  font-size: 12px;
  color: #666;
}

.code-block pre {
  margin: 0;
  padding: 12px 16px;
  overflow-x: auto;
  font-size: 13px;
  line-height: 1.6;
}

.code-block code {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
}
</style>
