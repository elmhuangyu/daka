import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, effectScope, h, nextTick } from 'vue'
import {
  installShortcutListener,
  isTypingTarget,
  normalizeEvent,
  resetSequence,
  useShortcut,
  useShortcutList,
  type ShortcutDef,
} from './useShortcuts'

function press(key: string, init: KeyboardEventInit = {}, target: EventTarget = document.body) {
  const e = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...init })
  target.dispatchEvent(e)
  return e
}

function openDaisyModal(): HTMLElement {
  const dlg = document.createElement('div')
  dlg.className = 'modal modal-open'
  dlg.setAttribute('role', 'dialog')
  document.body.append(dlg)
  return dlg
}

let uninstall: () => void
let scopes: ReturnType<typeof effectScope>[] = []

function reg(def: Partial<ShortcutDef> & { id: string; keys: string[] }) {
  const scope = effectScope()
  scopes.push(scope)
  scope.run(() => useShortcut({ scope: 'global', labelKey: 'common.close', ...def }))
}

beforeEach(() => {
  uninstall = installShortcutListener()
})
afterEach(() => {
  uninstall()
  scopes.forEach((s) => s.stop())
  scopes = []
  document.body.innerHTML = ''
  vi.useRealTimers()
  vi.restoreAllMocks()
  resetSequence()
})

describe('normalizeEvent', () => {
  it('orders modifiers and lowercases', () => {
    const e = new KeyboardEvent('keydown', {
      key: 'H',
      ctrlKey: true,
      shiftKey: true,
      metaKey: true,
      altKey: true,
    })
    expect(normalizeEvent(e)).toBe('ctrl+alt+shift+meta+h')
  })
  it('omits shift for printable symbols', () => {
    expect(normalizeEvent(new KeyboardEvent('keydown', { key: '?', shiftKey: true }))).toBe('?')
    expect(normalizeEvent(new KeyboardEvent('keydown', { key: '[', shiftKey: true }))).toBe('[')
  })
})

describe('dispatch', () => {
  it('fires a single key and prevents default', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'a', keys: ['x'], handler })
    const e = press('x')
    expect(handler).toHaveBeenCalledOnce()
    expect(e.defaultPrevented).toBe(true)
  })

  it('leaves the default alone when a handler returns false', () => {
    reg({ id: 'a', keys: ['x'], handler: () => false })
    expect(press('x').defaultPrevented).toBe(false)
    reg({ id: 'b', keys: ['y'], handler: () => {} })
    expect(press('y').defaultPrevented).toBe(true)
  })

  it('honours a false return on the sequence path too', () => {
    reg({ id: 's', keys: ['g t'], handler: () => false })
    press('g')
    expect(press('t').defaultPrevented).toBe(false)
  })

  it('ignores single keys with ctrl/alt/meta', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'a', keys: ['x'], handler })
    press('x', { ctrlKey: true })
    press('x', { altKey: true })
    press('x', { metaKey: true })
    expect(handler).not.toHaveBeenCalled()
  })

  it('matches modifier chords', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'p', keys: ['ctrl+shift+h'], handler })
    press('H', { ctrlKey: true, shiftKey: true })
    expect(handler).toHaveBeenCalledOnce()
  })

  it('matches symbols like ? with shift', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'h', keys: ['?'], handler })
    press('?', { shiftKey: true })
    expect(handler).toHaveBeenCalledOnce()
  })

  it('ignores typing targets', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'a', keys: ['x'], handler })
    const input = document.createElement('input')
    const textarea = document.createElement('textarea')
    const select = document.createElement('select')
    const div = document.createElement('div')
    div.contentEditable = 'true'
    for (const el of [input, textarea, select, div]) {
      document.body.append(el)
      press('x', {}, el)
    }
    expect(handler).not.toHaveBeenCalled()
  })

  it('does not treat checkbox as typing', () => {
    const cb = document.createElement('input')
    cb.type = 'checkbox'
    expect(isTypingTarget(cb)).toBe(false)
  })

  it('allowInInput works in inputs and modals', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'a', keys: ['x'], handler, allowInInput: true })
    const input = document.createElement('input')
    document.body.append(input)
    press('x', {}, input)
    expect(handler).toHaveBeenCalledTimes(1)
    openDaisyModal()
    press('x')
    expect(handler).toHaveBeenCalledTimes(2)
  })

  it('blocks plain keys while a DaisyUI modal without aria-modal is open', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'a', keys: ['x'], handler })
    const dlg = openDaisyModal()
    press('x')
    expect(handler).not.toHaveBeenCalled()
    dlg.remove()
    press('x')
    expect(handler).toHaveBeenCalledOnce()
  })

  it('blocks a sequence start while a DaisyUI modal is open', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'gt', keys: ['g t'], handler })
    const dlg = openDaisyModal()
    press('g')
    dlg.remove()
    press('t')
    expect(handler).not.toHaveBeenCalled()
  })

  it('allowInInput sequences start while a DaisyUI modal is open', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'gt', keys: ['g t'], handler, allowInInput: true })
    openDaisyModal()
    press('g')
    press('t')
    expect(handler).toHaveBeenCalledOnce()
  })

  it('applies the modifier rule to the second step of a sequence', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'gc', keys: ['g ctrl+x'], handler })
    const input = document.createElement('input')
    document.body.append(input)
    press('g')
    press('x', { ctrlKey: true }, input)
    expect(handler).toHaveBeenCalledOnce()
  })

  it('ignores plain keys while a modal is open', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'a', keys: ['x'], handler })
    const dlg = document.createElement('div')
    dlg.setAttribute('role', 'dialog')
    dlg.setAttribute('aria-modal', 'true')
    document.body.append(dlg)
    press('x')
    expect(handler).not.toHaveBeenCalled()
    dlg.remove()
    press('x')
    expect(handler).toHaveBeenCalledOnce()
  })

  it('ignores defaultPrevented, IME composition and repeat', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'a', keys: ['x'], handler })
    const pre = new KeyboardEvent('keydown', { key: 'x', bubbles: true, cancelable: true })
    pre.preventDefault()
    document.body.dispatchEvent(pre)
    press('x', { isComposing: true })
    press('x', { repeat: true })
    expect(handler).not.toHaveBeenCalled()
  })

  it('fires on repeat when repeatable', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'a', keys: ['j'], handler, repeatable: true })
    press('j', { repeat: true })
    expect(handler).toHaveBeenCalledOnce()
  })

  it('never dispatches displayOnly defs', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'a', keys: ['x'], handler, displayOnly: true })
    press('x')
    expect(handler).not.toHaveBeenCalled()
  })
})

