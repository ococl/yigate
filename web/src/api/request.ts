import axios from 'axios'
import { message } from 'antdv-next'

const TOKEN_KEY = 'yigate_token'

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY)
}

const request = axios.create({
  timeout: 15000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// 请求拦截器：注入 Token
// 调用方显式指定 Authorization 时（如登录页验证尚未写入的令牌）不覆盖，避免被旧令牌污染
request.interceptors.request.use(
  (config) => {
    if (config.headers?.Authorization) {
      return config
    }
    const token = getToken()
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  (error) => Promise.reject(error),
)

// 响应拦截器：统一错误处理
request.interceptors.response.use(
  (response) => response,
  (error) => {
    // 调用方可通过 config._skipErrorHandler 跳过全局错误提示
    if (error.config?._skipErrorHandler) {
      return Promise.reject(error)
    }

    if (error.response) {
      const { status } = error.response

      if (status === 401) {
        clearToken()
        message.error('认证失败，请重新登录')
        // 避免在登录页重复跳转
        if (window.location.pathname !== '/login') {
          window.location.href = '/login'
        }
      } else if (status >= 500) {
        message.error(`服务器错误 (${status})`)
      } else if (status >= 400) {
        const msg = error.response.data?.error || error.response.data?.status || `请求错误 (${status})`
        message.error(msg)
      }
    } else if (error.code === 'ECONNABORTED') {
      message.error('请求超时')
    } else {
      message.error('网络错误，请检查后端服务是否运行')
    }

    return Promise.reject(error)
  },
)

export default request
