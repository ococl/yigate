<script setup lang="ts">
</script>

<template>
  <div>
    <a-typography-title :level="2">YiGate 管理面板</a-typography-title>
    <a-typography-paragraph>
      YiGate 是一个轻量级统一代理网关，支持 HTTP、HTTPS CONNECT 和 SOCKS5 协议。
    </a-typography-paragraph>
    <a-row :gutter="16">
      <a-col :span="8">
        <a-card title="代理服务" size="small">
          <a-statistic title="状态" value="运行中" />
        </a-card>
      </a-col>
      <a-col :span="8">
        <a-card title="上游连接" size="small">
          <a-statistic title="活跃连接" :value="0" />
        </a-card>
      </a-col>
      <a-col :span="8">
        <a-card title="流量" size="small">
          <a-statistic title="今日流量" :value="0" suffix="MB" />
        </a-card>
      </a-col>
    </a-row>
  </div>
</template>
