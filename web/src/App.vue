<script setup lang="ts">
import { ConfigProvider } from 'antdv-next'
import { App as AntApp } from 'antdv-next'
</script>

<template>
  <ConfigProvider :locale="undefined">
    <AntApp>
      <RouterView />
    </AntApp>
  </ConfigProvider>
</template>
