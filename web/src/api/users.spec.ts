import { beforeEach, describe, expect, it, vi } from 'vitest'
import request from './request'
import { batchDeleteUsers, deleteUser, listUsers, setUserEnabled, updateUserPassword } from './users'

vi.mock('./request', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

describe('users API', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(request.put).mockResolvedValue({ data: { status: 'ok' } } as never)
    vi.mocked(request.delete).mockResolvedValue({ data: { status: 'ok' } } as never)
    vi.mocked(request.post).mockResolvedValue({ data: { deleted: [], failed: [] } } as never)
  })

  it('lists users', async () => {
    vi.mocked(request.get).mockResolvedValueOnce({ data: { users: [] } } as never)
    await listUsers()
    expect(request.get).toHaveBeenCalledWith('/api/v1/config/users')
  })

  it('updates the password only', async () => {
    await updateUserPassword('alice', 'new-pass')
    expect(request.put).toHaveBeenCalledWith('/api/v1/config/users/alice', { password: 'new-pass' })
  })

  it('toggles a user without force by default', async () => {
    await setUserEnabled('alice', false)
    expect(request.put).toHaveBeenCalledWith(
      '/api/v1/config/users/alice',
      { enabled: false },
      { params: undefined, _skipErrorHandler: true },
    )
  })

  it('retries enabling state changes with force after confirmation', async () => {
    await setUserEnabled('alice', false, true)
    expect(request.put).toHaveBeenCalledWith(
      '/api/v1/config/users/alice',
      { enabled: false },
      { params: { force: true }, _skipErrorHandler: true },
    )
  })

  it('deletes a user and supports the forced fallback to anonymous access', async () => {
    await deleteUser('alice')
    expect(request.delete).toHaveBeenNthCalledWith(1, '/api/v1/config/users/alice', {
      params: undefined,
      _skipErrorHandler: true,
    })

    await deleteUser('alice', true)
    expect(request.delete).toHaveBeenNthCalledWith(2, '/api/v1/config/users/alice', {
      params: { force: true },
      _skipErrorHandler: true,
    })
  })

  it('passes force through batch deletion', async () => {
    await batchDeleteUsers(['alice', 'bob'], true)
    expect(request.post).toHaveBeenCalledWith(
      '/api/v1/config/users/batch-delete',
      { usernames: ['alice', 'bob'] },
      { params: { force: true }, _skipErrorHandler: true },
    )
  })
})
