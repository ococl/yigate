import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, type App } from 'vue'
import { useHotkeys, type Hotkey } from './useHotkeys'

let app: App | null = null

function mount(hotkeys: Hotkey[]) {
  app = createApp(
    defineComponent({
      setup() {
        useHotkeys(hotkeys)
        return () => h('div')
      },
    }),
  )
  app.mount(document.createElement('div'))
}

function press(key: string, init: KeyboardEventInit = {}, target: EventTarget = window) {
  const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...init })
  target.dispatchEvent(event)
  return event
}

afterEach(() => {
  app?.unmount()
  app = null
  document.body.innerHTML = ''
})

describe('useHotkeys', () => {
  it('fires on Ctrl+S and swallows the browser save dialog', () => {
    const handler = vi.fn()
    mount([{ key: 's', ctrlOrCmd: true, allowInInput: true, handler }])

    const event = press('s', { ctrlKey: true })

    expect(handler).toHaveBeenCalledTimes(1)
    expect(event.defaultPrevented).toBe(true)
  })

  it('accepts Cmd on macOS', () => {
    const handler = vi.fn()
    mount([{ key: 's', ctrlOrCmd: true, allowInInput: true, handler }])

    press('s', { metaKey: true })

    expect(handler).toHaveBeenCalledTimes(1)
  })

  it('ignores the same key without a modifier', () => {
    const handler = vi.fn()
    mount([{ key: 's', ctrlOrCmd: true, allowInInput: true, handler }])

    press('s')

    expect(handler).not.toHaveBeenCalled()
  })

  it('ignores plain keys while typing, so `/` does not steal input', () => {
    const handler = vi.fn()
    mount([{ key: '/', handler }])
    const input = document.createElement('input')
    document.body.appendChild(input)

    press('/', {}, input)

    expect(handler).not.toHaveBeenCalled()
  })

  it('still fires `/` when focus is not in a field', () => {
    const handler = vi.fn()
    mount([{ key: '/', handler }])

    press('/')

    expect(handler).toHaveBeenCalledTimes(1)
  })

  it('fires modifier shortcuts from inside an input when allowed', () => {
    const handler = vi.fn()
    mount([{ key: 's', ctrlOrCmd: true, allowInInput: true, handler }])
    const input = document.createElement('input')
    document.body.appendChild(input)

    press('s', { ctrlKey: true }, input)

    expect(handler).toHaveBeenCalledTimes(1)
  })

  it('unbinds on unmount', () => {
    const handler = vi.fn()
    mount([{ key: '/', handler }])
    app?.unmount()
    app = null

    press('/')

    expect(handler).not.toHaveBeenCalled()
  })
})
