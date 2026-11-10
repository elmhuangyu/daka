import { nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import { makeRouter } from '../router'
import { usePrivacyStore } from '../stores/privacy'
import ProjectDetailView from './ProjectDetailView.vue'
import type { ItemDetail, Project } from '../api'

function jsonRes(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function sampleProject(overrides: Partial<Project> = {}): Project {
  return {
    id: 1,
    category_id: 10,
    name: 'Apollo Mission',
    description: 'Landing on the moon',
    color: '#3b82f6',
    status: 'active',
    target_date: '2026-11-20',
    is_sensitive: false,
    sort_order: 0,
    created_at: '2026-11-01T00:00:00Z',
    updated_at: '2026-11-01T00:00:00Z',
    ...overrides,
  }
}

let calls: { url: string; method: string; body?: unknown }[]
let currentProject: Project
let currentTasks: ItemDetail[]

beforeEach(() => {
  localStorage.clear()
  calls = []
  currentProject = sampleProject()
  currentTasks = [
    {
      id: 101,
      name: 'Build Lunar Module',
      tracker_type: 'boolean',
      kind: 'event',
      polarity: 'positive',
      project_id: 1,
      category_id: 10,
      effective_sensitive: false,
      options: [],
      current_rule: {
        schedule_type: 'fixed_rrule',
        dtstart: '2026-11-15',
        option_targets: {},
      },
      stats: { counts: { last_30_days: 1, this_year: 1, total: 1 } },
    },
    {
      id: 102,
      name: 'Astronaut Training',
      tracker_type: 'boolean',
      kind: 'event',
      polarity: 'positive',
      project_id: 1,
      category_id: 10,
      effective_sensitive: false,
      options: [],
      current_rule: {
        schedule_type: 'none',
        option_targets: {},
      },
      stats: { counts: { last_30_days: 0, this_year: 0, total: 0 } },
    },
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
            name: 'Science',
            color: '#10b981',
            icon: 'flask',
            is_sensitive: false,
            sort_order: 1,
          },
        ],
      })
    }
    if (url.startsWith('/api/projects/1') && method === 'GET') {
      return jsonRes(currentProject)
    }
    if (url.startsWith('/api/projects/1') && method === 'PATCH') {
      const b = calls.at(-1)!.body as Partial<Project>
      Object.assign(currentProject, b)
      return jsonRes(currentProject)
    }
    if (url.startsWith('/api/projects/1') && method === 'DELETE') {
      return jsonRes({ deleted: true })
    }
    if (url.startsWith('/api/items?') && method === 'GET') {
      return jsonRes({ items: currentTasks })
    }
    if (url === '/api/items' && method === 'POST') {
      const b = calls.at(-1)!.body as {
        name: string
        project_id?: number
        rule?: { dtstart?: string; schedule_type: string }
      }
      const newTask: ItemDetail = {
        id: 103,
        name: b.name,
        tracker_type: 'boolean',
        kind: 'event',
        polarity: 'positive',
        project_id: b.project_id ?? null,
        category_id: 10,
        effective_sensitive: false,
        options: [],
        current_rule: {
          schedule_type: b.rule?.schedule_type || 'none',
          dtstart: b.rule?.dtstart,
          option_targets: {},
        },
        stats: { counts: { last_30_days: 0, this_year: 0, total: 0 } },
      }
      currentTasks.push(newTask)
      return jsonRes({ id: 103 }, 201)
    }
    if (url.startsWith('/api/items/') && method === 'PATCH') {
      const id = Number(url.split('/')[3]?.split('?')[0])
      const task = currentTasks.find((t) => t.id === id)
      if (task) {
        const b = calls.at(-1)!.body as Partial<ItemDetail>
        Object.assign(task, b)
      }
      return jsonRes(task ?? { id })
    }
    if (url.startsWith('/api/items/') && method === 'DELETE') {
      const id = Number(url.split('/')[3]?.split('?')[0])
      currentTasks = currentTasks.filter((t) => t.id !== id)
      return jsonRes({ deleted: true })
    }
    if (url === '/api/checkins' && method === 'POST') {
      return jsonRes({ ok: true })
    }
    return jsonRes({})
  })
})

async function mountDetail(projectId = 1) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = makeRouter(createMemoryHistory())
  await router.push(`/projects/${projectId}`)
  await router.isReady()
  const w = mount(ProjectDetailView, {
    global: { plugins: [pinia, router] },
  })
  await flushPromises()
  return { w, router }
}

