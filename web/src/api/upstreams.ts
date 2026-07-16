import request from './request'
import type {
  UpstreamListResponse,
  AddUpstreamRequest,
  UpdateUpstreamRequest,
  ApiResult,
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
