import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import request, { clearToken, setToken } from '@/api/request'
import { useAuthStore } from './auth'

vi.mock('@/api/request', () => ({
  default: { get: vi.fn() },
  getToken: vi.fn(() => null),
  setToken: vi.fn(),
  clearToken: vi.fn(),
}))

describe('auth store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('persists a token only after a valid health response', async () => {
    vi.mocked(request.get).mockResolvedValueOnce({ data: { status: 'up' } } as never)
    const store = useAuthStore()

    await expect(store.login(' valid-token ')).resolves.toBe(true)

    expect(request.get).toHaveBeenCalledWith('/health', {
      headers: { Authorization: 'Bearer  valid-token ' },
      _skipErrorHandler: true,
    })
    expect(setToken).toHaveBeenCalledWith(' valid-token ')
    expect(store.token).toBe(' valid-token ')
    expect(store.loading).toBe(false)
  })

  it('rejects non-JSON proxy responses without persisting the token', async () => {
    vi.mocked(request.get).mockResolvedValueOnce({ data: '<html>login</html>' } as never)
    const store = useAuthStore()

    await expect(store.login('bad-token')).resolves.toBe(false)

    expect(setToken).not.toHaveBeenCalled()
    expect(clearToken).toHaveBeenCalled()
    expect(store.token).toBeNull()
    expect(store.loading).toBe(false)
  })

  it('clears credentials when health authentication fails', async () => {
    vi.mocked(request.get).mockRejectedValueOnce(new Error('unauthorized'))
    const store = useAuthStore()

    await expect(store.login('expired-token')).resolves.toBe(false)

    expect(store.isAuthenticated).toBe(false)
    expect(clearToken).toHaveBeenCalled()
  })

  it('logs out locally without calling the backend', () => {
    const store = useAuthStore()
    store.token = 'active-token'

    store.logout()

    expect(clearToken).toHaveBeenCalledOnce()
    expect(store.token).toBeNull()
    expect(store.isAuthenticated).toBe(false)
  })
})
