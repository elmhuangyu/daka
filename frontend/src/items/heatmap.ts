// Pure helpers for the Heatmap component: date grid, level buckets, labels.

export interface HeatmapCell {
  date: string
  level: number
  value: number
  options_met?: number
  options_total?: number
}

export type GridCell = HeatmapCell | null

const DAY_MS = 86_400_000
const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

const toMs = (date: string) => Date.parse(`${date}T00:00:00Z`)
const toDate = (ms: number) => new Date(ms).toISOString().slice(0, 10)

/** Weekday names in row order for the given week start (0 = Sunday, 1 = Monday). */
export function weekdayNames(weekStart: number): string[] {
  const ws = weekStart === 0 ? 0 : 1
  return Array.from({ length: 7 }, (_, i) => WEEKDAYS[(ws + i) % 7])
}

/**
 * Lay days out in week columns (rows run from the week start). Days missing
 * between the first and last date are filled with empty cells; the first column
 * is padded with null. Input is sorted and de-duplicated by date.
 */
export function buildGrid(days: HeatmapCell[], weekStart: number): GridCell[][] {
  if (!days.length) return []
  const ws = weekStart === 0 ? 0 : 1
  const byDate = new Map(days.map((d) => [d.date, d]))
  const dates = [...byDate.keys()].sort()
  const first = toMs(dates[0])
  const last = toMs(dates[dates.length - 1])
  const lead = (new Date(first).getUTCDay() - ws + 7) % 7
  const cells: GridCell[] = Array<null>(lead).fill(null)
  for (let t = first; t <= last; t += DAY_MS) {
    const date = toDate(t)
    cells.push(byDate.get(date) ?? { date, level: 0, value: 0 })
  }
  const cols: GridCell[][] = []
  for (let i = 0; i < cells.length; i += 7) cols.push(cells.slice(i, i + 7))
  return cols
}

/** Intensity bucket 0..4: 0 = nothing done; any progress is at least 1; full is 4. */
export function levelBucket(level: number): 0 | 1 | 2 | 3 | 4 {
  if (!(level > 0)) return 0
  if (level >= 1) return 4
  return Math.max(1, Math.min(3, Math.ceil(level * 4))) as 1 | 2 | 3
}

/** Month label per column: set on the first column of a new month, at least 3 columns apart. */
export function monthLabels(cols: GridCell[][]): string[] {
  let prev = -1
  let lastCol = -10
  return cols.map((col, ci) => {
    const cell = col.find((c) => c !== null)
    if (!cell) return ''
    const m = Number(cell.date.slice(5, 7)) - 1
    if (m === prev) return ''
    prev = m
    if (ci - lastCol < 3) return ''
    lastCol = ci
    return MONTHS[m]
  })
}

export interface LabelOpts {
  negative?: boolean
  redact?: boolean
}

/** Text for tooltip and aria-label. Never contains option or item names; counts are hidden when redacted. */
export function cellLabel(cell: HeatmapCell, opts: LabelOpts = {}): string {
  const d = cell.date
  const bucket = levelBucket(cell.level)
  if (opts.negative) {
    if (!bucket) return `${d}: none`
    if (opts.redact) return `${d}: occurred`
    return `${d}: occurred${cell.value > 1 ? ` ${cell.value} times` : ''}`
  }
  const total = cell.options_total ?? 0
  const met = cell.options_met ?? 0
  if (!bucket && !(cell.value > 0) && !total) return `${d}: no check-in`
  let status = bucket === 4 ? 'completed' : bucket ? 'partly completed' : 'not completed'
  if (opts.redact) return `${d}: ${status}`
  if (total > 1) status += `, ${met} of ${total} options`
  return `${d}: ${status}`
}
