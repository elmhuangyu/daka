import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import { makeRouter } from '../router'
import App from '../App.vue'
import { usePrivacyStore } from '../stores/privacy'
import { badgesOf, groupItems, heatmapColumns, monthRange, shiftMonth } from '../items/logic'
import type { ItemDetail } from '../api'

function jsonRes(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const rule = {
  schedule_type: 'fixed_rrule',
  rrule_string: 'FREQ=DAILY',
  effective_from: '2026-01-01',
  option_targets: {},
}
const item = (id: number, name: string, extra: Record<string, unknown> = {}) => ({
  id,
  name,
  tracker_type: 'boolean',
  kind: 'ongoing',
  polarity: 'positive',
  category_id: null,
  effective_sensitive: false,
  pinned: false,
  archived: false,
  options: [],
  current_rule: rule,
  stats: {},
  ...extra,
})

let calls: { url: string; method: string; body?: unknown }[]
let listed: unknown[]
let hist: unknown

beforeEach(() => {
  localStorage.clear()
  calls = []
  listed = [
    item(1, 'Run'),
    item(2, 'Cola', {
      polarity: 'negative',
      pinned: true,
      stats: { quota: { used: 1, limit: 4, exceeded: false, traffic: 'green' } },
    }),
  ]
  hist = {
    checkins: [
      {
        id: 9,
        item_id: 1,
        option_id: null,
        date: '2026-11-05',
        time: '',
        value: 1,
        note: '',
        source: 'web',
        is_supplement: false,
      },
    ],
    next_cursor: 'abc',
  }
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    calls.push({ url, method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    if (url === '/api/auth/status')
      return jsonRes({ oidc_enabled: false, login_required: false, authenticated: true })
    if (url.startsWith('/api/items?')) return jsonRes({ items: listed })
    if (url === '/api/categories') return jsonRes({ categories: [] })
    if (/^\/api\/items\/1\/stats/.test(url)) {
      return jsonRes({
        item_id: 1,
        name: 'Run',
        today: '2026-11-07',
        last_at: null,
        last_date: '2026-11-05',
        days_since_last: 2,
        summary: '',
        counts: { last_30_days: 3, this_year: 9, total: 12 },
        active_days: {},
        options: [],
        streak: { applicable: true, unit: 'days', current: 4, longest: 7 },
        completion_rate_90: 0.5,
        heatmap: [
          { date: '2026-11-05', level: 1, value: 1 },
          { date: '2026-11-06', level: 0, value: 0 },
        ],
        quota: null,
        maintenance: null,
      })
    }
    if (/^\/api\/items\/1(\?|$)/.test(url) && method === 'GET') return jsonRes(item(1, 'Run'))
    if (/^\/api\/items\/1$/.test(url) && method === 'PATCH') return jsonRes(item(1, 'Run'))
    if (/^\/api\/items\/1$/.test(url) && method === 'DELETE')
      return new Response(null, { status: 204 })
    if (url.startsWith('/api/checkins?')) return jsonRes(hist)
    if (/^\/api\/checkins\/9$/.test(url)) return new Response(null, { status: 204 })
    if (url.startsWith('/api/view/calendar')) {
      return jsonRes({
        today: '2026-11-07',
        week_start: 0,
        days: [
          {
            date: '2026-11-01',
            weekday: 0,
            status: '',
            frozen: false,
            items: [
              {
                item_id: 1,
                due: true,
                status: 'done',
                done: true,
                forecast: false,
                overdue: false,
                log_count: 1,
              },
            ],
          },
        ],
      })
    }
    return jsonRes({})
  })
})
afterEach(() => vi.useRealTimers())

async function mountAt(path: string) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = makeRouter(createMemoryHistory())
  await router.push(path)
  await router.isReady()
  const w = mount(App, { global: { plugins: [pinia, router] } })
  await flushPromises()
  return { w, router }
}

