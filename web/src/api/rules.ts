import request from './request'
import type { RuleListResponse, RuleRequest, ApiResult } from '@/types/api'

export function listRules(): Promise<RuleListResponse> {
  return request.get('/api/v1/config/rules').then((r) => r.data)
}

export function addRule(data: RuleRequest): Promise<ApiResult> {
  return request.post('/api/v1/config/rules', data).then((r) => r.data)
}

export function updateRule(index: number, data: RuleRequest): Promise<ApiResult> {
  return request.put(`/api/v1/config/rules/${index}`, data).then((r) => r.data)
}

export function moveRule(index: number, direction: 'up' | 'down'): Promise<ApiResult> {
  return request.post(`/api/v1/config/rules/${index}/move`, { direction }).then((r) => r.data)
}

export function deleteRule(index: number): Promise<ApiResult> {
  return request.delete(`/api/v1/config/rules/${index}`).then((r) => r.data)
}
