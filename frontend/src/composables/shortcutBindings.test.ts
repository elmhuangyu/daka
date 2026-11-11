import { describe, expect, it } from 'vitest'
import {
  bindingKeyParts,
  canonicalBinding,
  findConflicts,
  validateBinding,
} from './shortcutBindings'
import { SHORTCUT_CATALOG } from './shortcutCatalog'
import { NAV_ITEMS } from '../nav'

describe('validateBinding', () => {
  it.each([
    'x',
    '?',
    '[',
    'shift+a',
    'g t',
    'ctrl+shift+h',
    'alt+k',
    'ArrowLeft',
    'PageUp',
    'ctrl++',
  ])('accepts %s', (b) => expect(validateBinding(b)).toBeNull())
  it.each([
    'Tab',
    'Enter',
    'Space',
    ' ',
    'Escape',
    'shift+tab',
    'ctrl',
    'shift',
    'ctrl+shift',
    'g t x',
    'ctrl+k x',
    'g ctrl+x',
    'ctrl+t',
    'ctrl+w',
    'ctrl+n',
    'ctrl+r',
    'ctrl+l',
    'ctrl+q',
    'ctrl+tab',
    'ctrl+shift+t',
    'ctrl+shift+n',
    'ctrl+shift+w',
    'ctrl+shift+b',
    'ctrl+shift+o',
    'ctrl+shift+delete',
    'ctrl+shift+a',
    'ctrl+shift+p',
    'ctrl+shift+k',
    'meta+shift+n',
    'meta+shift+t',
    'meta+shift+w',
    'shift+?',
    'shift+/',
    'shift+[',
    'shift+1',
    'F5',
    'alt+ArrowLeft',
    'alt+ArrowRight',
    'ctrl+1',
    'ctrl+9',
    'ctrl+a+b',
    'ctrl+ctrl+k',
    'hyper+k',
    'foo',
    '',
  ])('rejects %j', (b) => expect(validateBinding(b)).not.toBeNull())
})

describe('bindingKeyParts', () => {
  it.each([
    ['ctrl++', ['ctrl', '+']],
    ['+', ['+']],
    ['g t', ['g', 't']],
    ['ctrl+shift+h', ['ctrl', 'shift', 'h']],
    ['Shift+Ctrl+H', ['ctrl', 'shift', 'h']],
    ['  ', []],
    ['arrowleft', ['ArrowLeft']],
    ['ctrl+pagedown', ['ctrl', 'PageDown']],
    ['f5', ['F5']],
  ])('splits %j', (b, parts) => expect(bindingKeyParts(b)).toEqual(parts))
})

describe('bindingKeyParts display case', () => {
  it('pretty-cases catalog default named keys', () => {
    const parts = SHORTCUT_CATALOG.flatMap((e) => e.keys.flatMap((k) => bindingKeyParts(k)))
    for (const name of [
      'ArrowLeft',
      'ArrowRight',
      'ArrowUp',
      'ArrowDown',
      'PageUp',
      'PageDown',
      'Enter',
    ]) {
      expect(parts).toContain(name)
    }
    const named = parts.filter((p) => p.length > 1 && !['ctrl', 'alt', 'shift', 'meta'].includes(p))
    expect(named.filter((p) => !/^[A-Z]/.test(p))).toEqual([])
  })
})

describe('canonicalBinding', () => {
  it('orders modifiers and lowercases', () => {
    expect(canonicalBinding('Shift+Ctrl+H')).toBe('ctrl+shift+h')
    expect(canonicalBinding('G  T')).toBe('g t')
  })
})

describe('findConflicts', () => {
  const defs = [
    { id: 'a', scope: 'global' as const },
    { id: 'b', scope: 'today' as const },
    { id: 'c', scope: 'calendar' as const },
    { id: 'd', scope: 'today' as const },
  ]
  it('detects same binding in the same scope', () => {
    expect(findConflicts({ b: ['x'], d: ['x'] }, defs)).toEqual([{ a: 'b', b: 'd', binding: 'x' }])
  })
  it('global overlaps every scope', () => {
    expect(findConflicts({ a: ['x'], c: ['x'] }, defs)).toHaveLength(1)
  })
  it('today and calendar do not conflict', () => {
    expect(findConflicts({ b: ['t'], c: ['t'] }, defs)).toEqual([])
  })
  it('detects prefix conflicts', () => {
    expect(findConflicts({ a: ['g'], b: ['g t'] }, defs)).toHaveLength(1)
    expect(findConflicts({ a: ['g t'], b: ['g c'] }, defs)).toEqual([])
  })
  it('ignores display-only entries and an action against itself', () => {
    expect(
      findConflicts({ a: ['x', 'x'], b: ['x'] }, [defs[0]!, { ...defs[1]!, displayOnly: true }]),
    ).toEqual([])
  })
})

describe('catalog', () => {
  it('has unique ids, valid defaults and no default conflicts', () => {
    const ids = SHORTCUT_CATALOG.map((e) => e.id)
    expect(new Set(ids).size).toBe(ids.length)
    const map: Record<string, string[]> = {}
    const invalid: string[] = []
    for (const e of SHORTCUT_CATALOG) {
      map[e.id] = e.keys
      if (!e.displayOnly && e.keys.some((k) => validateBinding(k) !== null)) invalid.push(e.id)
    }
    expect(invalid).toEqual([])
    expect(findConflicts(map, SHORTCUT_CATALOG)).toEqual([])
  })
  it('covers every nav destination', () => {
    for (const n of NAV_ITEMS)
      expect(SHORTCUT_CATALOG.some((e) => e.id === `nav.${n.name}`)).toBe(true)
  })
})
