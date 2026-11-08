import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import {
  ApiError, describeError, getAuthStatus, logout, resetCsrf, setCheckin, setAuthHooks,
  broadcastAuthEvent,
} from './api'
import { useAuthStore } from './stores/auth'
import ReauthModal from './components/ReauthModal.vue'

function jsonRes(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

let fetchMock: ReturnType<typeof vi.fn>
let routes: Record<string, () => Response>

beforeEach(() => {
  resetCsrf()
  setAuthHooks({})
  routes = {
    '/api/auth/me': () => jsonRes({ id: 'u', name: 'U', csrf_token: 'tok' }),
    '/api/auth/status': () => jsonRes({ oidc_enabled: false, login_required: true, authenticated: true }),
  }
  fetchMock = vi.fn(async (url: string) => (routes[url] ? routes[url]() : jsonRes({})))
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => {
  vi.unstubAllGlobals()
})

const callsTo = (url: string) => fetchMock.mock.calls.filter((c) => c[0] === url)

describe('api auth helpers', () => {
  it('sends credentials and maps JSON errors to ApiError with code', async () => {
    routes['/api/auth/status'] = () => jsonRes({ error: 'too_many_attempts' }, 429)
    const err = await getAuthStatus().catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(429)
    expect(err.code).toBe('too_many_attempts')
    expect(describeError(err)).toBe('Too many attempts, try again later')
    expect(fetchMock.mock.calls[0][1].credentials).toBe('same-origin')
  })

  it('maps network failures to status 0', async () => {
    fetchMock.mockImplementation(async () => {
      throw new TypeError('offline')
    })
    const err = await getAuthStatus().catch((e) => e)
    expect(err.status).toBe(0)
    expect(describeError(err)).toBe('Network error')
  })

  it('treats 204 logout as success and drops the cached csrf token', async () => {
    routes['/api/auth/logout'] = () => new Response(null, { status: 204 })
    await logout()
    expect(callsTo('/api/auth/logout')[0][1].headers['X-Daka-CSRF']).toBe('tok')
    await setCheckin(1, true)
    expect(callsTo('/api/auth/me')).toHaveLength(2)
  })

  it('calls onUnauthorized on 401 unauthorized but not on invalid_secret', async () => {
    const onUnauthorized = vi.fn()
    setAuthHooks({ onUnauthorized })
    routes['/api/checkins'] = () => jsonRes({ error: 'unauthorized' }, 401)
    await expect(setCheckin(1, true)).rejects.toThrow()
    expect(onUnauthorized).toHaveBeenCalledTimes(1)
    routes['/api/checkins'] = () => jsonRes({ error: 'invalid_secret' }, 401)
    await expect(setCheckin(1, true)).rejects.toThrow()
    expect(onUnauthorized).toHaveBeenCalledTimes(1)
  })

  it('retries once after reauth_required when the hook resolves true', async () => {
    const onReauthRequired = vi.fn(async () => true)
    setAuthHooks({ onReauthRequired })
    let n = 0
    routes['/api/checkins'] = () => (n++ === 0 ? jsonRes({ error: 'reauth_required' }, 403) : jsonRes({ ok: true }))
    await setCheckin(1, true)
    expect(onReauthRequired).toHaveBeenCalledTimes(1)
    expect(callsTo('/api/checkins')).toHaveLength(2)
  })

  it('surfaces reauth_required when the user cancels', async () => {
    setAuthHooks({ onReauthRequired: async () => false })
    routes['/api/checkins'] = () => jsonRes({ error: 'reauth_required' }, 403)
    await expect(setCheckin(1, true)).rejects.toMatchObject({ code: 'reauth_required' })
    expect(callsTo('/api/checkins')).toHaveLength(1)
  })
})

describe('auth store', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('single-user mode: no login required, user loaded', async () => {
    routes['/api/auth/status'] = () => jsonRes({ oidc_enabled: false, login_required: false, authenticated: true })
    const auth = useAuthStore()
    await auth.init()
    expect(auth.loginRequired).toBe(false)
    expect(auth.authenticated).toBe(true)
    expect(auth.user?.name).toBe('U')
  })

  it('unauthenticated: does not load the user; init runs once', async () => {
    routes['/api/auth/status'] = () => jsonRes({ oidc_enabled: true, login_required: true, authenticated: false })
    const auth = useAuthStore()
    await auth.init()
    await auth.init()
    expect(auth.oidcEnabled).toBe(true)
    expect(auth.authenticated).toBe(false)
    expect(callsTo('/api/auth/status')).toHaveLength(1)
    expect(callsTo('/api/auth/me')).toHaveLength(0)
  })

  it('secret login posts the secret without a csrf header and authenticates', async () => {
    routes['/api/auth/status'] = () => jsonRes({ oidc_enabled: false, login_required: true, authenticated: false })
    const auth = useAuthStore()
    await auth.init()
    await auth.loginWithSecret('s3')
    const [, init] = callsTo('/api/auth/login')[0]
    expect(JSON.parse(init.body)).toEqual({ secret: 's3' })
    expect(init.headers['X-Daka-CSRF']).toBeUndefined()
    expect(auth.authenticated).toBe(true)
    expect(auth.user?.name).toBe('U')
  })

  it('failed login keeps the user signed out', async () => {
    routes['/api/auth/login'] = () => jsonRes({ error: 'invalid_secret' }, 401)
    const auth = useAuthStore()
    await expect(auth.loginWithSecret('bad')).rejects.toMatchObject({ code: 'invalid_secret' })
    expect(auth.authenticated).toBe(false)
  })

  it('logout clears state even if the request fails', async () => {
    const auth = useAuthStore()
    await auth.init()
    routes['/api/auth/logout'] = () => jsonRes({ error: 'internal error' }, 500)
    await expect(auth.logout()).rejects.toThrow()
    expect(auth.authenticated).toBe(false)
    expect(auth.user).toBeNull()
  })

  it('a 401 mid-session marks the store unauthenticated when login is required', async () => {
    const auth = useAuthStore()
    await auth.init()
    routes['/api/checkins'] = () => jsonRes({ error: 'unauthorized' }, 401)
    await expect(setCheckin(1, true)).rejects.toThrow()
    expect(auth.authenticated).toBe(false)
  })

  it('syncs authentication across tabs via broadcast channel', async () => {
    let currentAuth = false
    let currentName = ''
    routes['/api/auth/status'] = () =>
      jsonRes({ oidc_enabled: false, login_required: true, authenticated: currentAuth })
    routes['/api/auth/me'] = () => jsonRes({ id: '1', name: currentName })

    const auth = useAuthStore()
    await auth.init()
    auth.listenBroadcast()
    expect(auth.authenticated).toBe(false)

    // Simulate tab 2 logging in and broadcasting 'logged_in'
    currentAuth = true
    currentName = 'Other Tab'
    broadcastAuthEvent({ type: 'logged_in' })
    await vi.waitFor(() => expect(auth.authenticated).toBe(true))
    expect(auth.user?.name).toBe('Other Tab')

    // Simulate tab 2 logging out and broadcasting 'logged_out'
    broadcastAuthEvent({ type: 'logged_out' })
    await vi.waitFor(() => expect(auth.authenticated).toBe(false))
    expect(auth.user).toBeNull()
  })

  it('reauth modal (secret mode) calls the reauth endpoint and unblocks the request', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    await auth.init()
    let n = 0
    routes['/api/checkins'] = () => (n++ === 0 ? jsonRes({ error: 'reauth_required' }, 403) : jsonRes({ ok: true }))
    routes['/api/auth/reauth'] = () => jsonRes({ ok: true })
    const w = mount(ReauthModal, { global: { plugins: [pinia] } })
    const pending = setCheckin(1, true)
    await flushPromises()
    expect(w.find('[data-testid=reauth-modal]').exists()).toBe(true)
    await w.get('input[type=password]').setValue('s3')
    await w.get('form').trigger('submit')
    await flushPromises()
    await pending
    const [, init] = callsTo('/api/auth/reauth')[0]
    expect(JSON.parse(init.body)).toEqual({ secret: 's3' })
    expect(init.headers['X-Daka-CSRF']).toBe('tok')
    expect(w.find('[data-testid=reauth-modal]').exists()).toBe(false)
    expect(n).toBe(2)
  })

  it('reauth modal shows wrong-secret errors and stays open; cancel rejects', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    await auth.init()
    routes['/api/checkins'] = () => jsonRes({ error: 'reauth_required' }, 403)
    routes['/api/auth/reauth'] = () => jsonRes({ error: 'invalid_secret' }, 401)
    const w = mount(ReauthModal, { global: { plugins: [pinia] } })
    const pending = setCheckin(1, true).catch((e) => e)
    await flushPromises()
    await w.get('input[type=password]').setValue('bad')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[data-testid=reauth-error]').text()).toBe('Invalid secret')
    await w.get('button[type=button]').trigger('click')
    expect(await pending).toMatchObject({ code: 'reauth_required' })
    expect(w.find('[data-testid=reauth-modal]').exists()).toBe(false)
  })

  it('reauth modal (OIDC mode) navigates to the server-provided redirect', async () => {
    routes['/api/auth/status'] = () => jsonRes({ oidc_enabled: true, login_required: true, authenticated: true })
    routes['/api/auth/reauth'] = () => jsonRes({ redirect: '/api/auth/oidc/login?reauth=1' })
    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    await auth.init()
    const assign = vi.fn()
    vi.stubGlobal('location', { ...window.location, assign })
    const w = mount(ReauthModal, { global: { plugins: [pinia] } })
    void auth.requestReauth()
    await flushPromises()
    expect(w.find('input[type=password]').exists()).toBe(false)
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(assign).toHaveBeenCalledWith('/api/auth/oidc/login?reauth=1')
  })
})
