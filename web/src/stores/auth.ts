import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { getToken, setToken, clearToken } from '@/api/request'
import { getHealth } from '@/api/system'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(getToken())
  const loading = ref(false)

  const isAuthenticated = computed(() => !!token.value)

  async function login(t: string): Promise<boolean> {
    loading.value = true
    try {
      setToken(t)
      token.value = t
      // 验证 Token 是否有效
      await getHealth()
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
