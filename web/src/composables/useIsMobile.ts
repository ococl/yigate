import { onMounted, onUnmounted, ref } from 'vue'

/**
 * 订阅一个媒体查询，返回其当前是否命中。
 * 默认断点与布局侧栏抽屉保持一致（<= 768px 视为移动端）。
 */
export function useIsMobile(query = '(max-width: 768px)') {
  const matches = ref(false)
  let mediaQuery: MediaQueryList | null = null

  function sync(event: MediaQueryList | MediaQueryListEvent) {
    matches.value = event.matches
  }

  onMounted(() => {
    mediaQuery = window.matchMedia(query)
    sync(mediaQuery)
    mediaQuery.addEventListener('change', sync)
  })

  onUnmounted(() => {
    mediaQuery?.removeEventListener('change', sync)
  })

  return matches
}
