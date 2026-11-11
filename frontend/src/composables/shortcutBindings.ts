import type { MessageKey } from '../i18n'
import type { ShortcutScope } from './useShortcuts'

// Pure helpers for binding strings: parsing, validation and conflict detection.
// A binding is `x`, `shift+a`, `ctrl+shift+h` or a two-step sequence such as `g t`.

const MODIFIER_ORDER = ['ctrl', 'alt', 'shift', 'meta'] as const
const MODS = new Set<string>(MODIFIER_ORDER)
const SEQUENCE_PREFIX_MODS = ['ctrl', 'alt', 'meta']

/** Named non-printable keys that may be bound (lowercased, as normalizeEvent emits them). */
const NAMED_KEYS = new Set([
  'arrowup',
  'arrowdown',
  'arrowleft',
  'arrowright',
  'pageup',
  'pagedown',
  'home',
  'end',
  ...Array.from({ length: 12 }, (_, i) => `f${i + 1}`),
])
const FORBIDDEN_KEYS = new Set(['tab', 'enter', 'space', 'escape', 'esc'])

/** Chords the browser or OS owns. Canonical form: modifiers in ctrl, alt, shift, meta order. */
const RESERVED = new Set([
  'ctrl+t',
  'ctrl+w',
  'ctrl+n',
  'ctrl+r',
  'ctrl+l',
  'ctrl+q',
  'ctrl+d',
  'ctrl+h',
  'ctrl+j',
  'ctrl+p',
  'ctrl+s',
  'ctrl+o',
  'ctrl+u',
  'ctrl+f',
  'ctrl+g',
  'ctrl+tab',
  'ctrl+shift+tab',
  'ctrl+pageup',
  'ctrl+pagedown',
  'ctrl+shift+t',
  'ctrl+shift+n',
  'ctrl+shift+w',
  'ctrl+shift+q',
  'ctrl+shift+r',
  'ctrl+shift+i',
  'ctrl+shift+j',
  'ctrl+shift+delete',
  'ctrl+shift+a',
  'ctrl+shift+b',
  'ctrl+shift+o',
  'ctrl+shift+p',
  'ctrl+shift+k',
  'ctrl+shift+m',
  'ctrl+shift+c',
  'shift+meta+n',
  'shift+meta+t',
  'shift+meta+w',
  'shift+meta+p',
  'shift+meta+b',
  'shift+meta+delete',
  'meta+t',
  'meta+w',
  'meta+n',
  'meta+r',
  'meta+l',
  'meta+q',
  'meta+h',
  'meta+m',
  'meta+tab',
  'alt+arrowleft',
  'alt+arrowright',
  'alt+f4',
  'alt+home',
  'f1',
  'f5',
  'f11',
  'f12',
  'ctrl+f5',
  'ctrl+shift+f5',
  ...Array.from({ length: 9 }, (_, i) => `ctrl+${i + 1}`),
  ...Array.from({ length: 9 }, (_, i) => `meta+${i + 1}`),
  ...Array.from({ length: 9 }, (_, i) => `alt+${i + 1}`),
])

export interface ParsedStep {
  mods: string[]
  key: string
}

/** Split one step into modifiers (canonical order, deduplicated) and key. Handles `+` as a key. */
export function parseStep(step: string): ParsedStep {
  const s = step.trim().toLowerCase()
  let key: string
  let rest: string[]
  if (s === '+') {
    key = '+'
    rest = []
  } else if (s.endsWith('++')) {
    key = '+'
    rest = s.slice(0, -2).split('+')
  } else {
    const parts = s.split('+')
    key = parts.pop() ?? ''
    rest = parts
  }
  const mods = MODIFIER_ORDER.filter((m) => rest.includes(m))
  return { mods, key }
}

export function stepToString(p: ParsedStep): string {
  return [...p.mods, p.key].join('+')
}

/** Canonical steps of a binding, at most two; comparable with normalizeEvent output. */
export function parseBinding(binding: string): string[] {
  return binding
    .trim()
    .split(/\s+/)
    .slice(0, 2)
    .map((step) => stepToString(parseStep(step)))
}

/**
 * Display parts of a binding, steps flattened: modifiers then key for each step.
 * `ctrl++` yields ['ctrl', '+'] and `g t` yields ['g', 't'].
 */
export function bindingKeyParts(binding: string): string[] {
  return binding
    .trim()
    .split(/\s+/)
    .filter((step) => step !== '')
    .flatMap((step) => {
      const { mods, key } = parseStep(step)
      return [...mods, displayKey(key)]
    })
}

