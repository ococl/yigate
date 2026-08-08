<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { message } from 'antdv-next'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const authStore = useAuthStore()

const token = ref('')

async function handleLogin() {
  if (!token.value.trim()) {
    message.warning('请输入管理 Token')
    return
  }

  const success = await authStore.login(token.value.trim())
  if (success) {
    message.success('登录成功')
    router.push('/dashboard')
  } else {
    message.error('Token 验证失败，请检查 Token 或后端服务')
  }
}
</script>

<template>
  <a-card class="login-card" variant="borderless">
    <div class="login-header">
      <h1 class="login-title">YiGate</h1>
      <p class="login-subtitle">统一代理网关管理后台</p>
    </div>

    <a-form layout="vertical" @submit.prevent="handleLogin">
      <a-form-item label="管理 Token">
        <a-input-password
          v-model:value="token"
          placeholder="请输入 management_auth_token"
          size="large"
          autocomplete="off"
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
          登录
        </a-button>
      </a-form-item>
    </a-form>

    <a-typography-text type="secondary" class="login-hint">
      Token 对应配置文件中的 management_auth_token 字段。
      如果后端未设置 Token，可输入任意值登录。
    </a-typography-text>
  </a-card>
</template>

<style scoped>
.login-card {
  width: 400px;
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
  color: #1677ff;
}

.login-subtitle {
  color: #666;
  margin-top: 4px;
}

.login-hint {
  display: block;
  text-align: center;
  font-size: 12px;
  margin-top: 16px;
}
</style>
