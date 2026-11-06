import request from './request'
import type { SettingsView, SettingsPatch, ApiResult } from '@/types/api'

export function getSettings(): Promise<SettingsView> {
  return request.get('/api/v1/config/settings').then((r) => r.data)
}

export function updateSettings(patch: SettingsPatch): Promise<ApiResult> {
  return request.put('/api/v1/config/settings', patch).then((r) => r.data)
}
