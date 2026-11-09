import { describe, expect, it } from 'vitest'
import zhCN from './zh-CN'
import enUS from './en-US'

/** 把嵌套消息拍平成 a.b.c 形式的 key。 */
function collectKeys(value: unknown, prefix = ''): string[] {
  if (value === null || typeof value !== 'object') return [prefix]
  return Object.entries(value as Record<string, unknown>).flatMap(([key, child]) =>
    collectKeys(child, prefix ? `${prefix}.${key}` : key),
  )
}

/** 直接读取源码文本，避免为测试引入 node 类型依赖。 */
const sources = import.meta.glob(['/src/**/*.vue', '/src/**/*.ts', '!/src/locales/**', '!/src/**/*.spec.ts'], {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

/** 收集源码里所有字面量形式的 t('...') 调用，动态拼接的 key 无法静态校验，跳过。 */
function collectUsedKeys(): Set<string> {
  const used = new Set<string>()
  for (const source of Object.values(sources)) {
    for (const match of source.matchAll(/\bt\(\s*'([a-zA-Z0-9_.]+)'/g)) {
      used.add(match[1])
    }
    // 三元分支里的 key 也是字面量，单独匹配 t(cond ? 'a' : 'b')
    for (const match of source.matchAll(/\bt\(\s*[^)]*\?\s*'([a-zA-Z0-9_.]+)'\s*:\s*'([a-zA-Z0-9_.]+)'/g)) {
      used.add(match[1])
      used.add(match[2])
    }
  }
  return used
}

/** 界面引用了不存在的 key 时，用户会直接看到原始 key，这里提前拦住。 */
describe('locale key usage', () => {
  const zhKeys = new Set(collectKeys(zhCN))
  const enKeys = new Set(collectKeys(enUS))
  const used = collectUsedKeys()

  it('收集到了源码里的 key 引用', () => {
    expect(used.size).toBeGreaterThan(100)
  })

  it('所有 t() 引用的 key 都存在于 zh-CN', () => {
    expect([...used].filter((key) => !zhKeys.has(key)).sort()).toEqual([])
  })

  it('所有 t() 引用的 key 都存在于 en-US', () => {
    expect([...used].filter((key) => !enKeys.has(key)).sort()).toEqual([])
  })
})
