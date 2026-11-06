import request from './request'
import type {
  HealthStatus,
  StatusSnapshot,
  TrafficSnapshot,
  VersionInfo,
  ApiResult,
} from '@/types/api'

export function getHealth(): Promise<HealthStatus> {
  return request.get('/health').then((r) => r.data)
}

export function getMetrics(): Promise<string> {
  return request.get('/metrics', { responseType: 'text' }).then((r) => r.data)
}

export function getStatus(): Promise<StatusSnapshot> {
  return request.get('/api/v1/status').then((r) => r.data)
}

export function getVersion(): Promise<VersionInfo> {
  return request.get('/api/v1/version').then((r) => r.data)
}

export function reloadConfig(): Promise<ApiResult> {
  return request.post('/api/v1/reload').then((r) => r.data)
}

export function getTraffic(): Promise<TrafficSnapshot> {
  return request.get('/api/v1/traffic').then((r) => r.data)
}
