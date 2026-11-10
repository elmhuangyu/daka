import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import App from './App.vue'
import { makeRouter } from './router'
import { fmt, matchesFilter, percent, ringStats, valueRows } from './today/logic'
import type { ItemDetail } from './api'

function jsonRes(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const rule = (o: object = {}) => ({ schedule_type: 'fixed_rrule', option_targets: {}, ...o })
function detail(o: Partial<ItemDetail> & { id: number; name: string }): ItemDetail {
  return {
    tracker_type: 'boolean',
    kind: 'habit',
    polarity: 'positive',
    category_id: null,
    effective_sensitive: false,
    options: [],
    current_rule: rule(),
    ...o,
  } as ItemDetail
}
const tItem = (id: number, name: string, o: object = {}) => ({
  id,
  name,
  category_id: null,
  tracker_type: 'boolean',
  is_sensitive: false,
  due: true,
  done: false,
  log_ids: [],
  ...o,
})

interface World {
  today: object[]
  items: ItemDetail[]
  cats: object[]
  logs: object[]
  progress: { done: number; total: number }
}
let world: World
let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  localStorage.clear()
  world = { today: [], items: [], cats: [], logs: [], progress: { done: 0, total: 0 } }
  fetchMock = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>(
    async (url: string, init?: RequestInit) => {
      url = String(url)
      if (url === '/api/auth/status')
        return jsonRes({ oidc_enabled: false, login_required: false, authenticated: true })
      if (url.startsWith('/api/view/today')) {
        const u = new URL(url, 'http://localhost')
        const qDate = u.searchParams.get('date') || '2026-11-07'
        return jsonRes({
          date: qDate,
          weekday: 6,
          timezone: 'UTC',
          progress: world.progress,
          items: world.today,
        })
      }
      if (url === '/api/items' && init?.method === 'POST') {
        const body = JSON.parse(init.body as string)
        const newItem = {
          id: 99,
          name: body.name,
          tracker_type: body.tracker_type || 'boolean',
          kind: body.kind || 'habit',
          polarity: body.polarity || 'positive',
          category_id: body.category_id ?? null,
          effective_sensitive: false,
          options: [],
          current_rule: body.rule || rule(),
          stats: { counts: { last_30_days: 0, this_year: 0, total: 0 } },
        } as unknown as ItemDetail
        world.items.push(newItem)
        return jsonRes({ id: 99, ...body }, 201)
      }
      if (url.startsWith('/api/items/') && init?.method === 'PATCH') {
        const id = Number(url.split('/').pop())
        const body = JSON.parse(init.body as string)
        const it = world.items.find((x) => x.id === id)
        if (it && body.rule) {
          it.current_rule = { ...it.current_rule, ...body.rule }
          if (body.rule.schedule_type === 'fixed_rrule') {
            world.today.push(tItem(id, it.name, { due: true, done: false }))
          } else if (body.rule.schedule_type === 'none') {
            world.today = world.today.filter((x: any) => x.id !== id)
          }
        }
        return jsonRes({ id, ...body })
      }
      if (url.startsWith('/api/items')) return jsonRes({ items: world.items })
      if (url.startsWith('/api/categories')) return jsonRes({ categories: world.cats })
      if (url.startsWith('/api/checkins/') && init?.method === 'PATCH') {
        const id = Number(url.split('/').pop())
        const body = JSON.parse(init.body as string)
        const existing = (world.logs as Array<{ id: number; note: string; item_id: number }>).find(
          (l) => l.id === id,
        )
        if (existing) existing.note = body.note
        return jsonRes({ id, item_id: existing?.item_id ?? 1, date: '2026-11-07', note: body.note })
      }
      if (url.startsWith('/api/checkins') && (!init || !init.method))
        return jsonRes({ checkins: world.logs, next_cursor: null })
      return jsonRes({})
    },
  )
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => {
  vi.unstubAllGlobals()
})

