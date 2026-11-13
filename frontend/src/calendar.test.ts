import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import { makeRouter } from './router'
import CalendarView from './views/CalendarView.vue'
import { usePrivacyStore } from './stores/privacy'
import { useCalendarStore } from './stores/calendar'
import { installShortcutListener, useShortcutList } from './composables/useShortcuts'
import {
  addDays,
  addMonths,
  eventDates,
  gridRange,
  shiftAnchor,
  startOfWeek,
} from './calendar/logic'

function jsonRes(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('calendar logic', () => {
  it('computes week starts', () => {
    expect(startOfWeek('2026-11-08', 1)).toBe('2026-11-02') // Sunday -> Monday
    expect(startOfWeek('2026-11-08', 0)).toBe('2026-11-08')
  })
  it('month grid covers whole weeks', () => {
    const r = gridRange('month', '2026-11-15', 1)
    expect(r.start).toBe('2026-10-26')
    expect(r.end).toBe('2026-12-06')
    expect(r.dates).toHaveLength(42)
    expect(gridRange('month', '2026-11-15', 0).start).toBe('2026-11-01')
  })
  it('week grid and paging', () => {
    expect(gridRange('week', '2026-11-08', 1).dates).toHaveLength(7)
    expect(shiftAnchor('week', '2026-11-08', 1)).toBe('2026-11-15')
    expect(shiftAnchor('month', '2026-01-31', -1)).toBe('2025-12-01')
    expect(addMonths('2026-12-15', 1)).toBe('2027-01-01')
    expect(addDays('2026-02-28', 1)).toBe('2026-03-01')
  })
  it('expands external events over days', () => {
    expect(
      eventDates({ title: 'x', start: '2026-11-02', end: '2026-11-04', all_day: true }),
    ).toEqual(['2026-11-02', '2026-11-03', '2026-11-04'])
    expect(
      eventDates({
        title: 'x',
        start: '2026-11-02T10:00:00+08:00',
        end: '2026-11-03T00:00:00+08:00',
      }),
    ).toEqual(['2026-11-02'])
  })
})

const item = (id: number, name: string, o: object = {}) => ({
  item_id: id,
  name,
  category_id: null,
  tracker_type: 'boolean',
  is_sensitive: false,
  due: true,
  status: 'missed',
  done: false,
  log_count: 0,
  has_supplement: false,
  forecast: false,
  overdue: false,
  ...o,
})
const day = (date: string, o: object = {}) => ({
  date,
  weekday: 0,
  day_type: 'normal',
  is_off_day: false,
  makeup_workday: false,
  frozen: false,
  status: 'none',
  due_count: 0,
  done_count: 0,
  partial_count: 0,
  has_overdue: false,
  items: [],
  ...o,
})

interface Call {
  url: string
  method: string
  headers: Record<string, string>
  body?: Record<string, unknown>
}
let calls: Call[]
let extResponses: Record<string, () => Response | Promise<Response>>
let viewDays: Record<string, object>
let viewStatus: number
let extraDetails: object[]
let checkinLogs: Array<{
  id: number
  item_id: number
  option_id: number | null
  date: string
  time: string
  value: number
  note: string
  source: string
  is_supplement: boolean
}>

beforeEach(() => {
  localStorage.clear()
  calls = []
  extResponses = {}
  checkinLogs = []
  viewStatus = 200
  extraDetails = []
  viewDays = {
    '2026-11-06': day('2026-11-06', {
      status: 'incomplete',
      due_count: 1,
      items: [item(1, 'Water')],
    }),
    '2026-11-07': day('2026-11-07', { day_type: 'holiday', holiday_name: 'Break' }),
    '2026-11-08': day('2026-11-08', {
      status: 'pending',
      due_count: 1,
      has_overdue: true,
      items: [item(2, 'Filter', { status: 'pending', overdue: true })],
    }),
    '2026-11-20': day('2026-11-20', {
      day_type: 'workday',
      status: 'upcoming',
      due_count: 1,
      items: [item(1, 'Water', { status: 'upcoming', forecast: true })],
    }),
  }
  const detail = (id: number, name: string) => ({
    id,
    name,
    tracker_type: 'boolean',
    kind: 'ongoing',
    polarity: 'positive',
    category_id: null,
    effective_sensitive: false,
    options: [],
    current_rule: { schedule_type: 'fixed_rrule', option_targets: {} },
  })
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    url = String(url)
    const method = init?.method ?? 'GET'
    calls.push({
      url,
      method,
      headers: (init?.headers ?? {}) as Record<string, string>,
      body: init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : undefined,
    })
    if (url === '/api/auth/status')
      return jsonRes({ oidc_enabled: false, login_required: false, authenticated: true })
    if (url === '/api/settings/calendar')
      return jsonRes({
        timezone: 'UTC',
        week_start: 1,
        external_calendars: [
          { id: 'a', name: 'Work', url_hint: '' },
          { id: 'b', name: 'Home', url_hint: '' },
        ],
      })
    if (url.startsWith('/api/view/calendar')) {
      if (viewStatus !== 200) return jsonRes({ error: 'internal error' }, viewStatus)
      const q = new URL(url, 'http://x').searchParams
      const days = []
      for (let d = q.get('start')!; d <= q.get('end')!; d = addDays(d, 1))
        days.push(viewDays[d] ?? day(d))
      return jsonRes({
        start: q.get('start'),
        end: q.get('end'),
        timezone: 'UTC',
        today: '2026-11-08',
        week_start: 1,
        holiday_region: 'CN',
        holiday_published: true,
        freezes: [],
        days,
      })
    }
    if (url.startsWith('/api/calendar/external')) {
      const id = new URL(url, 'http://x').searchParams.get('source_id')!
      return extResponses[id]?.() ?? jsonRes({ sources: [{ id, name: id, ok: true, events: [] }] })
    }
    if (url.startsWith('/api/items'))
      return jsonRes({ items: [detail(1, 'Water'), detail(3, 'Read'), ...extraDetails] })
    if (url.startsWith('/api/checkins') && method === 'GET')
      return jsonRes({ checkins: checkinLogs, next_cursor: null })
    if (url.startsWith('/api/checkins/') && method === 'PATCH') {
      const id = Number(url.split('/').pop())
      const body = init?.body ? (JSON.parse(String(init.body)) as { note?: string }) : {}
      const log = checkinLogs.find((l) => l.id === id)
      if (log && body.note !== undefined) log.note = body.note
      return jsonRes({
        id,
        item_id: log?.item_id ?? 1,
        date: log?.date ?? '2026-11-06',
        note: body.note ?? '',
      })
    }
    return jsonRes({})
  })
  vi.useFakeTimers({ toFake: ['Date'], now: new Date('2026-11-08T12:00:00Z') })
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

const mountedViews: Array<{ unmount: () => void }> = []
afterEach(() => {
  // Leaked views would keep their shortcut registrations alive across tests.
  for (const w of mountedViews.splice(0)) w.unmount()
})

async function mountView(path = '/calendar') {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = makeRouter(createMemoryHistory())
  await router.push(path)
  await router.isReady()
  const w = mount(CalendarView, { global: { plugins: [pinia, router] }, attachTo: document.body })
  mountedViews.push(w)
  await flushPromises()
  return { w, pinia, router }
}
const viewCalls = () =>
  calls.filter((c) => c.url.startsWith('/api/view/calendar')).map((c) => c.url)

describe('CalendarView', () => {
  it('renders the month grid with badges, ribbon and overdue marker', async () => {
    const { w } = await mountView()
    expect(viewCalls()[0]).toBe('/api/view/calendar?start=2026-10-26&end=2026-12-06')
    expect(w.findAll('[role=gridcell]')).toHaveLength(42)
    expect(w.find('[data-testid=day-2026-11-07] [data-testid=badge-off]').exists()).toBe(true)
    expect(w.find('[data-testid=day-2026-11-20] [data-testid=badge-work]').exists()).toBe(true)
    expect(w.get('[data-testid=day-2026-11-06] [data-ribbon]').attributes('data-ribbon')).toBe(
      'incomplete',
    )
    expect(w.find('[data-testid=day-2026-11-08] [data-testid=overdue-mark]').exists()).toBe(true)
    expect(w.get('[data-testid=day-2026-11-08]').attributes('aria-current')).toBe('date')
    expect(w.get('[data-testid=period-title]').text()).toBe('November 2026')
    w.unmount()
  })

  it('pages months and switches to week view', async () => {
    const { w } = await mountView()
    await w.get('[data-testid=next]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid=period-title]').text()).toBe('December 2026')
    await w.get('[data-testid=go-today]').trigger('click')
    await w.get('[data-testid=mode-week]').trigger('click')
    await flushPromises()
    expect(viewCalls().at(-1)).toBe('/api/view/calendar?start=2026-11-02&end=2026-11-08')
    expect(w.findAll('[role=gridcell]')).toHaveLength(7)
    w.unmount()
  })

  it('moves focus with arrow keys and pages with PageDown', async () => {
    const { w } = await mountView()
    expect(w.get('[data-testid=day-2026-11-08]').attributes('tabindex')).toBe('0')
    await w.get('[data-testid=grid]').trigger('keydown', { key: 'ArrowLeft' })
    await flushPromises()
    expect(w.get('[data-testid=day-2026-11-07]').attributes('tabindex')).toBe('0')
    await w.get('[data-testid=grid]').trigger('keydown', { key: 'PageDown' })
    await flushPromises()
    expect(w.get('[data-testid=period-title]').text()).toBe('December 2026')
    w.unmount()
  })

  it('isolates external source failures and shows other sources', async () => {
    extResponses.a = () =>
      jsonRes({
        sources: [{ id: 'a', name: 'Work', ok: false, error: 'fetch_failed', events: [] }],
      })
    extResponses.b = () =>
      jsonRes({
        sources: [
          {
            id: 'b',
            name: 'Home',
            ok: true,
            events: [{ title: 'Dentist', start: '2026-11-06', end: '2026-11-06', all_day: true }],
          },
        ],
      })
    const { w } = await mountView()
    expect(w.findAll('[role=gridcell]')).toHaveLength(42)
    expect(w.get('[data-testid=ext-error-a]').text()).toContain('the download failed')
    expect(w.find('[data-testid=day-2026-11-06] [data-testid=external-chips]').exists()).toBe(true)
    expect(
      calls.filter((c) => c.url.startsWith('/api/calendar/external')).map((c) => c.url),
    ).toEqual([
      '/api/calendar/external?start=2026-10-26&end=2026-12-06&source_id=a',
      '/api/calendar/external?start=2026-10-26&end=2026-12-06&source_id=b',
    ])
    w.unmount()
  })

  it('does not block the grid on a slow external source', async () => {
    extResponses.a = () => new Promise<Response>(() => {})
    const { w } = await mountView()
    expect(w.findAll('[role=gridcell]')).toHaveLength(42)
    expect(w.find('[data-testid=ext-loading]').exists()).toBe(true)
    w.unmount()
  })

  it('opens the panel and posts a makeup check-in with date and Idempotency-Key', async () => {
    const { w } = await mountView()
    await w.get('[data-testid=day-2026-11-06]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid=day-panel]').text()).toContain('Water')
    await w.get('[data-testid=makeup-toggle-1]').trigger('click')
    await flushPromises()
    const post = calls.find((c) => c.method === 'POST' && c.url === '/api/checkins')!
    expect(post.body).toEqual({ item_id: 1, mode: 'set', date: '2026-11-06' })
    expect(post.headers['Idempotency-Key']).toBeTruthy()
    w.unmount()
  })

  it('groups the day panel by project and nests sub-tasks under their ancestors', async () => {
    const d = (id: number, name: string, o: object) => ({
      id,
      name,
      tracker_type: 'boolean',
      kind: 'event',
      polarity: 'positive',
      category_id: null,
      effective_sensitive: false,
      options: [],
      current_rule: { schedule_type: 'none', option_targets: {} },
      ...o,
    })
    extraDetails = [
      d(5, 'Open', { project_id: 1 }),
      d(6, 'ChildA', { project_id: 1, parent_id: 5 }),
      d(7, 'Grandchild', { project_id: 1, parent_id: 6 }),
      d(10, 'Sub3', { project_id: 1 }),
    ]
    viewDays['2026-11-06'] = day('2026-11-06', {
      items: [
        item(1, 'Water'),
        item(7, 'Grandchild', { project_id: 1 }),
        item(3, 'Read'),
        item(10, 'Sub3', { project_id: 1 }),
      ],
    })
    const { w } = await mountView()
    await w.get('[data-testid=day-2026-11-06]').trigger('click')
    await flushPromises()
    const order = w
      .findAll('[data-testid^=panel-item-]')
      .map((e) => e.attributes('data-testid'))
      .filter((id) => /^panel-item-\d+$/.test(id ?? ''))
    expect(order).toEqual(['panel-item-1', 'panel-item-7', 'panel-item-10', 'panel-item-3'])
    w.unmount()
  })

  it('URL: ?date selects that day, selecting updates the URL, no params means today', async () => {
    const { w, router } = await mountView('/calendar?date=2026-11-06&mode=week')
    expect(w.find('[data-testid=day-panel]').exists()).toBe(true)
    expect(w.get('[data-testid=day-panel]').text()).toContain('Water')
    expect(w.findAll('[role=gridcell]')).toHaveLength(7)
    await w.get('[data-testid=day-2026-11-08]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ date: '2026-11-08', mode: 'week' })
    await router.back()
    await flushPromises()
    expect(w.get('[data-testid=day-panel]').text()).toContain('Water')
    await router.push('/calendar')
    await flushPromises()
    expect(w.find('[data-testid=day-panel]').exists()).toBe(false)
    w.unmount()
  })

  it('shows forecasts without makeup controls and the overdue marker', async () => {
    const { w } = await mountView()
    await w.get('[data-testid=day-2026-11-20]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid=panel-forecast]').text()).toContain('Water')
    expect(w.find('[data-testid=makeup-1]').exists()).toBe(false)
    await w.get('[data-testid=day-2026-11-08]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid=panel-item-2]').text()).toContain('Overdue')
    w.unmount()
  })

  it('privacy: requests redact=1 and drops plaintext synchronously', async () => {
    const { w, pinia } = await mountView()
    await w.get('[data-testid=day-2026-11-06]').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('Water')
    const dot = w.get('[data-testid=day-2026-11-06] [data-testid=dots] span')
    expect(dot.attributes('title')).toContain('Water')

    usePrivacyStore(pinia).set(true)
    await w.vm.$nextTick()
    expect(w.text()).not.toContain('Water')
    expect(w.html()).not.toContain('Water')
    await flushPromises()
    expect(viewCalls().at(-1)).toContain('redact=1')
    expect(calls.filter((c) => c.url.startsWith('/api/items')).at(-1)!.url).toContain('redact=1')
    w.unmount()
  })

  it('shows an error with retry when the view fails', async () => {
    viewStatus = 500
    const { w } = await mountView()
    expect(w.get('[data-testid=error]').text()).toContain('went wrong')
    expect(w.find('[data-testid=retry]').exists()).toBe(true)
    w.unmount()
  })

  it('dots have title attributes with item name and status', async () => {
    const { w } = await mountView()
    const dot = w.get('[data-testid=day-2026-11-06] [data-testid=dots] span')
    expect(dot.attributes('title')).toContain('Water')
    w.unmount()
  })

  it('supports header date picker', async () => {
    const { w } = await mountView()
    expect(w.find('[data-testid=open-calendar-date-picker]').exists()).toBe(true)
    const picker = w.get('[data-testid=calendar-date-picker]')
    await picker.setValue('2026-12-15')
    await picker.trigger('change')
    await flushPromises()
    expect(w.find('[data-testid=day-2026-12-15]').exists()).toBe(true)
    w.unmount()
  })

  it('quick add on a future date in day panel creates scheduled event', async () => {
    const { w } = await mountView()
    await w.get('[data-testid=day-2026-11-20]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid=day-panel]').exists()).toBe(true)
    await w.get('[data-testid=panel-quick-input]').setValue('Dentist')
    await w.get('[data-testid=panel-quick-submit]').trigger('click')
    await flushPromises()

    const itemPost = calls.find(
      (c) => c.method === 'POST' && c.url === '/api/items' && c.body?.name === 'Dentist',
    )
    expect(itemPost).toBeTruthy()
    const rule = itemPost!.body!.rule as Record<string, unknown>
    expect(rule.dtstart).toBe('2026-11-20')
    w.unmount()
  })

  it('quick add on today or past date in day panel creates uncompleted scheduled event', async () => {
    const { w } = await mountView()
    await w.get('[data-testid=day-2026-11-06]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid=day-panel]').exists()).toBe(true)
    await w.get('[data-testid=panel-quick-input]').setValue('Dentist checkup')
    await w.get('[data-testid=panel-quick-submit]').trigger('click')
    await flushPromises()

    const itemPost = calls.find(
      (c) => c.method === 'POST' && c.url === '/api/items' && c.body?.name === 'Dentist checkup',
    )
    expect(itemPost).toBeTruthy()
    const rule = itemPost!.body!.rule as Record<string, unknown>
    expect(rule.dtstart).toBe('2026-11-06')

    // No check-in call was made for this new item (so it is not marked as done!)
    const checkinPost = calls.find(
      (c) =>
        c.method === 'POST' &&
        c.url === '/api/checkins' &&
        (c.body as any)?.item_id === (itemPost?.body as any)?.id,
    )
    expect(checkinPost).toBeFalsy()
    w.unmount()
  })

  it('opens details modal from DayPanel and updates check-in note via PATCH', async () => {
    checkinLogs = [
      {
        id: 42,
        item_id: 1,
        option_id: null,
        date: '2026-11-06',
        time: '10:00:00',
        value: 1,
        note: 'Drank morning glass',
        source: 'web',
        is_supplement: false,
      },
    ]
    const { w } = await mountView()
    await w.get('[data-testid=day-2026-11-06]').trigger('click')
    await flushPromises()

    // Note snippet should appear in DayPanel
    expect(w.get('[data-testid=panel-note-1]').text()).toContain('Drank morning glass')

    // Click details button for item 1
    await w.get('[data-testid=panel-item-details-1]').trigger('click')
    await flushPromises()

    expect(w.find('[data-testid=occurrence-modal]').exists()).toBe(true)
    const input = w.get('[data-testid=occurrence-note-input]')
    expect((input.element as HTMLTextAreaElement).value).toBe('Drank morning glass')

    await input.setValue('Drank 2 glasses')
    await w.get('[data-testid=save-occurrence-note]').trigger('click')
    await flushPromises()

    const patch = calls.find((c) => c.method === 'PATCH' && c.url === '/api/checkins/42')
    expect(patch).toBeTruthy()
    expect(patch!.body).toEqual({ note: 'Drank 2 glasses' })
    w.unmount()
  })
})