describe('ProjectDetailView', () => {
  it('renders project detail, progress, and task list', async () => {
    const { w } = await mountDetail()

    expect(w.find('[data-testid="project-name"]').text()).toBe('Apollo Mission')
    expect(w.find('[data-testid="project-description"]').text()).toBe('Landing on the moon')
    expect(w.find('[data-testid="project-status"]').text()).toBe('Active')
    expect(w.find('[data-testid="project-category-badge"]').text()).toContain('Science')

    // Progress: 1 of 2 completed (50%)
    expect(w.find('[data-testid="project-progress-stats"]').text()).toContain('1 of 2')
    expect(w.find('[data-testid="project-progress-stats"]').text()).toContain('50%')

    // Task items
    expect(w.find('[data-testid="task-item-101"]').exists()).toBe(true)
    expect(w.find('[data-testid="task-item-102"]').exists()).toBe(true)
    expect(w.find('[data-testid="task-item-101"]').text()).toContain('Build Lunar Module')
    expect(w.find('[data-testid="task-item-102"]').text()).toContain('Astronaut Training')
  })

  it('adds a task to the project', async () => {
    const { w } = await mountDetail()

    const input = w.find<HTMLInputElement>('[data-testid="input-quick-task-name"]')
    await input.setValue('Test Rover Driving')

    await w.find('[data-testid="btn-quick-add-task"]').trigger('submit')
    await flushPromises()

    const postCall = calls.find((c) => c.url === '/api/items' && c.method === 'POST')
    expect(postCall).toBeTruthy()
    expect(postCall?.body).toMatchObject({
      name: 'Test Rover Driving',
      project_id: 1,
    })

    expect(w.text()).toContain('Test Rover Driving')
  })

  it('toggles task completion', async () => {
    const { w } = await mountDetail()

    const checkbox = w.find<HTMLInputElement>('[data-testid="task-checkbox-102"]')
    expect(checkbox.element.checked).toBe(false)

    await checkbox.trigger('change')
    await flushPromises()

    const checkinCall = calls.find((c) => c.url === '/api/checkins' && c.method === 'POST')
    expect(checkinCall).toBeTruthy()
    expect(checkinCall?.body).toMatchObject({
      item_id: 102,
      mode: 'set',
    })
  })

  it('changes project status (complete, archive, reopen)', async () => {
    const { w } = await mountDetail()

    // 1. Complete project
    await w.find('[data-testid="btn-complete-project"]').trigger('click')
    await flushPromises()

    let patch = calls.find((c) => c.url.startsWith('/api/projects/1') && c.method === 'PATCH')
    expect(patch?.body).toEqual({ status: 'completed' })
    expect(currentProject.status).toBe('completed')

    // 2. Reopen project
    const reopenBtn = w.find('[data-testid="btn-reopen-project"]')
    expect(reopenBtn.exists()).toBe(true)
    await reopenBtn.trigger('click')
    await flushPromises()

    patch = calls.filter((c) => c.url.startsWith('/api/projects/1') && c.method === 'PATCH').at(-1)
    expect(patch?.body).toEqual({ status: 'active' })

    // 3. Archive project
    await w.find('[data-testid="btn-archive-project"]').trigger('click')
    await flushPromises()

    patch = calls.filter((c) => c.url.startsWith('/api/projects/1') && c.method === 'PATCH').at(-1)
    expect(patch?.body).toEqual({ status: 'archived' })
  })

  it('unlinks and deletes tasks from project', async () => {
    const { w } = await mountDetail()

    // Unlink task 101
    await w.find('[data-testid="btn-unlink-task-101"]').trigger('click')
    await flushPromises()

    const updateCall = calls.find((c) => c.url.startsWith('/api/items/101') && c.method === 'PATCH')
    expect(updateCall?.body).toEqual({ project_id: null })
    expect(w.find('[data-testid="task-item-101"]').exists()).toBe(false)

    // Delete task 102
    await w.find('[data-testid="btn-delete-task-102"]').trigger('click')
    await flushPromises()

    const deleteCall = calls.find(
      (c) => c.url.startsWith('/api/items/102') && c.method === 'DELETE',
    )
    expect(deleteCall).toBeTruthy()
    expect(w.find('[data-testid="task-item-102"]').exists()).toBe(false)
  })

  it('deletes project with cascade', async () => {
    const { w, router } = await mountDetail()
    const pushSpy = vi.spyOn(router, 'push')

    await w.find('[data-testid="btn-delete-project"]').trigger('click')
    expect(w.find('[data-testid="modal-delete-project"]').exists()).toBe(true)

    // Select cascade mode
    const cascadeRadio = w.find('[data-testid="radio-delete-cascade"]')
    await cascadeRadio.trigger('click')
    await cascadeRadio.trigger('change')

    await w.find('[data-testid="btn-confirm-delete-project"]').trigger('click')
    await flushPromises()
    await pushSpy.mock.results[0]?.value

    const deleteCall = calls.find(
      (c) => c.url.startsWith('/api/projects/1') && c.method === 'DELETE',
    )
    expect(deleteCall).toBeTruthy()
    expect(deleteCall?.url).toContain('cascade=1')

    // Navigates back to /projects
    expect(router.currentRoute.value.path).toBe('/projects')
  })

  it('deletes project with unlink', async () => {
    const { w, router } = await mountDetail()
    const pushSpy = vi.spyOn(router, 'push')

    await w.find('[data-testid="btn-delete-project"]').trigger('click')
    // Default is unlink
    await w.find('[data-testid="btn-confirm-delete-project"]').trigger('click')
    await flushPromises()
    await pushSpy.mock.results[0]?.value

    const deleteCall = calls.find(
      (c) => c.url.startsWith('/api/projects/1') && c.method === 'DELETE',
    )
    expect(deleteCall).toBeTruthy()
    expect(deleteCall?.url).toContain('unlink=1')

    expect(router.currentRoute.value.path).toBe('/projects')
  })

  it('shows error for invalid project ID', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const router = makeRouter(createMemoryHistory())
    await router.push('/projects/invalid-id')
    await router.isReady()
    const w = mount(ProjectDetailView, {
      global: { plugins: [pinia, router] },
    })
    await flushPromises()

    expect(w.find('[data-testid="project-error"]').text()).toBe('Invalid project ID')
  })

  it('guard on drops rendered plaintext synchronously and requests redacted data', async () => {
    const { w } = await mountDetail()

    expect(w.find('[data-testid="project-name"]').text()).toBe('Apollo Mission')
    expect(w.find('[data-testid="project-description"]').text()).toBe('Landing on the moon')
    expect(w.find('[data-testid="task-item-101"]').exists()).toBe(true)

    const privacy = usePrivacyStore()
    privacy.set(true)
    await nextTick()

    // Plaintext is cleared synchronously
    expect(w.find('[data-testid="project-name"]').exists()).toBe(false)
    expect(w.find('[data-testid="project-description"]').exists()).toBe(false)
    expect(w.find('[data-testid="task-item-101"]').exists()).toBe(false)
    expect(w.text()).not.toContain('Apollo Mission')
    expect(w.text()).not.toContain('Landing on the moon')
    expect(w.text()).not.toContain('Build Lunar Module')

    await flushPromises()
    expect(
      calls.some((c) => c.url.startsWith('/api/projects/1') && c.url.includes('redact=1')),
    ).toBe(true)
    expect(calls.some((c) => c.url.startsWith('/api/items?') && c.url.includes('redact=1'))).toBe(
      true,
    )
  })

  it('discards in-flight stale responses when privacy mode changes', async () => {
    let resolveStaleProject!: (value: Response) => void
    let interceptNext = true

    const origFetch = globalThis.fetch
    vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
      if (url.startsWith('/api/projects/1') && interceptNext) {
        interceptNext = false
        return new Promise<Response>((resolve) => {
          resolveStaleProject = resolve
        })
      }
      return origFetch(url, init)
    })

    const pinia = createPinia()
    setActivePinia(pinia)
    const router = makeRouter(createMemoryHistory())
    await router.push('/projects/1')
    await router.isReady()
    const w = mount(ProjectDetailView, {
      global: { plugins: [pinia, router] },
    })

    // While initial loadProject request is in flight, toggle privacy mode
    const privacy = usePrivacyStore()
    privacy.set(true)
    await nextTick()

    // Plaintext is cleared
    expect(w.text()).not.toContain('Apollo Mission')

    // Stale fetch completes with plaintext name
    resolveStaleProject(
      jsonRes(
        sampleProject({
          id: 1,
          name: 'Stale Plaintext Leaked',
          description: 'Secret Stale Description',
        }),
      ),
    )
    await flushPromises()

    expect(w.text()).not.toContain('Stale Plaintext Leaked')
    expect(w.text()).not.toContain('Secret Stale Description')
  })

  it('masks sensitive categories as Private category when privacy mode is active', async () => {
    const origFetch = globalThis.fetch
    vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
      if (url === '/api/categories') {
        return jsonRes({
          categories: [
            {
              id: 10,
              name: 'Classified Science',
              color: '#10b981',
              icon: 'flask',
              is_sensitive: true,
              sort_order: 1,
            },
          ],
        })
      }
      return origFetch(url, init)
    })

    const { w } = await mountDetail()
    expect(w.text()).toContain('Classified Science')

    const privacy = usePrivacyStore()
    privacy.set(true)
    await flushPromises()

    expect(w.text()).not.toContain('Classified Science')
    expect(w.find('[data-testid="project-category-badge"]').text()).toContain('Private category')
  })

  it('does not repaint plaintext from unredacted PATCH responses when privacy mode is active', async () => {
    const { w } = await mountDetail()
    const privacy = usePrivacyStore()
    privacy.set(true)
    await flushPromises()

    const origFetch = globalThis.fetch
    vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (url.startsWith('/api/projects/1') && method === 'PATCH') {
        return jsonRes(
          sampleProject({ id: 1, name: 'Leaked Plaintext Project', status: 'completed' }),
        )
      }
      if (url.startsWith('/api/projects/1') && method === 'GET' && url.includes('redact=1')) {
        return jsonRes(sampleProject({ id: 1, name: 'Redacted Name', status: 'completed' }))
      }
      return origFetch(url, init)
    })

    await w.find('[data-testid="btn-complete-project"]').trigger('click')
    await flushPromises()

    expect(w.text()).not.toContain('Leaked Plaintext Project')
    expect(w.find('[data-testid="project-name"]').text()).toBe('Redacted Name')
  })

  it('does not repaint stale unredacted tasks if privacy mode turns on during quick add task', async () => {
    let interceptTasks = false
    let resolveStaleTasks: (res: Response) => void = () => {}

    const origFetch = globalThis.fetch
    vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (url.startsWith('/api/items?') && method === 'GET' && interceptTasks) {
        interceptTasks = false
        return new Promise<Response>((resolve) => {
          resolveStaleTasks = resolve
        })
      }
      if (url.startsWith('/api/items?') && method === 'GET' && url.includes('redact=1')) {
        return jsonRes({
          items: [
            {
              id: 999,
              name: 'Private task',
              tracker_type: 'boolean',
              kind: 'event',
              polarity: 'positive',
              project_id: 1,
              category_id: 10,
              effective_sensitive: true,
              options: [],
              current_rule: { schedule_type: 'none', option_targets: {} },
              stats: { counts: { last_30_days: 0, this_year: 0, total: 0 } },
            },
          ],
        })
      }
      return origFetch(url, init)
    })

    const { w } = await mountDetail()
    const privacy = usePrivacyStore()

    // Enable interception for the loadProject call after quick add
    interceptTasks = true

    const input = w.find<HTMLInputElement>('[data-testid="input-quick-task-name"]')
    await input.setValue('Top Secret Task')
    await w.find('[data-testid="btn-quick-add-task"]').trigger('submit')

    // While refresh is in flight, privacy mode is toggled on
    privacy.set(true)
    await nextTick()

    // Stale in-flight fetch completes with unredacted plaintext task
    resolveStaleTasks(
      jsonRes({
        items: [
          {
            id: 888,
            name: 'Stale Unredacted Secret Task',
            tracker_type: 'boolean',
            kind: 'event',
            polarity: 'positive',
            project_id: 1,
            category_id: 10,
            effective_sensitive: true,
            options: [],
            current_rule: { schedule_type: 'none', option_targets: {} },
            stats: { counts: { last_30_days: 0, this_year: 0, total: 0 } },
          },
        ],
      }),
    )
    await flushPromises()

    expect(w.text()).not.toContain('Stale Unredacted Secret Task')
    expect(w.text()).not.toContain('Top Secret Task')
  })
})
