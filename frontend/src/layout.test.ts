import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory } from 'vue-router'
import App from './App.vue'
import { makeRouter } from './router'
import { usePrivacyStore } from './stores/privacy'

function jsonRes(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

let fetchMock: ReturnType<typeof vi.fn>
let status: { oidc_enabled: boolean; login_required: boolean; authenticated: boolean }

beforeEach(() => {
  localStorage.clear()
  status = { oidc_enabled: false, login_required: true, authenticated: true }
  fetchMock = vi.fn(async (url: string) => {
    if (url === '/api/auth/status') return jsonRes(status)
    if (url === '/api/auth/me') return jsonRes({ id: 'u', name: 'Alice', csrf_token: 't' })
    if (url === '/api/auth/logout') {
      status = { ...status, authenticated: false }
      return new Response(null, { status: 204 })
    }
    if (url.startsWith('/api/view/today'))
      return jsonRes({ date: '2026-11-07', weekday: 6, timezone: 'UTC', progress: { done: 0, total: 0 }, items: [] })
    return jsonRes({})
  })
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

async function mountAt(path: string) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = makeRouter(createMemoryHistory())
  await router.push(path)
  await router.isReady()
  const w = mount(App, { global: { plugins: [pinia, router] }, attachTo: document.body })
  await flushPromises()
  return { w, router }
}

describe('layout', () => {
  it('renders sidebar and bottom bar with the four destinations', async () => {
    const { w } = await mountAt('/')
    for (const n of ['today', 'calendar', 'items', 'settings']) {
      expect(w.find(`[data-testid=nav-side-${n}]`).exists()).toBe(true)
      expect(w.find(`[data-testid=nav-bottom-${n}]`).exists()).toBe(true)
    }
    expect(w.findAll('nav[aria-label=Main]')).toHaveLength(2)
    expect(w.get('[data-testid=nav-side-today]').attributes('aria-current')).toBe('page')
    w.unmount()
  })

  it.each(['calendar', 'items', 'settings'])('placeholder route /%s renders', async (name) => {
    const { w } = await mountAt(`/${name}`)
    expect(w.get('h1').text().toLowerCase()).toBe(name)
    w.unmount()
  })

  it('Ctrl+Shift+H toggles the guard and persists it', async () => {
    const { w } = await mountAt('/')
    const t = w.get('[data-testid=redact-toggle]')
    expect(t.attributes('aria-pressed')).toBe('false')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'H', ctrlKey: true, shiftKey: true }))
    await flushPromises()
    expect(t.attributes('aria-pressed')).toBe('true')
    expect(localStorage.getItem('daka.redact')).toBe('1')
    expect(fetchMock.mock.calls.map((c) => String(c[0])).filter((u) => u.startsWith('/api/view/today')).at(-1)).toBe('/api/view/today?redact=1')
    expect(w.get('[data-privacy]').attributes('data-privacy')).toBe('on')
    w.unmount()
  })

  it('works when localStorage throws', () => {
    setActivePinia(createPinia())
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })
    const p = usePrivacyStore()
    expect(p.redact).toBe(false)
    p.toggle()
    expect(p.redact).toBe(true)
  })

  it('avatar menu shows the user and signs out to the login page', async () => {
    const { w, router } = await mountAt('/')
    await w.get('[data-testid=avatar-button]').trigger('click')
    expect(w.get('[data-testid=avatar-name]').text()).toBe('Alice')
    await w.get('[data-testid=logout]').trigger('click')
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
    await flushPromises()
    expect(w.find('[data-testid=avatar-button]').exists()).toBe(false)
    w.unmount()
  })

  it('single-user mode has no sign-out entry', async () => {
    status = { oidc_enabled: false, login_required: false, authenticated: true }
    const { w } = await mountAt('/')
    await w.get('[data-testid=avatar-button]').trigger('click')
    expect(w.find('[data-testid=logout]').exists()).toBe(false)
    w.unmount()
  })

  it('unauthenticated users are sent to the login page without the layout', async () => {
    status = { oidc_enabled: false, login_required: true, authenticated: false }
    const { w, router } = await mountAt('/items')
    expect(router.currentRoute.value.name).toBe('login')
    expect(w.find('nav').exists()).toBe(false)
    expect(w.find('input[type=password]').exists()).toBe(true)
    w.unmount()
  })
})
