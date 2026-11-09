import { describe, expect, it } from 'vitest'
import { ApiError, describeError } from '../api'
import { en } from './en'
import {
  ERROR_KEYS,
  EXT_CAL_ERROR_KEYS,
  errorKeyForCode,
  errorKeyForStatus,
  extCalErrorKey,
} from './errors'
import { setLocale } from './index'
import zhCN from './zh-CN'

const apiSource = Object.values(
  import.meta.glob<string>('../api.ts', { query: '?raw', import: 'default', eager: true }),
)[0]!

describe('error code mapping', () => {
  it('maps every code to an existing message key in every locale', () => {
    for (const [code, key] of Object.entries({ ...ERROR_KEYS, ...EXT_CAL_ERROR_KEYS })) {
      expect({ code, has: key in en }).toEqual({ code, has: true })
      expect({ code, has: key in zhCN }).toEqual({ code, has: true })
    }
  })

  it('maps every error code that api.ts mentions', () => {
    const codes = new Set([...apiSource.matchAll(/code === '([\w ]+)'/g)].map((m) => m[1]!))
    expect(codes.size).toBeGreaterThan(0)
    expect([...codes].filter((c) => errorKeyForCode(c) === null)).toEqual([])
  })

  it('falls back to a status based key for unknown codes', () => {
    expect(errorKeyForCode('something new')).toBeNull()
    expect(errorKeyForStatus(0)).toBe('errors.network')
    expect(errorKeyForStatus(401)).toBe('errors.unauthorized')
    expect(errorKeyForStatus(500)).toBe('errors.internal')
    expect(errorKeyForStatus(418)).toBe('errors.generic')
    expect(extCalErrorKey('nope')).toBe('calendar.extErr.unknown')
  })
})

describe('describeError', () => {
  it('localizes known codes, network errors and unknown codes', async () => {
    const known = new ApiError('too_many_attempts', 429, 'too_many_attempts')
    expect(describeError(known)).toBe('Too many attempts, try again later')
    expect(describeError(new ApiError('Network error', 0))).toBe('Network error')
    expect(describeError(new ApiError('boom', 418, 'boom'))).toBe('Request failed (418)')
    await setLocale('zh-CN')
    try {
      expect(describeError(known)).toBe(zhCN['errors.tooManyAttempts'])
      expect(describeError(new ApiError('Network error', 0))).toBe(zhCN['errors.network'])
    } finally {
      await setLocale('en')
    }
  })

  it('never echoes the server text of an unknown code', () => {
    const e = new ApiError('Item "Secret therapy" failed', 400, 'Item "Secret therapy" failed')
    expect(describeError(e)).not.toContain('Secret')
  })
})
