import request from './request'
import type {
  HealthStatus,
  StatusSnapshot,
  TrafficSnapshot,
  VersionInfo,
  ApiResult,
} from '@/types/api'

// silent=true 时不触发全局错误提示，供轮询使用：失败由页面自身的状态位表达
export function getHealth(options?: { silent?: boolean }): Promise<HealthStatus> {
  return request.get('/health', options?.silent ? { _skipErrorHandler: true } : undefined).then((r) => r.data)
}

export function getMetrics(): Promise<string> {
  return request.get('/metrics', { responseType: 'text' }).then((r) => r.data)
}

export function getStatus(options?: { silent?: boolean }): Promise<StatusSnapshot> {
  return request
    .get('/api/v1/status', options?.silent ? { _skipErrorHandler: true } : undefined)
    .then((r) => r.data)
}

export function getVersion(): Promise<VersionInfo> {
  return request.get('/api/v1/version').then((r) => r.data)
}

export function reloadConfig(): Promise<ApiResult> {
  return request.post('/api/v1/reload').then((r) => r.data)
}

export function getTraffic(options?: { silent?: boolean }): Promise<TrafficSnapshot> {
  return request
    .get('/api/v1/traffic', options?.silent ? { _skipErrorHandler: true } : undefined)
    .then((r) => r.data)
}
