import type { MessageKey } from './i18n'

export interface NavItem {
  name: string
  path: string
  labelKey: MessageKey
  icon: string
}

export const NAV_ITEMS: NavItem[] = [
  { name: 'today', path: '/', labelKey: 'nav.today', icon: 'lucide:calendar-check' },
  { name: 'calendar', path: '/calendar', labelKey: 'nav.calendar', icon: 'lucide:calendar' },
  { name: 'items', path: '/items', labelKey: 'nav.items', icon: 'lucide:list-checks' },
  { name: 'settings', path: '/settings', labelKey: 'nav.settings', icon: 'lucide:settings' },
]
