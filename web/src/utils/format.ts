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
  if (d > 0) return `${d} 天 ${h} 小时 ${m} 分钟`
  if (h > 0) return `${h} 小时 ${m} 分钟`
  return `${m} 分钟`
}
