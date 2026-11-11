import request from './request'
import type { SettingsView, SettingsPatch, ApiResult } from '@/types/api'

export function getSettings(): Promise<SettingsView> {
  return request.get('/api/v1/config/settings').then((r) => r.data)
}

export function updateSettings(patch: SettingsPatch): Promise<ApiResult> {
  return request.put('/api/v1/config/settings', patch).then((r) => r.data)
}

/** 列出配置目录内的证书文件，供 CA 路径输入补全。 */
export function listCertificateFiles(options?: { silent?: boolean }): Promise<string[]> {
  return request
    .get(
      '/api/v1/config/certificates',
      options?.silent ? { _skipErrorHandler: true } : undefined,
    )
    .then((r) => r.data?.files ?? [])
}
