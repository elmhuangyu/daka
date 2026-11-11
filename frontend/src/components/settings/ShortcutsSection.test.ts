import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { effectScope, nextTick } from 'vue'
import ShortcutsSection from './ShortcutsSection.vue'
import ShortcutHelp from '../ShortcutHelp.vue'
import { helpOpen } from '../../composables/useGlobalShortcuts'
import {
  reloadShortcutPrefs,
  setBinding,
  setShortcutsEnabled,
  shortcutPrefs,
} from '../../composables/shortcutPrefs'
import { catalogDef } from '../../composables/shortcutCatalog'
import {
  installShortcutListener,
  setShortcutRecording,
  useShortcut,
} from '../../composables/useShortcuts'

let w: VueWrapper
let uninstall: () => void

function key(k: string, init: KeyboardEventInit = {}) {
  const e = new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true, ...init })
  document.body.dispatchEvent(e)
  return e
}
const row = (id: string) => w.get(`[data-testid="sc-row-${id}"]`)

beforeEach(() => {
  localStorage.clear()
  reloadShortcutPrefs()
  uninstall = installShortcutListener()
  w = mount(ShortcutsSection, { attachTo: document.body })
})
afterEach(async () => {
  vi.useRealTimers()
  w.unmount()
  uninstall()
  await new Promise((r) => setTimeout(r, 0))
  setShortcutRecording(false)
  localStorage.clear()
  reloadShortcutPrefs()
  document.body.innerHTML = ''
})

