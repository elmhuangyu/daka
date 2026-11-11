import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h, ref } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import ShortcutHelp from '../components/ShortcutHelp.vue'
import { useItemsStore } from '../stores/items'
import { helpOpen, useGlobalShortcuts } from './useGlobalShortcuts'
import { installShortcutListener, resetSequence } from './useShortcuts'

const Page = defineComponent({
  render: () =>
    h('div', [
      h('input', { 'data-shortcut-search': '', 'data-testid': 'search' }),
      h('input', { 'data-testid': 'other' }),
      h('button', { 'data-testid': 'opener' }, 'x'),
    ]),
})
const Host = defineComponent({
  setup() {
    useGlobalShortcuts()
    return () => h('div', [h(RouterView), h(ShortcutHelp)])
  },
})

const names = ['today', 'calendar', 'projects', 'items', 'settings', 'item-new']
const paths: Record<string, string> = {
  today: '/',
  calendar: '/calendar',
  projects: '/projects',
  items: '/items',
  settings: '/settings',
  'item-new': '/items/new',
}

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      ...names.map((name) => ({ path: paths[name]!, name, component: Page })),
      { path: '/login', name: 'login', component: Page, meta: { public: true } },
    ],
  })
}

function press(key: string, target: EventTarget = document.body) {
  const e = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
  target.dispatchEvent(e)
  return e
}

let w: VueWrapper
let uninstall: () => void
async function setup(start = '/items') {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = makeRouter()
  await router.push(start)
  await router.isReady()
  w = mount(Host, { attachTo: document.body, global: { plugins: [pinia, router] } })
  return router
}
const tick = () => new Promise((r) => setTimeout(r, 0))

beforeEach(() => {
  uninstall = installShortcutListener()
})
afterEach(() => {
  uninstall()
  w.unmount()
  helpOpen.value = false
  resetSequence()
  document.body.innerHTML = ''
})

describe('global shortcuts', () => {
  it.each([
    ['t', 'today'],
    ['c', 'calendar'],
    ['p', 'projects'],
    ['i', 'items'],
    ['s', 'settings'],
  ])('g %s navigates to %s', async (k, name) => {
    const router = await setup(name === 'items' ? '/settings' : '/items')
    press('g')
    press(k)
    await tick()
    expect(router.currentRoute.value.name).toBe(name)
  })

  it('n opens the new item page', async () => {
    const router = await setup('/')
    press('n')
    await tick()
    expect(router.currentRoute.value.name).toBe('item-new')
  })

  it('/ focuses the search input without typing', async () => {
    await setup('/')
    const e = press('/')
    expect(e.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(w.get('[data-testid="search"]').element)
  })

  it('does nothing inside an input', async () => {
    const router = await setup('/')
    const input = w.get('[data-testid="other"]').element
    ;(input as HTMLElement).focus()
    press('n', input)
    press('?', input)
    await tick()
    expect(router.currentRoute.value.name).toBe('today')
    expect(helpOpen.value).toBe(false)
  })

  it('is disabled on public routes', async () => {
    const router = await setup('/login')
    press('n')
    press('g')
    press('t')
    press('?')
    await tick()
    expect(router.currentRoute.value.name).toBe('login')
    expect(helpOpen.value).toBe(false)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
})

describe('focusSearch', () => {
  it('skips disabled search inputs', async () => {
    await setup('/')
    const first = w.get('[data-testid="search"]').element as HTMLInputElement
    first.disabled = true
    const second = document.createElement('input')
    second.setAttribute('data-shortcut-search', '')
    document.body.appendChild(second)
    press('/')
    expect(document.activeElement).toBe(second)
  })
})

describe('shortcut help', () => {
  it('resets state when unmounted while open and mounts closed again', async () => {
    const show = ref(true)
    const Wrap = defineComponent({
      setup: () => () =>
        h('div', [h('button', { 'data-testid': 'opener' }), show.value ? h(ShortcutHelp) : null]),
    })
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrap = mount(Wrap, { attachTo: document.body, global: { plugins: [pinia] } })
    const opener = wrap.get('[data-testid="opener"]').element as HTMLElement
    opener.focus()
    helpOpen.value = true
    await tick()
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    show.value = false
    await tick()
    expect(helpOpen.value).toBe(false)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(opener)
    // The Esc listener must be gone: a keydown must not throw or reopen anything.
    press('Escape')
    show.value = true
    await tick()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    wrap.unmount()
  })

  it('renders chords as separate kbd elements', async () => {
    await setup('/')
    press('?')
    await tick()
    const kbds = [...document.querySelectorAll('[role="dialog"] kbd')].map((k) => k.textContent)
    expect(kbds).not.toContain('ctrl+shift+h')
    expect(kbds.some((k) => k?.includes('+') && k.length > 1)).toBe(false)
  })

  it('opens with ? and closes with Esc, restoring focus', async () => {
    await setup('/')
    const opener = w.get('[data-testid="opener"]').element as HTMLElement
    opener.focus()
    press('?')
    await tick()
    const dlg = document.querySelector('[role="dialog"]') as HTMLElement
    expect(dlg).not.toBeNull()
    expect(dlg.getAttribute('aria-modal')).toBe('true')
    expect(dlg.contains(document.activeElement)).toBe(true)
    press('Escape', document.activeElement as HTMLElement)
    await tick()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(opener)
  })

  it('closes on backdrop click', async () => {
    await setup('/')
    press('?')
    await tick()
    ;(document.querySelector('[data-testid="shortcut-help-backdrop"]') as HTMLElement).click()
    await tick()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })

  it('lists registry entries and static rows, with no user data', async () => {
    await setup('/')
    const items = useItemsStore()
    items.$patch({ items: [{ id: 1, name: 'SecretItemName' }] } as never)
    press('?')
    await tick()
    const dlg = document.querySelector('[role="dialog"]') as HTMLElement
    const text = dlg.textContent ?? ''
    expect(text).toContain('Go to Calendar')
    expect(text).toContain('Focus search')
    expect(text).toContain('Move between controls')
    expect(dlg.querySelectorAll('kbd').length).toBeGreaterThan(10)
    expect(document.body.innerHTML).not.toContain('SecretItemName')
  })
})