describe('items panel', () => {
  it('lists items, groups pinned first and shows the quota badge', async () => {
    const { w } = await mountAt('/items')
    const itemCall = calls.find((c) => c.url.startsWith('/api/items?'))
    expect(itemCall?.url).toContain('kind=ongoing')
    expect(w.get('[data-testid=group-pinned]').text()).toContain('Cola')
    expect(w.get('[data-testid=group-pinned] [data-testid=badge]').text()).toBe('1/4')
    expect(w.get('[data-testid=group-none]').text()).toContain('Run')
    w.unmount()
  })

  it('excludes event items from the panel', async () => {
    listed = [
      item(1, 'Ongoing 1', { kind: 'ongoing' }),
      item(2, 'One-off Event', { kind: 'event' }),
    ]
    const { w } = await mountAt('/items')
    expect(w.text()).toContain('Ongoing 1')
    expect(w.text()).not.toContain('One-off Event')
    w.unmount()
  })

  it('debounces search and sends q', async () => {
    const { w } = await mountAt('/items')
    vi.useFakeTimers()
    const n = calls.length
    const input = w.get('[data-testid=search]')
    await input.setValue('r')
    await input.setValue('ru')
    vi.advanceTimersByTime(100)
    expect(calls.length).toBe(n)
    vi.advanceTimersByTime(300)
    await flushPromises()
    const searched = calls.slice(n).filter((c) => c.url.startsWith('/api/items?'))
    expect(searched).toHaveLength(1)
    expect(searched[0].url).toContain('q=ru')
    w.unmount()
  })

  it('pins via PATCH and queries archived items', async () => {
    const { w } = await mountAt('/items')
    await w.get('[data-testid=pin-1]').trigger('click')
    await flushPromises()
    expect(calls.find((c) => c.method === 'PATCH')).toMatchObject({
      url: '/api/items/1',
      body: { pinned: true },
    })
    await w.get('[data-testid=archived-toggle]').setValue(true)
    await flushPromises()
    expect(calls.at(-2)!.url + calls.at(-1)!.url).toContain('archived=true')
    w.unmount()
  })

  it('guard on drops names synchronously, disables search and requests redact', async () => {
    const { w } = await mountAt('/items')
    expect(w.text()).toContain('Run')
    listed = [item(1, 'Private item')]
    usePrivacyStore().set(true)
    await nextTick()
    expect(w.text()).not.toContain('Run')
    expect(w.text()).not.toContain('Cola')
    await flushPromises()
    expect(calls.at(-2)!.url + calls.at(-1)!.url).toContain('redact=1')
    expect((w.get('[data-testid=search]').element as HTMLInputElement).disabled).toBe(true)
    w.unmount()
  })

  it('moves focus between item rows with ArrowDown and ArrowUp', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const router = makeRouter(createMemoryHistory())
    await router.push('/items')
    await router.isReady()
    const w = mount(App, { global: { plugins: [pinia, router] }, attachTo: document.body })
    await flushPromises()
    ;(document.activeElement as HTMLElement | null)?.blur?.()

    window.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }),
    )
    expect((document.activeElement as HTMLElement | null)?.dataset.testid).toBe('row-2')

    window.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }),
    )
    expect((document.activeElement as HTMLElement | null)?.dataset.testid).toBe('row-1')

    window.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true, cancelable: true }),
    )
    expect((document.activeElement as HTMLElement | null)?.dataset.testid).toBe('row-2')

    w.unmount()
    document.body.innerHTML = ''
  })
})

