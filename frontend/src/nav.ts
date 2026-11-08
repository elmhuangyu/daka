export interface NavItem {
  name: string
  path: string
  label: string
  icon: string
}

export const NAV_ITEMS: NavItem[] = [
  { name: 'today', path: '/', label: 'Today', icon: 'lucide:calendar-check' },
  { name: 'calendar', path: '/calendar', label: 'Calendar', icon: 'lucide:calendar' },
  { name: 'items', path: '/items', label: 'Items', icon: 'lucide:list-checks' },
  { name: 'settings', path: '/settings', label: 'Settings', icon: 'lucide:settings' },
]