async function mountApp() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = makeRouter(createMemoryHistory())
  await router.push('/')
  await router.isReady()
  const w = mount(App, { global: { plugins: [pinia, router] }, attachTo: document.body })
  await flushPromises()
  return w
}
const writes = () =>
  fetchMock.mock.calls.filter((c) => c[1]?.method === 'POST' && String(c[0]) === '/api/checkins')
const lastBody = () => JSON.parse(writes().at(-1)![1].body)

describe('logic', () => {
  it('formats and filters', () => {
    expect(fmt(0.1 + 0.2)).toBe('0.3')
    expect(percent(1, 3)).toBe(33)
    expect(percent(0, 0)).toBe(0)
    expect(matchesFilter(null, 'none')).toBe(true)
    expect(matchesFilter(2, 1)).toBe(false)
  })

  it('ring counts only positive fixed_rrule non-maintenance habits', () => {
    const mk = (d: ItemDetail, done: boolean) => ({
      detail: d,
      today: tItem(d.id, d.name, { done }) as never,
    })
    const r = ringStats([
      mk(detail({ id: 1, name: 'a' }), true),
      mk(detail({ id: 2, name: 'b' }), false),
      mk(detail({ id: 3, name: 'm', tracker_type: 'maintenance' }), true),
      mk(detail({ id: 4, name: 'n', polarity: 'negative' }), true),
      mk(detail({ id: 5, name: 'q', current_rule: rule({ schedule_type: 'quota' }) }), true),
    ])
    expect(r).toEqual({ done: 1, total: 2 })
  })

  it('builds option rows with targets and per-option totals', () => {
    const d = detail({
      id: 1,
      name: 'Ex',
      tracker_type: 'counter',
      options: [
        {
          id: 10,
          name: 'Run',
          unit: 'km',
          step: 1,
          min_val: null,
          max_val: null,
          aggregate: 'sum',
          sort_order: 0,
          target_value: null,
        },
        {
          id: 11,
          name: 'Lift',
          unit: 'sets',
          step: 5,
          min_val: null,
          max_val: null,
          aggregate: 'sum',
          sort_order: 1,
          target_value: null,
        },
      ],
      current_rule: rule({ option_targets: { '10': 2 } }),
    })
    const rows = valueRows(d, [
      { id: 1, item_id: 1, option_id: 10, date: '', value: 1, note: '' },
      { id: 2, item_id: 1, option_id: 10, date: '', value: 1.5, note: '' },
      { id: 3, item_id: 1, option_id: null, date: '', value: 9, note: 'swim' },
    ])
    expect(rows.map((r) => [r.label, r.current, r.target, r.step])).toEqual([
      ['Run', 2.5, 2, 1],
      ['Lift', 0, null, 5],
    ])
  })
})