const DISPLAY_NAMES: Record<string, string> = {
  arrowup: 'ArrowUp',
  arrowdown: 'ArrowDown',
  arrowleft: 'ArrowLeft',
  arrowright: 'ArrowRight',
  pageup: 'PageUp',
  pagedown: 'PageDown',
  home: 'Home',
  end: 'End',
  enter: 'Enter',
  tab: 'Tab',
  escape: 'Esc',
}

/** Pretty-cased key label: ArrowLeft, PageUp, F5. Single characters are unchanged. */
function displayKey(key: string): string {
  if (/^f\d{1,2}$/.test(key)) return key.toUpperCase()
  return DISPLAY_NAMES[key] ?? key
}

export function canonicalBinding(binding: string): string {
  return parseBinding(binding).join(' ')
}

function validateStep(raw: string): MessageKey | null {
  const s = raw.trim().toLowerCase()
  // Reject duplicated or unknown modifiers before canonicalizing hides them.
  const body =
    s === '+' ? '' : s.endsWith('++') ? s.slice(0, -2) : s.split('+').slice(0, -1).join('+')
  const given = body === '' ? [] : body.split('+')
  if (given.some((m) => !MODS.has(m)) || new Set(given).size !== given.length) {
    return 'shortcuts.error.modifierCombo'
  }
  const { mods, key } = parseStep(s)
  if (key === '' || MODS.has(key)) return 'shortcuts.error.modifierAlone'
  if (FORBIDDEN_KEYS.has(key)) return 'shortcuts.error.forbiddenKey'
  if (key.length > 1 && !NAMED_KEYS.has(key)) return 'shortcuts.error.unsupportedKey'
  // normalizeEvent drops shift for printable symbols and digits (the key already carries it),
  // so such a binding could never match a real key event.
  if (mods.includes('shift') && key.length === 1 && key.toLowerCase() === key.toUpperCase()) {
    return 'shortcuts.error.shiftSymbol'
  }
  if (RESERVED.has(stepToString({ mods, key }))) return 'shortcuts.error.reserved'
  return null
}

const isChord = (step: string) => parseStep(step).mods.some((m) => SEQUENCE_PREFIX_MODS.includes(m))

/**
 * Returns an error message key, or null when the binding is allowed: a single key (shift
 * allowed with letters and named keys), a ctrl/alt/meta chord, or a two-step sequence whose
 * steps use no ctrl/alt/meta (shift is fine).
 */
export function validateBinding(binding: string): MessageKey | null {
  const steps = binding.trim().split(/\s+/)
  if (binding.trim() === '') return 'shortcuts.error.empty'
  if (steps.length > 2) return 'shortcuts.error.tooManySteps'
  for (const step of steps) {
    const err = validateStep(step)
    if (err) return err
  }
  if (steps.length === 2 && steps.some(isChord)) {
    return 'shortcuts.error.sequencePlain'
  }
  return null
}

export interface ConflictDef {
  id: string
  scope: ShortcutScope
  displayOnly?: boolean
}

export interface Conflict {
  a: string
  b: string
  /** The binding of `a` that collides with `b`. */
  binding: string
}

const scopesOverlap = (x: ShortcutScope, y: ShortcutScope) =>
  x === y || x === 'global' || y === 'global'

function collides(x: string[], y: string[]): boolean {
  // Equal, or one is a strict prefix of the other (`g` vs `g t`).
  const n = Math.min(x.length, y.length)
  for (let i = 0; i < n; i++) if (x[i] !== y[i]) return false
  return true
}

/** Pure conflict detection over effective bindings (id -> bindings). */
export function findConflicts(bindings: Record<string, string[]>, defs: ConflictDef[]): Conflict[] {
  const out: Conflict[] = []
  const live = defs.filter((d) => !d.displayOnly)
  for (let i = 0; i < live.length; i++) {
    for (let j = i + 1; j < live.length; j++) {
      const a = live[i]!
      const b = live[j]!
      if (!scopesOverlap(a.scope, b.scope)) continue
      let hit: string | null = null
      for (const ka of bindings[a.id] ?? []) {
        for (const kb of bindings[b.id] ?? []) {
          if (hit === null && collides(parseBinding(ka), parseBinding(kb))) hit = ka
        }
      }
      if (hit !== null) out.push({ a: a.id, b: b.id, binding: hit })
    }
  }
  return out
}
