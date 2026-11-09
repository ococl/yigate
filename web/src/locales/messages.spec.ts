import { describe, expect, it } from 'vitest'
import zhCN from './zh-CN'
import enUS from './en-US'

/** 把嵌套消息拍平成 a.b.c 形式的 key，便于逐条比对中英文是否对齐。 */
function collectKeys(value: unknown, prefix = ''): string[] {
  if (value === null || typeof value !== 'object') return [prefix]
  return Object.entries(value as Record<string, unknown>).flatMap(([key, child]) =>
    collectKeys(child, prefix ? `${prefix}.${key}` : key),
  )
}

/** 只比对 key 结构：漏翻或删词会在这里暴露，而不是等到界面上显示 key。 */
describe('locale messages', () => {
  const zhKeys = collectKeys(zhCN).sort()
  const enKeys = collectKeys(enUS).sort()

  it('en-US 覆盖了 zh-CN 的全部 key', () => {
    expect(zhKeys.filter((key) => !enKeys.includes(key))).toEqual([])
  })

  it('en-US 没有多余的 key', () => {
    expect(enKeys.filter((key) => !zhKeys.includes(key))).toEqual([])
  })

  it('没有空文案', () => {
    const empty = [...collectKeys(zhCN), ...collectKeys(enUS)].length
    expect(empty).toBeGreaterThan(0)
    const walk = (value: unknown, prefix: string, out: string[]) => {
      if (typeof value === 'string') {
        if (value.trim() === '') out.push(prefix)
        return
      }
      if (value && typeof value === 'object') {
        for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
          walk(child, prefix ? `${prefix}.${key}` : key, out)
        }
      }
    }
    const blanks: string[] = []
    walk(zhCN, '', blanks)
    walk(enUS, '', blanks)
    expect(blanks).toEqual([])
  })
})
