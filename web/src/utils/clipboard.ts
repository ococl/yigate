/**
 * 统一复制实现。
 * Clipboard API 只在安全上下文（HTTPS / localhost）可用，HTTP 部署下会直接失败，
 * 因此失败或不可用时回退到 textarea + execCommand，保证整站复制按钮都能工作。
 */
export async function copyText(text: string): Promise<boolean> {
  if (!text) return false
  if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // 非安全上下文或权限被拒，继续尝试兼容实现
    }
  }
  return legacyCopy(text)
}

function legacyCopy(text: string): boolean {
  if (typeof document === 'undefined') return false
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.setAttribute('readonly', '')
  textarea.style.position = 'fixed'
  textarea.style.top = '-1000px'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.select()
  textarea.setSelectionRange(0, textarea.value.length)
  let copied = false
  try {
    copied = document.execCommand('copy')
  } catch {
    copied = false
  }
  document.body.removeChild(textarea)
  return copied
}
