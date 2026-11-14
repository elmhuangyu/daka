import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import { makeRouter } from '../router'
import { usePrivacyStore } from '../stores/privacy'
import ProjectDetailView from './ProjectDetailView.vue'
import { setAuthHooks, type ItemDetail, type Project } from '../api'
import { installShortcutListener, useShortcutList } from '../composables/useShortcuts'
import { resetAllBindings, setBinding } from '../composables/shortcutPrefs'
import { findConflicts } from '../composables/shortcutBindings'
import { SHORTCUT_CATALOG, catalogEntry } from '../composables/shortcutCatalog'
import { en } from '../i18n/en'
import zhCN from '../i18n/zh-CN'
import ShortcutHelp from '../components/ShortcutHelp.vue'
import { helpOpen } from '../composables/useGlobalShortcuts'

enableAutoUnmount(afterEach)

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
    if (url === '/api/projects/1/shares' && method === 'POST') {
      const b = calls.at(-1)!.body as { email: string }
      currentProject = {
        ...currentProject,
        shares: [
          ...(currentProject.shares ?? []),
          {
            user_id: 'u2',
            email: b.email,
            name: 'Bob',
            avatar_url: '',
            created_at: '2026-11-10T00:00:00Z',
          },
        ],
      }
      return jsonRes(currentProject)
    }
    if (url.startsWith('/api/projects/1/shares/') && method === 'DELETE') {
      const target = url.split('/').pop()
      if (target === 'me') {
        return jsonRes({ unshared: true })
      }
      currentProject = {
        ...currentProject,
        shares: (currentProject.shares ?? []).filter((s) => s.user_id !== target),
      }
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
        parent_id?: number
        rule?: { dtstart?: string; schedule_type: string }
      }
      const newTask: ItemDetail = {
        id: 103,
        name: b.name,
        tracker_type: 'boolean',
        kind: 'event',
        polarity: 'positive',
        project_id: b.project_id ?? null,
        parent_id: b.parent_id,
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

  it('lists recurring items apart from the todo checklist', async () => {
    currentTasks.push({
      id: 201,
      name: 'Daily Review',
      tracker_type: 'boolean',
      kind: 'ongoing',
      polarity: 'positive',
      project_id: 1,
      category_id: 10,
      effective_sensitive: false,
      options: [],
      current_rule: {
        schedule_type: 'fixed_rrule',
        rrule_string: 'FREQ=DAILY',
        option_targets: {},
      },
      stats: { counts: { last_30_days: 3, this_year: 3, total: 3 } },
    })
    const { w } = await mountDetail()
    expect(w.find('[data-testid="ongoing-item-201"]').text()).toContain('Daily Review')
    expect(w.find('[data-testid="task-item-201"]').exists()).toBe(false)
    expect(w.find('[data-testid="task-checkbox-201"]').exists()).toBe(false)
    // Progress counts only the todos.
    expect(w.find('[data-testid="project-progress-stats"]').text()).toContain('1 of 2')
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

describe('ProjectDetailView keyboard', () => {
  let off: (() => void) | undefined
  const press = (key: string, target: EventTarget = document.body) => {
    const e = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
    target.dispatchEvent(e)
    return e
  }
  const active = () => (document.activeElement as HTMLElement | null)?.dataset.taskId
  const rowIds = () =>
    [...document.querySelectorAll<HTMLElement>('[data-task-row]')].map((r) => r.dataset.taskId)
  const click = (testid: string) =>
    (document.querySelector(`[data-testid="${testid}"]`) as HTMLElement).click()
  const checkins = () => calls.filter((c) => c.url === '/api/checkins')

  function task(id: number, name: string, parent?: number): ItemDetail {
    return {
      ...currentTasks[1]!,
      id,
      name,
      parent_id: parent,
      stats: { counts: { last_30_days: 0, this_year: 0, total: 0 } },
    }
  }

  async function mountWith(list: ItemDetail[]) {
    currentTasks = list
    off = installShortcutListener()
    const pinia = createPinia()
    setActivePinia(pinia)
    const router = makeRouter(createMemoryHistory())
    await router.push('/projects/1')
    await router.isReady()
    const w = mount(ProjectDetailView, {
      global: { plugins: [pinia, router] },
      attachTo: document.body,
    })
    await flushPromises()
    return w
  }

  // Tree: 201 > (202 > 203), 204. Listed out of order to prove DOM order is the tree order.
  const mountTree = () =>
    mountWith([task(201, 'A'), task(204, 'D'), task(203, 'C', 202), task(202, 'B', 201)])

  beforeEach(() => {
    setAuthHooks({})
  })
  afterEach(() => {
    off?.()
    document.body.innerHTML = ''
  })

  it('Up/Down follow tree order, clamp at the ends and start from body', async () => {
    const w = await mountTree()
    expect(rowIds()).toEqual(['201', '202', '203', '204'])
    expect(document.querySelector('[data-task-row]')!.getAttribute('tabindex')).toBe('-1')
    expect(press('ArrowDown').defaultPrevented).toBe(true)
    expect(active()).toBe('201')
    press('ArrowDown', document.activeElement!)
    press('ArrowDown', document.activeElement!)
    expect(active()).toBe('203')
    press('ArrowDown', document.activeElement!)
    press('ArrowDown', document.activeElement!)
    expect(active()).toBe('204')
    press('ArrowUp', document.activeElement!)
    expect(active()).toBe('203')
    ;(document.activeElement as HTMLElement).blur()
    press('ArrowUp')
    expect(active()).toBe('204')
    w.unmount()
  })

  it('Up/Down are not handled without rows', async () => {
    const w = await mountWith([])
    expect(press('ArrowDown').defaultPrevented).toBe(false)
    w.unmount()
  })

  it('x toggles the focused row only, and not while typing', async () => {
    const w = await mountTree()
    press('x')
    await flushPromises()
    expect(checkins()).toHaveLength(0)
    press('ArrowDown')
    press('x', document.activeElement!)
    await flushPromises()
    expect(checkins().at(-1)?.body).toMatchObject({ item_id: 201, mode: 'set' })

    const n = checkins().length
    const input = document.createElement('input')
    document.querySelector('[data-task-row]')!.appendChild(input)
    input.focus()
    press('x', input)
    await flushPromises()
    expect(checkins()).toHaveLength(n)
    w.unmount()
  })

  it('d opens a confirm with the sub-task count; Esc cancels; confirm deletes', async () => {
    const w = await mountTree()
    press('ArrowDown')
    press('d', document.activeElement!)
    await flushPromises()
    const dlg = document.querySelector('[data-testid="modal-delete-task"]')!
    expect(dlg.getAttribute('role')).toBe('dialog')
    expect(dlg.getAttribute('aria-modal')).toBe('true')
    expect(dlg.textContent).toContain('2 sub-tasks will be deleted too.')
    expect((document.activeElement as HTMLElement).dataset.testid).toBe('btn-cancel-delete-task')
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)

    press('Escape', document.activeElement!)
    await flushPromises()
    expect(document.querySelector('[data-testid="modal-delete-task"]')).toBeNull()
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
    expect(active()).toBe('201')

    // Shortcuts are ignored while the dialog is open.
    press('d', document.activeElement!)
    await flushPromises()
    press('x', document.querySelector('[data-testid="btn-cancel-delete-task"]')!)
    press('ArrowDown', document.querySelector('[data-testid="btn-cancel-delete-task"]')!)
    expect(checkins()).toHaveLength(0)
    expect(document.activeElement).toBe(
      document.querySelector('[data-testid="btn-cancel-delete-task"]'),
    )

    click('btn-confirm-delete-task')
    await flushPromises()
    expect(calls.filter((c) => c.method === 'DELETE').map((c) => c.url)).toEqual(['/api/items/201'])
    expect(rowIds()).toEqual(['204'])
    expect(active()).toBe('204')
    expect(document.querySelector('[data-testid="modal-delete-task"]')).toBeNull()
    w.unmount()
  })

  it('deleting the last row focuses the previous row; singular wording', async () => {
    const w = await mountTree()
    press('ArrowUp')
    press('d', document.activeElement!)
    await flushPromises()
    expect(document.querySelector('[data-testid="delete-task-subtasks"]')).toBeNull()
    click('btn-confirm-delete-task')
    await flushPromises()
    expect(active()).toBe('203')
    press('ArrowUp', document.activeElement!)
    press('d', document.activeElement!)
    await flushPromises()
    expect(document.querySelector('[data-testid="delete-task-subtasks"]')!.textContent).toContain(
      '1 sub-task will',
    )
    w.unmount()
  })

  it('a failed delete shows the page error and closes the dialog', async () => {
    const w = await mountTree()
    const inner = globalThis.fetch
    vi.stubGlobal('fetch', async (url: string, init?: RequestInit) =>
      init?.method === 'DELETE' ? jsonRes({ error: 'boom' }, 500) : inner(url, init),
    )
    press('ArrowDown')
    press('d', document.activeElement!)
    await flushPromises()
    click('btn-confirm-delete-task')
    await flushPromises()
    expect(document.querySelector('[data-testid="modal-delete-task"]')).toBeNull()
    expect(document.querySelector('[data-testid="project-error"]')).not.toBeNull()
    expect(rowIds()).toHaveLength(4)
    w.unmount()
  })

  it('Enter on a nested row opens a sibling form after its subtree, then creates', async () => {
    const w = await mountTree()
    press('ArrowDown')
    press('ArrowDown', document.activeElement!)
    expect(active()).toBe('202')
    press('Enter', document.activeElement!)
    await flushPromises()
    const input = document.querySelector<HTMLInputElement>('[data-testid="input-task-form"]')!
    expect(document.activeElement).toBe(input)
    const rows = [...document.querySelector('[data-testid="project-tasks-list"]')!.children]
    const formEl = input.closest('form')!
    // After 202's subtree (203), before 204, at 202's indent.
    expect(rows.indexOf(formEl)).toBe(rows.indexOf(document.getElementById('task-203')!) + 1)
    expect(formEl.style.marginLeft).toBe(document.getElementById('task-202')!.style.marginLeft)
    // Name is required.
    press('Enter', input)
    await flushPromises()
    expect(calls.some((c) => c.url === '/api/items' && c.method === 'POST')).toBe(false)
    input.value = 'New sibling'
    input.dispatchEvent(new Event('input'))
    formEl.dispatchEvent(new Event('submit', { cancelable: true }))
    await flushPromises()
    const post = calls.find((c) => c.url === '/api/items' && c.method === 'POST')
    expect(post?.body).toMatchObject({ name: 'New sibling', project_id: 1, parent_id: 201 })
    expect(document.querySelector('[data-testid="input-task-form"]')).toBeNull()
    expect(active()).toBe('103')
    w.unmount()
  })

  it('Enter on a top-level row creates a top-level task', async () => {
    const w = await mountTree()
    press('ArrowUp')
    press('Enter', document.activeElement!)
    await flushPromises()
    const input = document.querySelector<HTMLInputElement>('[data-testid="input-task-form"]')!
    input.value = 'Top'
    input.dispatchEvent(new Event('input'))
    input.closest('form')!.dispatchEvent(new Event('submit', { cancelable: true }))
    await flushPromises()
    const post = calls.find((c) => c.url === '/api/items' && c.method === 'POST')!
    expect((post.body as { parent_id?: number }).parent_id ?? null).toBeNull()
    w.unmount()
  })

  it('Esc in the sibling form cancels and refocuses the origin row', async () => {
    const w = await mountTree()
    press('ArrowDown')
    press('Enter', document.activeElement!)
    await flushPromises()
    press('Escape', document.querySelector('[data-testid="input-task-form"]')!)
    await flushPromises()
    expect(document.querySelector('[data-testid="input-task-form"]')).toBeNull()
    expect(active()).toBe('201')
    expect(calls.some((c) => c.url === '/api/items' && c.method === 'POST')).toBe(false)
    w.unmount()
  })

  const formInput = () =>
    document.querySelector<HTMLInputElement>('[data-testid="input-task-form"]')
  const itemPosts = () => calls.filter((c) => c.url === '/api/items' && c.method === 'POST')
  const submitForm = async (name: string) => {
    const input = formInput()!
    input.value = name
    input.dispatchEvent(new Event('input'))
    input.closest('form')!.dispatchEvent(new Event('submit', { cancelable: true }))
    await flushPromises()
  }
  const listRows = () => [...document.querySelector('[data-testid="project-tasks-list"]')!.children]

  it('the add-sub-task button on a nested row opens a form after its subtree, one level deeper', async () => {
    const w = await mountTree()
    click('btn-add-subtask-202')
    await flushPromises()
    const input = formInput()!
    expect(document.activeElement).toBe(input)
    const formEl = input.closest('form')!
    const rows = listRows()
    expect(rows.indexOf(formEl)).toBe(rows.indexOf(document.getElementById('task-203')!) + 1)
    const base = parseFloat(document.getElementById('task-202')!.style.marginLeft || '0')
    expect(parseFloat(formEl.style.marginLeft || '0')).toBeGreaterThan(base)
    expect(formEl.style.marginLeft).toBe(document.getElementById('task-203')!.style.marginLeft)

    await submitForm('  Child of B ')
    expect(itemPosts()).toHaveLength(1)
    expect(itemPosts()[0]!.body).toMatchObject({
      name: 'Child of B',
      project_id: 1,
      parent_id: 202,
    })
    // The form stays open for the next sub-task, with the input cleared.
    expect(formInput()).not.toBeNull()
    expect(formInput()!.value).toBe('')
    w.unmount()
  })

  it('the add-sub-task button on a root row posts the root as parent', async () => {
    const w = await mountTree()
    click('btn-add-subtask-204')
    await flushPromises()
    const formEl = formInput()!.closest('form')!
    const rows = listRows()
    expect(rows.indexOf(formEl)).toBe(rows.indexOf(document.getElementById('task-204')!) + 1)
    expect(parseFloat(formEl.style.marginLeft || '0')).toBeGreaterThan(
      parseFloat(document.getElementById('task-204')!.style.marginLeft || '0'),
    )
    await submitForm('First')
    await submitForm('Second')
    expect(itemPosts().map((c) => c.body)).toMatchObject([
      { name: 'First', project_id: 1, parent_id: 204 },
      { name: 'Second', project_id: 1, parent_id: 204 },
    ])
    expect(formInput()).not.toBeNull()
    w.unmount()
  })

  it('Esc and Cancel close the sub-task form without creating; only Esc refocuses the row', async () => {
    const w = await mountTree()
    click('btn-add-subtask-202')
    await flushPromises()
    press('Escape', formInput()!)
    await flushPromises()
    expect(formInput()).toBeNull()
    expect(active()).toBe('202')

    click('btn-add-subtask-202')
    await flushPromises()
    const cancel = formInput()!
      .closest('form')!
      .querySelector<HTMLElement>('button[type="button"]')!
    cancel.click()
    await flushPromises()
    expect(formInput()).toBeNull()
    expect(itemPosts()).toHaveLength(0)
    w.unmount()
  })

  it('typing in the form keeps native keys', async () => {
    const w = await mountTree()
    press('ArrowDown')
    press('Enter', document.activeElement!)
    await flushPromises()
    const input = document.querySelector('[data-testid="input-task-form"]')!
    expect(press('d', input).defaultPrevented).toBe(false)
    expect(press('ArrowDown', input).defaultPrevented).toBe(false)
    expect(document.querySelector('[data-testid="modal-delete-task"]')).toBeNull()
    w.unmount()
  })

  describe('move up and down', () => {
    const moves = () => calls.filter((c) => c.url.endsWith('/move'))
    const alt = (dir: 'Up' | 'Down', target: EventTarget, extra: KeyboardEventInit = {}) => {
      const e = new KeyboardEvent('keydown', {
        key: `Arrow${dir}`,
        altKey: true,
        bubbles: true,
        cancelable: true,
        ...extra,
      })
      target.dispatchEvent(e)
      return e
    }
    const row = (id: number) =>
      document.querySelector<HTMLElement>(`[data-task-row][data-task-id="${id}"]`)!
    const banner = () => document.querySelector('[data-testid="project-error"]')
    const bodies = () => moves().map((m) => [m.url, m.body])

    function serverMoves(respond?: () => Response | Promise<Response>) {
      const inner = globalThis.fetch
      vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
        if (url.endsWith('/move') && init?.method === 'POST') {
          calls.push({ url, method: 'POST', body: JSON.parse(String(init.body)) })
          if (respond) return respond()
          return jsonRes({})
        }
        return inner(url, init)
      })
    }
    // 1, 2, 3 top level; 2 has no children.
    const flat = () => mountWith([task(1, 'A'), task(2, 'B'), task(3, 'C')])

    it('reorders top-level siblings with before/after bodies and refocuses', async () => {
      const w = await flat()
      serverMoves()
      row(2).focus()
      expect(alt('Up', row(2)).defaultPrevented).toBe(true)
      await flushPromises()
      expect(bodies()).toEqual([['/api/items/2/move', { parent_id: null, before_id: 1 }]])
      expect(active()).toBe('2')
      row(2).focus()
      expect(alt('Down', row(2)).defaultPrevented).toBe(true)
      await flushPromises()
      expect(bodies().at(-1)).toEqual(['/api/items/2/move', { parent_id: null, after_id: 3 }])
      w.unmount()
    })

    it('moves a first child out above its parent', async () => {
      const w = await mountWith([task(1, 'A'), task(2, 'B', 1), task(3, 'C', 1)])
      serverMoves()
      row(2).focus()
      alt('Up', row(2))
      await flushPromises()
      expect(bodies()).toEqual([['/api/items/2/move', { parent_id: null, before_id: 1 }]])
      w.unmount()
    })

    it('moves a last child down after the next top-level row', async () => {
      const w = await mountWith([task(1, 'A'), task(2, 'B', 1), task(3, 'C')])
      serverMoves()
      row(2).focus()
      alt('Down', row(2))
      await flushPromises()
      expect(bodies()).toEqual([['/api/items/2/move', { parent_id: null, after_id: 3 }]])
      w.unmount()
    })

    it('Alt+Up enters the previous sibling subtree before its last descendant', async () => {
      const w = await mountWith([task(1, 'A'), task(2, 'B', 1), task(3, 'C')])
      serverMoves()
      row(3).focus()
      alt('Up', row(3))
      await flushPromises()
      expect(bodies()).toEqual([['/api/items/3/move', { parent_id: 1, before_id: 2 }]])
      w.unmount()
    })

    describe('sibling order is (sort_order, id), not pinned-first', () => {
      const so = (t: ItemDetail, sort_order: number, pinned = false): ItemDetail => ({
        ...t,
        sort_order,
        pinned,
      })

      it('orders roots and children by sort_order then id', async () => {
        const w = await mountWith([
          so(task(1, 'P'), 5, true),
          so(task(2, 'A'), 1),
          so(task(3, 'B'), 1),
          so(task(4, 'c1', 2), 9, true),
          so(task(5, 'c2', 2), 3),
        ])
        expect(rowIds()).toEqual(['2', '5', '4', '3', '1'])
        w.unmount()
      })

      it('Alt+Down onto a parent uses the minimum (sort_order, id) child', async () => {
        const w = await mountWith(
          [
            so(task(1, 'A'), 0),
            so(task(2, 'B'), 1),
            so(task(3, 'pinned child'), 0, true),
            so(task(4, 'first child'), 0),
          ].map((t, i) => (i >= 2 ? { ...t, parent_id: 2, sort_order: i === 2 ? 7 : 2 } : t)),
        )
        serverMoves()
        row(1).focus()
        alt('Down', row(1))
        await flushPromises()
        expect(bodies()).toEqual([['/api/items/1/move', { parent_id: 2, before_id: 4 }]])
        w.unmount()
      })

      it('Alt+Up and Tab use the previous sibling in that order', async () => {
        const w = await mountWith([
          so(task(1, 'pinned'), 5, true),
          so(task(2, 'low'), 1),
          so(task(3, 'mid'), 3),
        ])
        serverMoves()
        row(1).focus()
        alt('Up', row(1))
        await flushPromises()
        expect(bodies()).toEqual([['/api/items/1/move', { parent_id: null, before_id: 3 }]])
        row(1).focus()
        row(1).dispatchEvent(
          new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }),
        )
        await flushPromises()
        expect(bodies().at(-1)).toEqual(['/api/items/1/move', { parent_id: 3 }])
        w.unmount()
      })
    })

    it('Alt+Down onto a row with children becomes its first child', async () => {
      const w = await mountWith([task(1, 'A'), task(2, 'B'), task(3, 'C', 2), task(4, 'D', 2)])
      serverMoves()
      row(1).focus()
      alt('Down', row(1))
      await flushPromises()
      expect(bodies()).toEqual([['/api/items/1/move', { parent_id: 2, before_id: 3 }]])
      w.unmount()
    })

    it('skips the moved subtree when moving down', async () => {
      const w = await mountWith([task(1, 'A'), task(2, 'B', 1), task(3, 'C')])
      serverMoves()
      row(1).focus()
      alt('Down', row(1))
      await flushPromises()
      expect(bodies()).toEqual([['/api/items/1/move', { parent_id: null, after_id: 3 }]])
      w.unmount()
    })

    it('is a no-op at the very top and very end and leaves the native key alone', async () => {
      const w = await flat()
      row(1).focus()
      expect(alt('Up', row(1)).defaultPrevented).toBe(false)
      row(3).focus()
      expect(alt('Down', row(3)).defaultPrevented).toBe(false)
      expect(moves()).toHaveLength(0)
      w.unmount()
    })

    it('does not act without a focused row', async () => {
      const w = await flat()
      expect(alt('Up', document.body).defaultPrevented).toBe(false)
      expect(alt('Down', document.body).defaultPrevented).toBe(false)
      expect(moves()).toHaveLength(0)
      w.unmount()
    })

    it('blocks a move beyond 5 levels without a request', async () => {
      // 1 > 2 > 3 > 4 > 5 and a top-level 6 with a child 7 (height 2) after it.
      const w = await mountWith([
        task(1, 'L1'),
        task(2, 'L2', 1),
        task(3, 'L3', 2),
        task(4, 'L4', 3),
        task(5, 'L5', 4),
        task(6, 'T'),
        task(7, 'U', 6),
      ])
      // Up: 6 (height 2) before 5 would sit under 4 at level 5, reaching level 6.
      row(6).focus()
      expect(alt('Up', row(6)).defaultPrevented).toBe(true)
      await flushPromises()
      expect(moves()).toHaveLength(0)
      expect(banner()!.textContent).toContain('at most 5 levels')
      w.unmount()
    })

    it('shows the banner and refreshes on server 409 and 400', async () => {
      for (const status of [409, 400]) {
        const w = await flat()
        serverMoves(() => jsonRes({ error: 'the item tree changed concurrently, retry' }, status))
        const lists = () => calls.filter((c) => c.url.startsWith('/api/items?')).length
        const before = lists()
        row(2).focus()
        alt('Up', row(2))
        await flushPromises()
        expect(banner()).not.toBeNull()
        expect(banner()!.textContent).not.toContain('B')
        expect(lists()).toBe(before + 1)
        expect(active()).toBe('2')
        w.unmount()
        document.body.innerHTML = ''
        off?.()
      }
    })

    it('swallows a second chord while a move is in flight', async () => {
      const w = await flat()
      let release!: () => void
      const gate = new Promise<void>((r) => (release = r))
      serverMoves(async () => {
        await gate
        return jsonRes({})
      })
      row(2).focus()
      alt('Up', row(2))
      await flushPromises()
      expect(alt('Down', row(2)).defaultPrevented).toBe(true)
      await flushPromises()
      expect(moves()).toHaveLength(1)
      release()
      await flushPromises()
      w.unmount()
    })

    it('is ignored while typing and while a dialog or form is open', async () => {
      const w = await flat()
      const input = document.createElement('input')
      row(2).appendChild(input)
      input.focus()
      expect(alt('Up', input).defaultPrevented).toBe(false)
      row(2).focus()
      press('d', row(2))
      await flushPromises()
      expect(alt('Up', row(2)).defaultPrevented).toBe(false)
      expect(moves()).toHaveLength(0)
      w.unmount()
    })

    it('is ignored while the add form is open', async () => {
      const w = await flat()
      row(2).focus()
      press('Enter', row(2))
      await flushPromises()
      const formInput = document.querySelector('[data-testid="input-task-form"]')!
      expect(alt('Up', formInput).defaultPrevented).toBe(false)
      expect(alt('Up', row(2)).defaultPrevented).toBe(false)
      expect(moves()).toHaveLength(0)
      w.unmount()
    })

    it('can be rebound', async () => {
      const w = await flat()
      serverMoves()
      setBinding('project.moveUp', ['k'])
      row(2).focus()
      expect(alt('Up', row(2)).defaultPrevented).toBe(false)
      expect(press('k', row(2)).defaultPrevented).toBe(true)
      await flushPromises()
      expect(bodies()).toEqual([['/api/items/2/move', { parent_id: null, before_id: 1 }]])
      resetAllBindings()
      w.unmount()
    })

    it('alt+arrow does not conflict inside the projects scope or with other scopes', () => {
      const bindings = Object.fromEntries(SHORTCUT_CATALOG.map((e) => [e.id, e.keys]))
      expect(findConflicts(bindings, SHORTCUT_CATALOG)).toEqual([])
      const clash = findConflicts(
        { ...bindings, 'project.rowUp': ['alt+ArrowUp'] },
        SHORTCUT_CATALOG,
      )
      expect(clash.map((c) => [c.a, c.b])).toEqual([['project.rowUp', 'project.moveUp']])
    })
  })

  describe('indent and outdent', () => {
    const moves = () => calls.filter((c) => c.url.endsWith('/move'))
    const tab = (target: EventTarget, shiftKey = false, extra: KeyboardEventInit = {}) => {
      const e = new KeyboardEvent('keydown', {
        key: 'Tab',
        shiftKey,
        bubbles: true,
        cancelable: true,
        ...extra,
      })
      target.dispatchEvent(e)
      return e
    }
    const row = (id: number) =>
      document.querySelector<HTMLElement>(`[data-task-row][data-task-id="${id}"]`)!
    const banner = () => document.querySelector('[data-testid="project-error"]')

    // Server stand-in that applies a move to the task list.
    function serverMoves(respond?: () => Response | Promise<Response>) {
      const inner = globalThis.fetch
      vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
        if (url.endsWith('/move') && init?.method === 'POST') {
          calls.push({ url, method: 'POST', body: JSON.parse(String(init.body)) })
          if (respond) return respond()
          const id = Number(url.split('/')[3])
          const b = JSON.parse(String(init.body)) as { parent_id?: number | null }
          const t = currentTasks.find((x) => x.id === id)!
          t.parent_id = b.parent_id ?? undefined
          return jsonRes(t)
        }
        return inner(url, init)
      })
    }

    it('indents under the previous sibling and keeps focus on the moved row', async () => {
      const w = await mountTree()
      serverMoves()
      row(204).focus()
      const e = tab(row(204))
      expect(e.defaultPrevented).toBe(true)
      await flushPromises()
      expect(moves()).toEqual([
        { url: '/api/items/204/move', method: 'POST', body: { parent_id: 201 } },
      ])
      expect(row(204).style.marginLeft).toBe('1.5rem')
      expect(active()).toBe('204')
      w.unmount()
    })

    it('does nothing and leaves native Tab alone without a previous sibling', async () => {
      const w = await mountTree()
      for (const id of [201, 202]) {
        row(id).focus()
        expect(tab(row(id)).defaultPrevented).toBe(false)
      }
      expect(moves()).toHaveLength(0)
      w.unmount()
    })

    it('outdents after the former parent', async () => {
      const w = await mountTree()
      serverMoves()
      row(203).focus()
      expect(tab(row(203), true).defaultPrevented).toBe(true)
      await flushPromises()
      expect(moves().map((m) => [m.url, m.body])).toEqual([
        ['/api/items/203/move', { parent_id: 201, after_id: 202 }],
      ])
      expect(active()).toBe('203')
      w.unmount()
    })

    it('outdents a depth-2 row to the top level with parent_id null', async () => {
      const w = await mountTree()
      serverMoves()
      row(202).focus()
      tab(row(202), true)
      await flushPromises()
      expect(moves().at(-1)?.body).toEqual({ parent_id: null, after_id: 201 })
      w.unmount()
    })

    it('leaves Shift+Tab alone on a top-level row', async () => {
      const w = await mountTree()
      row(204).focus()
      expect(tab(row(204), true).defaultPrevented).toBe(false)
      expect(moves()).toHaveLength(0)
      w.unmount()
    })

    it('ignores Tab with Ctrl, Alt or Meta', async () => {
      const w = await mountTree()
      row(204).focus()
      for (const mod of ['ctrlKey', 'altKey', 'metaKey']) {
        expect(tab(row(204), false, { [mod]: true }).defaultPrevented).toBe(false)
      }
      expect(moves()).toHaveLength(0)
      w.unmount()
    })

    it('blocks a move beyond 5 levels without a request, and allows exactly 5', async () => {
      // 1 > 2 > 3 > 4 > (5, 6): indenting 6 under 5 would make level 6.
      const w = await mountWith([
        task(1, 'L1'),
        task(2, 'L2', 1),
        task(3, 'L3', 2),
        task(4, 'L4', 3),
        task(5, 'L5', 4),
        task(6, 'L5b', 4),
      ])
      row(6).focus()
      expect(tab(row(6)).defaultPrevented).toBe(true)
      await flushPromises()
      expect(moves()).toHaveLength(0)
      expect(banner()!.textContent).toContain('at most 5 levels')

      // Outdenting is always within the limit.
      serverMoves()
      row(4).focus()
      tab(row(4), true) // outdent 4 to level 3: allowed
      await flushPromises()
      expect(moves()).toHaveLength(1)
      w.unmount()
    })

    it('counts the height of the moved subtree against the limit', async () => {
      // 7 > 8 > 9 > 10 has height 4; indenting 7 under 2 (level 2) would reach level 6.
      const w = await mountWith([
        task(1, 'A'),
        task(2, 'B', 1),
        task(7, 'C', 1),
        task(8, 'D', 7),
        task(9, 'E', 8),
        task(10, 'F', 9),
      ])
      row(7).focus()
      tab(row(7))
      await flushPromises()
      expect(moves()).toHaveLength(0)
      expect(banner()).not.toBeNull()
      w.unmount()
    })

    it('shows the server error on 409 and refreshes the tree', async () => {
      const w = await mountTree()
      serverMoves(() => jsonRes({ error: 'the item tree changed concurrently, retry' }, 409))
      const lists = () => calls.filter((c) => c.url.startsWith('/api/items?')).length
      const before = lists()
      row(204).focus()
      tab(row(204))
      await flushPromises()
      expect(banner()!.textContent).toContain('changed in the meantime')
      expect(banner()!.textContent).not.toContain('Astronaut')
      expect(lists()).toBe(before + 1)
      expect(rowIds()).toEqual(['201', '202', '203', '204'])
      expect(active()).toBe('204')
      w.unmount()
    })

    it('shows the server 400 depth error via the banner', async () => {
      const w = await mountTree()
      serverMoves(() => jsonRes({ error: 'sub-tasks can nest at most 5 levels' }, 400))
      row(204).focus()
      tab(row(204))
      await flushPromises()
      expect(banner()!.textContent).toContain('at most 5 levels')
      w.unmount()
    })

    it('shows the banner on a network failure without refreshing', async () => {
      const w = await mountTree()
      serverMoves(() => {
        throw new TypeError('offline')
      })
      const lists = () => calls.filter((c) => c.url.startsWith('/api/items?')).length
      const before = lists()
      row(204).focus()
      tab(row(204))
      await flushPromises()
      expect(banner()).not.toBeNull()
      expect(lists()).toBe(before)
      expect(moves()).toHaveLength(1)
      w.unmount()
    })

    it('does not intercept Tab on controls inside the row', async () => {
      const w = await mountTree()
      const btn = document.querySelector<HTMLElement>('[data-testid="btn-delete-task-204"]')!
      btn.focus()
      expect(tab(btn).defaultPrevented).toBe(false)
      expect(tab(btn, true).defaultPrevented).toBe(false)
      const input = document.createElement('input')
      row(204).appendChild(input)
      input.focus()
      expect(tab(input).defaultPrevented).toBe(false)
      expect(moves()).toHaveLength(0)
      w.unmount()
    })

    it('ignores a second Tab while a move is in flight', async () => {
      const w = await mountTree()
      let release!: () => void
      const gate = new Promise<void>((r) => (release = r))
      serverMoves(async () => {
        await gate
        return jsonRes({})
      })
      row(204).focus()
      tab(row(204))
      await flushPromises()
      const second = tab(row(204))
      expect(second.defaultPrevented).toBe(true)
      await flushPromises()
      expect(moves()).toHaveLength(1)
      release()
      await flushPromises()
      expect(moves()).toHaveLength(1)
      w.unmount()
    })

    it('is ignored while the delete dialog is open', async () => {
      const w = await mountTree()
      row(204).focus()
      press('d', row(204))
      await flushPromises()
      expect(document.querySelector('[data-testid="modal-delete-task"]')).not.toBeNull()
      expect(tab(row(204)).defaultPrevented).toBe(false)
      expect(moves()).toHaveLength(0)
      w.unmount()
    })
  })

  it('registers its shortcuts and cleans up on unmount', async () => {
    const list = useShortcutList()
    const ids = () => list.value.filter((d) => d.scope === 'projects').map((d) => d.id)
    const w = await mountTree()
    expect(ids().sort()).toEqual(
      [
        'project.deleteRow',
        'project.indent',
        'project.moveDown',
        'project.moveUp',
        'project.newSibling',
        'project.outdent',
        'project.rowDown',
        'project.rowUp',
        'project.toggleRow',
      ].sort(),
    )
    w.unmount()
    expect(ids()).toEqual([])
  })

  it('scope title and labels resolve in both locales; the help panel lists them', async () => {
    const msgs: Record<string, string>[] = [en, zhCN]
    for (const m of msgs) {
      expect(m['shortcuts.scope.projects']).toBeTruthy()
      for (const id of [
        'rowUp',
        'rowDown',
        'toggleRow',
        'deleteRow',
        'newSibling',
        'indent',
        'outdent',
        'moveUp',
        'moveDown',
      ]) {
        expect(m[catalogEntry(`project.${id}`)!.labelKey]).toBeTruthy()
      }
    }
    const w = await mountTree()
    helpOpen.value = true
    const help = mount(ShortcutHelp, { attachTo: document.body })
    await flushPromises()
    expect(help.text()).toContain('Projects')
    expect(help.text()).toContain('Delete the focused task')
    expect(help.text()).toContain('Add a task below the focused task')
    expect(help.text()).toContain('Make the focused task a sub-task of the task above')
    expect(help.text()).toContain('Move the focused sub-task up one level')
    expect(help.text()).toContain('Tab')
    expect(help.text()).toContain('Move the focused task up')
    expect(help.text()).toContain('Move the focused task down')
    helpOpen.value = false
    help.unmount()
    w.unmount()
  })
})

