import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import App from './App.vue'
import { makeRouter } from './router'
import { fmt, matchesFilter, percent, quotaPace, ringStats, valueRows } from './today/logic'
import type { ItemDetail } from './api'
import { catalogEntry } from './composables/shortcutCatalog'

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
    kind: 'ongoing',
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
  projects: object[]
  logs: object[]
  progress: { done: number; total: number }
}
let world: World
let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  localStorage.clear()
  world = {
    today: [],
    items: [],
    cats: [],
    projects: [],
    logs: [],
    progress: { done: 0, total: 0 },
  }
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
          kind: body.kind || 'ongoing',
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
        if (it) {
          if (body.category_id !== undefined) it.category_id = body.category_id
          if (body.kind !== undefined) {
            it.kind = body.kind
            const t = world.today.find((x: any) => x.id === id) as any
            if (t) t.kind = body.kind
          }
          if (body.rule) {
            it.current_rule = { ...it.current_rule, ...body.rule }
            if (body.rule.schedule_type === 'fixed_rrule') {
              world.today.push(tItem(id, it.name, { due: true, done: false }))
            } else if (body.rule.schedule_type === 'none') {
              world.today = world.today.filter((x: any) => x.id !== id)
            }
          }
        }
        return jsonRes({ id, ...body })
      }
      if (url.startsWith('/api/items/') && init?.method === 'DELETE') {
        const id = Number(url.split('/').pop())
        world.items = world.items.filter((x) => x.id !== id)
        world.today = world.today.filter((x: any) => x.id !== id)
        world.logs = world.logs.filter((x: any) => x.item_id !== id)
        return jsonRes({ ok: true })
      }
      if (url.startsWith('/api/items')) return jsonRes({ items: world.items })
      if (url.startsWith('/api/categories')) return jsonRes({ categories: world.cats })
      if (url.startsWith('/api/projects')) return jsonRes(world.projects)
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
const mounted: Array<{ unmount: () => void }> = []
afterEach(() => {
  // Leaked apps would keep their shortcut registrations and global listeners alive.
  for (const w of mounted.splice(0)) w.unmount()
  vi.unstubAllGlobals()
})

async function mountApp(path = '/', withRouter = false) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = makeRouter(createMemoryHistory())
  await router.push(path)
  await router.isReady()
  const w = mount(App, { global: { plugins: [pinia, router] }, attachTo: document.body })
  mounted.push(w)
  await flushPromises()
  return withRouter ? Object.assign(w, { router }) : w
}
const writes = () =>
  fetchMock.mock.calls.filter((c) => c[1]?.method === 'POST' && String(c[0]) === '/api/checkins')
const lastBody = () => JSON.parse(writes().at(-1)![1].body)

describe('Today URL', () => {
  it('?date loads that day, navigating updates the URL, and no parameter means today', async () => {
    const w = (await mountApp('/?date=2026-11-03', true)) as Awaited<
      ReturnType<typeof mountApp>
    > & {
      router: ReturnType<typeof makeRouter>
    }
    const todayUrls = () =>
      fetchMock.mock.calls.map((c) => String(c[0])).filter((u) => u.startsWith('/api/view/today'))
    expect(todayUrls().at(-1)).toContain('date=2026-11-03')
    await w.get('[data-testid="next-day"]').trigger('click')
    await flushPromises()
    expect(w.router.currentRoute.value.query.date).toBe('2026-11-04')
    await w.router.back()
    await flushPromises()
    expect(todayUrls().at(-1)).toContain('date=2026-11-03')
    await w.router.push('/')
    await flushPromises()
    expect(todayUrls().at(-1)).not.toContain('date=')
    w.unmount()
  })
})

describe('quotaPace', () => {
  const q = (used: number, over: Record<string, unknown> = {}) =>
    ({
      stats: {
        quota: {
          comparator: 'at_least',
          window: 'calendar',
          period: 'week',
          window_end: '2026-11-15',
          used,
          limit: 3,
          remaining: 3 - used,
          ratio: used / 3,
          met: used >= 3,
          exceeded: false,
          ...over,
        },
      },
    }) as unknown as ItemDetail
  it.each([
    ['2026-11-12', 1, 'relaxed', 2, 4],
    ['2026-11-14', 1, 'must', 2, 2],
    ['2026-11-15', 1, 'behind', 2, 1],
    ['2026-11-12', 3, 'met', 0, 4],
  ])('%s used %i -> %s', (date, used, state, needed, daysLeft) => {
    expect(quotaPace(q(used), date)).toMatchObject({ state, needed, daysLeft })
  })
  it('counts bonus check-ins and ignores at_most quotas', () => {
    expect(quotaPace(q(5), '2026-11-12')?.extra).toBe(2)
    expect(quotaPace(q(1, { comparator: 'at_most' }), '2026-11-12')).toBeNull()
  })
})

