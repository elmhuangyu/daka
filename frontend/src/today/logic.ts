import { t, tn } from '../i18n'
import type { Category, CheckinLog, ItemDetail, OptionDetail, TodayItem } from '../api'

/** A card in the Today view: item definition, today's server flags and today's logs. */
export interface CardVM {
  detail: ItemDetail
  today: TodayItem | null
  logs: CheckinLog[]
}

/** Category filter value: everything, uncategorized items, or one category id. */
export type CategoryFilter = 'all' | 'none' | number

/** Trim float noise for display (0.1 + 0.2 -> 0.3). */
export function fmt(n: number): string {
  return String(Number(n.toFixed(2)))
}

export function matchesFilter(categoryId: number | null, filter: CategoryFilter): boolean {
  if (filter === 'all') return true
  if (filter === 'none') return categoryId === null
  return categoryId === filter
}

/** Completion-ring rule (DESIGN 2): positive, non-maintenance habit on a fixed_rrule schedule. */
export function countsInRing(d: ItemDetail): boolean {
  return (
    d.polarity === 'positive' &&
    d.kind !== 'event' &&
    d.tracker_type !== 'maintenance' &&
    d.current_rule.schedule_type === 'fixed_rrule'
  )
}

export function ringStats(items: Array<{ detail: ItemDetail; today: TodayItem | null }>): {
  done: number
  total: number
} {
  let done = 0
  let total = 0
  for (const { detail, today } of items) {
    if (!today?.due || !countsInRing(detail)) continue
    total++
    if (today.done) done++
  }
  return { done, total }
}

export function percent(done: number, total: number): number {
  return total > 0 ? Math.round((done / total) * 100) : 0
}

/** One value line of a counter / metric card (one per option, or a single one). */
export interface ValueRow {
  key: string
  optionId: number | null
  label: string
  unit: string
  step: number
  target: number | null
  /** Counter: today's total. Metric: aggregated value, null when nothing recorded. */
  current: number | null
  min: number | null
  max: number | null
}

function aggregate(values: number[], how: string): number | null {
  if (values.length === 0) return null
  if (how === 'sum') return values.reduce((a, b) => a + b, 0)
  if (how === 'avg') return values.reduce((a, b) => a + b, 0) / values.length
  return values[values.length - 1] // latest (logs are ordered by id)
}

function rowValue(
  d: ItemDetail,
  logs: CheckinLog[],
  optionId: number | null,
  how: string,
): number | null {
  const vals = logs.filter((l) => (l.option_id ?? null) === optionId).map((l) => l.value)
  if (d.tracker_type === 'counter') return vals.reduce((a, b) => a + b, 0)
  return aggregate(vals, how)
}

export function valueRows(d: ItemDetail, logs: CheckinLog[]): ValueRow[] {
  if (d.tracker_type !== 'counter' && d.tracker_type !== 'metric') return []
  const sorted = (logs: CheckinLog[]) => [...logs].sort((a, b) => a.id - b.id)
  const ls = sorted(logs)
  const opts: OptionDetail[] = [...d.options].sort(
    (a, b) => a.sort_order - b.sort_order || a.id - b.id,
  )
  if (opts.length === 0) {
    return [
      {
        key: 'item',
        optionId: null,
        label: '',
        unit: d.unit ?? '',
        step: d.step || 1,
        target: d.current_rule.target_value ?? null,
        current: rowValue(d, ls, null, d.aggregate || 'latest'),
        min: d.min_val ?? null,
        max: d.max_val ?? null,
      },
    ]
  }
  return opts.map((o) => ({
    key: `opt-${o.id}`,
    optionId: o.id,
    label: o.name,
    unit: o.unit,
    step: o.step || 1,
    target: d.current_rule.option_targets?.[String(o.id)] ?? o.target_value ?? null,
    current: rowValue(d, ls, o.id, o.aggregate || 'latest'),
    min: o.min_val,
    max: o.max_val,
  }))
}

/** Ad-hoc entries: logs without an option on an item that has options. */
export function adhocLogs(d: ItemDetail, logs: CheckinLog[]): CheckinLog[] {
  return d.options.length > 0 ? logs.filter((l) => l.option_id == null) : []
}

/** Rolling item whose maintenance state calls for attention. */
export function needsAttention(d: ItemDetail): boolean {
  const m = d.stats?.maintenance
  return (
    d.current_rule.schedule_type === 'rolling' &&
    !!m &&
    (m.state === 'overdue' || m.state === 'warning')
  )
}

const days = (n: number) => tn('common.days', n)

export function maintenanceText(d: ItemDetail): string {
  const m = d.stats?.maintenance
  if (!m) return ''
  const last = d.stats?.days_since_last
  const lastText =
    last == null
      ? t('today.maint.never')
      : last === 0
        ? t('today.maint.lastToday')
        : t('today.maint.lastAgo', { days: days(last) })
  const state =
    m.state === 'overdue'
      ? t('today.maint.overdue', { days: days(m.overdue_days) })
      : m.days_until_due === 0
        ? t('today.maint.dueToday')
        : t('today.maint.dueIn', { days: days(m.days_until_due) })
  return t('today.maint.line', { last: lastText, state })
}

export function quotaText(d: ItemDetail): string {
  const q = d.stats?.quota
  if (!q) return ''
  const cmp = q.comparator === 'at_most' ? t('today.quota.atMost') : t('today.quota.atLeast')
  const period = q.period || q.window
  const periodText =
    period === 'week' || period === 'month' || period === 'year'
      ? t(`today.quota.${period}`)
      : period
  return t('today.quota.text', { used: fmt(q.used), limit: fmt(q.limit), cmp, period: periodText })
}

export function findCategory(cats: Category[], id: number | null): Category | null {
  return id == null ? null : (cats.find((c) => c.id === id) ?? null)
}
