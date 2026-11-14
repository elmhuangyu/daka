import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory } from 'vue-router'
import App from './App.vue'
import { makeRouter } from './router'
import { usePrivacyStore } from './stores/privacy'

function jsonRes(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

let fetchMock: ReturnType<typeof vi.fn>
let status: { oidc_enabled: boolean; login_required: boolean; authenticated: boolean }

beforeEach(() => {
  localStorage.clear()
  status = { oidc_enabled: false, login_required: true, authenticated: true }
  fetchMock = vi.fn<(url: string) => Promise<Response>>(async (url: string) => {
    if (url === '/api/auth/status') return jsonRes(status)
    if (url === '/api/auth/me') return jsonRes({ id: 'u', name: 'Alice', csrf_token: 't' })
    if (url === '/api/auth/logout') {
      status = { ...status, authenticated: false }
      return new Response(null, { status: 204 })
    }
    if (url.startsWith('/api/view/today'))
      return jsonRes({
        date: '2026-11-07',
        weekday: 6,
        timezone: 'UTC',
        progress: { done: 0, total: 0 },
        items: [],
      })
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

  it.each(['items', 'settings'])('placeholder route /%s renders', async (name) => {
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
    expect(
      fetchMock.mock.calls
        .map((c) => String(c[0]))
        .filter((u) => u.startsWith('/api/view/today'))
        .at(-1),
    ).toBe('/api/view/today?redact=1')
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

  it('avatar button and menu display avatar image when avatar_url is provided, falling back to initial on error', async () => {
    fetchMock = vi.fn<(url: string) => Promise<Response>>(async (url: string) => {
      if (url === '/api/auth/status') return jsonRes(status)
      if (url === '/api/auth/me')
        return jsonRes({
          id: 'u',
          name: 'Alice',
          email: 'alice@example.com',
          avatar_url: 'https://example.com/pic.png',
          csrf_token: 't',
        })
      if (url.startsWith('/api/view/today'))
        return jsonRes({
          date: '2026-11-07',
          weekday: 6,
          timezone: 'UTC',
          progress: { done: 0, total: 0 },
          items: [],
        })
      return jsonRes({})
    })
    vi.stubGlobal('fetch', fetchMock)

    const { w } = await mountAt('/')
    const img = w.find('[data-testid=avatar-image]')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toBe('https://example.com/pic.png')

    // Simulate image loading error: falls back to initial
    await img.trigger('error')
    expect(w.find('[data-testid=avatar-image]').exists()).toBe(false)
    expect(w.get('[data-testid=avatar-initial]').text()).toBe('A')

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

  it('topbar shows contextual Today date pill, Settings sub-crumb, and shortcuts button', async () => {
    const { w, router } = await mountAt('/')
    expect(w.find('[data-testid=topbar-today-date]').text()).toContain('2026-11-07')
    expect(w.find('[data-testid=shortcut-help]').exists()).toBe(false)
    await w.get('[data-testid=topbar-shortcuts]').trigger('click')
    expect(w.find('[data-testid=shortcut-help]').exists()).toBe(true)
    await w.get('[data-testid=shortcut-help-close]').trigger('click')
    expect(w.find('[data-testid=shortcut-help]').exists()).toBe(false)

    await router.push('/settings?tab=categories')
    await flushPromises()
    expect(w.get('[data-testid=topbar-subcrumb]').text()).toBe('Categories')
    w.unmount()
  })

  it('topbar sub-crumb clears sensitive item name synchronously when privacy guard turns on', async () => {
    let resolveRedactedItem!: (r: Response) => void
    const redactedItemDeferred = new Promise<Response>((resolve) => {
      resolveRedactedItem = resolve
    })
    const itemPayload = (name: string) => ({
      id: 42,
      name,
      description: '',
      remind_time: null,
      tracker_type: 'boolean',
      kind: 'ongoing',
      polarity: 'positive',
      category_id: null,
      is_sensitive: true,
      effective_sensitive: true,
      options: [],
      current_rule: {
        id: 1,
        effective_from: '2026-01-01',
        rrule_string: 'FREQ=DAILY',
        holiday_mode: 'NORMAL',
        option_targets: {},
      },
    })
    const statsPayload = (name: string) => ({
      item_id: 42,
      name,
      today: '2026-11-07',
      last_at: null,
      last_date: null,
      days_since_last: null,
      summary: '',
      counts: { last_30_days: 0, this_year: 0, total: 0 },
      active_days: { last_30_days: 0, this_year: 0, total: 0 },
      options: [],
      streak: { applicable: false, current: 0, longest: 0 },
      completion_rate_90: null,
      heatmap: [],
      quota: null,
      maintenance: null,
    })

    const baseFetch = fetchMock as unknown as (url: string) => Promise<Response>
    fetchMock = vi.fn<(url: string) => Promise<Response>>(async (url: string) => {
      if (url === '/api/items/42') return jsonRes(itemPayload('Secret Journal'))
      if (url === '/api/items/42?redact=1') return redactedItemDeferred
      if (url.startsWith('/api/items/42/stats')) {
        return jsonRes(statsPayload(url.includes('redact=1') ? 'Private item' : 'Secret Journal'))
      }
      if (url.startsWith('/api/checkins')) return jsonRes({ checkins: [], next_cursor: '' })
      if (url.startsWith('/api/view/calendar')) {
        return jsonRes({ today: '2026-11-07', week_start: 1, days: [] })
      }
      return baseFetch(url)
    })
    vi.stubGlobal('fetch', fetchMock)

    const { w } = await mountAt('/items/42')
    expect(w.get('[data-testid=topbar-subcrumb]').text()).toBe('Secret Journal')

    // Plaintext crumb disappears before the ?redact=1 refetch resolves
    await w.get('[data-testid=redact-toggle]').trigger('click')
    expect(w.find('[data-testid=topbar-subcrumb]').exists()).toBe(false)

    resolveRedactedItem(jsonRes(itemPayload('Private item')))
    await flushPromises()
    expect(w.get('[data-testid=topbar-subcrumb]').text()).toBe('Private item')
    w.unmount()
  })
})
