import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import { makeRouter } from '../router'
import ItemEditorView from './ItemEditorView.vue'
import { usePrivacyStore } from '../stores/privacy'
import { setDateFormat } from '../i18n/format'

function jsonRes(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

let calls: { url: string; method: string; body?: unknown }[]
let mockProjects: object[] = []

beforeEach(() => {
  localStorage.clear()
  calls = []
  mockProjects = []
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    calls.push({ url, method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    if (url === '/api/auth/status')
      return jsonRes({ oidc_enabled: false, login_required: false, authenticated: true })
    if (url === '/api/categories' && method === 'GET') return jsonRes({ categories: [] })
    if (url.startsWith('/api/projects') && method === 'GET') return jsonRes(mockProjects)
    if (url.startsWith('/api/view/today'))
      return jsonRes({
        date: '2026-11-07',
        weekday: 6,
        timezone: 'UTC',
        progress: { done: 0, total: 0 },
        items: [],
      })
    if (url === '/api/items' && method === 'POST') {
      return (calls.at(-1)!.body as { name: string }).name === 'bad'
        ? jsonRes({ error: 'invalid combination', reason: 'quota_option_required' }, 400)
        : jsonRes({ id: 5 }, 201)
    }
    if (url === '/api/items/6' && method === 'DELETE') return jsonRes({}, 200)
    if (url === '/api/items/6' && method === 'PATCH') {
      return jsonRes({ id: 6, ...(calls.at(-1)!.body as object) })
    }
    if (url === '/api/items/6' && method === 'GET') {
      return jsonRes({
        id: 6,
        name: 'Gym',
        tracker_type: 'boolean',
        kind: 'ongoing',
        polarity: 'positive',
        category_id: null,
        project_id: 42,
        effective_sensitive: false,
        pinned: false,
        archived: false,
        options: [],
        current_rule: {
          schedule_type: 'fixed_rrule',
          rrule_string: 'FREQ=DAILY',
          effective_from: '2026-01-01',
        },
      })
    }
    if (url === '/api/items/5' && method === 'GET') {
      return jsonRes({
        id: 5,
        name: 'Dentist',
        tracker_type: 'boolean',
        kind: 'event',
        polarity: 'positive',
        category_id: null,
        effective_sensitive: false,
        pinned: false,
        archived: false,
        options: [],
        current_rule: {
          schedule_type: 'fixed_rrule',
          rrule_string: 'FREQ=DAILY;COUNT=1',
          effective_from: '2026-01-01',
        },
      })
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
    expect(post.body).toMatchObject({
      name: 'Run',
      rule: { schedule_type: 'fixed_rrule', rrule_string: 'FREQ=WEEKLY;BYDAY=MO,WE' },
    })
  })

  it('hides the holiday policy and daily target for quota schedules', async () => {
    const { w } = await mountAt('/items/new')
    expect(w.find('[data-testid=holiday]').exists()).toBe(true)
    await w.get('[data-testid=tracker-metric]').trigger('click')
    expect(w.find('[data-testid=f-target]').exists()).toBe(true)
    await w.get('[data-testid=f-schedule]').setValue('quota')
    expect(w.find('[data-testid=holiday]').exists()).toBe(false)
    expect(w.find('[data-testid=f-target]').exists()).toBe(false)
    await w.get('[data-testid=f-schedule]').setValue('none')
    expect(w.find('[data-testid=f-target]').exists()).toBe(true)
  })

  it('shows counter limit hint when counter tracker is selected', async () => {
    const { w } = await mountAt('/items/new')
    expect(w.text()).not.toContain('Counters only track positive actions')
    await w.get('[data-testid=tracker-counter]').trigger('click')
    expect(w.text()).toContain('Counters only track positive actions')
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

  it('renders start and end dates according to the date format setting and parses input', async () => {
    setDateFormat('YYYY/MM/DD')
    try {
      const { w } = await mountAt('/items/new')
      const startsOn = w.get<HTMLInputElement>('[data-testid=f-starts-on]')
      // Starts on defaults to today, formatted as YYYY/MM/DD
      expect(startsOn.element.value).toMatch(/^\d{4}\/\d{2}\/\d{2}$/)

      await w.get('[data-testid=f-name]').setValue('Yoga')
      await startsOn.setValue('2026/11/15')
      await startsOn.trigger('blur')

      const endsOn = w.get<HTMLInputElement>('[data-testid=f-ends-on]')
      expect(endsOn.attributes('placeholder')).toBe('YYYY/MM/DD')
      await endsOn.setValue('2026/11/25')
      await endsOn.trigger('blur')

      // Preview sentence reflects until formatted date
      expect(w.get('[data-testid=preview-sentence]').text()).toContain('2026/11/25')

      // Preview dates list reflects YYYY/MM/DD formatting
      const previewDates = w.findAll('[data-testid=preview-dates] li')
      expect(previewDates.length).toBeGreaterThan(0)
      expect(previewDates[0]!.text()).toBe('2026/11/15')

      await w.get('form').trigger('submit')
      await flushPromises()

      const post = calls.find((c) => c.url === '/api/items' && c.method === 'POST')!
      expect(post.body).toMatchObject({
        name: 'Yoga',
        rule: {
          schedule_type: 'fixed_rrule',
          dtstart: '2026-11-15',
          until: '2026-11-25',
        },
      })
    } finally {
      setDateFormat('YYYY-MM-DD')
    }
  })

  it('renders strictly ongoing-only with no kind tabs and collapsed more settings', async () => {
    const { w } = await mountAt('/items/new')
    expect(w.find('[data-testid=kind-ongoing]').exists()).toBe(false)
    expect(w.find('[data-testid=kind-event]').exists()).toBe(false)
    const details = w.findAll('details')
    expect(details.length).toBeGreaterThan(0)
    for (const d of details) {
      expect(d.attributes('open')).toBeUndefined()
    }
  })

  it('redirects to / when attempting to edit an event', async () => {
    const { router } = await mountAt('/items/5/edit')
    // The target route is lazy-loaded, so wait for the navigation instead of a fixed number of ticks.
    await vi.waitFor(() => expect(router.currentRoute.value.path).toBe('/'))
  })

  it('renders project selector, updates form when project is selected, and includes project_id on submit', async () => {
    mockProjects = [
      { id: 42, name: 'Q4 Goals', status: 'active', color: '#3b82f6' },
      { id: 99, name: 'Personal', status: 'active', color: '#10b981' },
    ]
    const { w } = await mountAt('/items/new')
    expect(w.find('[data-testid=f-project]').exists()).toBe(true)
    const select = w.get<HTMLSelectElement>('[data-testid=f-project]')

    const options = select.findAll('option')
    expect(options.length).toBe(3) // null + 2 projects
    expect(options[0].text()).toBe('No Project')
    expect(options[1].text()).toBe('Q4 Goals')
    expect(options[2].text()).toBe('Personal')

    await w.get('[data-testid=f-name]').setValue('Read Strategy Doc')
    await select.setValue('42')
    await w.get('form').trigger('submit')
    await flushPromises()

    const post = calls.find((c) => c.url === '/api/items' && c.method === 'POST')!
    expect(post).toBeTruthy()
    expect(post.body).toMatchObject({
      name: 'Read Strategy Doc',
      project_id: 42,
    })
  })

  it('populates project_id when editing existing item and patches project_id on submit', async () => {
    mockProjects = [
      { id: 42, name: 'Q4 Goals', status: 'active', color: '#3b82f6' },
      { id: 99, name: 'Personal', status: 'active', color: '#10b981' },
    ]
    const { w } = await mountAt('/items/6/edit')
    const select = w.get<HTMLSelectElement>('[data-testid=f-project]')
    expect(select.element.value).toBe('42')

    // Change to project 99
    await select.setValue('99')
    await w.get('form').trigger('submit')
    await flushPromises()

    const patch = calls.find((c) => c.url === '/api/items/6' && c.method === 'PATCH')!
    expect(patch).toBeTruthy()
    expect(patch.body).toMatchObject({
      project_id: 99,
    })
  })

  it('shows sched-none-hint when scheduleType is none', async () => {
    const { w } = await mountAt('/items/new')
    expect(w.find('[data-testid=sched-none-hint]').exists()).toBe(false)
    await w.get('[data-testid=f-schedule]').setValue('none')
    expect(w.find('[data-testid=sched-none-hint]').exists()).toBe(true)
  })

  it('deletes an item from the edit page after confirmation', async () => {
    const { w, router } = await mountAt('/items/6/edit')
    expect(w.find('[data-testid=delete-confirm]').exists()).toBe(false)
    await w.get('[data-testid=delete]').trigger('click')
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
    await w.get('[data-testid=delete-confirm]').trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.url === '/api/items/6' && c.method === 'DELETE')).toBe(true)
    await vi.waitFor(() => expect(router.currentRoute.value.path).toBe('/items'))
  })

  it('has no delete button when creating', async () => {
    const { w } = await mountAt('/items/new')
    expect(w.find('[data-testid=delete]').exists()).toBe(false)
  })
})
