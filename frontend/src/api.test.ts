import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAdHocEvent, getMe, resetCsrf, setCheckin } from './api'

function jsonRes(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

let fetchMock: ReturnType<typeof vi.fn>
let me: () => Response

beforeEach(() => {
  resetCsrf()
  me = () => jsonRes({ id: 'u', name: 'U', csrf_token: 'tok-1' })
  fetchMock = vi.fn<(url: string) => Promise<Response>>(async (url: string) =>
    url === '/api/auth/me' ? me() : jsonRes({}),
  )
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => {
  vi.unstubAllGlobals()
})

const writes = () => fetchMock.mock.calls.filter((c) => c[0] !== '/api/auth/me')
const meCalls = () => fetchMock.mock.calls.filter((c) => c[0] === '/api/auth/me')

describe('CSRF header', () => {
  it('is sent on writes with the token from /api/auth/me, fetched once', async () => {
    await setCheckin(1, true)
    await createAdHocEvent('x')
    expect(writes()).toHaveLength(2)
    for (const [, init] of writes()) expect(init.headers['X-Daka-CSRF']).toBe('tok-1')
    expect(meCalls()).toHaveLength(1)
  })

  it('is omitted when there is no session token (single-user loopback mode)', async () => {
    me = () => jsonRes({ id: 'default_user', name: 'default_user' })
    await setCheckin(1, true)
    expect(writes()[0][1].headers['X-Daka-CSRF']).toBeUndefined()
  })

  it('is omitted and retried later when /api/auth/me is unauthorized', async () => {
    me = () => jsonRes({ error: 'unauthorized' }, 401)
    await setCheckin(1, true)
    expect(writes()[0][1].headers['X-Daka-CSRF']).toBeUndefined()
    me = () => jsonRes({ id: 'u', name: 'U', csrf_token: 'tok-2' })
    await setCheckin(1, true)
    expect(writes()[1][1].headers['X-Daka-CSRF']).toBe('tok-2')
  })

  it('is refreshed and the write retried once after the server rejects it', async () => {
    await setCheckin(1, true)
    fetchMock.mockImplementationOnce(async () => jsonRes({ error: 'csrf' }, 403))
    me = () => jsonRes({ id: 'u', name: 'U', csrf_token: 'tok-new' })
    await setCheckin(1, true)
    expect(writes()).toHaveLength(3)
    expect(writes().at(-1)![1].headers['X-Daka-CSRF']).toBe('tok-new')
  })

  it('gives up after one retry when csrf keeps failing', async () => {
    fetchMock.mockImplementation(async (url: string) =>
      url === '/api/auth/me' ? me() : jsonRes({ error: 'csrf' }, 403),
    )
    await expect(setCheckin(1, true)).rejects.toThrow('csrf')
    expect(writes()).toHaveLength(2)
  })

  it('getMe returns the profile', async () => {
    const m = await getMe()
    expect(m.name).toBe('U')
  })
})