describe('sequences', () => {
  it('completes within the timeout', () => {
    vi.useFakeTimers()
    const handler = vi.fn<() => void>()
    reg({ id: 'gt', keys: ['g t'], handler })
    press('g')
    vi.advanceTimersByTime(900)
    press('t')
    expect(handler).toHaveBeenCalledOnce()
  })

  it('expires after 1s', () => {
    vi.useFakeTimers()
    const handler = vi.fn<() => void>()
    reg({ id: 'gt', keys: ['g t'], handler })
    press('g')
    vi.advanceTimersByTime(1001)
    press('t')
    expect(handler).not.toHaveBeenCalled()
  })

  it('is cancelled by a wrong key', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'gt', keys: ['g t'], handler })
    press('g')
    press('z')
    press('t')
    expect(handler).not.toHaveBeenCalled()
  })

  it('is not started in typing targets', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'gt', keys: ['g t'], handler })
    const input = document.createElement('input')
    document.body.append(input)
    press('g', {}, input)
    press('t')
    expect(handler).not.toHaveBeenCalled()
  })

  it('is cancelled when focus moves to a typing target', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'gt', keys: ['g t'], handler })
    const input = document.createElement('input')
    document.body.append(input)
    press('g')
    press('t', {}, input)
    press('t')
    expect(handler).not.toHaveBeenCalled()
  })

  it('does not cancel on bare modifier presses', () => {
    const handler = vi.fn<() => void>()
    reg({ id: 'gt', keys: ['g t'], handler })
    press('g')
    press('Shift', { shiftKey: true })
    press('t')
    expect(handler).toHaveBeenCalledOnce()
  })
})

describe('registry', () => {
  it('unregisters on unmount', async () => {
    const list = useShortcutList()
    const handler = vi.fn<() => void>()
    const C = defineComponent({
      setup() {
        useShortcut({ id: 'c', keys: ['x'], scope: 'global', labelKey: 'common.close', handler })
        return () => h('div')
      },
    })
    const app = createApp(C)
    app.mount(document.createElement('div'))
    expect(list.value.map((d) => d.id)).toContain('c')
    press('x')
    expect(handler).toHaveBeenCalledOnce()
    app.unmount()
    await nextTick()
    expect(list.value.map((d) => d.id)).not.toContain('c')
    press('x')
    expect(handler).toHaveBeenCalledOnce()
  })

  it('warns on duplicate id and ref-counts owners', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    const list = useShortcutList()
    const first = effectScope()
    const second = effectScope()
    const def: ShortcutDef = { id: 'dup', keys: ['x'], scope: 'global', labelKey: 'common.close' }
    first.run(() => useShortcut(def))
    second.run(() => useShortcut(def))
    expect(warn).toHaveBeenCalledOnce()
    first.stop()
    expect(list.value.map((d) => d.id)).toContain('dup')
    second.stop()
    expect(list.value.map((d) => d.id)).not.toContain('dup')
  })
})
