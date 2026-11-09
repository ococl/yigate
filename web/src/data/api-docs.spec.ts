import { describe, expect, it } from 'vitest'
import { apiGroups } from './api-docs'

// 文档里的示例是给人（和 AI）照抄的：枚举写错、字段名写错，照抄一次就是 400。
// 这里把示例中出现的取值与后端真实枚举对齐，避免复制粘贴把错误扩散到每一处。

const UPSTREAM_STRATEGIES = [
  'weighted_round_robin',
  'least_connections',
  'consistent_hash',
  'source_ip_hash',
]

const RULE_ACTIONS = ['direct', 'proxy', 'auto']

function exampleTexts(): { id: string; text: string }[] {
  const out: { id: string; text: string }[] = []
  for (const group of apiGroups) {
    for (const endpoint of group.endpoints) {
      const parts = [
        endpoint.requestBody,
        endpoint.responseBody,
        endpoint.example.request,
        endpoint.example.response,
      ]
      out.push({
        id: endpoint.id,
        text: parts.filter((part): part is string => Boolean(part)).join('\n'),
      })
    }
  }
  return out
}

function fieldValues(text: string, field: string): string[] {
  const pattern = new RegExp(`"${field}"\\s*:\\s*"([^"]+)"`, 'g')
  return [...text.matchAll(pattern)].map((match) => match[1] as string)
}

describe('API 文档示例', () => {
  it('上游 strategy 只用后端接受的枚举', () => {
    for (const { id, text } of exampleTexts()) {
      for (const value of fieldValues(text, 'strategy')) {
        expect(UPSTREAM_STRATEGIES, `${id} 的 strategy=${value}`).toContain(value)
      }
    }
  })

  it('规则 action 只用 direct / proxy / auto', () => {
    for (const { id, text } of exampleTexts()) {
      for (const value of fieldValues(text, 'action')) {
        expect(RULE_ACTIONS, `${id} 的 action=${value}`).toContain(value)
      }
    }
  })

  it('不出现后端没有的 health_check_type 字段', () => {
    // 真实字段是 health_check 对象（type/path/expected_codes/...）
    for (const { id, text } of exampleTexts()) {
      expect(text, `${id} 用了不存在的字段 health_check_type`).not.toContain('health_check_type')
    }
  })
})