describe('Project sharing', () => {
  it('owner can share by email, see who it is shared with, and unshare', async () => {
    currentProject = sampleProject({ is_owner: true, shares: [] })
    const { w: wrapper } = await mountDetail()

    await wrapper.get('[data-testid="btn-share-project"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="modal-share-project"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="no-shares-text"]').exists()).toBe(true)

    await wrapper.get('[data-testid="input-share-email"]').setValue('bob@example.com')
    await wrapper.get('[data-testid="modal-share-project"] form').trigger('submit')
    await flushPromises()

    expect(wrapper.find('[data-testid="share-row-u2"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="project-shared-badge"]').text()).toContain('Bob')

    await wrapper.get('[data-testid="btn-unshare-u2"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="share-row-u2"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="no-shares-text"]').exists()).toBe(true)
  })

  it('disables share button for sensitive projects', async () => {
    currentProject = sampleProject({ is_owner: true, is_sensitive: true, shares: [] })
    const { w: wrapper } = await mountDetail()

    const shareBtn = wrapper.get('[data-testid="btn-share-project"]')
    expect((shareBtn.element as HTMLButtonElement).disabled).toBe(true)
  })

  it('collaborator sees shared badge and leave button, not share or delete, and can leave', async () => {
    currentProject = sampleProject({
      is_owner: false,
      owner: { id: 'u1', email: 'alice@example.com', name: 'Alice', avatar_url: '' },
      shares: [
        { user_id: 'u2', email: 'bob@example.com', name: 'Bob', avatar_url: '', created_at: '' },
      ],
    })
    const { w: wrapper, router } = await mountDetail()

    expect(wrapper.find('[data-testid="btn-share-project"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="btn-delete-project"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="project-shared-badge"]').text()).toContain('Alice')

    await wrapper.get('[data-testid="btn-leave-project"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="modal-leave-project"]').exists()).toBe(true)

    await wrapper.get('[data-testid="btn-confirm-leave-project"]').trigger('click')
    await flushPromises()

    expect(calls.some((c) => c.method === 'DELETE' && c.url === '/api/projects/1/shares/me')).toBe(
      true,
    )
    expect(router.currentRoute.value.path).toBe('/projects')
  })
})
