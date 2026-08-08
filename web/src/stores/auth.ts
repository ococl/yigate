import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { getToken, setToken, clearToken } from '@/api/request'
import request from '@/api/request'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(getToken())
  const loading = ref(false)

  const isAuthenticated = computed(() => !!token.value)

  async function login(t: string): Promise<boolean> {
    loading.value = true
    try {
      // 先验证 Token，不提前写入 localStorage
      // 直接在请求头中携带 Token，绕过拦截器从 localStorage 读取的逻辑
      // _skipErrorHandler 跳过全局错误提示，由登录页统一处理
      const resp = await request.get('/health', {
        headers: { Authorization: `Bearer ${t}` },
        _skipErrorHandler: true,
      })
      // 校验响应格式，防止代理返回 HTML 等非 JSON 内容被误判为成功
      if (!resp.data || typeof resp.data.status !== 'string') {
        throw new Error('invalid response')
      }
      // 验证通过后才持久化 Token
      setToken(t)
      token.value = t
      return true
    } catch {
      clearToken()
      token.value = null
      return false
    } finally {
      loading.value = false
    }
  }

  function logout(): void {
    clearToken()
    token.value = null
  }

  return {
    token,
    loading,
    isAuthenticated,
    login,
    logout,
  }
})
