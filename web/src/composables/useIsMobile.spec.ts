import { describe, expect, it, vi, afterEach } from 'vitest'
import { createApp, defineComponent, h, nextTick, type Ref } from 'vue'
import { useIsMobile } from './useIsMobile'

type Listener = (event: { matches: boolean }) => void

function mockMatchMedia(matches: boolean) {
  const listeners = new Set<Listener>()
  const mql = {
    matches,
    addEventListener: (_: string, cb: Listener) => listeners.add(cb),
    removeEventListener: (_: string, cb: Listener) => listeners.delete(cb),
  }
  window.matchMedia = vi.fn(() => mql) as unknown as typeof window.matchMedia
  return {
    emit(next: boolean) {
      mql.matches = next
      for (const cb of listeners) cb({ matches: next })
    },
  }
}

function mount() {
  let mobile!: Ref<boolean>
  const app = createApp(
    defineComponent({
      setup() {
        mobile = useIsMobile()
        return () => h('div')
      },
    }),
  )
  const host = document.createElement('div')
  app.mount(host)
  return {
    app,
    get value() {
      return mobile.value
    },
  }
}

afterEach(() => {
  delete (window as Partial<Window>).matchMedia
})

describe('useIsMobile', () => {
  it('tracks the media query and stops listening after unmount', async () => {
    const media = mockMatchMedia(false)
    const mounted = mount()
    await nextTick()
    expect(mounted.value).toBe(false)

    media.emit(true)
    await nextTick()
    expect(mounted.value).toBe(true)

    mounted.app.unmount()
    media.emit(false)
    expect(mounted.value).toBe(true)
  })
})
