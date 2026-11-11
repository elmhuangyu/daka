import type { ShortcutDef } from './useShortcuts'

/**
 * Every shortcut the app can register. Views take id, scope, label and default keys
 * from here (via `catalogDef`) so the settings page can list actions whose view is not
 * mounted. Static English text only (no user data).
 */
export type CatalogEntry = Omit<ShortcutDef, 'handler'>

export const PRIVACY_TOGGLE_ID = 'privacy.toggle'

export const SHORTCUT_CATALOG: CatalogEntry[] = [
  { id: 'help.open', keys: ['?'], scope: 'global', label: 'Show keyboard shortcuts' },
  { id: 'search.focus', keys: ['/'], scope: 'global', label: 'Focus search' },
  { id: 'item.new', keys: ['n'], scope: 'global', label: 'New item' },
  { id: 'nav.today', keys: ['g t'], scope: 'global', label: 'Go to Today' },
  { id: 'nav.calendar', keys: ['g c'], scope: 'global', label: 'Go to Calendar' },
  { id: 'nav.projects', keys: ['g p'], scope: 'global', label: 'Go to Projects' },
  { id: 'nav.items', keys: ['g i'], scope: 'global', label: 'Go to Items' },
  { id: 'nav.settings', keys: ['g s'], scope: 'global', label: 'Go to Settings' },
  {
    id: PRIVACY_TOGGLE_ID,
    keys: ['ctrl+shift+h'],
    scope: 'global',
    label: 'Toggle privacy mask',
    allowInInput: true,
  },
  { id: 'today.prevDay', keys: ['['], scope: 'today', label: 'Previous day' },
  { id: 'today.nextDay', keys: [']'], scope: 'today', label: 'Next day' },
  { id: 'today.goToday', keys: ['t'], scope: 'today', label: 'Jump to today' },
  {
    id: 'today.toggleCard',
    keys: ['x'],
    scope: 'today',
    label: 'Check or uncheck the focused item',
  },
  {
    id: 'calendar.moveLeft',
    keys: ['ArrowLeft'],
    scope: 'calendar',
    label: 'Move focus back (one day)',
  },
  {
    id: 'calendar.moveRight',
    keys: ['ArrowRight'],
    scope: 'calendar',
    label: 'Move focus forward (one day)',
  },
  {
    id: 'calendar.moveUp',
    keys: ['ArrowUp'],
    scope: 'calendar',
    label: 'Move focus back (one week)',
  },
  {
    id: 'calendar.moveDown',
    keys: ['ArrowDown'],
    scope: 'calendar',
    label: 'Move focus forward (one week)',
  },
  { id: 'calendar.pageUp', keys: ['PageUp'], scope: 'calendar', label: 'Previous period' },
  { id: 'calendar.pageDown', keys: ['PageDown'], scope: 'calendar', label: 'Next period' },
  {
    id: 'calendar.select',
    keys: ['Enter'],
    scope: 'calendar',
    label: 'Select the focused day',
    displayOnly: true,
  },
  { id: 'calendar.month', keys: ['m'], scope: 'calendar', label: 'Month view' },
  { id: 'calendar.week', keys: ['w'], scope: 'calendar', label: 'Week view' },
  { id: 'calendar.today', keys: ['t'], scope: 'calendar', label: 'Jump to today' },
  { id: 'calendar.prev', keys: ['['], scope: 'calendar', label: 'Previous period' },
  { id: 'calendar.next', keys: [']'], scope: 'calendar', label: 'Next period' },
]

const BY_ID = new Map(SHORTCUT_CATALOG.map((e) => [e.id, e]))

export const catalogEntry = (id: string): CatalogEntry | undefined => BY_ID.get(id)

/** Build a registrable def from the catalog; throws on an unknown id (a programming error). */
export function catalogDef(id: string, handler?: ShortcutDef['handler']): ShortcutDef {
  const entry = BY_ID.get(id)
  if (!entry) throw new Error(`Unknown shortcut id: ${id}`)
  return { ...entry, keys: [...entry.keys], handler }
}