describe('CalendarView shortcuts', () => {
  let uninstall: () => void
  beforeEach(() => {
    uninstall = installShortcutListener()
  })
  afterEach(() => {
    uninstall()
    document.body.innerHTML = ''
  })
  const press = (key: string, target: EventTarget = document.activeElement ?? document.body) => {
    // Vue skips nested listeners when the frozen fake Date makes them look attached later.
    vi.advanceTimersByTime(10)
    target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))
    return flushPromises()
  }
  const focusedDate = () => (document.activeElement as HTMLElement | null)?.dataset.date

  it('focuses today on entry and ArrowRight moves from there', async () => {
    const { w } = await mountView()
    expect(focusedDate()).toBe('2026-11-08')
    ;(document.activeElement as HTMLElement).blur()
    expect(document.activeElement).toBe(document.body)
    await press('ArrowRight')
    expect(focusedDate()).toBe('2026-11-09')
    await press('ArrowDown')
    expect(focusedDate()).toBe('2026-11-16')
    w.unmount()
  })

  it('leaves arrow keys and PageDown alone while the view is still loading', async () => {
    const { w } = await mountView()
    const store = useCalendarStore()
    ;(document.activeElement as HTMLElement).blur()
    store.view = null
    for (const key of ['ArrowRight', 'PageDown']) {
      const e = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
      document.body.dispatchEvent(e)
      expect(e.defaultPrevented).toBe(false)
    }
    w.unmount()
  })

  it('does not steal focus on entry or while typing', async () => {
    const input = document.createElement('input')
    document.body.appendChild(input)
    input.focus()
    const { w } = await mountView()
    expect(document.activeElement).toBe(input)
    await press('ArrowRight', input)
    await press('m', input)
    expect(document.activeElement).toBe(input)
    expect(useCalendarStore().mode).toBe('month')
    await press('w', input)
    expect(useCalendarStore().mode).toBe('month')
    w.unmount()
  })

  it('claims focus from a focused nav link', async () => {
    const nav = document.createElement('nav')
    nav.setAttribute('data-nav', '')
    const a = document.createElement('a')
    a.href = '#side'
    nav.appendChild(a)
    document.body.appendChild(nav)
    a.focus()
    expect(document.activeElement).toBe(a)
    const { w } = await mountView()
    expect(focusedDate()).toBe('2026-11-08')
    w.unmount()
  })

  it('leaves focus alone while a modal is open', async () => {
    const dlg = document.createElement('div')
    dlg.setAttribute('role', 'dialog')
    dlg.setAttribute('aria-modal', 'true')
    const nav = document.createElement('a')
    nav.setAttribute('data-nav', '')
    nav.href = '#x'
    document.body.append(dlg, nav)
    nav.focus()
    const { w } = await mountView()
    expect(document.activeElement).toBe(nav)
    w.unmount()
  })

  it('Enter on the focused cell selects it and enters the panel immediately', async () => {
    const { w } = await mountView()
    const cell = document.activeElement as HTMLButtonElement
    await press('Enter', cell)
    expect(useCalendarStore().selected).toBe('2026-11-08')
    expect((document.activeElement as HTMLElement | null)?.dataset.testid).toBe('panel-item-2')
    w.unmount()
  })

  it('m, w, t and PageDown behave', async () => {
    const { w } = await mountView()
    const store = useCalendarStore()
    await press('w')
    expect(store.mode).toBe('week')
    await press('m')
    expect(store.mode).toBe('month')
    await press('PageDown')
    expect(store.anchor.startsWith('2026-12')).toBe(true)
    expect(focusedDate()).toBeTruthy()
    expect(store.range.dates).toContain(focusedDate())
    await press(']')
    await press('[')
    await press('t')
    expect(store.anchor).toBe('2026-11-08')
    expect(focusedDate()).toBe('2026-11-08')
    w.unmount()
  })

  it('ignores keys while a modal is open and in a DayPanel input', async () => {
    const { w } = await mountView()
    const store = useCalendarStore()
    const dlg = document.createElement('div')
    dlg.setAttribute('role', 'dialog')
    dlg.setAttribute('aria-modal', 'true')
    document.body.appendChild(dlg)
    await press('w')
    expect(store.mode).toBe('month')
    dlg.remove()
    const ta = document.createElement('textarea')
    document.body.appendChild(ta)
    ta.focus()
    await press('w', ta)
    await press('ArrowRight', ta)
    expect(store.mode).toBe('month')
    expect(document.activeElement).toBe(ta)
    w.unmount()
  })

  it('header buttons keep focus on themselves', async () => {
    const { w } = await mountView()
    for (const id of ['prev', 'next', 'go-today']) {
      const btn = w.get(`[data-testid="${id}"]`).element as HTMLButtonElement
      btn.focus()
      btn.click()
      await flushPromises()
      expect(document.activeElement).toBe(btn)
    }
    w.unmount()
  })

  it('keeps exactly one tabbable cell after Today and mode switches', async () => {
    const { w } = await mountView()
    const tabbable = () => w.findAll('[data-date][tabindex="0"]')
    await w.get('[data-testid="next"]').trigger('click')
    await w.get('[data-testid="next"]').trigger('click')
    await flushPromises()
    expect(tabbable()).toHaveLength(1)
    await w.get('[data-testid="go-today"]').trigger('click')
    await flushPromises()
    expect(tabbable()).toHaveLength(1)
    expect(tabbable()[0]!.attributes('data-date')).toBe('2026-11-08')
    ;(document.activeElement as HTMLElement).blur()
    await press('ArrowRight')
    expect(focusedDate()).toBe('2026-11-09')
    await w.get('[data-testid="mode-week"]').trigger('click')
    await flushPromises()
    expect(tabbable()).toHaveLength(1)
    await w.get('[data-testid="next"]').trigger('click')
    await w.get('[data-testid="mode-month"]').trigger('click')
    await flushPromises()
    expect(tabbable()).toHaveLength(1)
    w.unmount()
  })

  describe('day item list', () => {
    const rowIds = (w: {
      findAll: (s: string) => Array<{ attributes: (k: string) => string | undefined }>
    }) => w.findAll('[data-panel-item]').map((r) => r.attributes('data-testid'))
    const activeId = () => (document.activeElement as HTMLElement | null)?.dataset.testid
    async function openDay(date = '2026-11-06', items = [item(1, 'Water'), item(3, 'Read')]) {
      viewDays[date] = day(date, { status: 'incomplete', due_count: items.length, items })
      const v = await mountView(`/calendar?date=${date}`)
      await flushPromises()
      const cell = () => document.querySelector<HTMLElement>(`[data-date="${date}"]`)!
      cell().focus()
      return { ...v, cell, store: useCalendarStore() }
    }

    it('Enter on the selected cell focuses the first row; arrows navigate the grid', async () => {
      const { w, store } = await openDay()
      expect(rowIds(w)).toEqual(['panel-item-1', 'panel-item-3'])
      await press('ArrowDown')
      expect(focusedDate()).toBe('2026-11-13')
      await press('ArrowUp')
      expect(focusedDate()).toBe('2026-11-06')
      await press('Enter')
      expect(activeId()).toBe('panel-item-1')
      expect(store.selected).toBe('2026-11-06')
      for (const r of w.findAll('[data-panel-item]')) expect(r.attributes('tabindex')).toBe('-1')
      w.unmount()
    })

    it('navigates the grid from body focus even with a day selected', async () => {
      const { w } = await openDay()
      ;(document.activeElement as HTMLElement).blur()
      await press('ArrowDown')
      expect(focusedDate()).toBe('2026-11-13')
      w.unmount()
    })

    it('moves within the list, clamps, and leaves the day alone', async () => {
      const { w, store } = await openDay()
      await press('Enter')
      await press('ArrowDown')
      expect(activeId()).toBe('panel-item-3')
      await press('ArrowDown')
      expect(activeId()).toBe('panel-item-3')
      await press('ArrowUp')
      expect(activeId()).toBe('panel-item-1')
      await press('ArrowUp')
      expect(activeId()).toBe('panel-item-1')
      await press('End')
      expect(activeId()).toBe('panel-item-3')
      await press('Home')
      expect(activeId()).toBe('panel-item-1')
      expect(store.selected).toBe('2026-11-06')
      expect(store.anchor).toBe('2026-11-06')
      w.unmount()
    })

    it('Esc returns to the cell keeping the selection, a second Esc closes, then weeks navigate', async () => {
      const { w, store } = await openDay()
      await press('Enter')
      await press('Escape')
      expect(focusedDate()).toBe('2026-11-06')
      expect(store.selected).toBe('2026-11-06')
      await press('Escape')
      expect(store.selected).toBeNull()
      expect(focusedDate()).toBe('2026-11-06')
      await press('ArrowDown')
      expect(focusedDate()).toBe('2026-11-13')
      w.unmount()
    })

    it('Left and Right still move between days with a day selected', async () => {
      const { w, store } = await openDay()
      await press('ArrowRight')
      expect(focusedDate()).toBe('2026-11-07')
      await press('ArrowLeft')
      expect(focusedDate()).toBe('2026-11-06')
      expect(store.selected).toBe('2026-11-06')
      w.unmount()
    })

    it('keeps week navigation for a selected day without rows and Enter focuses quick-add input', async () => {
      const { w } = await openDay('2026-11-10', [])
      expect(rowIds(w)).toEqual([])
      await press('ArrowDown')
      expect(focusedDate()).toBe('2026-11-17')
      await press('ArrowUp')
      expect(focusedDate()).toBe('2026-11-10')
      await press('Enter')
      expect(activeId()).toBe('panel-quick-input')
      w.unmount()
    })

    it('keeps week navigation with no day selected', async () => {
      const { w } = await mountView()
      expect(useCalendarStore().selected).toBeNull()
      await press('ArrowDown')
      expect(focusedDate()).toBe('2026-11-15')
      w.unmount()
    })

    it('x toggles via the row check button', async () => {
      const { w } = await openDay()
      await press('Enter')
      await press('x')
      const post = calls.filter((c) => c.method === 'POST' && c.url.startsWith('/api/checkins'))
      expect(post).toHaveLength(1)
      expect(post[0]!.body).toMatchObject({ item_id: 1 })
      w.unmount()
    })

    it('+ / = and - step a counter row in the panel', async () => {
      extraDetails = [
        {
          id: 4,
          name: 'Drink water',
          tracker_type: 'counter',
          kind: 'ongoing',
          polarity: 'positive',
          category_id: null,
          effective_sensitive: false,
          step: 1,
          options: [],
          current_rule: { schedule_type: 'fixed_rrule', target_value: 8, option_targets: {} },
        },
      ]
      checkinLogs = [
        {
          id: 99,
          item_id: 4,
          option_id: null,
          date: '2026-11-06',
          time: '10:00:00',
          value: 2,
          note: '',
          source: 'web',
          is_supplement: false,
        },
      ]
      const { w } = await openDay('2026-11-06', [
        item(4, 'Drink water', { tracker_type: 'counter' }),
      ])
      await press('Enter')
      expect(activeId()).toBe('panel-item-4')
      await press('+')
      await press('-')
      const posts = calls.filter((c) => c.method === 'POST' && c.url.startsWith('/api/checkins'))
      expect(posts).toHaveLength(2)
      expect(posts[0]!.body).toMatchObject({ item_id: 4, mode: 'delta', value: 1 })
      expect(posts[1]!.body).toMatchObject({ item_id: 4, mode: 'delta', value: -1 })
      w.unmount()
    })

    it('Enter on a row opens details once; Enter on child buttons does not', async () => {
      const { w } = await openDay()
      await press('Enter')
      await press('Enter')
      expect(document.querySelectorAll('[role="dialog"]').length).toBeGreaterThan(0)
      w.unmount()
      const again = await openDay()
      const toggle = again.w.get('[data-testid="makeup-toggle-1"]').element as HTMLElement
      toggle.focus()
      await press('Enter', toggle)
      expect(document.querySelector('[role="dialog"][open], dialog[open]')).toBeNull()
      again.w.unmount()
    })

    it('leaves typing in the quick-add input untouched and exits on Escape', async () => {
      const { w, store } = await openDay()
      const input = w.get('[data-testid="panel-quick-input"]').element as HTMLInputElement
      input.focus()
      const down = new KeyboardEvent('keydown', {
        key: 'ArrowDown',
        bubbles: true,
        cancelable: true,
      })
      input.dispatchEvent(down)
      expect(down.defaultPrevented).toBe(false)
      expect(document.activeElement).toBe(input)
      const esc = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
      input.dispatchEvent(esc)
      await flushPromises()
      expect(esc.defaultPrevented).toBe(true)
      expect(activeId()).toBe('panel-item-3')
      expect(store.selected).toBe('2026-11-06')
      w.unmount()
    })

    it('registry arrows do not fire with focus in the panel', async () => {
      const { w } = await openDay()
      const close = w.get('[data-testid="panel-close"]').element as HTMLElement
      close.focus()
      await press('ArrowRight')
      await press('ArrowDown')
      expect(document.activeElement).toBe(close)
      expect(focusedDate()).toBeUndefined()
      w.unmount()
    })
  })

  it('resolves the item list labels in both locales', async () => {
    const { en } = await import('./i18n/en')
    const { default: zh } = await import('./i18n/zh-CN')
    for (const id of ['itemUp', 'itemDown', 'itemBack', 'itemToggle']) {
      const k = `shortcuts.label.calendar.${id}`
      expect((en as Record<string, string>)[k]).toBeTruthy()
      expect((zh as Record<string, string>)[k]).toBeTruthy()
    }
  })

  it('unregisters on unmount', async () => {
    const { w } = await mountView()
    const ids = () => useShortcutList().value.map((d) => d.id)
    expect(ids()).toContain('calendar.today')
    w.unmount()
    expect(ids().filter((i) => i.startsWith('calendar.'))).toEqual([])
  })
})
