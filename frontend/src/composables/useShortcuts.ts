import type { MessageKey } from '../i18n'
import { computed, getCurrentScope, onScopeDispose, reactive } from 'vue'
import { parseBinding } from './shortcutBindings'
import { PRIVACY_TOGGLE_ID } from './shortcutCatalog'
import { shortcutPrefs } from './shortcutPrefs'

export type ShortcutScope = 'global' | 'today' | 'calendar'

export interface ShortcutDef {
  id: string
  /** Default bindings: `x`, `g t` (sequence, max 2 steps) or `ctrl+shift+h`. */
  keys: string[]
  /** Metadata for the help panel only; dispatch does not consult it. */
  scope: ShortcutScope
  /** Message key of the label; resolve with t() at render time. */
  labelKey: MessageKey
  /** Fire even when focus is in a typing target or a modal is open. */
  allowInInput?: boolean
  /** Fire on auto-repeat keydown events too. */
  repeatable?: boolean
  handler?: (e: KeyboardEvent) => void
  /** Listed in the help panel only; never dispatched. */
  displayOnly?: boolean
}

interface Entry {
  def: ShortcutDef
  owners: Set<symbol>
}

const SEQUENCE_TIMEOUT_MS = 1000
const MODIFIER_KEYS = new Set(['control', 'alt', 'shift', 'meta'])
const NON_TYPING_INPUT_TYPES = new Set(['checkbox', 'radio', 'button', 'submit'])

const registry = reactive(new Map<string, Entry>())

/** The user's override (null = unbound) or the default keys. */
export function effectiveKeys(def: ShortcutDef): string[] {
  const o = shortcutPrefs.bindings[def.id]
  return o === undefined ? def.keys : (o ?? [])
}

/** True while the settings page captures a new binding; all dispatch is suspended. */
let recording = false
export function setShortcutRecording(on: boolean) {
  recording = on
  if (on) resetSequence()
}

function register(def: ShortcutDef, owner: symbol) {
  const existing = registry.get(def.id)
  if (existing) {
    console.warn(`Shortcut "${def.id}" is already registered; keeping the first definition`)
    existing.owners.add(owner)
    return
  }
  registry.set(def.id, { def, owners: new Set([owner]) })
}

function unregister(id: string, owner: symbol) {
  const entry = registry.get(id)
  if (!entry) return
  entry.owners.delete(owner)
  if (entry.owners.size === 0) registry.delete(id)
}

export function useShortcut(defs: ShortcutDef | ShortcutDef[]): void {
  const list = Array.isArray(defs) ? defs : [defs]
  const owner = Symbol('shortcut-owner')
  for (const def of list) register(def, owner)
  if (getCurrentScope()) {
    // A component's setup runs in an effect scope that is disposed on unmount.
    onScopeDispose(() => {
      for (const def of list) unregister(def.id, owner)
    })
  }
}

export function useShortcutList() {
  return computed(() => Array.from(registry.values(), (e) => e.def))
}

/** Normalized chord: ctrl, alt, shift, meta in that order, then the lowercased key. */
export function normalizeEvent(e: KeyboardEvent): string {
  let key = e.key === ' ' ? 'space' : e.key.toLowerCase()
  const parts: string[] = []
  if (e.ctrlKey) parts.push('ctrl')
  if (e.altKey) parts.push('alt')
  // Printable symbols such as "?" or "[" already carry shift in e.key.
  const symbol = e.key.length === 1 && e.key.toLowerCase() === e.key.toUpperCase() && e.key !== ' '
  if (e.shiftKey && !symbol) parts.push('shift')
  if (e.metaKey) parts.push('meta')
  parts.push(key)
  return parts.join('+')
}

export function isTypingTarget(el: EventTarget | null): boolean {
  if (!(el instanceof HTMLElement)) return false
  if (el instanceof HTMLInputElement) return !NON_TYPING_INPUT_TYPES.has(el.type)
  if (el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement) return true
  return (
    el.isContentEditable || el.contentEditable === 'true' || el.contentEditable === 'plaintext-only'
  )
}

export function isModalOpen(): boolean {
  return (
    document.querySelector(
      '[role="dialog"][aria-modal="true"], dialog[open], .modal.modal-open',
    ) !== null
  )
}

/** Plain keys (no ctrl/alt/meta) are subject to the typing and modal filters. */
function isPlainStep(step: string): boolean {
  return !/(^|\+)(ctrl|alt|meta)\+/.test(step)
}

/** First step of a pending sequence. While set, the next key is consumed by the sequence: a non-matching key cancels it and is swallowed (no other shortcut fires). */
let pending: string | null = null
let pendingTimer: ReturnType<typeof setTimeout> | null = null

export function resetSequence() {
  pending = null
  if (pendingTimer !== null) {
    clearTimeout(pendingTimer)
    pendingTimer = null
  }
}

function startSequence(first: string) {
  resetSequence()
  pending = first
  pendingTimer = setTimeout(resetSequence, SEQUENCE_TIMEOUT_MS)
}

/** With the master toggle off only the privacy toggle stays live (Esc is handled outside the registry). */
function active(def: ShortcutDef): boolean {
  return shortcutPrefs.enabled || def.id === PRIVACY_TOGGLE_ID
}

function fire(def: ShortcutDef, e: KeyboardEvent): void {
  e.preventDefault()
  def.handler?.(e)
}

function onKeydown(e: KeyboardEvent) {
  if (recording || e.defaultPrevented || e.isComposing) return
  if (MODIFIER_KEYS.has(e.key.toLowerCase())) return

  const chord = normalizeEvent(e)
  const blocked = isTypingTarget(e.target) || isModalOpen()
  const blockedPlain = (def: ShortcutDef) => blocked && !def.allowInInput
  const allowed = (def: ShortcutDef, plain: boolean) =>
    !def.displayOnly &&
    active(def) &&
    (!e.repeat || def.repeatable) &&
    (!plain || !blockedPlain(def))

  if (pending !== null) {
    const first = pending
    resetSequence()
    for (const { def } of registry.values()) {
      for (const binding of effectiveKeys(def)) {
        const steps = parseBinding(binding)
        if (
          steps.length === 2 &&
          steps[0] === first &&
          steps[1] === chord &&
          allowed(def, isPlainStep(chord))
        ) {
          fire(def, e)
          return
        }
      }
    }
    return
  }

  let sequenceStart = false
  for (const { def } of registry.values()) {
    for (const binding of effectiveKeys(def)) {
      const steps = parseBinding(binding)
      if (steps[0] !== chord) continue
      if (steps.length === 1) {
        if (allowed(def, isPlainStep(chord))) {
          fire(def, e)
          return
        }
      } else if (
        !e.repeat &&
        !def.displayOnly &&
        active(def) &&
        isPlainStep(chord) &&
        !blockedPlain(def)
      ) {
        sequenceStart = true
      }
    }
  }
  if (sequenceStart) startSequence(chord)
}

export function installShortcutListener(): () => void {
  window.addEventListener('keydown', onKeydown)
  return () => {
    window.removeEventListener('keydown', onKeydown)
    resetSequence()
  }
}
