import { describe, expect, it } from 'vitest'
import { formatBytes, formatUptime } from './format'

describe('formatBytes', () => {
  it('handles empty and invalid values', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(-1)).toBe('0 B')
    expect(formatBytes(Number.NaN)).toBe('0 B')
    expect(formatBytes(0, '—')).toBe('—')
  })

  it('uses binary units without trailing zeros', () => {
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(1024)).toBe('1 KB')
    expect(formatBytes(1536)).toBe('1.5 KB')
    expect(formatBytes(1024 * 1024 * 3)).toBe('3 MB')
  })
})

describe('formatUptime', () => {
  it('renders days, hours and minutes', () => {
    expect(formatUptime(0)).toBe('0 分钟')
    expect(formatUptime(90)).toBe('1 分钟')
    expect(formatUptime(3600 * 2 + 120)).toBe('2 小时 2 分钟')
    expect(formatUptime(86400 + 3600 + 60)).toBe('1 天 1 小时 1 分钟')
  })

  it('renders a placeholder for invalid values', () => {
    expect(formatUptime(-1)).toBe('—')
    expect(formatUptime(Number.NaN)).toBe('—')
  })
})
