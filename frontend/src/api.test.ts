import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  createAdHocEvent,
  createProject,
  deleteProject,
  getMe,
  getProject,
  listProjects,
  patchCheckin,
  resetCsrf,
  setCheckin,
  updateProject,
} from './api'

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

  it('patchCheckin sends PATCH /api/checkins/{id} with CSRF header, idempotency key and JSON body', async () => {
    let capturedUrl = ''
    let capturedInit: RequestInit | undefined
    fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
      if (url === '/api/auth/me') return me()
      capturedUrl = url
      capturedInit = init
      return jsonRes({ id: 42, item_id: 1, date: '2026-11-07', note: 'my note' })
    })
    const res = await patchCheckin(42, { note: 'my note' })
    expect(res).toEqual({ id: 42, item_id: 1, date: '2026-11-07', note: 'my note' })
    expect(capturedUrl).toBe('/api/checkins/42')
    expect(capturedInit?.method).toBe('PATCH')
    const headers = capturedInit?.headers as Record<string, string>
    expect(headers['X-Daka-CSRF']).toBe('tok-1')
    expect(headers['Content-Type']).toBe('application/json')
    expect(headers['Idempotency-Key']).toBeDefined()
    expect(JSON.parse(capturedInit?.body as string)).toEqual({ note: 'my note' })
  })
})

describe('Projects API', () => {
  it('listProjects sends GET /api/projects with query params and redact', async () => {
    let capturedUrl = ''
    fetchMock.mockImplementation(async (url: string) => {
      capturedUrl = url
      return jsonRes([{ id: 1, name: 'P1' }])
    })

    const res = await listProjects({ status: 'active', category_id: 2, redact: true })
    expect(res).toEqual([{ id: 1, name: 'P1' }])
    expect(capturedUrl).toContain('/api/projects?')
    expect(capturedUrl).toContain('status=active')
    expect(capturedUrl).toContain('category_id=2')
    expect(capturedUrl).toContain('redact=1')
  })

  it('getProject sends GET /api/projects/{id}', async () => {
    let capturedUrl = ''
    fetchMock.mockImplementation(async (url: string) => {
      capturedUrl = url
      return jsonRes({ id: 5, name: 'P5' })
    })

    const res = await getProject(5, true)
    expect(res).toEqual({ id: 5, name: 'P5' })
    expect(capturedUrl).toBe('/api/projects/5?redact=1')
  })

  it('createProject sends POST /api/projects with JSON payload', async () => {
    let capturedUrl = ''
    let capturedInit: RequestInit | undefined
    fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
      if (url === '/api/auth/me') return me()
      capturedUrl = url
      capturedInit = init
      return jsonRes({ id: 10, name: 'Created' }, 201)
    })

    const res = await createProject({ name: 'Created', color: '#10b981' })
    expect(res).toEqual({ id: 10, name: 'Created' })
    expect(capturedUrl).toBe('/api/projects')
    expect(capturedInit?.method).toBe('POST')
    expect(JSON.parse(capturedInit?.body as string)).toEqual({
      name: 'Created',
      color: '#10b981',
    })
  })

  it('updateProject sends PATCH /api/projects/{id} with confirmExposure', async () => {
    let capturedUrl = ''
    let capturedInit: RequestInit | undefined
    fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
      if (url === '/api/auth/me') return me()
      capturedUrl = url
      capturedInit = init
      return jsonRes({ id: 10, name: 'Updated' })
    })

    const res = await updateProject(10, { name: 'Updated' }, true)
    expect(res).toEqual({ id: 10, name: 'Updated' })
    expect(capturedUrl).toBe('/api/projects/10?confirm_exposure=1')
    expect(capturedInit?.method).toBe('PATCH')
    expect(JSON.parse(capturedInit?.body as string)).toEqual({ name: 'Updated' })
  })

  it('deleteProject sends DELETE /api/projects/{id} with query options', async () => {
    let capturedUrl = ''
    let capturedInit: RequestInit | undefined
    fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
      if (url === '/api/auth/me') return me()
      capturedUrl = url
      capturedInit = init
      return jsonRes({ deleted: true })
    })

    const res = await deleteProject(10, { cascade: true, confirmExposure: true })
    expect(res).toEqual({ deleted: true })
    expect(capturedUrl).toContain('/api/projects/10?')
    expect(capturedUrl).toContain('cascade=1')
    expect(capturedUrl).toContain('confirm_exposure=1')
    expect(capturedInit?.method).toBe('DELETE')
  })
})
