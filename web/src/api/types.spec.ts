import { describe, expect, it } from 'vitest'
import { i18n } from '@/locales'
import { ApiError, parseApiError } from './types'

describe('parseApiError', () => {
  it('extracts structured HTTP errors', () => {
    const parsed = parseApiError({
      response: { status: 403, data: { error: 'quota exceeded', code: 'QUOTA' } },
    })
    expect(parsed).toBeInstanceOf(ApiError)
    expect(parsed?.status).toBe(403)
    expect(parsed?.detail).toBe('quota exceeded')
    expect(parsed?.code).toBe('QUOTA')
  })

  it('falls back to the status field and generic message', () => {
    expect(parseApiError({ response: { status: 401, data: { status: 'unauthorized' } } })?.detail).toBe('unauthorized')
    expect(parseApiError({ response: { status: 502, data: null } })?.detail).toBe(
      i18n.global.t('common.requestErrorStatus', { status: 502 }),
    )
  })

  it('returns null for transport errors and cancellations', () => {
    expect(parseApiError(null)).toBeNull()
    expect(parseApiError(new Error('network'))).toBeNull()
    expect(parseApiError({ code: 'ECONNABORTED' })).toBeNull()
  })

  it('preserves an existing ApiError', () => {
    const original = new ApiError(500, { error: 'boom' })
    expect(parseApiError(original)).toBe(original)
  })
})