describe('ShortcutsSection', () => {
  it('lists every catalog action grouped by scope', () => {
    expect(w.find('[data-testid="sc-group-global"]').exists()).toBe(true)
    expect(w.find('[data-testid="sc-group-today"]').exists()).toBe(true)
    expect(w.find('[data-testid="sc-group-calendar"]').exists()).toBe(true)
    expect(row('item.new').text()).toContain('n')
  })

  it('records a new single key after the sequence window', async () => {
    vi.useFakeTimers()
    await w.get('[data-testid="sc-change-item.new"]').trigger('click')
    expect(row('item.new').find('[data-testid="sc-recording"]').exists()).toBe(true)
    const e = key('q')
    expect(e.defaultPrevented).toBe(true)
    vi.advanceTimersByTime(1100)
    await nextTick()
    expect(shortcutPrefs.bindings['item.new']).toEqual(['q'])
  })

  it('records a two-key sequence', async () => {
    await w.get('[data-testid="sc-change-item.new"]').trigger('click')
    key('a')
    key('b')
    await nextTick()
    expect(shortcutPrefs.bindings['item.new']).toEqual(['a b'])
  })

  it('records a chord immediately and suppresses registry dispatch while recording', async () => {
    const calls: string[] = []
    const h = (e: KeyboardEvent) => calls.push(e.key)
    window.addEventListener('keydown', h)
    await w.get('[data-testid="sc-change-item.new"]').trigger('click')
    key('k', { ctrlKey: true })
    window.removeEventListener('keydown', h)
    expect(shortcutPrefs.bindings['item.new']).toEqual(['ctrl+k'])
  })

  it('Esc cancels recording without changes', async () => {
    await w.get('[data-testid="sc-change-item.new"]').trigger('click')
    key('Escape')
    await nextTick()
    expect(shortcutPrefs.bindings['item.new']).toBeUndefined()
    expect(row('item.new').find('[data-testid="sc-recording"]').exists()).toBe(false)
  })

  it('rejects reserved keys with an inline error', async () => {
    await w.get('[data-testid="sc-change-item.new"]').trigger('click')
    key('t', { ctrlKey: true })
    await nextTick()
    expect(w.get('[data-testid="sc-error"]').text()).toContain('Reserved')
    expect(shortcutPrefs.bindings['item.new']).toBeUndefined()
  })

  it('offers swap on a conflict and never overwrites silently', async () => {
    vi.useFakeTimers()
    await w.get('[data-testid="sc-change-item.new"]').trigger('click')
    key('/')
    // '/' is plain, so it waits for the sequence window.
    vi.advanceTimersByTime(1100)
    await nextTick()
    const box = w.get('[data-testid="sc-conflict"]')
    expect(box.text()).toContain('Focus search')
    expect(shortcutPrefs.bindings['item.new']).toBeUndefined()
    await w.get('[data-testid="sc-swap"]').trigger('click')
    expect(shortcutPrefs.bindings['item.new']).toEqual(['/'])
    expect(shortcutPrefs.bindings['search.focus']).toEqual(['n'])
  })

  it('cancel keeps everything', async () => {
    vi.useFakeTimers()
    await w.get('[data-testid="sc-change-item.new"]').trigger('click')
    key('/')
    vi.advanceTimersByTime(1100)
    await nextTick()
    await w.get('[data-testid="sc-swap-cancel"]').trigger('click')
    expect(w.find('[data-testid="sc-conflict"]').exists()).toBe(false)
    expect(shortcutPrefs.bindings).toEqual({})
  })

  it('unbind, reset and reset all', async () => {
    await w.get('[data-testid="sc-unbind-item.new"]').trigger('click')
    expect(row('item.new').find('[data-testid="sc-unbound"]').exists()).toBe(true)
    await w.get('[data-testid="sc-reset-item.new"]').trigger('click')
    expect(shortcutPrefs.bindings['item.new']).toBeUndefined()
    setBinding('item.new', ['q'])
    setBinding('help.open', null)
    await w.get('[data-testid="sc-reset-all"]').trigger('click')
    expect(shortcutPrefs.bindings).toEqual({})
  })

  it('privacy toggle has no unbind button', () => {
    expect(row('privacy.toggle').find('[data-testid="sc-unbind-privacy.toggle"]').exists()).toBe(
      false,
    )
  })

  it('master toggle persists', async () => {
    await w.get('[data-testid="sc-enabled"]').setValue(false)
    expect(shortcutPrefs.enabled).toBe(false)
    expect(JSON.parse(localStorage.getItem('daka.shortcuts')!).enabled).toBe(false)
  })

  async function importText(text: string) {
    const input = w.get('[data-testid="sc-import-input"]')
    const file = new File([text], 'x.json', { type: 'application/json' })
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
    await input.trigger('change')
    await new Promise((r) => setTimeout(r, 20))
    await flushPromises()
  }

  it('imports a valid file and ignores unknown ids', async () => {
    await importText(
      JSON.stringify({ v: 1, enabled: false, bindings: { 'item.new': ['q'], nope: ['q'] } }),
    )
    expect(shortcutPrefs.enabled).toBe(false)
    expect(shortcutPrefs.bindings).toEqual({ 'item.new': ['q'] })
    expect(w.get('[data-testid="sc-notice"]').text()).toContain('1 unknown')
  })

  it('rejects a bad import and a conflicting import', async () => {
    await importText('{"v":9}')
    expect(w.find('[data-testid="sc-error"]').exists()).toBe(true)
    expect(shortcutPrefs.bindings).toEqual({})
    await importText(JSON.stringify({ v: 1, enabled: true, bindings: { 'item.new': ['/'] } }))
    expect(w.get('[data-testid="sc-error"]').text()).toContain('conflict')
    expect(shortcutPrefs.bindings).toEqual({})
  })

  it('rejects an oversized import', async () => {
    await importText(JSON.stringify({ v: 1, enabled: false, bindings: {}, pad: 'x'.repeat(70000) }))
    expect(w.get('[data-testid="sc-error"]').text()).toContain('too large')
    expect(shortcutPrefs.enabled).toBe(true)
  })

  it('drops __proto__ ids from an import', async () => {
    await importText('{"v":1,"enabled":true,"bindings":{"__proto__":["q"],"item.new":["q"]}}')
    expect(w.find('[data-testid="sc-error"]').exists()).toBe(false)
    expect(Object.keys(shortcutPrefs.bindings)).toEqual(['item.new'])
    expect(shortcutPrefs.bindings['item.new']).toEqual(['q'])
    expect(Object.getPrototypeOf(shortcutPrefs.bindings)).toBe(Object.prototype)
    expect(w.get('[data-testid="sc-notice"]').text()).toContain('1 unknown')
    expect((Object.prototype as Record<string, unknown>)['0']).toBeUndefined()
  })

  it('shows ctrl++ as two keys', async () => {
    setBinding('item.new', ['ctrl++'])
    await nextTick()
    const kbds = row('item.new').findAll('[data-testid="sc-keys"] kbd')
    expect(kbds.map((k) => k.text())).toEqual(['ctrl', '+'])
  })

  it('recording state is announced and linked to the button', async () => {
    const btn = w.get('[data-testid="sc-change-item.new"]')
    expect(btn.attributes('aria-pressed')).toBe('false')
    await btn.trigger('click')
    const hint = w.get('[data-testid="sc-recording"]')
    expect(hint.attributes('aria-live')).toBe('polite')
    expect(hint.attributes('role')).toBe('status')
    expect(btn.attributes('aria-pressed')).toBe('true')
    expect(btn.attributes('aria-describedby')).toBe(hint.attributes('id'))
  })

  it('exports a JSON download', async () => {
    const create = vi.fn<() => string>(() => 'blob:x')
    const revoke = vi.fn<() => void>()
    Object.assign(URL, { createObjectURL: create, revokeObjectURL: revoke })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    await w.get('[data-testid="sc-export"]').trigger('click')
    expect(create).toHaveBeenCalled()
    expect(click).toHaveBeenCalled()
    click.mockRestore()
  })
})

describe('help panel', () => {
  it('shows effective keys, the unbound state and the disabled note', async () => {
    const scope = effectScope()
    scope.run(() =>
      useShortcut([catalogDef('item.new'), catalogDef('help.open'), catalogDef('privacy.toggle')]),
    )
    setBinding('item.new', ['q'])
    setBinding('help.open', null)
    const h = mount(ShortcutHelp, { attachTo: document.body })
    helpOpen.value = true
    await nextTick()
    const panel = () => document.querySelector('[data-testid="shortcut-help"]')!.textContent ?? ''
    const text = panel()
    expect(text).toContain('New item')
    const kbds = Array.from(document.querySelectorAll('[data-testid="shortcut-help"] kbd')).map(
      (k) => k.textContent,
    )
    expect(kbds).toContain('q')
    expect(kbds).not.toContain('n')
    expect(text).toContain('Unbound')
    setShortcutsEnabled(false)
    await nextTick()
    expect(document.querySelector('[data-testid="shortcut-help-off"]')).not.toBeNull()
    expect(panel()).not.toContain('New item')
    expect(panel()).toContain('Toggle privacy guard')
    h.unmount()
    helpOpen.value = false
    scope.stop()
  })
})
