import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import { makeRouter } from '../../router'
import { resetCsrf } from '../../api'
import { useAuthStore } from '../../stores/auth'
import DataSection from './DataSection.vue'

function jsonRes(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const summary = { dry_run: true, categories: 1, items: 2, options: 3, rule_versions: 4, checkins: 5, freezes: 6, settings_applied: false }
let calls: { url: string; method: string; body?: string }[]
let importError: string | null
let reauthNeeded: boolean

beforeEach(() => {
  resetCsrf()
  calls = []
  importError = null
  reauthNeeded = false
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    calls.push({ url, method, body: init?.body ? String(init.body) : undefined })
    if (url === '/api/auth/me') return jsonRes({ id: 'u', name: 'U' })
    if (url === '/api/auth/status') return jsonRes({ oidc_enabled: false, login_required: true, authenticated: true })
    if (url === '/api/export') return new Response('{"schema":"daka.export"}', { status: 200 })
    if (url.startsWith('/api/import')) {
      if (importError) return jsonRes({ error: importError }, 400)
      return jsonRes({ ...summary, dry_run: url.includes('dry_run=1') })
    }
    if (url === '/api/account') {
      if (reauthNeeded) return jsonRes({ error: 'reauth_required' }, 403)
      return jsonRes({ deleted: true })
    }
    return jsonRes({})
  })
  URL.createObjectURL = vi.fn(() => 'blob:x')
  URL.revokeObjectURL = vi.fn()
})

async function mountSection() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore()
  await auth.init()
  const router = makeRouter(createMemoryHistory())
  await router.push('/settings')
  await router.isReady()
  const w = mount(DataSection, { global: { plugins: [pinia, router] } })
  return { w, router, auth }
}

function pickFile(w: VueWrapper, text: string) {
  const input = w.get('[data-testid=data-file]')
  const file = new File([text], 'x.json', { type: 'application/json' })
  Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
  return input.trigger('change')
}

describe('DataSection', () => {
  it('downloads the export', async () => {
    const { w } = await mountSection()
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    await w.get('[data-testid=data-export]').trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.url === '/api/export' && c.method === 'GET')).toBe(true)
    expect(click).toHaveBeenCalled()
    expect(URL.createObjectURL).toHaveBeenCalled()
    expect(w.get('[data-testid=data-notice]').text()).toContain('Export downloaded')
    click.mockRestore()
  })

  it('dry-runs a chosen file, shows the summary, then imports with settings', async () => {
    const { w } = await mountSection()
    expect(w.find('[data-testid=data-import]').exists()).toBe(false)
    await pickFile(w, '{"doc":1}')
    await flushPromises()
    const dry = calls.find((c) => c.url === '/api/import?dry_run=1')
    expect(dry?.method).toBe('POST')
    expect(dry?.body).toBe('{"doc":1}')
    expect(w.get('[data-testid=data-summary]').text()).toContain('2 items, 3 options, 4 rule versions, 5 check-ins')

    await w.get('[data-testid=data-apply-settings]').setValue(true)
    await w.get('[data-testid=data-import]').trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.url === '/api/import?apply_settings=1')).toBe(true)
    expect(w.get('[data-testid=data-notice]').text()).toContain('Imported 2 items and 5 check-ins')
    expect(w.find('[data-testid=data-import]').exists()).toBe(false)
  })

  it('shows the server validation error and offers no import', async () => {
    importError = 'invalid import: items[0]: bad_name'
    const { w } = await mountSection()
    await pickFile(w, '{}')
    await flushPromises()
    expect(w.get('[data-testid=data-error]').text()).toContain('items[0]: bad_name')
    expect(w.find('[data-testid=data-import]').exists()).toBe(false)
  })

  it('requires the typed confirmation before deleting the account', async () => {
    const { w, router, auth } = await mountSection()
    const btn = w.get('[data-testid=data-delete]')
    expect((btn.element as HTMLButtonElement).disabled).toBe(true)
    await w.get('[data-testid=data-confirm]').setValue('delete')
    expect((btn.element as HTMLButtonElement).disabled).toBe(true)
    await w.get('[data-testid=data-confirm]').setValue('DELETE')
    expect((btn.element as HTMLButtonElement).disabled).toBe(false)
    await btn.trigger('click')
    await flushPromises()
    const del = calls.find((c) => c.url === '/api/account')
    expect(del?.method).toBe('DELETE')
    expect(JSON.parse(del?.body ?? '{}')).toEqual({ confirm: 'DELETE' })
    expect(auth.authenticated).toBe(false)
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
  })

  it('reports a failed delete (reauth declined) and stays signed in', async () => {
    reauthNeeded = true
    const { w, router, auth } = await mountSection()
    await w.get('[data-testid=data-confirm]').setValue('DELETE')
    await w.get('[data-testid=data-delete]').trigger('click')
    await flushPromises()
    expect(auth.reauthOpen).toBe(true)
    auth.settleReauth(false)
    await flushPromises()
    expect(w.get('[data-testid=data-error]').text()).toContain('Confirm your identity')
    expect(router.currentRoute.value.name).toBe('settings')
    expect(auth.authenticated).toBe(true)
  })
})
