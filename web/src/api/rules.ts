import request from './request'
import type { RuleListResponse, AddRuleRequest, ApiResult } from '@/types/api'

export function listRules(): Promise<RuleListResponse> {
  return request.get('/api/v1/config/rules').then((r) => r.data)
}

export function addRule(data: AddRuleRequest): Promise<ApiResult> {
  return request.post('/api/v1/config/rules', data).then((r) => r.data)
}

export function deleteRule(index: number): Promise<ApiResult> {
  return request.delete(`/api/v1/config/rules/${index}`).then((r) => r.data)
}