describe('Today page', () => {
  it('shows loading, then empty state', async () => {
    const w = await mountApp()
    expect(w.find('[data-testid=empty]').exists()).toBe(true)
    expect(w.find('[data-testid=ring-percent]').text()).toBe('0%')
  })

  it('shows an error with retry', async () => {
    fetchMock.mockImplementation(async (url: string) =>
      String(url) === '/api/auth/status'
        ? jsonRes({ login_required: false, authenticated: true })
        : jsonRes({ error: 'internal error' }, 500),
    )
    const w = await mountApp()
    expect(w.get('[data-testid=error]').text()).toContain('went wrong')
    expect(w.find('[data-testid=retry]').exists()).toBe(true)
  })

  it('renders ring from server progress, and check/uncheck send set/unset with an Idempotency-Key', async () => {
    world.today = [tItem(1, 'Read'), tItem(2, 'Vitamin', { done: true, log_ids: [9] })]
    world.items = [detail({ id: 1, name: 'Read' }), detail({ id: 2, name: 'Vitamin' })]
    world.logs = [{ id: 9, item_id: 2, option_id: null, date: '2026-11-07', value: 0, note: '' }]
    world.progress = { done: 1, total: 2 }
    const w = await mountApp()
    expect(w.get('[data-testid=ring-percent]').text()).toBe('50%')
    expect(w.get('[data-testid=progress]').text()).toBe('1/2')
    await w.get('[data-testid=card-1] [data-testid=toggle]').trigger('click')
    await flushPromises()
    expect(lastBody()).toEqual({ item_id: 1, mode: 'set' })
    expect(writes()[0][1].headers['Idempotency-Key']).toMatch(/^[0-9a-f-]{36}$/)
    await w.get('[data-testid=done-list] [data-testid=toggle]').trigger('click')
    await flushPromises()
    expect(lastBody()).toEqual({ item_id: 2, mode: 'unset' })
  })

  it('disables the card while a write is in flight and shows API errors', async () => {
    world.today = [tItem(1, 'Read')]
    world.items = [detail({ id: 1, name: 'Read' })]
    const w = await mountApp()
    const base = fetchMock.getMockImplementation() as (
      u: string,
      i?: RequestInit,
    ) => Promise<Response>
    let release!: (r: Response) => void
    fetchMock.mockImplementation((url: string, init?: RequestInit) =>
      init?.method === 'POST' ? new Promise<Response>((r) => (release = r)) : base(url, init),
    )
    await w.get('[data-testid=toggle]').trigger('click')
    expect(w.get('[data-testid=toggle]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid=toggle]').trigger('click')
    release(jsonRes({ error: 'invalid body' }, 400))
    await flushPromises()
    expect(writes()).toHaveLength(1)
    expect(w.get('[data-testid=error]').text()).toContain('not valid')
  })

  it('counter: + / - / set send delta values', async () => {
    world.today = [tItem(1, 'Water', { tracker_type: 'counter' })]
    world.items = [
      detail({
        id: 1,
        name: 'Water',
        tracker_type: 'counter',
        unit: 'ml',
        step: 250,
        current_rule: rule({ target_value: 2000 }),
      }),
    ]
    world.logs = [{ id: 1, item_id: 1, option_id: null, date: '', value: 500, note: '' }]
    const w = await mountApp()
    expect(w.get('[data-testid=row-value]').text()).toContain('500 / 2000 ml')
    await w.get('[data-testid=inc]').trigger('click')
    await flushPromises()
    expect(lastBody()).toEqual({ item_id: 1, mode: 'delta', value: 250 })
    await w.get('[data-testid=dec]').trigger('click')
    await flushPromises()
    expect(lastBody()).toEqual({ item_id: 1, mode: 'delta', value: -250 })
    await w.get('[data-testid="card-1"] [data-testid="open-details"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid=set-input]').setValue('1200')
    await w.get('[data-testid=set]').trigger('click')
    await flushPromises()
    expect(lastBody()).toEqual({ item_id: 1, mode: 'delta', value: 700 })
  })

  it('options: option pick sends option_id; ad-hoc sends adhoc with name in note', async () => {
    world.today = [tItem(1, 'Exercise', { tracker_type: 'counter' })]
    world.items = [
      detail({
        id: 1,
        name: 'Exercise',
        tracker_type: 'counter',
        options: [
          {
            id: 10,
            name: 'Run',
            unit: 'km',
            step: 2,
            min_val: null,
            max_val: null,
            aggregate: 'sum',
            sort_order: 0,
            target_value: null,
          },
        ],
      }),
    ]
    const w = await mountApp()
    await w.get('[data-testid=row-opt-10] [data-testid=inc]').trigger('click')
    await flushPromises()
    expect(lastBody()).toEqual({ item_id: 1, mode: 'delta', value: 2, option_id: 10 })
    await w.get('[data-testid="card-1"] [data-testid="open-details"]').trigger('click')
    await flushPromises()
    await w.get('input[aria-label="Ad-hoc option name"]').setValue('Swim')
    await w.get('input[aria-label="Ad-hoc value"]').setValue('3')
    await w.get('[data-testid=adhoc-form]').trigger('submit')
    await flushPromises()
    expect(lastBody()).toEqual({ item_id: 1, mode: 'delta', value: 3, adhoc: true, note: 'Swim' })
  })

  it('metric: records a validated value', async () => {
    world.today = [tItem(1, 'Weight', { tracker_type: 'metric' })]
    world.items = [
      detail({
        id: 1,
        name: 'Weight',
        tracker_type: 'metric',
        unit: 'kg',
        min_val: 30,
        max_val: 200,
      }),
    ]
    const w = await mountApp()
    await w.get('[data-testid=value-input]').setValue('10')
    await w.get('[data-testid=record]').trigger('click')
    await flushPromises()
    expect(writes()).toHaveLength(0)
    expect(w.text()).toContain('Value must be between')
    await w.get('[data-testid=value-input]').setValue('72.5')
    await w.get('[data-testid=record]').trigger('click')
    await flushPromises()
    expect(lastBody()).toEqual({ item_id: 1, mode: 'append', value: 72.5 })
  })

  it('maintenance-overdue card marks done with a note', async () => {
    world.items = [
      detail({
        id: 3,
        name: 'Filter',
        tracker_type: 'maintenance',
        current_rule: rule({ schedule_type: 'rolling' }),
        stats: {
          last_at: null,
          days_since_last: 95,
          quota: null,
          maintenance: {
            next_due: '2026-11-02',
            state: 'overdue',
            days_until_due: 0,
            overdue_days: 5,
          },
        },
      }),
      detail({
        id: 4,
        name: 'Fine',
        tracker_type: 'maintenance',
        current_rule: rule({ schedule_type: 'rolling' }),
        stats: {
          last_at: null,
          days_since_last: 1,
          quota: null,
          maintenance: { next_due: '2027-01-01', state: 'ok', days_until_due: 50, overdue_days: 0 },
        },
      }),
    ]
    const w = await mountApp()
    const card = w.get('[data-testid=maintenance-list] [data-testid=card-3]')
    expect(card.get('[data-testid=maintenance-text]').text()).toContain('overdue by 5 days')
    expect(w.find('[data-testid=card-4]').exists()).toBe(false)
    await card.get('[data-testid="open-details"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="occurrence-note-input"]').setValue('new filter')
    await w.get('[data-testid="save-occurrence-note"]').trigger('click')
    await flushPromises()
    expect(lastBody()).toEqual({ item_id: 3, mode: 'set', value: 1, note: 'new filter' })
  })

  it('quota section: negative boolean logs occurrences and shows progress', async () => {
    world.items = [
      detail({
        id: 5,
        name: 'Cola',
        polarity: 'negative',
        current_rule: rule({ schedule_type: 'quota' }),
        stats: {
          last_at: null,
          days_since_last: 3,
          maintenance: null,
          quota: {
            comparator: 'at_most',
            window: 'year',
            period: 'year',
            used: 3,
            limit: 4,
            remaining: 1,
            ratio: 0.75,
            met: true,
            exceeded: false,
            traffic: 'yellow',
          },
        },
      }),
    ]
    const w = await mountApp()
    expect(w.get('[data-testid=quota-text]').text()).toContain('3 / 4')
    await w.get('[data-testid=occurrence]').trigger('click')
    await flushPromises()
    expect(lastBody()).toEqual({ item_id: 5, mode: 'append' })
  })

  it('category filter narrows cards and recomputes the ring', async () => {
    world.cats = [
      { id: 1, name: 'Work', color: '', icon: '', is_sensitive: false, sort_order: 0 },
      { id: 2, name: 'Life', color: '', icon: '', is_sensitive: false, sort_order: 1 },
    ]
    world.today = [
      tItem(1, 'Email', { category_id: 1, done: true }),
      tItem(2, 'Walk', { category_id: 2 }),
    ]
    world.items = [
      detail({ id: 1, name: 'Email', category_id: 1 }),
      detail({ id: 2, name: 'Walk', category_id: 2 }),
    ]
    world.logs = [{ id: 1, item_id: 1, option_id: null, date: '', value: 0, note: '' }]
    world.progress = { done: 1, total: 2 }
    const w = await mountApp()
    expect(w.get('[data-testid=filter-all]').text()).toContain('(2)')
    await w.get('[data-testid=filter-2]').trigger('click')
    expect(w.find('[data-testid=card-1]').exists()).toBe(false)
    expect(w.find('[data-testid=card-2]').exists()).toBe(true)
    expect(w.get('[data-testid=progress]').text()).toBe('0/1')
  })

  it('privacy: clears plaintext synchronously, requests redact=1, masks sensitive categories', async () => {
    world.cats = [
      { id: 1, name: 'SecretCat', color: '', icon: '', is_sensitive: true, sort_order: 0 },
    ]
    world.today = [tItem(1, 'Secret', { category_id: 1 })]
    world.items = [detail({ id: 1, name: 'Secret', category_id: 1, effective_sensitive: true })]
    const w = await mountApp()
    expect(w.html()).toContain('Secret')
    const base = fetchMock.getMockImplementation() as (
      u: string,
      i?: RequestInit,
    ) => Promise<Response>
    fetchMock.mockImplementation(async () => {
      throw new Error('down')
    })
    await w.get('[data-testid=redact-toggle]').trigger('click')
    expect(w.html()).not.toContain('Secret')
    await flushPromises()
    expect(w.html()).not.toContain('Secret')

    fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
      if (String(url).startsWith('/api/items'))
        return jsonRes({
          items: [
            detail({ id: 1, name: 'Private item', category_id: 1, effective_sensitive: true }),
          ],
        })
      return base(url, init)
    })
    await w.get('[data-testid=retry]').trigger('click')
    await flushPromises()
    const urls = fetchMock.mock.calls.map((c) => String(c[0]))
    expect(urls.filter((u) => u.startsWith('/api/view/today')).at(-1)).toBe(
      '/api/view/today?redact=1',
    )
    expect(urls.filter((u) => u.startsWith('/api/items')).at(-1)).toContain('redact=1')
    expect(urls.filter((u) => u.startsWith('/api/checkins')).at(-1)).toContain('redact=1')
    expect(w.html()).not.toContain('SecretCat')
    expect(w.html()).toContain('Private category')
  })

  it('ignores out-of-order responses', async () => {
    world.today = [tItem(1, 'Old')]
    world.items = [detail({ id: 1, name: 'Old' })]
    const w = await mountApp()
    const base = fetchMock.getMockImplementation() as (
      u: string,
      i?: RequestInit,
    ) => Promise<Response>
    let slow!: (r: Response) => void
    fetchMock.mockImplementationOnce(() => new Promise<Response>((r) => (slow = r))) // today, redact on
    await w.get('[data-testid=redact-toggle]').trigger('click')
    world.today = [tItem(1, 'NEWER')]
    world.items = [detail({ id: 1, name: 'NEWER' })]
    fetchMock.mockImplementation(base)
    await w.get('[data-testid=redact-toggle]').trigger('click')
    await flushPromises()
    slow(
      jsonRes({
        date: '2026-11-07',
        weekday: 6,
        timezone: 'UTC',
        progress: { done: 0, total: 0 },
        items: [tItem(1, 'STALE')],
      }),
    )
    await flushPromises()
    expect(w.html()).toContain('NEWER')
    expect(w.html()).not.toContain('STALE')
  })

  it('supports date picker and schedules future events without check-in', async () => {
    const w = await mountApp()
    expect(w.find('[data-testid=open-date-picker]').exists()).toBe(true)
    expect(w.find('[data-testid=today-date-picker]').exists()).toBe(true)

    // Navigate to next day (future)
    await w.get('[data-testid=next-day]').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('2026-11-08')

    // Quick log on future date creates scheduled event
    await w.get('[data-testid=quick-log-input]').setValue('Dentist')
    await w.get('[data-testid=quick-log-submit]').trigger('click')
    await flushPromises()

    const itemPost = fetchMock.mock.calls.find((c) => {
      if (c[1]?.method !== 'POST' || String(c[0]) !== '/api/items') return false
      const b = JSON.parse(c[1].body)
      return b.name === 'Dentist'
    })
    expect(itemPost).toBeTruthy()
    const itemBody = JSON.parse(itemPost![1].body)
    expect(itemBody.kind).toBe('event')
    expect(itemBody.rule.schedule_type).toBe('fixed_rrule')
    expect(itemBody.rule.dtstart).toBe('2026-11-08')
    expect(itemBody.rule.until).toBe('2026-11-08')

    // No check-in was posted for this future date
    const checkinPost = fetchMock.mock.calls.find(
      (c) => c[1]?.method === 'POST' && String(c[0]) === '/api/checkins',
    )
    expect(checkinPost).toBeUndefined()
  })

  it('clicking open-details on checked-in item edits note via PATCH and shows preview', async () => {
    world.today = [tItem(1, 'Read', { done: true, log_ids: [10] })]
    world.items = [detail({ id: 1, name: 'Read' })]
    world.logs = [
      {
        id: 10,
        item_id: 1,
        option_id: null,
        date: '2026-11-07',
        value: 1,
        note: 'Initial chapter',
      },
    ]
    const w = await mountApp()

    // Preview rendered on card
    expect(w.get('[data-testid="card-1"] [data-testid="card-note-preview"]').text()).toContain(
      'Initial chapter',
    )

    // Clicking card note preview opens modal
    await w.get('[data-testid="card-1"] [data-testid="card-note-preview"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="occurrence-modal"]').exists()).toBe(true)

    const noteInput = w.get('[data-testid="occurrence-note-input"]')
    expect((noteInput.element as HTMLTextAreaElement).value).toBe('Initial chapter')
    await noteInput.setValue('Finished chapter 2')

    await w.get('[data-testid="save-occurrence-note"]').trigger('click')
    await flushPromises()

    const patchCall = fetchMock.mock.calls.find(
      (c) => c[1]?.method === 'PATCH' && String(c[0]) === '/api/checkins/10',
    )
    expect(patchCall).toBeTruthy()
    expect(JSON.parse(patchCall![1].body)).toEqual({ note: 'Finished chapter 2' })

    expect(w.get('[data-testid="card-1"] [data-testid="card-note-preview"]').text()).toContain(
      'Finished chapter 2',
    )
  })

  it('modal blocks note editing when privacy.redact is active', async () => {
    world.today = [tItem(1, 'Secret Item', { done: true, log_ids: [10], is_sensitive: true })]
    world.items = [detail({ id: 1, name: 'Secret Item', effective_sensitive: true })]
    world.logs = [
      { id: 10, item_id: 1, option_id: null, date: '2026-11-07', value: 1, note: 'Private note' },
    ]
    const w = await mountApp()

    await w.get('[data-testid="card-1"] [data-testid="open-details"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="occurrence-modal"]').exists()).toBe(true)
    expect(w.find('[data-testid="occurrence-note-input"]').exists()).toBe(true)

    // Activate privacy mode
    const { usePrivacyStore } = await import('./stores/privacy')
    usePrivacyStore().set(true)
    await flushPromises()

    // Modal shows redact notice and hides note input
    expect(w.find('[data-testid="modal-redact-notice"]').exists()).toBe(true)
    expect(w.find('[data-testid="occurrence-note-input"]').exists()).toBe(false)
  })

  it('creates an uncompleted undated Todo via quick-todo-submit and verifies section-unscheduled-todos', async () => {
    const w = await mountApp()
    expect(w.find('[data-testid="section-unscheduled-todos"]').exists()).toBe(false)

    // Clear date chip to create an undated Todo (Inbox)
    await w.get('[data-testid="quick-date-clear"]').trigger('click')
    await w.get('[data-testid="quick-log-input"]').setValue('Buy groceries')
    await w.get('[data-testid="quick-todo-submit"]').trigger('click')
    await flushPromises()

    const itemPost = fetchMock.mock.calls.find((c) => {
      if (c[1]?.method !== 'POST' || String(c[0]) !== '/api/items') return false
      const b = JSON.parse(c[1].body)
      return b.name === 'Buy groceries'
    })
    expect(itemPost).toBeTruthy()
    const itemBody = JSON.parse(itemPost![1].body)
    expect(itemBody.kind).toBe('event')
    expect(itemBody.rule.schedule_type).toBe('none')

    // Appears in unscheduled todos section
    expect(w.find('[data-testid="section-unscheduled-todos"]').exists()).toBe(true)
    expect(w.find('[data-testid="card-99"]').exists()).toBe(true)
    expect(w.get('[data-testid="card-99"] [data-testid="todo-date-badge"]').text()).toBe('Undated')

    // Check off the todo
    await w.get('[data-testid="card-99"] button[data-testid="toggle"]').trigger('click')
    // Push the log to world.logs to simulate backend state
    world.logs.push({ id: 101, item_id: 99, date: '2026-11-07', value: 1 })
    const { useTodayStore } = await import('./stores/today')
    await useTodayStore().refresh('2026-11-07')
    await flushPromises()

    // Unscheduled todos section is now gone, card is in done section
    expect(w.find('[data-testid="section-unscheduled-todos"]').exists()).toBe(false)
    expect(w.find('[data-testid="section-done"]').exists()).toBe(true)
  })

  it('creates a dated Todo with quick-todo-date + quick-todo-submit', async () => {
    const w = await mountApp()

    await w.get('[data-testid="quick-todo-date"]').setValue('2026-11-15')
    await w.get('[data-testid="quick-log-input"]').setValue('Dentist checkup')
    await w.get('[data-testid="quick-todo-submit"]').trigger('click')
    await flushPromises()

    const itemPost = fetchMock.mock.calls.find((c) => {
      if (c[1]?.method !== 'POST' || String(c[0]) !== '/api/items') return false
      const b = JSON.parse(c[1].body)
      return b.name === 'Dentist checkup'
    })
    expect(itemPost).toBeTruthy()
    const itemBody = JSON.parse(itemPost![1].body)
    expect(itemBody.kind).toBe('event')
    expect(itemBody.rule.schedule_type).toBe('fixed_rrule')
    expect(itemBody.rule.dtstart).toBe('2026-11-15')
    expect(itemBody.rule.until).toBe('2026-11-15')
  })

  it('creates a Todo for Today by default via quick-todo-submit', async () => {
    const w = await mountApp()

    expect(w.find('[data-testid="quick-date-chip"]').text()).toContain('Today')
    await w.get('[data-testid="quick-log-input"]').setValue('Call dentist')
    await w.get('[data-testid="quick-todo-submit"]').trigger('click')
    await flushPromises()

    const itemPost = fetchMock.mock.calls.find((c) => {
      if (c[1]?.method !== 'POST' || String(c[0]) !== '/api/items') return false
      const b = JSON.parse(c[1].body)
      return b.name === 'Call dentist'
    })
    expect(itemPost).toBeTruthy()
    const itemBody = JSON.parse(itemPost![1].body)
    expect(itemBody.kind).toBe('event')
    expect(itemBody.rule.schedule_type).toBe('fixed_rrule')
    expect(itemBody.rule.dtstart).toBe('2026-11-07')
    expect(itemBody.rule.until).toBe('2026-11-07')
  })

  it('selects tomorrow and custom date via quick date dropdown', async () => {
    const w = await mountApp()

    // Open dropdown
    await w.get('[data-testid="quick-date-chip"]').trigger('click')
    expect(w.find('[data-testid="quick-date-dropdown"]').isVisible()).toBe(true)

    // Select Tomorrow
    await w.get('[data-testid="quick-date-tomorrow"]').trigger('click')
    expect(w.find('[data-testid="quick-date-chip"]').text()).toContain('Tomorrow')

    // Open dropdown again and select No date
    await w.get('[data-testid="quick-date-chip"]').trigger('click')
    await w.get('[data-testid="quick-date-nodate"]').trigger('click')
    expect(w.find('[data-testid="quick-date-chip"]').text()).toContain('No date (Inbox)')
    expect(w.find('[data-testid="quick-date-clear"]').exists()).toBe(false)
  })

  it('never duplicates items in doneCards', async () => {
    const { useTodayStore } = await import('./stores/today')
    const store = useTodayStore()

    const item = {
      id: 42,
      name: 'Duplicate check',
      tracker_type: 'boolean',
      kind: 'event',
      polarity: 'positive',
      current_rule: { schedule_type: 'none', holiday_mode: 'none' },
      stats: { counts: { last_30_days: 1, this_year: 1, total: 1 } },
    } as any

    store.items = [item]
    store.view = {
      date: '2026-11-07',
      weekday: 6,
      timezone: 'UTC',
      items: [
        {
          id: 42,
          name: 'Duplicate check',
          tracker_type: 'boolean',
          kind: 'event',
          due: true,
          done: true,
        } as any,
      ],
    } as any
    store.logs = [{ id: 1, item_id: 42, option_id: null, date: '2026-11-07', value: 1, note: '' }]

    const done = store.doneCards
    const ids = done.map((c) => c.detail.id)
    expect(ids.filter((id) => id === 42).length).toBe(1)
  })

  it('assigns or clears a Todo due date inside OccurrenceDetailModal', async () => {
    world.items = [
      detail({
        id: 10,
        name: 'File taxes',
        kind: 'event',
        current_rule: rule({ schedule_type: 'none' }),
        stats: { counts: { last_30_days: 0, this_year: 0, total: 0 } },
      }),
    ]
    const w = await mountApp()

    // Card exists in unscheduled todos section
    expect(w.find('[data-testid="section-unscheduled-todos"]').exists()).toBe(true)
    await w.get('[data-testid="card-10"] [data-testid="open-details"]').trigger('click')
    await flushPromises()

    expect(w.find('[data-testid="occurrence-modal"]').exists()).toBe(true)
    expect(w.find('[data-testid="modal-todo-date-section"]').exists()).toBe(true)

    // Assign due date
    await w.get('[data-testid="modal-todo-date-input"]').setValue('2026-11-20')
    await w.get('[data-testid="modal-todo-date-save"]').trigger('click')
    await flushPromises()

    const patchCall = fetchMock.mock.calls.find(
      (c) => c[1]?.method === 'PATCH' && String(c[0]) === '/api/items/10',
    )
    expect(patchCall).toBeTruthy()
    const patchBody = JSON.parse(patchCall![1].body)
    expect(patchBody.rule.schedule_type).toBe('fixed_rrule')
    expect(patchBody.rule.dtstart).toBe('2026-11-20')

    // Clear due date (make undated)
    await w.get('[data-testid="modal-todo-date-clear"]').trigger('click')
    await flushPromises()

    const clearCall = fetchMock.mock.calls
      .filter((c) => c[1]?.method === 'PATCH' && String(c[0]) === '/api/items/10')
      .pop()
    expect(clearCall).toBeTruthy()
    const clearBody = JSON.parse(clearCall![1].body)
    expect(clearBody.rule.schedule_type).toBe('none')
  })
})
