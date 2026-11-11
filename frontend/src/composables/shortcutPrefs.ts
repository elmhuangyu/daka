import { reactive } from 'vue'
import type { MessageKey } from '../i18n'
import { PRIVACY_TOGGLE_ID, SHORTCUT_CATALOG } from './shortcutCatalog'
import { canonicalBinding, findConflicts, validateBinding } from './shortcutBindings'

// Per-device keyboard shortcut preferences. Like theme and locale they live in
// localStorage only and are deliberately NOT part of the account export/import.

export const SHORTCUTS_STORAGE_KEY = 'daka.shortcuts'
const VERSION = 1

/** null = explicitly unbound. Absent = default keys. */
export type BindingOverrides = Record<string, string[] | null>

export interface ShortcutPrefs {
  v: 1
  enabled: boolean
  bindings: BindingOverrides
}

export type ParseResult =
  | { ok: true; prefs: ShortcutPrefs; ignored: number }
  | { ok: false; error: MessageKey }

// Never keep these as binding ids, whatever the catalog says (prototype pollution guard).
const UNSAFE_IDS = new Set(['__proto__', 'constructor', 'prototype'])

const STORED_IDS = new Set(SHORTCUT_CATALOG.map((c) => c.id))

const isRecord = (x: unknown): x is Record<string, unknown> =>
  typeof x === 'object' && x !== null && !Array.isArray(x)

/**
 * Strict parser for stored and imported data. Unknown ids (when `knownIds` is given)
 * are dropped and counted; any malformed value rejects the whole document.
 */
export function parseShortcutPrefs(text: string, knownIds?: Set<string>): ParseResult {
  let raw: unknown
  try {
    raw = JSON.parse(text)
  } catch {
    return { ok: false, error: 'shortcuts.error.notJson' }
  }
  if (!isRecord(raw)) return { ok: false, error: 'shortcuts.error.unexpected' }
  if (raw.v !== VERSION) return { ok: false, error: 'shortcuts.error.version' }
  if (typeof raw.enabled !== 'boolean')
    return { ok: false, error: 'shortcuts.error.missingEnabled' }
  if (!isRecord(raw.bindings)) return { ok: false, error: 'shortcuts.error.missingBindings' }
  const bindings: BindingOverrides = {}
  let ignored = 0
  for (const [id, value] of Object.entries(raw.bindings)) {
    if (UNSAFE_IDS.has(id) || (knownIds && !knownIds.has(id))) {
      ignored++
      continue
    }
    if (value === null) {
      if (id === PRIVACY_TOGGLE_ID) return { ok: false, error: 'shortcuts.error.privacyUnbound' }
      bindings[id] = null
      continue
    }
    if (!Array.isArray(value) || value.length === 0 || value.some((k) => typeof k !== 'string')) {
      return { ok: false, error: 'shortcuts.error.badList' }
    }
    for (const k of value as string[]) {
      if (validateBinding(k) !== null) return { ok: false, error: 'shortcuts.error.badShortcut' }
    }
    bindings[id] = (value as string[]).map(canonicalBinding)
  }
  return { ok: true, prefs: { v: VERSION, enabled: raw.enabled, bindings }, ignored }
}

export const shortcutPrefs = reactive<{ enabled: boolean; bindings: BindingOverrides }>({
  enabled: true,
  bindings: {},
})

export function serializeShortcutPrefs(): string {
  const prefs: ShortcutPrefs = {
    v: VERSION,
    enabled: shortcutPrefs.enabled,
    bindings: { ...shortcutPrefs.bindings },
  }
  return JSON.stringify(prefs, null, 2)
}

function persist() {
  try {
    localStorage.setItem(SHORTCUTS_STORAGE_KEY, serializeShortcutPrefs())
  } catch {
    // storage may be unavailable
  }
}

/** Reload from localStorage; corrupt, unknown-version or missing data means defaults. */
export function reloadShortcutPrefs() {
  let text: string | null = null
  try {
    text = localStorage.getItem(SHORTCUTS_STORAGE_KEY)
  } catch {
    // storage may be unavailable
  }
  const parsed = text === null ? null : parseShortcutPrefs(text, STORED_IDS)
  if (parsed?.ok) {
    shortcutPrefs.enabled = parsed.prefs.enabled
    shortcutPrefs.bindings = parsed.prefs.bindings
  } else {
    shortcutPrefs.enabled = true
    shortcutPrefs.bindings = {}
  }
}

export function setShortcutsEnabled(enabled: boolean) {
  shortcutPrefs.enabled = enabled
  persist()
}

/**
 * Override an action's keys. null unbinds, except for the privacy toggle, which can be
 * rebound but never left without a key (reset-to-default is always available).
 */
export function setBinding(id: string, keys: string[] | null): boolean {
  if (keys === null && id === PRIVACY_TOGGLE_ID) return false
  if (keys !== null && (keys.length === 0 || keys.some((k) => validateBinding(k) !== null))) {
    return false
  }
  shortcutPrefs.bindings[id] = keys === null ? null : keys.map(canonicalBinding)
  persist()
  return true
}

export function resetBinding(id: string) {
  delete shortcutPrefs.bindings[id]
  persist()
}

export function resetAllBindings() {
  shortcutPrefs.bindings = {}
  persist()
}

export function replaceBindings(bindings: BindingOverrides) {
  shortcutPrefs.bindings = { ...bindings }
  persist()
}

/** Replace everything from a validated import. */
export function applyPrefs(prefs: ShortcutPrefs) {
  shortcutPrefs.enabled = prefs.enabled
  shortcutPrefs.bindings = { ...prefs.bindings }
  persist()
}

export function overrideFor(id: string): string[] | null | undefined {
  return shortcutPrefs.bindings[id]
}

/** Effective keys for every catalog action under the given overrides. */
export function effectiveMap(overrides: BindingOverrides = shortcutPrefs.bindings) {
  const out: Record<string, string[]> = {}
  for (const e of SHORTCUT_CATALOG) {
    const o = overrides[e.id]
    out[e.id] = o === undefined ? e.keys : (o ?? [])
  }
  return out
}

export function catalogConflicts(overrides: BindingOverrides) {
  return findConflicts(effectiveMap(overrides), SHORTCUT_CATALOG)
}

reloadShortcutPrefs()
if (typeof window !== 'undefined') {
  window.addEventListener('storage', (e) => {
    if (e.key === null || e.key === SHORTCUTS_STORAGE_KEY) reloadShortcutPrefs()
  })
}
