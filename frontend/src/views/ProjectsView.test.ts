import { nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import { makeRouter } from '../router'
import { usePrivacyStore } from '../stores/privacy'
import ProjectsView from './ProjectsView.vue'
import type { Project } from '../api'

function jsonRes(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function sampleProject(overrides: Partial<Project> = {}): Project {
  return {
    id: 1,
    category_id: null,
    name: 'Sample Project',
    description: 'Project description',
    color: '#3b82f6',
    status: 'active',
    target_date: '2026-12-31',
    is_sensitive: false,
    sort_order: 0,
    created_at: '2026-11-01T00:00:00Z',
    updated_at: '2026-11-01T00:00:00Z',
    ...overrides,
  }
}

let calls: { url: string; method: string; body?: unknown }[]
let projectList: Project[]

beforeEach(() => {
  localStorage.clear()
  calls = []
  projectList = [
    sampleProject({ id: 1, name: 'Active Project', status: 'active', category_id: 10 }),
    sampleProject({ id: 2, name: 'Completed Project', status: 'completed' }),
    sampleProject({ id: 3, name: 'Archived Project', status: 'archived' }),
  ]

  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    calls.push({ url, method, body: init?.body ? JSON.parse(String(init.body)) : undefined })

    if (url === '/api/auth/status') {
      return jsonRes({ oidc_enabled: false, login_required: false, authenticated: true })
    }
    if (url === '/api/categories') {
      return jsonRes({
        categories: [
          {
            id: 10,
            name: 'Work',
            color: '#10b981',
            icon: 'briefcase',
            is_sensitive: false,
            sort_order: 1,
          },
        ],
      })
    }
    if (url.startsWith('/api/items?')) {
      return jsonRes({
        items: [
          {
            id: 101,
            name: 'Task 1',
            tracker_type: 'boolean',
            kind: 'event',
            polarity: 'positive',
            project_id: 1,
            category_id: 10,
            effective_sensitive: false,
            options: [],
            current_rule: { schedule_type: 'none', option_targets: {} },
            stats: { counts: { last_30_days: 1, this_year: 1, total: 1 } },
          },
          {
            id: 102,
            name: 'Task 2',
            tracker_type: 'boolean',
            kind: 'event',
            polarity: 'positive',
            project_id: 1,
            category_id: 10,
            effective_sensitive: false,
            options: [],
            current_rule: { schedule_type: 'none', option_targets: {} },
            stats: { counts: { last_30_days: 0, this_year: 0, total: 0 } },
          },
        ],
      })
    }
    if (url.startsWith('/api/projects') && method === 'GET') {
      return jsonRes(projectList)
    }
    if (url === '/api/projects' && method === 'POST') {
      const b = calls.at(-1)!.body as Partial<Project>
      const created = sampleProject({
        id: 4,
        name: b.name || 'New Project',
        description: b.description || '',
        color: b.color || '',
        category_id: b.category_id ?? null,
        target_date: b.target_date || null,
        is_sensitive: b.is_sensitive ?? false,
      })
      projectList.push(created)
      return jsonRes(created, 201)
    }
    if (url.startsWith('/api/projects/') && method === 'PATCH') {
      const id = Number(url.split('/')[3]?.split('?')[0])
      const b = calls.at(-1)!.body as Partial<Project>
      const p = projectList.find((it) => it.id === id)
      if (p) Object.assign(p, b)
      return jsonRes(p ?? sampleProject({ id, ...b }))
    }
    return jsonRes({})
  })
})

async function mountView() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = makeRouter(createMemoryHistory())
  await router.push('/projects')
  await router.isReady()
  const w = mount(ProjectsView, {
    global: { plugins: [pinia, router] },
  })
  await flushPromises()
  return { w, router }
}