describe('logic', () => {
  it('formats and filters', () => {
    expect(fmt(0.1 + 0.2)).toBe('0.3')
    expect(percent(1, 3)).toBe(33)
    expect(percent(0, 0)).toBe(0)
    expect(matchesFilter(null, 'none')).toBe(true)
    expect(matchesFilter(2, 1)).toBe(false)
  })

  it('ring counts only positive fixed_rrule non-maintenance ongoing items', () => {
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
    await w.get('[data-testid=card-2] [data-testid=toggle]').trigger('click')
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

  it('counter remains visible and incrementable in todoCards after meeting target', async () => {
    world.today = [tItem(1, 'Water', { tracker_type: 'counter', done: true })]
    world.items = [
      detail({
        id: 1,
        name: 'Water',
        tracker_type: 'counter',
        unit: 'glasses',
        step: 1,
        current_rule: rule({ target_value: 8 }),
      }),
    ]
    world.logs = [{ id: 1, item_id: 1, option_id: null, date: '', value: 8, note: '' }]
    const w = await mountApp()
    expect(w.find('[data-testid="card-1"]').exists()).toBe(true)
    expect(w.get('[data-testid=target-met-badge]').text()).toBe('Goal met')
    expect(w.get('[data-testid=row-value]').text()).toContain('8 / 8 glasses')
    await w.get('[data-testid=inc]').trigger('click')
    await flushPromises()
    expect(lastBody()).toEqual({ item_id: 1, mode: 'delta', value: 1 })
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
    expect(w.find('[data-testid="card-99"] [data-testid="todo-date-badge"]').exists()).toBe(false)

    // Check off the todo
    await w.get('[data-testid="card-99"] button[data-testid="toggle"]').trigger('click')
    // Push the log to world.logs to simulate backend state
    world.logs.push({ id: 101, item_id: 99, date: '2026-11-07', value: 1 })
    const { useTodayStore } = await import('./stores/today')
    await useTodayStore().refresh('2026-11-07')
    await flushPromises()

    // The completed todo stays in the pool, at the end
    expect(w.find('[data-testid="section-done"]').exists()).toBe(false)
    expect(
      w.find('[data-testid="section-unscheduled-todos"] [data-testid="card-99"]').exists(),
    ).toBe(true)
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

  it('selects a project in quick add and passes project_id to createTodoItem', async () => {
    world.projects = [
      { id: 12, name: 'Work', color: '#3b82f6', status: 'active', sort_order: 1 },
      { id: 15, name: 'Home', color: '#10b981', status: 'active', sort_order: 2 },
    ]
    const w = await mountApp()

    // Project chip is present with "No Project"
    const chip = w.get('[data-testid="quick-project-chip"]')
    expect(chip.text()).toContain('No Project')

    // Open project dropdown
    await chip.trigger('click')
    expect(w.find('[data-testid="quick-project-dropdown"]').isVisible()).toBe(true)

    // Select project 12 (Work)
    await w.get('[data-testid="quick-project-option-12"]').trigger('click')
    expect(w.get('[data-testid="quick-project-chip"]').text()).toContain('Work')

    // Fill query and submit
    await w.get('[data-testid="quick-log-input"]').setValue('Draft report')
    await w.get('[data-testid="quick-todo-submit"]').trigger('click')
    await flushPromises()

    const itemPosts = fetchMock.mock.calls.filter(
      (c) => c[1]?.method === 'POST' && String(c[0]) === '/api/items',
    )
    expect(itemPosts.length).toBeGreaterThan(0)
    const body = JSON.parse(itemPosts.at(-1)![1].body)
    expect(body.name).toBe('Draft report')
    expect(body.project_id).toBe(12)

    // After submit, project is reset to null
    expect(w.get('[data-testid="quick-project-chip"]').text()).toContain('No Project')
  })

  it('renders project badge on item card when item has project_id', async () => {
    world.projects = [{ id: 12, name: 'Work', color: '#3b82f6', status: 'active', sort_order: 1 }]
    world.today = [tItem(1, 'Project Task', { project_id: 12 })]
    world.items = [
      detail({
        id: 1,
        name: 'Project Task',
        project_id: 12,
        current_rule: rule({ schedule_type: 'fixed_rrule' }),
      }),
    ]
    const w = await mountApp()

    const card = w.get('[data-testid="card-1"]')
    const badge = card.find('[data-testid="project-badge"]')
    expect(badge.exists()).toBe(true)
    expect(badge.text()).toContain('Work')
  })

  it('increments an existing unscheduled counter item with delta mode from pinned quick log', async () => {
    world.items = [
      detail({
        id: 50,
        name: 'Coffee',
        tracker_type: 'counter',
        step: 1,
        pinned: true,
        current_rule: rule({ schedule_type: 'none' }),
      }),
    ]
    const w = await mountApp()

    const pinnedSection = w.find('[data-testid="pinned-quick-logs"]')
    expect(pinnedSection.exists()).toBe(true)
    const coffeeBtn = w.get('[data-testid="pinned-log-50"]')
    expect(coffeeBtn.text()).toContain('Coffee')

    await coffeeBtn.trigger('click')
    await flushPromises()

    const checkinPost = fetchMock.mock.calls.find((c) => {
      if (c[1]?.method !== 'POST' || String(c[0]) !== '/api/checkins') return false
      const b = JSON.parse(c[1].body)
      return b.item_id === 50
    })
    expect(checkinPost).toBeTruthy()
    const checkinBody = JSON.parse(checkinPost![1].body)
    expect(checkinBody.mode).toBe('delta')
    expect(checkinBody.value).toBe(1)
  })

  it('selects a category in quick add for ad-hoc log and avoids duplicate cards for undated logged events', async () => {
    world.cats = [
      { id: 1, name: 'Life', color: '#10b981', icon: '', is_sensitive: false, sort_order: 1 },
    ]
    world.today = [
      tItem(23, 'Buy dog food', { kind: 'event', due: true, done: true, log_ids: [25] }),
    ]
    world.items = [
      detail({
        id: 23,
        name: 'Buy dog food',
        kind: 'event',
        category_id: null,
        current_rule: rule({ schedule_type: 'none' }),
        stats: { counts: { last_30_days: 1, this_year: 1, total: 1 } },
      }),
    ]
    world.logs = [
      {
        id: 25,
        item_id: 23,
        option_id: null,
        date: '2026-11-07',
        time: '12:00:00',
        value: 0,
        note: '',
        source: 'web',
        is_supplement: false,
      },
    ]

    const w = await mountApp()

    // Logged undated event renders only once (in unscheduled-todos-list, not duplicated in todo-list)
    expect(w.find('[data-testid="section-todo"]').exists()).toBe(false)
    expect(w.findAll('[data-testid="card-23"]')).toHaveLength(1)
    expect(w.find('[data-testid="unscheduled-todos-list"] [data-testid="card-23"]').exists()).toBe(
      true,
    )

    // Pick Category 1 ("Life") in quick add and quick-log an ad-hoc event
    await w.get('[data-testid="quick-category-chip"]').trigger('click')
    expect(w.get('[data-testid="quick-category-dropdown"]').isVisible()).toBe(true)
    await w.get('[data-testid="quick-category-option-1"]').trigger('click')
    expect(w.get('[data-testid="quick-category-chip"]').text()).toContain('Life')

    await w.get('[data-testid="quick-log-input"]').setValue('Vet visit')
    await w.get('[data-testid="quick-log-submit"]').trigger('click')
    await flushPromises()

    const itemPosts = fetchMock.mock.calls.filter(
      (c) => c[1]?.method === 'POST' && String(c[0]) === '/api/items',
    )
    expect(itemPosts.length).toBeGreaterThan(0)
    const body = JSON.parse(itemPosts.at(-1)![1].body)
    expect(body.name).toBe('Vet visit')
    expect(body.kind).toBe('event')
    expect(body.category_id).toBe(1)

    // Category chip resets after submit
    expect(w.get('[data-testid="quick-category-chip"]').text()).toContain('No Category')
  })

  it('changes category, converts event to ongoing quick-log, and deletes event from OccurrenceDetailModal', async () => {
    world.cats = [
      { id: 1, name: 'Life', color: '#10b981', icon: '', is_sensitive: false, sort_order: 1 },
    ]
    world.items = [
      detail({
        id: 23,
        name: 'Buy dog food',
        kind: 'event',
        category_id: null,
        current_rule: rule({ schedule_type: 'none' }),
        stats: { counts: { last_30_days: 1, this_year: 1, total: 1 } },
      }),
    ]
    world.logs = [
      {
        id: 25,
        item_id: 23,
        option_id: null,
        date: '2026-11-07',
        time: '12:00:00',
        value: 0,
        note: '',
        source: 'web',
        is_supplement: false,
      },
    ]

    const w = await mountApp()

    // Open modal for item 23
    await w.get('[data-testid="card-23"] [data-testid="open-details"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="occurrence-modal"]').exists()).toBe(true)

    // Change category to Life (id 1)
    const catSelect = w.get('[data-testid="modal-todo-category-select"]')
    await catSelect.setValue(1)
    await catSelect.trigger('change')
    await flushPromises()

    const patchCatCall = fetchMock.mock.calls.find(
      (c) => c[1]?.method === 'PATCH' && String(c[0]) === '/api/items/23',
    )
    expect(patchCatCall).toBeTruthy()
    expect(JSON.parse(patchCatCall![1].body)).toEqual({ category_id: 1 })

    // Convert event to ongoing quick-log under the selected category
    await w.get('[data-testid="modal-convert-ongoing"]').trigger('click')
    await flushPromises()

    const convertCall = fetchMock.mock.calls
      .filter((c) => c[1]?.method === 'PATCH' && String(c[0]) === '/api/items/23')
      .pop()
    expect(convertCall).toBeTruthy()
    expect(JSON.parse(convertCall![1].body)).toEqual({
      kind: 'ongoing',
      category_id: 1,
      rule: { schedule_type: 'none', holiday_mode: 'none' },
    })

    // Item becomes ongoing (not event) so event-only sections hide while modal stays valid
    expect(w.find('[data-testid="modal-todo-category-section"]').exists()).toBe(false)
    await w.get('[data-testid="modal-close"]').trigger('click')
    await flushPromises()

    // Now test deleting an event item via inline confirmation
    world.items.push(
      detail({
        id: 24,
        name: 'Temp note',
        kind: 'event',
        category_id: null,
        current_rule: rule({ schedule_type: 'none' }),
        stats: { counts: { last_30_days: 0, this_year: 0, total: 0 } },
      }),
    )
    await w.get('[data-testid="next-day"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="prev-day"]').trigger('click')
    await flushPromises()

    await w.get('[data-testid="card-24"] [data-testid="open-details"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="modal-delete-item"]').trigger('click')
    expect(w.find('[data-testid="modal-delete-confirm-box"]').exists()).toBe(true)
    await w.get('[data-testid="modal-delete-confirm"]').trigger('click')
    await flushPromises()

    const deleteCall = fetchMock.mock.calls.find(
      (c) => c[1]?.method === 'DELETE' && String(c[0]) === '/api/items/24',
    )
    expect(deleteCall).toBeTruthy()
    expect(w.find('[data-testid="occurrence-modal"]').exists()).toBe(false)
    expect(w.find('[data-testid="card-24"]').exists()).toBe(false)
  })
})

describe('Today keyboard shortcuts', () => {
  const press = (key: string, target: EventTarget = document.body) =>
    target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))
  const todayUrls = () =>
    fetchMock.mock.calls.map((c) => String(c[0])).filter((u) => u.startsWith('/api/view/today'))
  const setup = () => {
    world.today = [tItem(1, 'Read'), tItem(2, 'Pushups', { tracker_type: 'counter' })]
    world.items = [
      detail({ id: 1, name: 'Read' }),
      detail({ id: 2, name: 'Pushups', tracker_type: 'counter' }),
    ]
  }
  const focusCard = (id: number) => {
    const el = document.querySelector<HTMLElement>(`[data-testid=card-${id}]`)!
    el.focus()
    return el
  }

  it('[ ] and t navigate days', async () => {
    const w = await mountApp('/?date=2026-11-03')
    press(']')
    await flushPromises()
    expect(todayUrls().at(-1)).toContain('date=2026-11-04')
    press('[')
    await flushPromises()
    expect(todayUrls().at(-1)).toContain('date=2026-11-03')
    press('t')
    await flushPromises()
    expect(todayUrls().at(-1)).not.toContain('date=2026-11-03')
    w.unmount()
  })

  it('x toggles the focused card only', async () => {
    setup()
    const w = await mountApp()
    press('x')
    await flushPromises()
    expect(writes()).toHaveLength(0)
    focusCard(1)
    press('x', document.activeElement!)
    await flushPromises()
    expect(writes()).toHaveLength(1)
    expect(lastBody()).toEqual({ item_id: 1, mode: 'set' })
    w.unmount()
  })

  it('+ / = and - step a focused counter card', async () => {
    world.today = [tItem(2, 'Water', { tracker_type: 'counter', current_value: 2 })]
    world.items = [detail({ id: 2, name: 'Water', tracker_type: 'counter', step: 1 })]
    world.logs = [
      {
        id: 10,
        item_id: 2,
        option_id: null,
        date: '2026-11-07',
        time: '10:00:00',
        value: 2,
        note: '',
        source: 'web',
        is_supplement: false,
      },
    ]
    const w = await mountApp()
    focusCard(2)
    press('+', document.activeElement!)
    await flushPromises()
    expect(lastBody()).toEqual({ item_id: 2, mode: 'delta', value: 1 })
    press('-', document.activeElement!)
    await flushPromises()
    expect(lastBody()).toEqual({ item_id: 2, mode: 'delta', value: -1 })
    w.unmount()
  })

  it('x does nothing for a counter card, while typing, or with a modal open', async () => {
    setup()
    const w = await mountApp()
    focusCard(2)
    press('x', document.activeElement!)
    await flushPromises()
    expect(writes()).toHaveLength(0)

    focusCard(1)
    const input = document.createElement('input')
    document.querySelector('[data-testid=card-1]')!.appendChild(input)
    input.focus()
    press('x', input)
    await flushPromises()
    expect(writes()).toHaveLength(0)
    input.remove()

    const modal = document.createElement('div')
    modal.setAttribute('role', 'dialog')
    modal.setAttribute('aria-modal', 'true')
    document.body.appendChild(modal)
    focusCard(1)
    press('x', document.activeElement!)
    await flushPromises()
    expect(writes()).toHaveLength(0)
    modal.remove()
    w.unmount()
  })

  it('Enter on the card opens details once; Enter on a child button does not', async () => {
    setup()
    const w = await mountApp()
    const card = w.getComponent({ name: 'ItemCard' })
    const article = focusCard(1)
    press('Enter', article)
    await flushPromises()
    expect(card.emitted('openDetails')).toHaveLength(1)
    press('Enter', article.querySelector('[data-testid=toggle]')!)
    await flushPromises()
    expect(card.emitted('openDetails')).toHaveLength(1)
    w.unmount()
  })

  describe('arrow keys move between cards', () => {
    const ids = () =>
      [...document.querySelectorAll<HTMLElement>('[data-card]')].map((c) => c.dataset.testid)
    const active = () => (document.activeElement as HTMLElement | null)?.dataset.testid
    const arrow = (key: string, target: EventTarget = document.body) => {
      const e = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
      target.dispatchEvent(e)
      return e
    }

    it('Down/Up follow DOM order and clamp at the ends', async () => {
      world.today = [tItem(1, 'A'), tItem(2, 'B'), tItem(3, 'C')]
      world.items = [1, 2, 3].map((id) => detail({ id, name: 'n' + id }))
      const w = await mountApp()
      const order = ids()
      expect(order.length).toBe(3)
      const e = arrow('ArrowDown')
      expect(e.defaultPrevented).toBe(true)
      expect(active()).toBe(order[0])
      arrow('ArrowDown', document.activeElement!)
      arrow('ArrowDown', document.activeElement!)
      expect(active()).toBe(order[2])
      const end = arrow('ArrowDown', document.activeElement!)
      expect(active()).toBe(order[2])
      expect(end.defaultPrevented).toBe(true)
      arrow('ArrowUp', document.activeElement!)
      expect(active()).toBe(order[1])
      arrow('ArrowUp', document.activeElement!)
      arrow('ArrowUp', document.activeElement!)
      expect(active()).toBe(order[0])
      w.unmount()
    })

    it('Up from body focuses the last card', async () => {
      setup()
      const w = await mountApp()
      const order = ids()
      arrow('ArrowUp')
      expect(active()).toBe(order.at(-1))
      w.unmount()
    })

    it('skips cards inside a closed details and visits all in DOM order', async () => {
      setup()
      const w = await mountApp()
      const all = [...document.querySelectorAll<HTMLElement>('[data-card]')]
      const det = document.createElement('details')
      const hidden = document.createElement('article')
      hidden.setAttribute('data-card', '')
      hidden.tabIndex = -1
      hidden.dataset.testid = 'card-hidden'
      det.appendChild(hidden)
      all.at(-1)!.after(det)
      arrow('ArrowUp')
      expect(active()).toBe(all.at(-1)!.dataset.testid)
      det.open = true
      arrow('ArrowDown', document.activeElement!)
      expect(active()).toBe('card-hidden')
      det.remove()
      w.unmount()
    })

    it('is not handled when there are no cards', async () => {
      world.today = []
      world.items = []
      const w = await mountApp()
      expect(document.querySelectorAll('[data-card]').length).toBe(0)
      expect(arrow('ArrowDown').defaultPrevented).toBe(false)
      w.unmount()
    })

    it('is ignored while typing and keeps x and Enter working on the focused card', async () => {
      setup()
      const w = await mountApp()
      const order = ids()
      arrow('ArrowDown')
      const card = w.getComponent({ name: 'ItemCard' })
      press('x', document.activeElement!)
      await flushPromises()
      expect(lastBody()).toEqual({ item_id: 1, mode: 'set' })
      press('Enter', document.activeElement!)
      await flushPromises()
      expect(card.emitted('openDetails')).toHaveLength(1)
      document.querySelector('[role=dialog]')?.remove()

      const input = document.createElement('input')
      document.querySelector(`[data-testid=${order[0]}]`)!.appendChild(input)
      input.focus()
      const e = arrow('ArrowDown', input)
      expect(e.defaultPrevented).toBe(false)
      expect(document.activeElement).toBe(input)
      input.remove()
      w.unmount()
    })

    it('unregisters on unmount', async () => {
      setup()
      const w = await mountApp()
      w.unmount()
      expect(arrow('ArrowDown').defaultPrevented).toBe(false)
    })

    it('has labels in both locales', async () => {
      const { en } = await import('./i18n/en')
      const { default: zh } = await import('./i18n/zh-CN')
      for (const id of ['today.cardUp', 'today.cardDown']) {
        const key = catalogEntry(id)!.labelKey
        expect((en as Record<string, string>)[key]).toBeTruthy()
        expect((zh as Record<string, string>)[key]).toBeTruthy()
      }
    })
  })

  it('keeps Tab order unchanged: no positive tabindex in cards', async () => {
    setup()
    const w = await mountApp()
    const cards = document.querySelectorAll('[data-card]')
    expect(cards.length).toBeGreaterThan(0)
    for (const c of cards) {
      expect(c.getAttribute('tabindex')).toBe('-1')
      expect(c.querySelector('[tabindex]:not([tabindex="-1"]):not([tabindex="0"])')).toBeNull()
    }
    w.unmount()
  })

  it('hides unscheduled todos on past and future dates', async () => {
    world.items = [
      detail({
        id: 88,
        name: 'Someday task',
        kind: 'event',
        current_rule: {
          id: 88,
          effective_from: '2026-04-01',
          schedule_type: 'none',
          target_value: 1,
          holiday_mode: 'normal',
          option_targets: {},
        },
      }),
    ]
    const w = await mountApp()
    expect(w.find('[data-testid="section-unscheduled-todos"]').exists()).toBe(true)

    await w.get('[data-testid="prev-day"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="section-unscheduled-todos"]').exists()).toBe(false)

    await w.get('[data-testid="next-day"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="section-unscheduled-todos"]').exists()).toBe(true)

    await w.get('[data-testid="next-day"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="section-unscheduled-todos"]').exists()).toBe(false)
    w.unmount()
  })
})
