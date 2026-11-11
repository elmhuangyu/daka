import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NAV_ITEMS } from '../nav'
import { useShortcut, type ShortcutDef } from './useShortcuts'

/** Whether the shortcut help panel is open. */
export const helpOpen = ref(false)

const NAV_SHORTCUTS: Record<string, { keys: string; label: string }> = {
  today: { keys: 'g t', label: 'Go to Today' },
  calendar: { keys: 'g c', label: 'Go to Calendar' },
  projects: { keys: 'g p', label: 'Go to Projects' },
  items: { keys: 'g i', label: 'Go to Items' },
  settings: { keys: 'g s', label: 'Go to Settings' },
}

function isVisible(el: HTMLElement): boolean {
  for (let n: HTMLElement | null = el; n; n = n.parentElement) {
    if (n.hidden) return false
    const style = getComputedStyle(n)
    if (style.display === 'none' || style.visibility === 'hidden') return false
  }
  return true
}

export function focusSearch(): void {
  const els = document.querySelectorAll<HTMLElement>('[data-shortcut-search]')
  for (const el of els) {
    if (el.matches(':disabled')) continue
    if (isVisible(el)) {
      el.focus()
      return
    }
  }
}

/** Global navigation shortcuts. They do nothing on public routes such as /login. */
export function useGlobalShortcuts(): void {
  const router = useRouter()
  const route = useRoute()
  const active = () => !route.meta.public

  const defs: ShortcutDef[] = [
    {
      id: 'help.open',
      keys: ['?'],
      scope: 'global',
      label: 'Show keyboard shortcuts',
      handler: () => {
        if (active()) helpOpen.value = true
      },
    },
    {
      id: 'search.focus',
      keys: ['/'],
      scope: 'global',
      label: 'Focus search',
      handler: () => {
        if (active()) focusSearch()
      },
    },
    {
      id: 'item.new',
      keys: ['n'],
      scope: 'global',
      label: 'New item',
      handler: () => {
        if (active()) void router.push({ name: 'item-new' })
      },
    },
  ]
  for (const nav of NAV_ITEMS) {
    const s = NAV_SHORTCUTS[nav.name]
    if (!s) continue
    defs.push({
      id: `nav.${nav.name}`,
      keys: [s.keys],
      scope: 'global',
      label: s.label,
      handler: () => {
        if (active()) void router.push({ name: nav.name })
      },
    })
  }
  useShortcut(defs)
}
