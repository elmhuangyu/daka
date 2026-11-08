import type { Category, ItemDetail } from '../api'
import { t, tn, type PluralBase } from '../i18n'
import { describeRRule } from '../rrule'

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
  if (pinned.length) groups.push({ key: 'pinned', title: t('items.pinned'), sensitive: false, items: pinned })
  const rest = shown.filter((i) => !i.pinned)
  for (const c of categories) {
    const its = rest.filter((i) => i.category_id === c.id)
    if (its.length) groups.push({ key: `c${c.id}`, title: c.name, sensitive: c.is_sensitive, items: its })
  }
  const known = new Set(categories.map((c) => c.id))
  const other = rest.filter((i) => i.category_id === null || !known.has(i.category_id))
  if (other.length) groups.push({ key: 'none', title: t('items.uncategorized'), sensitive: false, items: other })
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
  if (m?.state === 'overdue') out.push({ text: t('items.badge.overdue', { days: m.overdue_days }), tone: 'bad' })
  else if (m?.state === 'warning') out.push({ text: t('items.badge.due', { days: m.days_until_due }), tone: 'warn' })
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

const SPAN: Record<string, PluralBase> = { day: 'items.span.day', month: 'items.span.month', year: 'items.span.year' }

export function describeRule(r: RuleLike): string {
  const since = r.effective_from ? ' ' + t('items.rule.since', { date: r.effective_from }) : ''
  switch (r.schedule_type) {
    case 'fixed_rrule':
      return t('items.rule.fixed', { rule: describeRRule(r.rrule_string ?? ''), since })
    case 'rolling': {
      const n = r.rolling_interval ?? 0
      const span = tn(SPAN[r.rolling_unit ?? 'day'] ?? 'items.span.day', n)
      return t('items.rule.rolling', { span, since })
    }
    case 'quota': {
      const cmp = t(r.quota_comparator === 'at_most' ? 'today.quota.atMost' : 'today.quota.atLeast')
      const p = r.quota_period
      const period = p === 'week' || p === 'month' || p === 'year' ? t(`today.quota.${p}`) : t('items.rule.period')
      return t('items.rule.quota', { cmp, count: r.quota_count ?? '?', period, since })
    }
    default:
      return t('items.rule.none', { since })
  }
}
