import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import { makeRouter } from '../router'
import ItemEditorView from './ItemEditorView.vue'
import { usePrivacyStore } from '../stores/privacy'

function jsonRes(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

let calls: { url: string; method: string; body?: unknown }[]

beforeEach(() => {
  localStorage.clear()
  calls = []
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    calls.push({ url, method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    if (url === '/api/auth/status') return jsonRes({ oidc_enabled: false, login_required: false, authenticated: true })
    if (url === '/api/categories' && method === 'GET') return jsonRes({ categories: [] })
    if (url === '/api/items' && method === 'POST') {
      return (calls.at(-1)!.body as { name: string }).name === 'bad'
        ? jsonRes({ error: 'invalid combination', reason: 'quota_option_required' }, 400)
        : jsonRes({ id: 5 }, 201)
    }
    return jsonRes({})
  })
})

async function mountAt(path: string) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = makeRouter(createMemoryHistory())
  await router.push(path)
  await router.isReady()
  const w = mount(ItemEditorView, { global: { plugins: [pinia, router] } })
  await flushPromises()
  return { w, router }
}

describe('ItemEditorView', () => {
  it('creates an item with a weekly rule and shows a preview', async () => {
    const { w } = await mountAt('/items/new')
    await w.get('[data-testid=f-name]').setValue('Run')
    await w.get('[data-testid=f-period]').setValue('weekly')
    await w.get('[data-testid=wd-WE]').trigger('click')
    expect(w.get('[data-testid=preview-sentence]').text()).toBe('Every week on Monday, Wednesday.')
    await w.get('form').trigger('submit')
    await flushPromises()
    const post = calls.find((c) => c.url === '/api/items' && c.method === 'POST')!
    expect(post.body).toMatchObject({ name: 'Run', rule: { schedule_type: 'fixed_rrule', rrule_string: 'FREQ=WEEKLY;BYDAY=MO,WE' } })
  })

  it('hides the holiday policy for quota schedules', async () => {
    const { w } = await mountAt('/items/new')
    expect(w.find('[data-testid=holiday]').exists()).toBe(true)
    await w.get('[data-testid=f-schedule]').setValue('quota')
    expect(w.find('[data-testid=holiday]').exists()).toBe(false)
  })

  it('shows client validation and server reason codes in English', async () => {
    const { w } = await mountAt('/items/new')
    await w.get('form').trigger('submit')
    expect(w.get('[data-testid=form-errors]').text()).toContain('Name is required')
    await w.get('[data-testid=f-name]').setValue('bad')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[data-testid=submit-error]').text()).toContain('Choose which option')
  })

  it('does not load an item for editing while the privacy guard is on', async () => {
    localStorage.setItem('daka.redact', '1')
    const { w } = await mountAt('/items/3/edit')
    expect(w.find('[data-testid=privacy-blocked]').exists()).toBe(true)
    expect(calls.some((c) => c.url.startsWith('/api/items/3'))).toBe(false)
    usePrivacyStore()
  })
})
