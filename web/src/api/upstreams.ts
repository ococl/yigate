import request from './request'
import type {
  UpstreamListResponse,
  AddUpstreamRequest,
  UpdateUpstreamRequest,
  UpstreamCheckResponse,
  UpstreamCheckAllResponse,
  ApiResult,
  BatchDeleteResult,
} from '@/types/api'

export function listUpstreams(): Promise<UpstreamListResponse> {
  return request.get('/api/v1/config/upstreams').then((r) => r.data)
}

export function addUpstream(data: AddUpstreamRequest): Promise<ApiResult> {
  return request.post('/api/v1/config/upstreams', data).then((r) => r.data)
}

export function updateUpstream(name: string, data: UpdateUpstreamRequest): Promise<ApiResult> {
  return request.put(`/api/v1/config/upstreams/${encodeURIComponent(name)}`, data).then((r) => r.data)
}

export function deleteUpstream(name: string): Promise<ApiResult> {
  return request.delete(`/api/v1/config/upstreams/${encodeURIComponent(name)}`).then((r) => r.data)
}

/** 批量删除上游：服务端合并成一次落盘一次热加载，并清理规则引用。 */
export function batchDeleteUpstreams(names: string[]): Promise<BatchDeleteResult> {
  return request.post('/api/v1/config/upstreams/batch-delete', { names }).then((r) => r.data)
}

export function checkUpstream(name: string): Promise<UpstreamCheckResponse> {
  return request.post(`/api/v1/config/upstreams/${encodeURIComponent(name)}/check`).then((r) => r.data)
}

export function checkAllUpstreams(): Promise<UpstreamCheckAllResponse> {
  return request.post('/api/v1/config/upstreams/check').then((r) => r.data)
}
