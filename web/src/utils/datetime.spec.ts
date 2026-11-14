import { describe, expect, it } from 'vitest'
import { formatDateTime } from './datetime'

// 全站只允许 yyyy-MM-dd HH:mm:ss 一种时间格式，这里把它固定成契约。
describe('formatDateTime', () => {
  it('按 yyyy-MM-dd HH:mm:ss 输出本地时间', () => {
    expect(formatDateTime(new Date(2026, 10, 14, 13, 16, 17))).toBe('2026-11-14 13:16:17')
  })

  it('月日时分秒补零', () => {
    expect(formatDateTime(new Date(2026, 0, 2, 3, 4, 5))).toBe('2026-01-02 03:04:05')
  })

  it('空值返回占位符', () => {
    expect(formatDateTime(undefined)).toBe('—')
    expect(formatDateTime(null)).toBe('—')
    expect(formatDateTime('')).toBe('—')
  })

  it('非法值与 Go 零值时间返回占位符', () => {
    expect(formatDateTime('unknown')).toBe('—')
    expect(formatDateTime('0001-01-01T00:00:00Z')).toBe('—')
  })

  it('支持自定义占位符', () => {
    expect(formatDateTime('', '-')).toBe('-')
  })
})
