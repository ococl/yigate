import request from './request'
import type { GeoSiteInfo, GeoSiteUpdateResult } from '@/types/api'

// getGeoSiteInfo 读取 GeoSite 数据的版本与同步配置。
// silent 为 true 时不触发全局错误提示，由调用方自行处理（例如后端版本较旧返回 404）。
export function getGeoSiteInfo(options?: { silent?: boolean }): Promise<GeoSiteInfo> {
  return request
    .get('/api/v1/geosite', options?.silent ? { _skipErrorHandler: true } : undefined)
    .then((r) => r.data)
}

// updateGeoSite 立即从配置的更新地址拉取一次数据。
export function updateGeoSite(): Promise<GeoSiteUpdateResult> {
  return request.post('/api/v1/geosite/update').then((r) => r.data)
}
