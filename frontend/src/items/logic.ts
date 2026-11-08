import type { Category, ItemDetail } from '../api'

export type CategoryFilter = 'all' | 'none' | number

export interface PanelGroup {
  key: string
  title: string
  sensitive: boolean
  items: ItemDetail[]
}

/** Groups for the panel: pinned first, then one group per category, then uncategorized. */
export function groupItems(items: ItemDetail[], categories: Category[], filter: CategoryFilter): PanelGroup[] {
  const shown = items.filter((i) => filter === 'all' || (filter === 'none' ? i.category_id === null : i.category_id === filter))
  const groups: PanelGroup[] = []
  const pinned = shown.filter((i) => i.pinned)
  if (pinned.length) groups.push({ key: 'pinned', title: 'Pinned', sensitive: false, items: pinned })
  const rest = shown.filter((i) => !i.pinned)
  for (const c of categories) {
    const its = rest.filter((i) => i.category_id === c.id)
    if (its.length) groups.push({ key: `c${c.id}`, title: c.name, sensitive: c.is_sensitive, items: its })
  }
  const known = new Set(categories.map((c) => c.id))
  const other = rest.filter((i) => i.category_id === null || !known.has(i.category_id))
  if (other.length) groups.push({ key: 'none', title: 'Uncategorized', sensitive: false, items: other })
  return groups
}

export type BadgeTone = 'ok' | 'warn' | 'bad'
export interface Badge {
  text: string
  tone: BadgeTone
}

const fmt = (n: number) => String(Number(n.toFixed(2)))

/** Quota "used/limit" and maintenance overdue badges of a panel row. */
export function badgesOf(d: ItemDetail): Badge[] {
  const out: Badge[] = []
  const q = d.stats?.quota
  if (q) {
    const tone: BadgeTone = q.exceeded || q.traffic === 'red' ? 'bad' : q.traffic === 'yellow' ? 'warn' : 'ok'
    out.push({ text: `${fmt(q.used)}/${fmt(q.limit)}`, tone })
  }
  const m = d.stats?.maintenance
  if (m?.state === 'overdue') out.push({ text: `overdue ${m.overdue_days}d`, tone: 'bad' })
  else if (m?.state === 'warning') out.push({ text: `due in ${m.days_until_due}d`, tone: 'warn' })
  return out
}

/** Heatmap cells laid out in week columns (Sunday first); null pads the first column. */
export function heatmapColumns<T extends { date: string }>(days: T[]): Array<Array<T | null>> {
  if (!days.length) return []
  const first = new Date(`${days[0].date}T00:00:00Z`).getUTCDay()
  const cells: Array<T | null> = [...Array<null>(first).fill(null), ...days]
  const cols: Array<Array<T | null>> = []
  for (let i = 0; i < cells.length; i += 7) cols.push(cells.slice(i, i + 7))
  return cols
}

export function monthRange(ym: string): { start: string; end: string } {
  const [y, m] = ym.split('-').map(Number)
  const last = new Date(Date.UTC(y, m, 0)).getUTCDate()
  const p = (n: number) => String(n).padStart(2, '0')
  return { start: `${y}-${p(m)}-01`, end: `${y}-${p(m)}-${p(last)}` }
}

export function shiftMonth(ym: string, delta: number): string {
  const [y, m] = ym.split('-').map(Number)
  const d = new Date(Date.UTC(y, m - 1 + delta, 1))
  return `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, '0')}`
}

export interface RuleLike {
  schedule_type: string
  rrule_string?: string
  effective_from?: string
  quota_count?: number
  quota_comparator?: string
  quota_period?: string
  rolling_interval?: number
  rolling_unit?: string
}

export function describeRule(r: RuleLike): string {
  const since = r.effective_from ? ` (since ${r.effective_from})` : ''
  switch (r.schedule_type) {
    case 'fixed_rrule':
      return `Schedule rule ${r.rrule_string ?? ''}${since}`
    case 'rolling':
      return `Every ${r.rolling_interval ?? '?'} ${r.rolling_unit ?? 'days'}${since}`
    case 'quota':
      return `Quota: ${r.quota_comparator === 'at_most' ? 'at most' : 'at least'} ${r.quota_count ?? '?'} per ${r.quota_period ?? 'period'}${since}`
    default:
      return `No schedule${since}`
  }
}
