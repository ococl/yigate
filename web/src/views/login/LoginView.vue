<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { message } from 'antdv-next'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const authStore = useAuthStore()
const { t } = useI18n()

const token = ref('')
// 登录失败直接标在字段下方，不依赖一闪而过的 toast
const loginError = ref('')

watch(token, () => {
  loginError.value = ''
})

async function handleLogin() {
  if (!token.value.trim()) {
    loginError.value = t('login.errorEmpty')
    return
  }
  loginError.value = ''

  const success = await authStore.login(token.value.trim())
  if (success) {
    message.success(t('login.success'))
    router.push('/dashboard')
  } else {
    loginError.value = t('login.errorInvalid')
  }
}
</script>

<template>
  <a-card class="login-card" variant="borderless">
    <div class="login-header">
      <h1 class="login-title">YiGate</h1>
      <p class="login-subtitle">{{ t('login.subtitle') }}</p>
    </div>

    <a-form layout="vertical" @submit.prevent="handleLogin">
      <a-form-item
        html-for="login-token"
        :label="t('login.tokenLabel')"
        :validate-status="loginError ? 'error' : undefined"
        :help="loginError || undefined"
      >
        <a-input-password
          id="login-token"
          v-model:value="token"
          :placeholder="t('login.tokenPlaceholder')"
          size="large"
          autocomplete="current-password"
          @press-enter="handleLogin"
        />
      </a-form-item>

      <a-form-item>
        <a-button
          type="primary"
          size="large"
          block
          :loading="authStore.loading"
          @click="handleLogin"
        >
          {{ t('login.submit') }}
        </a-button>
      </a-form-item>
    </a-form>

    <a-typography-text type="secondary" class="login-hint">
      {{ t('login.hint') }}
    </a-typography-text>
  </a-card>
</template>

<style scoped>
.login-card {
  width: min(400px, calc(100vw - 32px));
  box-shadow: 0 4px 24px rgba(0, 0, 0, 0.08);
  border-radius: 8px;
}

.login-header {
  text-align: center;
  margin-bottom: 24px;
}

.login-title {
  font-size: 28px;
  font-weight: bold;
  margin: 0;
  color: var(--ant-color-primary, #1677ff);
}

.login-subtitle {
  color: var(--ant-color-text-secondary, #666);
  margin-top: 4px;
}

.login-hint {
  display: block;
  text-align: center;
  font-size: 12px;
  margin-top: 16px;
}
</style>
