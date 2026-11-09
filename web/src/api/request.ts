import axios from 'axios'
import { message } from 'antdv-next'
import { i18n } from '@/locales'
import { parseApiError } from './types'

const TOKEN_KEY = 'yigate_token'

/** API 层在收到 401 后广播该事件，由路由层执行登出跳转，避免循环依赖与硬刷新。 */
export const AUTH_EXPIRED_EVENT = 'yigate:auth-expired'

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

// 同一文案短时间内只提示一次：轮询或并发请求连续失败时，反复弹同一句会盖满页面
let lastErrorText = ''
let lastErrorAt = 0

function notifyError(text: string): void {
  const now = Date.now()
  if (text === lastErrorText && now - lastErrorAt < 3000) {
    return
  }
  lastErrorText = text
  lastErrorAt = now
  message.error(text)
}

// 响应拦截器：统一错误处理
request.interceptors.response.use(
  (response) => response,
  (error) => {
    // 调用方可通过 config._skipErrorHandler 跳过全局错误提示
    if (error.config?._skipErrorHandler) {
      return Promise.reject(error)
    }
    // 主动取消（AbortController / 组件卸载）属于正常流程，不应弹出错误提示
    if (axios.isCancel(error)) {
      return Promise.reject(error)
    }

    const apiError = parseApiError(error)
    if (apiError) {
      if (apiError.status === 401) {
        clearToken()
        // 登录页的令牌校验失败由登录视图自行提示，不重复广播登出事件
        if (window.location.pathname !== '/login') {
          window.dispatchEvent(new CustomEvent(AUTH_EXPIRED_EVENT))
        }
        notifyError(i18n.global.t('feedback.authFailed'))
      } else if (apiError.status === 403) {
        notifyError(apiError.detail || i18n.global.t('feedback.forbidden'))
      } else if (apiError.status >= 500) {
        notifyError(i18n.global.t('feedback.serverError', { status: apiError.status, detail: apiError.detail }))
      } else {
        notifyError(apiError.detail)
      }
    } else if (error.code === 'ECONNABORTED') {
      notifyError(i18n.global.t('feedback.timeout'))
    } else {
      notifyError(i18n.global.t('feedback.networkError'))
    }

    return Promise.reject(error)
  },
)

export default request
