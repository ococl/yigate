<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'

const router = useRouter()
const { t } = useI18n()
</script>

<template>
  <div class="not-found">
    <a-result status="404" title="404" :sub-title="t('pages.notFound.title')">
      <template #extra>
        <a-space>
          <a-button type="primary" @click="router.push('/dashboard')">{{ t('common.backHome') }}</a-button>
          <a-button @click="router.back()">{{ t('pages.notFound.back') }}</a-button>
        </a-space>
      </template>
    </a-result>
  </div>
</template>

<style scoped>
.not-found {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 60vh;
}
</style>
