import type { ShortcutDef } from './useShortcuts'

/**
 * Every shortcut the app can register. Views take id, scope, label key and default keys
 * from here (via `catalogDef`) so the settings page can list actions whose view is not
 * mounted. Labels are message keys resolved with t() at render time (no user data).
 */
export type CatalogEntry = Omit<ShortcutDef, 'handler'>

export const PRIVACY_TOGGLE_ID = 'privacy.toggle'

export const SHORTCUT_CATALOG: CatalogEntry[] = [
  { id: 'help.open', keys: ['?'], scope: 'global', labelKey: 'shortcuts.label.help.open' },
  { id: 'search.focus', keys: ['/'], scope: 'global', labelKey: 'shortcuts.label.search.focus' },
  { id: 'item.new', keys: ['n'], scope: 'global', labelKey: 'shortcuts.label.item.new' },
  { id: 'nav.today', keys: ['g t'], scope: 'global', labelKey: 'shortcuts.label.nav.today' },
  { id: 'nav.calendar', keys: ['g c'], scope: 'global', labelKey: 'shortcuts.label.nav.calendar' },
  { id: 'nav.projects', keys: ['g p'], scope: 'global', labelKey: 'shortcuts.label.nav.projects' },
  { id: 'nav.items', keys: ['g i'], scope: 'global', labelKey: 'shortcuts.label.nav.items' },
  { id: 'nav.settings', keys: ['g s'], scope: 'global', labelKey: 'shortcuts.label.nav.settings' },
  {
    id: 'nav.shortcuts',
    keys: ['g k'],
    scope: 'global',
    labelKey: 'shortcuts.label.nav.shortcuts',
  },
  {
    id: PRIVACY_TOGGLE_ID,
    keys: ['ctrl+shift+h'],
    scope: 'global',
    labelKey: 'shortcuts.label.privacy.toggle',
    allowInInput: true,
  },
  { id: 'today.prevDay', keys: ['['], scope: 'today', labelKey: 'shortcuts.label.today.prevDay' },
  { id: 'today.nextDay', keys: [']'], scope: 'today', labelKey: 'shortcuts.label.today.nextDay' },
  { id: 'today.goToday', keys: ['t'], scope: 'today', labelKey: 'shortcuts.label.today.goToday' },
  {
    id: 'today.toggleCard',
    keys: ['x'],
    scope: 'today',
    labelKey: 'shortcuts.label.today.toggleCard',
  },
  {
    id: 'calendar.moveLeft',
    keys: ['ArrowLeft'],
    scope: 'calendar',
    labelKey: 'shortcuts.label.calendar.moveLeft',
  },
  {
    id: 'calendar.moveRight',
    keys: ['ArrowRight'],
    scope: 'calendar',
    labelKey: 'shortcuts.label.calendar.moveRight',
  },
  {
    id: 'calendar.moveUp',
    keys: ['ArrowUp'],
    scope: 'calendar',
    labelKey: 'shortcuts.label.calendar.moveUp',
  },
  {
    id: 'calendar.moveDown',
    keys: ['ArrowDown'],
    scope: 'calendar',
    labelKey: 'shortcuts.label.calendar.moveDown',
  },
  {
    id: 'calendar.pageUp',
    keys: ['PageUp'],
    scope: 'calendar',
    labelKey: 'shortcuts.label.calendar.pageUp',
  },
  {
    id: 'calendar.pageDown',
    keys: ['PageDown'],
    scope: 'calendar',
    labelKey: 'shortcuts.label.calendar.pageDown',
  },
  {
    id: 'calendar.select',
    keys: ['Enter'],
    scope: 'calendar',
    labelKey: 'shortcuts.label.calendar.select',
    displayOnly: true,
  },
  {
    id: 'calendar.month',
    keys: ['m'],
    scope: 'calendar',
    labelKey: 'shortcuts.label.calendar.month',
  },
  {
    id: 'calendar.week',
    keys: ['w'],
    scope: 'calendar',
    labelKey: 'shortcuts.label.calendar.week',
  },
  {
    id: 'calendar.today',
    keys: ['t'],
    scope: 'calendar',
    labelKey: 'shortcuts.label.calendar.today',
  },
  {
    id: 'calendar.prev',
    keys: ['['],
    scope: 'calendar',
    labelKey: 'shortcuts.label.calendar.prev',
  },
  {
    id: 'calendar.next',
    keys: [']'],
    scope: 'calendar',
    labelKey: 'shortcuts.label.calendar.next',
  },
]

const BY_ID = new Map(SHORTCUT_CATALOG.map((e) => [e.id, e]))

export const catalogEntry = (id: string): CatalogEntry | undefined => BY_ID.get(id)

/** Build a registrable def from the catalog; throws on an unknown id (a programming error). */
export function catalogDef(id: string, handler?: ShortcutDef['handler']): ShortcutDef {
  const entry = BY_ID.get(id)
  if (!entry) throw new Error(`Unknown shortcut id: ${id}`)
  return { ...entry, keys: [...entry.keys], handler }
}
