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

/** Completion-ring rule (DESIGN 2): positive, non-maintenance ongoing item on a fixed_rrule schedule. */
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

export interface TreeInfo {
  id: number
  projectId: number | null
}

/**
 * Orders a flat list so related tasks sit together: entries of the same project are adjacent
 * (placed where the project first appears), and every sub-task follows its nearest ancestor that
 * is also in the list, at any depth. Relative order is otherwise kept and nothing is dropped.
 * `depth` counts the hops to that ancestor chain. `parentOf` resolves any item's parent id,
 * listed or not, so a grandchild still finds its grandparent when the parent is absent.
 */
export function groupByTree<T>(
  entries: T[],
  info: (e: T) => TreeInfo,
  parentOf: (id: number) => number | null,
): Array<T & { depth: number }> {
  const ids = new Set(entries.map((e) => info(e).id))
  const anchorOf = (id: number): number | null => {
    const seen = new Set<number>([id])
    let p = parentOf(id)
    while (p !== null && !seen.has(p)) {
      if (ids.has(p)) return p
      seen.add(p)
      p = parentOf(p)
    }
    return null
  }
  const children = new Map<number, T[]>()
  const roots: T[] = []
  for (const e of entries) {
    const a = anchorOf(info(e).id)
    if (a === null) roots.push(e)
    else children.set(a, [...(children.get(a) ?? []), e])
  }
  const buckets: T[][] = []
  const slot = new Map<number, T[]>()
  for (const r of roots) {
    const pid = info(r).projectId
    if (pid === null) {
      buckets.push([r])
      continue
    }
    let b = slot.get(pid)
    if (!b) {
      b = []
      slot.set(pid, b)
      buckets.push(b)
    }
    b.push(r)
  }
  const out: Array<T & { depth: number }> = []
  const visited = new Set<number>()
  const visit = (e: T, depth: number) => {
    const id = info(e).id
    if (visited.has(id)) return
    visited.add(id)
    out.push({ ...e, depth })
    for (const c of children.get(id) ?? []) visit(c, depth + 1)
  }
  for (const b of buckets) for (const r of b) visit(r, 0)
  for (const e of entries) visit(e, 0) // safety net for parent cycles
  return out
}

/**
 * Pace of an "at least" quota inside its calendar window:
 * - met: target reached (extra counts beyond it are a bonus)
 * - relaxed: more days left than check-ins needed, today is optional
 * - must: as many days left as check-ins needed, do it today
 * - behind: fewer days left than check-ins needed, the target is out of reach
 */
export type QuotaPaceState = 'met' | 'relaxed' | 'must' | 'behind'

export interface QuotaPace {
  state: QuotaPaceState
  /** Check-ins still needed (0 when met). */
  needed: number
  /** Days left in the window, today included. */
  daysLeft: number
  /** Check-ins beyond the target. */
  extra: number
  /** Whole-number target small enough to draw as one pip per check-in. */
  pips: boolean
}

const MAX_PIPS = 7

function dayNumber(date: string): number {
  const [y, m, d] = date.split('-').map(Number)
  return Date.UTC(y, m - 1, d) / 86400000
}

/** Returns null when the pace does not apply (at-most quotas, bad dates). */
export function quotaPace(d: ItemDetail, date: string | undefined): QuotaPace | null {
  const q = d.stats?.quota
  if (!q || q.comparator !== 'at_least' || !date || !q.window_end) return null
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || !/^\d{4}-\d{2}-\d{2}$/.test(q.window_end)) return null
  const daysLeft = Math.max(0, dayNumber(q.window_end) - dayNumber(date) + 1)
  const needed = q.met ? 0 : Math.max(1, Math.ceil(q.limit - q.used - 1e-9))
  const extra = q.met ? Math.max(0, Math.floor(q.used - q.limit + 1e-9)) : 0
  let state: QuotaPaceState = 'relaxed'
  if (q.met) state = 'met'
  else if (daysLeft < needed) state = 'behind'
  else if (daysLeft === needed) state = 'must'
  return {
    state,
    needed,
    daysLeft,
    extra,
    pips: Number.isInteger(q.limit) && q.limit >= 1 && q.limit <= MAX_PIPS,
  }
}

const PACE_ORDER: Record<QuotaPaceState, number> = { must: 0, behind: 1, relaxed: 2, met: 3 }

/** Sort key for quota cards: urgent first, finished last, other quotas in between. */
export function quotaUrgency(d: ItemDetail, date: string | undefined): number {
  const p = quotaPace(d, date)
  return p ? PACE_ORDER[p.state] : 2
}

export function quotaPaceText(p: QuotaPace): string {
  switch (p.state) {
    case 'met':
      return p.extra > 0
        ? t('today.quota.pace.bonus', { extra: p.extra })
        : t('today.quota.pace.met')
    case 'must':
      return t('today.quota.pace.must', { needed: p.needed })
    case 'behind':
      return t('today.quota.pace.behind')
    default:
      return t('today.quota.pace.relaxed', { needed: p.needed, days: p.daysLeft })
  }
}
