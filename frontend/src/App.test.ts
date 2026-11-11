import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import indexHtml from '../index.html?raw'
import { createMemoryHistory } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import App from './App.vue'
import { makeRouter } from './router'
import { usePrivacyStore } from './stores/privacy'

const wrappers: VueWrapper[] = []
function mountApp(pinia: ReturnType<typeof createPinia>, router: ReturnType<typeof makeRouter>) {
  const w = mount(App, { attachTo: document.body, global: { plugins: [pinia, router] } })
  wrappers.push(w)
  return w
}

function jsonRes(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

beforeEach(() => {
  localStorage.clear()
  document.title = 'Daka'
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) =>
      url === '/api/auth/status'
        ? jsonRes({ oidc_enabled: false, login_required: false, authenticated: true })
        : String(url).startsWith('/api/view/today')
          ? jsonRes({
              date: '2026-11-07',
              weekday: 6,
              timezone: 'UTC',
              progress: { done: 0, total: 0 },
              items: [],
            })
          : jsonRes({}),
    ),
  )
})
afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount())
  vi.unstubAllGlobals()
})

describe('App', () => {
  it('never changes the document title', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const router = makeRouter(createMemoryHistory())
    await router.push('/')
    await router.isReady()
    mountApp(pinia, router)
    await flushPromises()
    expect(document.title).toBe('Daka')
    expect(indexHtml).toContain('<title>Daka</title>')
  })

  it('does not redirect hidden tab immediately on unauthenticated, but rechecks status when visible', async () => {
    let authAuthenticated = true
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) =>
        url === '/api/auth/status'
          ? jsonRes({ oidc_enabled: false, login_required: true, authenticated: authAuthenticated })
          : url === '/api/auth/me'
            ? jsonRes({ id: '1', name: 'user' })
            : String(url).startsWith('/api/view/today')
              ? jsonRes({
                  date: '2026-11-07',
                  weekday: 6,
                  timezone: 'UTC',
                  progress: { done: 0, total: 0 },
                  items: [],
                })
              : jsonRes({}),
      ),
    )

    // Simulate tab in background (hidden)
    Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true })

    const pinia = createPinia()
    setActivePinia(pinia)
    const router = makeRouter(createMemoryHistory())
    await router.push('/')
    await router.isReady()
    mountApp(pinia, router)
    await flushPromises()

    const authStore = (await import('./stores/auth')).useAuthStore()
    expect(router.currentRoute.value.name).toBe('today')

    // Session invalidated while tab is hidden
    authStore.markUnauthenticated()
    await flushPromises()

    // Must NOT have redirected to login yet because tab is hidden
    expect(router.currentRoute.value.name).toBe('today')

    // Another tab re-authenticated in the meantime
    authAuthenticated = true

    // Tab becomes visible
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()

    // Should stay on today and not bounce to login since session was restored
    expect(authStore.authenticated).toBe(true)
    expect(router.currentRoute.value.name).toBe('today')

    // If session is indeed expired when becoming visible, it redirects
    authAuthenticated = false
    authStore.markUnauthenticated()
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
  })

  it('toggles privacy with Ctrl+Shift+H from a focused input and ignores auto-repeat', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const router = makeRouter(createMemoryHistory())
    await router.push('/')
    await router.isReady()
    mountApp(pinia, router)
    await flushPromises()
    const privacy = usePrivacyStore()
    const input = document.createElement('input')
    document.body.appendChild(input)
    input.focus()
    const press = (repeat: boolean) => {
      const e = new KeyboardEvent('keydown', {
        key: 'H',
        ctrlKey: true,
        shiftKey: true,
        repeat,
        bubbles: true,
        cancelable: true,
      })
      input.dispatchEvent(e)
      return e
    }
    const before = privacy.redact
    const first = press(false)
    expect(first.defaultPrevented).toBe(true)
    expect(privacy.redact).toBe(!before)
    press(true)
    expect(privacy.redact).toBe(!before)
    press(false)
    expect(privacy.redact).toBe(before)
    input.remove()
  })
})
