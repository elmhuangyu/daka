import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NAV_ITEMS } from '../nav'
import { catalogDef, catalogEntry } from './shortcutCatalog'
import { useShortcut, type ShortcutDef } from './useShortcuts'

/** Whether the shortcut help panel is open. */
export const helpOpen = ref(false)

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
    catalogDef('help.open', () => {
      if (active()) helpOpen.value = true
    }),
    catalogDef('search.focus', () => {
      if (active()) focusSearch()
    }),
    catalogDef('item.new', () => {
      if (active()) void router.push({ name: 'item-new' })
    }),
  ]
  defs.push(
    catalogDef('nav.shortcuts', () => {
      if (active()) void router.push({ name: 'settings', query: { tab: 'shortcuts' } })
    }),
  )
  for (const nav of NAV_ITEMS) {
    const id = `nav.${nav.name}`
    if (!catalogEntry(id)) continue
    defs.push(
      catalogDef(id, () => {
        if (active()) void router.push({ name: nav.name })
      }),
    )
  }
  useShortcut(defs)
}
