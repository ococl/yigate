import { onBeforeUnmount, onMounted } from 'vue'

export interface Hotkey {
  /** 主键，大小写不敏感 */
  key: string
  /** 需要 Ctrl（macOS 上是 Cmd） */
  ctrlOrCmd?: boolean
  /**
   * 焦点在输入框里也触发。
   * Ctrl+S 这类组合键需要；`/` 这种纯字母键不该抢输入，默认不触发。
   */
  allowInInput?: boolean
  handler: (event: KeyboardEvent) => void
}

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  const tag = target.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target.isContentEditable
}

/**
 * 注册页面级快捷键，组件卸载时自动解绑。
 * 只处理单键 + 可选 Ctrl/Cmd，够用且不会和其它组合键打架。
 */
export function useHotkeys(hotkeys: Hotkey[]): void {
  function onKeydown(event: KeyboardEvent) {
    // 已经被处理过，或长按重复触发，都不再响应
    if (event.defaultPrevented || event.repeat) return
    const hasModifier = event.ctrlKey || event.metaKey || event.altKey
    for (const hotkey of hotkeys) {
      if (event.key.toLowerCase() !== hotkey.key.toLowerCase()) continue
      const modifierMatches = hotkey.ctrlOrCmd ? event.ctrlKey || event.metaKey : !hasModifier
      if (!modifierMatches) continue
      if (!hotkey.allowInInput && isTypingTarget(event.target)) return
      event.preventDefault()
      hotkey.handler(event)
      return
    }
  }

  onMounted(() => window.addEventListener('keydown', onKeydown))
  onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
}
