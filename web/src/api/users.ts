import request from './request'
import type { UserListResponse, AddUserRequest, ApiResult } from '@/types/api'

export function listUsers(): Promise<UserListResponse> {
  return request.get('/api/v1/config/users').then((r) => r.data)
}

export function addUser(data: AddUserRequest): Promise<ApiResult> {
  return request.post('/api/v1/config/users', data).then((r) => r.data)
}

export function updateUserPassword(username: string, password: string): Promise<ApiResult> {
  return request.put(`/api/v1/config/users/${encodeURIComponent(username)}`, { password }).then((r) => r.data)
}

export function deleteUser(username: string): Promise<ApiResult> {
  return request.delete(`/api/v1/config/users/${encodeURIComponent(username)}`).then((r) => r.data)
}
