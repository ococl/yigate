<script setup lang="ts">
</script>

<template>
  <div>
    <a-typography-title :level="2">关于</a-typography-title>
    <a-typography-paragraph>
      YiGate — 轻量级统一代理网关。
    </a-typography-paragraph>
    <a-descriptions title="项目信息" :column="1" bordered>
      <a-descriptions-item label="版本">0.1.0</a-descriptions-item>
      <a-descriptions-item label="技术栈">Go + Vue 3 + antdv-next</a-descriptions-item>
      <a-descriptions-item label="协议支持">HTTP / HTTPS CONNECT / SOCKS5</a-descriptions-item>
    </a-descriptions>
  </div>
</template>
