import { i18n } from '@/locales'

/**
 * 全站统一的展示格式化函数。
 * 历史上四个视图各自实现了一份 formatBytes / formatUptime，行为与单位上限不一致，
 * 这里收敛为单一实现；zeroLabel 用于「无数据」场景（如 GeoSite 未加载）。
 */
export function formatBytes(value: number, zeroLabel = '0 B'): string {
  if (!Number.isFinite(value) || value <= 0) return zeroLabel
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let size = value
  let unit = 0
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024
    unit++
  }
  return `${parseFloat(size.toFixed(2))} ${units[unit]}`
}

export function formatUptime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '—'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const { t } = i18n.global
  if (d > 0) return t('common.uptime.days', { days: d, hours: h, minutes: m })
  if (h > 0) return t('common.uptime.hours', { hours: h, minutes: m })
  return t('common.uptime.minutes', { minutes: m })
}
