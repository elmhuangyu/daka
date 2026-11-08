import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import indexHtml from '../index.html?raw'
import App from './App.vue'

function today(items: object[] = []) {
  return { date: '2026-11-07', weekday: 6, timezone: 'UTC', progress: { done: 1, total: 3 }, items }
}
const A = { id: 1, name: 'Read', category_id: null, tracker_type: 'boolean', is_sensitive: false, due: true, done: false, log_ids: [] }
const B = { id: 2, name: 'Water', category_id: null, tracker_type: 'boolean', is_sensitive: false, due: true, done: true, log_ids: [9] }

function jsonRes(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  localStorage.clear()
  document.title = 'Daka' // mirrors index.html; the app must never change it
  fetchMock = vi.fn(async (url: string) =>
    String(url).startsWith('/api/view/today') ? jsonRes(today([A, B])) : jsonRes({}),
  )
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => {
  vi.unstubAllGlobals()
})

const calls = (prefix: string) => fetchMock.mock.calls.filter((c) => String(c[0]).startsWith(prefix))

describe('Today page', () => {
  it('renders due and done items and progress', async () => {
    const w = mount(App)
    await flushPromises()
    expect(w.get('[data-testid=due-list]').text()).toContain('Read')
    expect(w.get('[data-testid=done-list]').text()).toContain('Water')
    expect(w.get('[data-testid=progress]').text()).toBe('1/3')
    expect(document.title).toBe('Daka')
    expect(indexHtml).toContain('<title>Daka</title>')
  })

  it('clears plaintext immediately when redact turns on, even if refetch is slow or fails', async () => {
    const w = mount(App)
    await flushPromises()
    expect(w.html()).toContain('Read')
    fetchMock.mockImplementation(async () => {
      throw new Error('down')
    })
    await w.get('[data-testid=redact-toggle]').trigger('click')
    expect(w.html()).not.toContain('Read')
    await flushPromises()
    expect(w.html()).not.toContain('Read')
    expect(w.html()).not.toContain('Water')
  })

  it('ignores out-of-order responses', async () => {
    let slow!: (r: Response) => void
    const w = mount(App)
    await flushPromises()
    fetchMock.mockImplementationOnce(() => new Promise<Response>((r) => (slow = r)))
    await w.get('[data-testid=redact-toggle]').trigger('click') // slow, redact on
    fetchMock.mockImplementationOnce(async () => jsonRes(today([{ ...A, name: 'NEWER' }])))
    await w.get('[data-testid=redact-toggle]').trigger('click') // fast, redact off
    await flushPromises()
    slow(jsonRes(today([{ ...A, name: 'STALE' }])))
    await flushPromises()
    expect(w.html()).toContain('NEWER')
    expect(w.html()).not.toContain('STALE')
  })

  it('disables the button while a check is in flight', async () => {
    const w = mount(App)
    await flushPromises()
    let release!: (r: Response) => void
    fetchMock.mockImplementationOnce(() => new Promise<Response>((r) => (release = r)))
    await w.get('[data-testid=due-list] button').trigger('click')
    expect(w.get('[data-testid=due-list] button').attributes('disabled')).toBeDefined()
    release(jsonRes({}))
    await flushPromises()
    expect(calls('/api/checkins')).toHaveLength(1)
  })

  it('check sends set with Idempotency-Key then refreshes', async () => {
    const w = mount(App)
    await flushPromises()
    const before = calls('/api/view/today').length
    await w.get('[data-testid=due-list] button').trigger('click')
    await flushPromises()
    const [, init] = calls('/api/checkins')[0]
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toEqual({ item_id: 1, mode: 'set' })
    expect(init.headers['Idempotency-Key']).toMatch(/^[0-9a-f-]{36}$/)
    expect(calls('/api/view/today').length).toBe(before + 1)
  })

  it('uncheck sends unset', async () => {
    const w = mount(App)
    await flushPromises()
    await w.get('[data-testid=done-list] button').trigger('click')
    await flushPromises()
    const [, init] = calls('/api/checkins')[0]
    expect(JSON.parse(init.body)).toEqual({ item_id: 2, mode: 'unset' })
  })

  it('redact toggle adds ?redact=1 and persists', async () => {
    const w = mount(App)
    await flushPromises()
    await w.get('[data-testid=redact-toggle]').trigger('click')
    await flushPromises()
    expect(calls('/api/view/today').at(-1)![0]).toBe('/api/view/today?redact=1')
    expect(localStorage.getItem('daka.redact')).toBe('1')
    w.unmount()

    fetchMock.mockClear()
    mount(App)
    await flushPromises()
    expect(fetchMock.mock.calls[0][0]).toBe('/api/view/today?redact=1')
  })

  it('Ctrl+Shift+H toggles redact', async () => {
    const w = mount(App)
    await flushPromises()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'H', ctrlKey: true, shiftKey: true }))
    await flushPromises()
    expect(calls('/api/view/today').at(-1)![0]).toBe('/api/view/today?redact=1')
    w.unmount()
  })

  it('displays API errors', async () => {
    fetchMock.mockImplementation(async (url: string) =>
      String(url).startsWith('/api/checkins') ? jsonRes({ error: 'boom' }, 400) : jsonRes(today([A])),
    )
    const w = mount(App)
    await flushPromises()
    await w.get('[data-testid=due-list] button').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid=error]').text()).toBe('boom')
  })

  it('creates an item with the built rrule', async () => {
    const w = mount(App)
    await flushPromises()
    await w.get('input[aria-label=Name]').setValue('Run')
    await w.get('input[aria-label="Interval in days"]').setValue(3)
    await w.get('form').trigger('submit')
    await flushPromises()
    const [, init] = calls('/api/items')[0]
    expect(JSON.parse(init.body)).toEqual({
      name: 'Run',
      tracker_type: 'boolean',
      kind: 'habit',
      polarity: 'positive',
      rule: { schedule_type: 'fixed_rrule', rrule_string: 'FREQ=DAILY;INTERVAL=3', holiday_mode: 'none' },
    })
  })

  it('requires a weekday for weekly presets', async () => {
    const w = mount(App)
    await flushPromises()
    await w.get('input[aria-label=Name]').setValue('Run')
    await w.get('select').setValue('weekly')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[data-testid=error]').text()).toContain('at least one weekday')
    expect(calls('/api/items')).toHaveLength(0)
  })
})
