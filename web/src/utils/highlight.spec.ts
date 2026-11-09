import { describe, expect, it } from 'vitest'
import { escapeHtml, highlightText } from './highlight'

describe('highlightText', () => {
  it('marks every occurrence regardless of case', () => {
    expect(highlightText('上游健康检查，检查间隔', '检查')).toBe('上游健康<mark>检查</mark>，<mark>检查</mark>间隔')
  })

  it('returns escaped text when there is no keyword', () => {
    expect(highlightText('<b>上游</b>', '  ')).toBe('&lt;b&gt;上游&lt;/b&gt;')
  })

  it('treats regex metacharacters in the keyword literally', () => {
    expect(highlightText('rate: a+b (x)', 'a+b')).toBe('rate: <mark>a+b</mark> (x)')
    expect(highlightText('aXb', 'a.b')).toBe('aXb')
  })

  it('escapes html so descriptions cannot inject markup', () => {
    expect(highlightText('<img src=x onerror=alert(1)>', 'img')).toBe(
      '&lt;<mark>img</mark> src=x onerror=alert(1)&gt;',
    )
  })

  it('escapes a keyword that contains angle brackets', () => {
    expect(highlightText('use <value> here', '<value>')).toBe('use <mark>&lt;value&gt;</mark> here')
  })
})

describe('escapeHtml', () => {
  it('escapes the five html characters', () => {
    expect(escapeHtml(`<a href="x">'&'</a>`)).toBe('&lt;a href=&quot;x&quot;&gt;&#39;&amp;&#39;&lt;/a&gt;')
  })
})
