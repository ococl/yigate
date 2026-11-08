import { onMounted, onUnmounted } from 'vue'

/**
 * 可见时按固定间隔轮询，页面隐藏时停止计时，重新可见时立即刷新一次。
 * 避免后台标签页持续请求管理接口。
 */
export function useVisiblePolling(callback: () => void, intervalMs: number, immediate = false) {
  let timer: number | undefined

  function stop() {
    if (timer !== undefined) {
      window.clearInterval(timer)
      timer = undefined
    }
  }

  function start() {
    stop()
    if (document.hidden) return
    timer = window.setInterval(callback, intervalMs)
  }

  function handleVisibility() {
    if (document.hidden) {
      stop()
      return
    }
    callback()
    start()
  }

  onMounted(() => {
    if (immediate) callback()
    start()
    document.addEventListener('visibilitychange', handleVisibility)
  })

  onUnmounted(() => {
    stop()
    document.removeEventListener('visibilitychange', handleVisibility)
  })
}
