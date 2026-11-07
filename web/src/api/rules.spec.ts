import { beforeEach, describe, expect, it, vi } from 'vitest'
import request from './request'
import { addRule, deleteRule, listRules, moveRule, updateRule } from './rules'

vi.mock('./request', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

describe('rules API', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('uses the expected CRUD endpoints', async () => {
    vi.mocked(request.get).mockResolvedValueOnce({ data: { rules: [] } } as never)
    vi.mocked(request.post).mockResolvedValue({ data: { status: 'ok' } } as never)
    vi.mocked(request.put).mockResolvedValue({ data: { status: 'ok' } } as never)
    vi.mocked(request.delete).mockResolvedValueOnce({ data: { status: 'ok' } } as never)

    const payload = {
      type: 'domain_suffix',
      values: ['example.com'],
      action: 'proxy' as const,
      upstreams: [],
    }

    await listRules()
    await addRule(payload)
    await updateRule(2, payload)
    await moveRule(2, 'up')
    await deleteRule(2)

    expect(request.get).toHaveBeenCalledWith('/api/v1/config/rules')
    expect(request.post).toHaveBeenNthCalledWith(1, '/api/v1/config/rules', payload)
    expect(request.put).toHaveBeenCalledWith('/api/v1/config/rules/2', payload)
    expect(request.post).toHaveBeenNthCalledWith(2, '/api/v1/config/rules/2/move', { direction: 'up' })
    expect(request.delete).toHaveBeenCalledWith('/api/v1/config/rules/2')
  })
})
