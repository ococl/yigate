import request from './request'
import type { GeoSiteInfo, GeoSiteUpdateResult } from '@/types/api'

// getGeoSiteInfo 读取 GeoSite 数据的版本与同步配置。
export function getGeoSiteInfo(): Promise<GeoSiteInfo> {
  return request.get('/api/v1/geosite').then((r) => r.data)
}

// updateGeoSite 立即从配置的更新地址拉取一次数据。
export function updateGeoSite(): Promise<GeoSiteUpdateResult> {
  return request.post('/api/v1/geosite/update').then((r) => r.data)
}