describe('item detail', () => {
  it('shows stats, heatmap, calendar and history with pagination', async () => {
    const { w } = await mountAt('/items/1')
    expect(w.get('[data-testid=stat-streak]').text()).toContain('4')
    expect(w.get('[data-testid=stat-completion]').text()).toBe('50%')
    expect(w.findAll('[data-testid=heatmap] [data-date]')).toHaveLength(2)
    expect(w.find('[data-testid=calendar] [data-date="2026-11-01"]').classes()).toContain(
      'bg-success',
    )
    expect(w.find('[data-testid=log-9]').exists()).toBe(true)
    expect(w.get('[data-testid=edit]').attributes('href')).toBe('/items/1/edit')
    expect(w.get('[data-testid=rule-note]').text()).toContain('takes effect today')
    hist = {
      checkins: [
        {
          id: 8,
          item_id: 1,
          option_id: null,
          date: '2026-11-04',
          time: '',
          value: 1,
          note: '',
          source: 'web',
          is_supplement: false,
        },
      ],
    }
    await w.get('[data-testid=more]').trigger('click')
    await flushPromises()
    expect(calls.at(-1)!.url).toContain('cursor=abc')
    expect(w.find('[data-testid=log-8]').exists()).toBe(true)
    expect(w.find('[data-testid=more]').exists()).toBe(false)
    w.unmount()
  })

  it('deletes a check-in after confirmation', async () => {
    const { w } = await mountAt('/items/1')
    await w.get('[data-testid=del-9]').trigger('click')
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
    await w.get('[data-testid=del-9]').trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.method === 'DELETE' && c.url === '/api/checkins/9')).toBe(true)
    w.unmount()
  })

  it('archives and deletes the item', async () => {
    const { w, router } = await mountAt('/items/1')
    await w.get('[data-testid=archive]').trigger('click')
    await flushPromises()
    expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ archived: true })
    await w.get('[data-testid=delete]').trigger('click')
    await w.get('[data-testid=delete-confirm]').trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.method === 'DELETE' && c.url === '/api/items/1')).toBe(true)
    expect(router.currentRoute.value.path).toBe('/items')
    w.unmount()
  })

  it('aggregates check-ins by day with expand and collapse', async () => {
    hist = {
      checkins: [
        {
          id: 12,
          item_id: 1,
          option_id: null,
          date: '2026-11-10',
          time: '2026-11-10T15:30:00Z',
          value: 1,
          note: 'second coke',
          source: 'web',
          is_supplement: false,
        },
        {
          id: 11,
          item_id: 1,
          option_id: null,
          date: '2026-11-10',
          time: '2026-11-10T12:00:00Z',
          value: 1,
          note: 'first coke',
          source: 'web',
          is_supplement: false,
        },
        {
          id: 10,
          item_id: 1,
          option_id: null,
          date: '2026-11-09',
          time: '2026-11-09T09:00:00Z',
          value: 1,
          note: '',
          source: 'web',
          is_supplement: false,
        },
      ],
    }
    const { w } = await mountAt('/items/1')
    // Multi-entry day group exists
    expect(w.find('[data-testid=day-group-2026-11-10]').exists()).toBe(true)
    expect(w.find('[data-testid=day-header-2026-11-10]').text()).toContain('2')

    // Single-entry day renders directly
    expect(w.find('[data-testid=log-10]').exists()).toBe(true)
    expect(w.find('[data-testid=day-group-2026-11-09]').exists()).toBe(false)

    // Sub-items of 2026-11-10 are hidden initially
    const subList = w.get('[data-testid=day-list-2026-11-10]')
    expect(subList.attributes('style')).toContain('display: none')

    // Click header to expand
    await w.get('[data-testid=day-header-2026-11-10]').trigger('click')
    expect(subList.attributes('style')).not.toContain('display: none')
    expect(w.find('[data-testid=log-11]').exists()).toBe(true)
    expect(w.find('[data-testid=log-12]').exists()).toBe(true)

    // Toggle all days collapses it
    await w.get('[data-testid=toggle-all-days]').trigger('click')
    expect(subList.attributes('style')).toContain('display: none')
    w.unmount()
  })

  it('guard on clears the detail synchronously and refetches with redact', async () => {
    const { w } = await mountAt('/items/1')
    expect(w.find('[data-testid=item-name]').exists()).toBe(true)
    usePrivacyStore().set(true)
    await nextTick()
    expect(w.find('[data-testid=item-name]').exists()).toBe(false)
    await flushPromises()
    expect(calls.filter((c) => c.url.includes('redact=1')).length).toBeGreaterThanOrEqual(3)
    w.unmount()
  })

  it('hides edit button for event items in detail view', async () => {
    vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (url === '/api/auth/status')
        return jsonRes({ oidc_enabled: false, login_required: false, authenticated: true })
      if (url.startsWith('/api/items?')) return jsonRes({ items: [] })
      if (url === '/api/categories') return jsonRes({ categories: [] })
      if (/^\/api\/items\/2\/stats/.test(url)) {
        return jsonRes({
          item_id: 2,
          name: 'One-off',
          today: '2026-11-07',
          last_at: null,
          last_date: null,
          days_since_last: null,
          summary: '',
          counts: { last_30_days: 0, this_year: 0, total: 0 },
          active_days: {},
          options: [],
          streak: { applicable: false },
          completion_rate_90: null,
          heatmap: [],
          quota: null,
          maintenance: null,
        })
      }
      if (/^\/api\/items\/2(\?|$)/.test(url) && method === 'GET') {
        return jsonRes(item(2, 'One-off', { kind: 'event' }))
      }
      return jsonRes({})
    })
    const { w } = await mountAt('/items/2')
    expect(w.find('[data-testid=item-name]').exists()).toBe(true)
    expect(w.find('[data-testid=edit]').exists()).toBe(false)
    w.unmount()
  })
})

describe('items logic', () => {
  it('badges, grouping, heatmap and month helpers', () => {
    const d = {
      id: 1,
      name: 'x',
      category_id: 3,
      pinned: false,
      stats: { maintenance: { state: 'overdue', overdue_days: 5 } },
    } as unknown as ItemDetail
    expect(badgesOf(d)).toEqual([{ text: 'overdue 5d', tone: 'bad' }])
    const cats = [{ id: 3, name: 'Home', color: '', icon: '', is_sensitive: true, sort_order: 0 }]
    expect(groupItems([d], cats, 'all')[0]).toMatchObject({ title: 'Home', sensitive: true })
    expect(groupItems([d], cats, 'none')).toEqual([])
    expect(groupItems([{ ...d, kind: 'event' }], cats, 'all')).toEqual([])
    expect(heatmapColumns([{ date: '2026-11-04' }])[0].filter((c) => c === null)).toHaveLength(3)
    expect(monthRange('2024-02')).toEqual({ start: '2024-02-01', end: '2024-02-29' })
    expect(shiftMonth('2026-01', -1)).toBe('2025-12')
  })
})