describe('ProjectsView', () => {
  it('renders projects list tabs (Active, Completed, Archived)', async () => {
    const { w } = await mountView()

    // Default tab is active: contains Active Project, but not Completed or Archived
    expect(w.find('[data-testid="project-card-1"]').exists()).toBe(true)
    expect(w.text()).toContain('Active Project')
    expect(w.find('[data-testid="project-card-2"]').exists()).toBe(false)
    expect(w.find('[data-testid="project-card-3"]').exists()).toBe(false)

    // Shows category badge and progress stats (1/2 tasks, 50%)
    expect(w.text()).toContain('Work')
    expect(w.find('[data-testid="project-progress"]').text()).toContain('1/2')

    // Switch to Completed tab
    await w.find('[data-testid="tab-completed"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="project-card-2"]').exists()).toBe(true)
    expect(w.text()).toContain('Completed Project')
    expect(w.find('[data-testid="project-card-1"]').exists()).toBe(false)

    // Switch to Archived tab
    await w.find('[data-testid="tab-archived"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="project-card-3"]').exists()).toBe(true)
    expect(w.text()).toContain('Archived Project')
    expect(w.find('[data-testid="project-card-1"]').exists()).toBe(false)
  })

  it('creates a new project', async () => {
    const { w } = await mountView()

    expect(w.find('[data-testid="modal-new-project"]').exists()).toBe(false)
    await w.find('[data-testid="btn-new-project"]').trigger('click')
    expect(w.find('[data-testid="modal-new-project"]').exists()).toBe(true)

    const nameInput = w.find<HTMLInputElement>('[data-testid="input-project-name"]')
    await nameInput.setValue('Brand New Project')

    const descInput = w.find<HTMLTextAreaElement>('[data-testid="input-project-description"]')
    await descInput.setValue('Super exciting project')

    await w.find('form').trigger('submit')
    await flushPromises()

    const postCall = calls.find((c) => c.url === '/api/projects' && c.method === 'POST')
    expect(postCall).toBeTruthy()
    expect(postCall?.body).toMatchObject({
      name: 'Brand New Project',
      description: 'Super exciting project',
    })

    // Modal closed and new project visible in active list
    expect(w.find('[data-testid="modal-new-project"]').exists()).toBe(false)
    expect(w.text()).toContain('Brand New Project')
  })

  it('navigates to detail view when clicking project card', async () => {
    const { w, router } = await mountView()
    const pushSpy = vi.spyOn(router, 'push')

    await w.find('[data-testid="project-card-1"]').trigger('click')
    await flushPromises()
    await pushSpy.mock.results[0]?.value

    expect(pushSpy).toHaveBeenCalledWith('/projects/1')
    expect(router.currentRoute.value.path).toBe('/projects/1')
  })

  it('updates project status via card quick actions', async () => {
    const { w } = await mountView()

    // Mark project 1 completed
    const completeBtn = w.find('[data-testid="quick-action-complete"]')
    expect(completeBtn.exists()).toBe(true)
    await completeBtn.trigger('click')
    await flushPromises()

    const patchCall = calls.find((c) => c.url.startsWith('/api/projects/1') && c.method === 'PATCH')
    expect(patchCall).toBeTruthy()
    expect(patchCall?.body).toEqual({ status: 'completed' })
  })

  it('guard on drops rendered plaintext synchronously and requests redacted data', async () => {
    const { w } = await mountView()
    expect(w.text()).toContain('Active Project')
    expect(w.text()).toContain('Work')

    const privacy = usePrivacyStore()
    privacy.set(true)
    await nextTick()

    // Plaintext is cleared synchronously before flushPromises
    expect(w.text()).not.toContain('Active Project')
    expect(w.find('[data-testid="project-card-1"]').exists()).toBe(false)
    expect(w.text()).not.toContain('Work')

    await flushPromises()
    expect(calls.some((c) => c.url.includes('/api/projects') && c.url.includes('redact=1'))).toBe(
      true,
    )
    expect(calls.some((c) => c.url.includes('/api/items') && c.url.includes('redact=1'))).toBe(true)
  })

  it('discards in-flight stale category/data responses after privacy guard toggle', async () => {
    let resolveStaleCategories!: (value: Response) => void
    let interceptNext = true

    const origFetch = globalThis.fetch
    vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
      if (url === '/api/categories' && interceptNext) {
        interceptNext = false
        return new Promise<Response>((resolve) => {
          resolveStaleCategories = resolve
        })
      }
      return origFetch(url, init)
    })

    const pinia = createPinia()
    setActivePinia(pinia)
    const router = makeRouter(createMemoryHistory())
    await router.push('/projects')
    await router.isReady()
    const w = mount(ProjectsView, {
      global: { plugins: [pinia, router] },
    })

    // While categories fetch is in flight, toggle privacy mode on
    const privacy = usePrivacyStore()
    privacy.set(true)
    await nextTick()

    // Plaintext is cleared synchronously
    expect(w.text()).not.toContain('Work')

    // Resolve the initial in-flight fetch with plaintext / non-sensitive category
    resolveStaleCategories(
      jsonRes({
        categories: [
          {
            id: 10,
            name: 'Stale Leaked Category',
            color: '#10b981',
            icon: 'briefcase',
            is_sensitive: false,
            sort_order: 1,
          },
        ],
      }),
    )
    await flushPromises()

    expect(w.text()).not.toContain('Stale Leaked Category')
  })

  it('masks sensitive categories as Private category when privacy is active', async () => {
    const origFetch = globalThis.fetch
    vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
      if (url === '/api/categories') {
        return jsonRes({
          categories: [
            {
              id: 10,
              name: 'Secret Strategy',
              color: '#10b981',
              icon: 'briefcase',
              is_sensitive: true,
              sort_order: 1,
            },
          ],
        })
      }
      return origFetch(url, init)
    })

    const { w } = await mountView()
    const privacy = usePrivacyStore()
    privacy.set(true)
    await flushPromises()

    expect(w.text()).not.toContain('Secret Strategy')
    expect(w.text()).toContain('Private category')
  })
})
