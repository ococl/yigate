// 统一时间展示格式：yyyy-MM-dd HH:mm:ss（本地时区）。
// 全站只保留这一种时间格式：避免使用 toLocaleString 等本地化输出（不同浏览器/语言会生成 2026/11/7 之类的斜线格式），
// 也避免直接透传后端的 RFC3339 原始串。

function pad(value: number): string {
  return value < 10 ? `0${value}` : String(value)
}

function toDate(value: string | number | Date): Date | null {
  const date = value instanceof Date ? value : new Date(value)
  // Go 的零值时间会序列化成 0001-01-01T00:00:00Z，按「无数据」处理
  if (Number.isNaN(date.getTime()) || date.getTime() <= 0) return null
  return date
}

// formatDateTime 输出完整日期时间；空值或非法值返回占位符。
export function formatDateTime(value?: string | number | Date | null, placeholder = '—'): string {
  if (value === undefined || value === null || value === '') return placeholder
  const date = toDate(value)
  if (!date) return placeholder
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}
