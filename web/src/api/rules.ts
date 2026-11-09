import request from './request'
import type { RuleListResponse, RuleRequest, ApiResult, BatchDeleteResult } from '@/types/api'

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

/** 批量删除规则：索引按删除前的位置解释，服务端负责一次落盘。 */
export function batchDeleteRules(indexes: number[]): Promise<BatchDeleteResult> {
  return request.post('/api/v1/config/rules/batch-delete', { indexes }).then((r) => r.data)
}
