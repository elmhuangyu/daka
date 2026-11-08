import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import indexHtml from '../index.html?raw'
import { createMemoryHistory } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import App from './App.vue'
import { makeRouter } from './router'

function jsonRes(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
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
          ? jsonRes({ date: '2026-11-07', weekday: 6, timezone: 'UTC', progress: { done: 0, total: 0 }, items: [] })
          : jsonRes({}),
    ),
  )
})
afterEach(() => {
  vi.unstubAllGlobals()
})

describe('App', () => {
  it('never changes the document title', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const router = makeRouter(createMemoryHistory())
    await router.push('/')
    await router.isReady()
    mount(App, { global: { plugins: [pinia, router] } })
    await flushPromises()
    expect(document.title).toBe('Daka')
    expect(indexHtml).toContain('<title>Daka</title>')
  })
})
