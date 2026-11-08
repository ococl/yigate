import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick } from 'vue'
import { useVisiblePolling } from './useVisiblePolling'

function setHidden(hidden: boolean) {
  Object.defineProperty(document, 'hidden', {
    configurable: true,
    get: () => hidden,
  })
}

function mountPolling(callback: () => void, immediate = false) {
  const app = createApp(
    defineComponent({
      setup() {
        useVisiblePolling(callback, 1000, immediate)
        return () => h('div')
      },
    }),
  )
  app.mount(document.createElement('div'))
  return app
}

beforeEach(() => {
  vi.useFakeTimers()
  setHidden(false)
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('useVisiblePolling', () => {
  it('pauses while hidden and refreshes immediately when visible again', async () => {
    const callback = vi.fn()
    const app = mountPolling(callback)
    await nextTick()

    vi.advanceTimersByTime(1000)
    expect(callback).toHaveBeenCalledTimes(1)

    setHidden(true)
    document.dispatchEvent(new Event('visibilitychange'))
    vi.advanceTimersByTime(5000)
    expect(callback).toHaveBeenCalledTimes(1)

    setHidden(false)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(callback).toHaveBeenCalledTimes(2)

    vi.advanceTimersByTime(1000)
    expect(callback).toHaveBeenCalledTimes(3)

    app.unmount()
    vi.advanceTimersByTime(5000)
    expect(callback).toHaveBeenCalledTimes(3)
  })

  it('invokes the callback immediately when requested', async () => {
    const callback = vi.fn()
    mountPolling(callback, true)
    await nextTick()
    expect(callback).toHaveBeenCalledTimes(1)
  })
})
