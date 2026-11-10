import request from './request'
import type { UserListResponse, AddUserRequest, ApiResult, BatchDeleteResult } from '@/types/api'

export function listUsers(): Promise<UserListResponse> {
  return request.get('/api/v1/config/users').then((r) => r.data)
}

export function addUser(data: AddUserRequest): Promise<ApiResult> {
  return request.post('/api/v1/config/users', data).then((r) => r.data)
}

export function updateUserPassword(username: string, password: string): Promise<ApiResult> {
  return request.put(`/api/v1/config/users/${encodeURIComponent(username)}`, { password }).then((r) => r.data)
}

/**
 * 启用/禁用用户。
 * 禁用最后一个启用用户会让端口回落为匿名访问，服务端返回 409（code=last_active_user），
 * 由调用方提示风险后带 force=true 重试。
 */
export function setUserEnabled(username: string, enabled: boolean, force = false): Promise<ApiResult> {
  return request
    .put(
      `/api/v1/config/users/${encodeURIComponent(username)}`,
      { enabled },
      { params: force ? { force: true } : undefined, _skipErrorHandler: true },
    )
    .then((r) => r.data)
}

/**
 * 删除用户。
 * 删除最后一个启用用户同样返回 409，需要用户确认风险后带 force=true 重试。
 * 这里跳过全局错误提示，由调用方统一处理 409 与其他失败。
 */
export function deleteUser(username: string, force = false): Promise<ApiResult> {
  return request
    .delete(`/api/v1/config/users/${encodeURIComponent(username)}`, {
      params: force ? { force: true } : undefined,
      _skipErrorHandler: true,
    })
    .then((r) => r.data)
}

/** 批量删除用户：服务端合并成一次落盘一次热加载。 */
export function batchDeleteUsers(usernames: string[], force = false): Promise<BatchDeleteResult> {
  return request
    .post(
      '/api/v1/config/users/batch-delete',
      { usernames },
      { params: force ? { force: true } : undefined, _skipErrorHandler: true },
    )
    .then((r) => r.data)
}
