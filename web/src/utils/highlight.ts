const HTML_ESCAPES: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
}

export function escapeHtml(text: string): string {
  return text.replace(/[&<>"']/g, (ch) => HTML_ESCAPES[ch])
}

/**
 * 把命中的关键词包成 <mark>，供 v-html 渲染。
 * 文本与关键词都先做 HTML 转义，避免把接口描述里的尖括号当成标签执行。
 */
export function highlightText(text: string, keyword: string): string {
  const safeText = escapeHtml(text)
  const trimmed = keyword.trim()
  if (!trimmed) return safeText
  const pattern = escapeHtml(trimmed).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return safeText.replace(new RegExp(`(${pattern})`, 'gi'), '<mark>$1</mark>')
}
