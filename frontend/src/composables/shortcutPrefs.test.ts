import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope } from 'vue'
import {
  SHORTCUTS_STORAGE_KEY,
  applyPrefs,
  parseShortcutPrefs,
  reloadShortcutPrefs,
  resetAllBindings,
  resetBinding,
  setBinding,
  setShortcutsEnabled,
  shortcutPrefs,
} from './shortcutPrefs'
import {
  effectiveKeys,
  installShortcutListener,
  resetSequence,
  setShortcutRecording,
  useShortcut,
} from './useShortcuts'
import { catalogDef } from './shortcutCatalog'

function press(key: string, init: KeyboardEventInit = {}) {
  const e = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...init })
  document.body.dispatchEvent(e)
  return e
}

let uninstall: () => void
let scopes: ReturnType<typeof effectScope>[] = []

beforeEach(() => {
  localStorage.clear()
  reloadShortcutPrefs()
  uninstall = installShortcutListener()
})
afterEach(() => {
  uninstall()
  scopes.forEach((s) => s.stop())
  scopes = []
  resetSequence()
  setShortcutRecording(false)
  localStorage.clear()
  reloadShortcutPrefs()
})

function mountDefs(handlers: Record<string, () => void>) {
  const scope = effectScope()
  scopes.push(scope)
  scope.run(() => useShortcut(Object.entries(handlers).map(([id, h]) => catalogDef(id, h))))
}

describe('persistence', () => {
  it('round trips through localStorage', () => {
    setBinding('item.new', ['m'])
    setBinding('help.open', null)
    setShortcutsEnabled(false)
    shortcutPrefs.bindings = {}
    shortcutPrefs.enabled = true
    reloadShortcutPrefs()
    expect(shortcutPrefs.enabled).toBe(false)
    expect(shortcutPrefs.bindings).toEqual({ 'item.new': ['m'], 'help.open': null })
  })
  it.each([
    '{not json',
    '[]',
    '{"v":2,"enabled":true,"bindings":{}}',
    '{"v":1,"enabled":"yes","bindings":{}}',
    '{"v":1,"enabled":true,"bindings":{"item.new":["ctrl+t"]}}',
  ])('falls back to defaults for %s', (raw) => {
    localStorage.setItem(SHORTCUTS_STORAGE_KEY, raw)
    reloadShortcutPrefs()
    expect(shortcutPrefs.enabled).toBe(true)
    expect(shortcutPrefs.bindings).toEqual({})
  })
  it('refreshes on the storage event', () => {
    localStorage.setItem(
      SHORTCUTS_STORAGE_KEY,
      JSON.stringify({ v: 1, enabled: false, bindings: {} }),
    )
    window.dispatchEvent(new StorageEvent('storage', { key: SHORTCUTS_STORAGE_KEY }))
    expect(shortcutPrefs.enabled).toBe(false)
  })
  it('survives a throwing localStorage', () => {
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })
    expect(() => setBinding('item.new', ['m'])).not.toThrow()
    spy.mockRestore()
  })
})

describe('dispatch with overrides', () => {
  it('rebinding changes which key fires and the old key stops', () => {
    const fn = vi.fn<() => void>()
    mountDefs({ 'item.new': fn })
    setBinding('item.new', ['m'])
    press('n')
    expect(fn).not.toHaveBeenCalled()
    press('m')
    expect(fn).toHaveBeenCalledTimes(1)
  })
  it('supports rebinding to a sequence', () => {
    const fn = vi.fn<() => void>()
    mountDefs({ 'item.new': fn })
    setBinding('item.new', ['a b'])
    press('a')
    press('b')
    expect(fn).toHaveBeenCalledTimes(1)
  })
  it('unbind stops the action; reset restores defaults', () => {
    const fn = vi.fn<() => void>()
    mountDefs({ 'item.new': fn })
    setBinding('item.new', null)
    press('n')
    expect(fn).not.toHaveBeenCalled()
    resetBinding('item.new')
    press('n')
    expect(fn).toHaveBeenCalledTimes(1)
    setBinding('item.new', ['m'])
    resetAllBindings()
    expect(effectiveKeys(catalogDef('item.new'))).toEqual(['n'])
  })
  it('refuses to unbind the privacy toggle but allows rebinding it', () => {
    expect(setBinding('privacy.toggle', null)).toBe(false)
    expect(setBinding('privacy.toggle', ['ctrl+shift+p'])).toBe(true)
    expect(shortcutPrefs.bindings['privacy.toggle']).toEqual(['ctrl+shift+p'])
  })
  it('master toggle off disables everything except privacy.toggle', () => {
    const nav = vi.fn<() => void>()
    const priv = vi.fn<() => void>()
    mountDefs({ 'item.new': nav, 'nav.today': nav, 'privacy.toggle': priv })
    setShortcutsEnabled(false)
    press('n')
    press('g')
    press('t')
    expect(nav).not.toHaveBeenCalled()
    press('H', { ctrlKey: true, shiftKey: true })
    expect(priv).toHaveBeenCalledTimes(1)
    setShortcutsEnabled(true)
    press('n')
    expect(nav).toHaveBeenCalledTimes(1)
  })
  it('recording suppresses all dispatch', () => {
    const fn = vi.fn<() => void>()
    mountDefs({ 'item.new': fn })
    setShortcutRecording(true)
    press('n')
    expect(fn).not.toHaveBeenCalled()
    setShortcutRecording(false)
    press('n')
    expect(fn).toHaveBeenCalledTimes(1)
  })
})

describe('parseShortcutPrefs', () => {
  const doc = (bindings: unknown, extra: object = {}) =>
    JSON.stringify({ v: 1, enabled: true, bindings, ...extra })
  it('rejects bad shapes', () => {
    for (const raw of [
      'nope',
      '5',
      doc([]),
      doc({ 'item.new': 'm' }),
      doc({ 'item.new': [] }),
      doc({ 'item.new': [5] }),
      doc({ 'item.new': ['ctrl+w'] }),
      doc({ 'privacy.toggle': null }),
      JSON.stringify({ v: 1, bindings: {} }),
      JSON.stringify({ v: 1, enabled: true }),
    ]) {
      expect(parseShortcutPrefs(raw).ok).toBe(false)
    }
  })
  it('ignores unknown ids and counts them', () => {
    const r = parseShortcutPrefs(doc({ 'item.new': ['M'], bogus: ['q'] }), new Set(['item.new']))
    expect(r).toEqual({
      ok: true,
      ignored: 1,
      prefs: { v: 1, enabled: true, bindings: { 'item.new': ['m'] } },
    })
  })
  it('applyPrefs replaces state', () => {
    applyPrefs({ v: 1, enabled: false, bindings: { 'item.new': null } })
    expect(shortcutPrefs.enabled).toBe(false)
  })
})
