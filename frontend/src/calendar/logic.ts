import type { CalDayStatus, CalendarDay, ExternalEvent } from '../api'
import { t } from '../i18n'
import { formatLongDate, formatMonthYear, weekdayName } from '../i18n/format'

export type CalendarMode = 'month' | 'week'

const DAY_MS = 86_400_000

/** Date-only helpers work on YYYY-MM-DD strings via UTC so DST/timezones never shift a day. */
export function parseDate(s: string): Date {
  const [y, m, d] = s.split('-').map(Number)
  return new Date(Date.UTC(y!, m! - 1, d!))
}

export function formatDate(d: Date): string {
  return d.toISOString().slice(0, 10)
}

export function addDays(s: string, n: number): string {
  return formatDate(new Date(parseDate(s).getTime() + n * DAY_MS))
}

export function addMonths(s: string, n: number): string {
  const d = parseDate(s)
  return formatDate(new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + n, 1)))
}

/** First day of the week containing `s`. weekStart: 0 = Sunday, 1 = Monday. */
export function startOfWeek(s: string, weekStart: number): string {
  const wd = parseDate(s).getUTCDay()
  return addDays(s, -((wd - weekStart + 7) % 7))
}

export interface GridRange {
  start: string
  end: string
  dates: string[]
}

/** Dates shown by the grid: whole weeks covering the anchor's month, or the anchor's week. */
export function gridRange(mode: CalendarMode, anchor: string, weekStart: number): GridRange {
  let start: string
  let end: string
  if (mode === 'week') {
    start = startOfWeek(anchor, weekStart)
    end = addDays(start, 6)
  } else {
    const first = anchor.slice(0, 8) + '01'
    const last = addDays(addMonths(first, 1), -1)
    start = startOfWeek(first, weekStart)
    end = addDays(startOfWeek(last, weekStart), 6)
  }
  const dates: string[] = []
  for (let d = start; d <= end; d = addDays(d, 1)) dates.push(d)
  return { start, end, dates }
}

/** Anchor after paging by `n` months or weeks. */
export function shiftAnchor(mode: CalendarMode, anchor: string, n: number): string {
  return mode === 'week' ? addDays(anchor, 7 * n) : addMonths(anchor, n)
}

export function weekdayHeaders(weekStart: number): { short: string; long: string }[] {
  return Array.from({ length: 7 }, (_, i) => {
    const wd = (weekStart + i) % 7
    return { short: weekdayName(wd, 'short'), long: weekdayName(wd, 'long') }
  })
}

export function periodTitle(mode: CalendarMode, anchor: string, weekStart: number): string {
  if (mode === 'month') return formatMonthYear(anchor)
  const r = gridRange('week', anchor, weekStart)
  return t('calendar.range', { start: r.start, end: r.end })
}

export function dayNumber(date: string): number {
  return Number(date.slice(8, 10))
}

export function longDate(date: string): string {
  return formatLongDate(date)
}

/** DaisyUI/Tailwind classes of the day ribbon (DESIGN 5.2). */
export const RIBBON_CLASS: Record<CalDayStatus, string> = {
  complete: 'bg-success',
  partial: 'bg-warning',
  incomplete: 'bg-base-content/30',
  frozen: 'bg-info/60',
  pending: 'bg-primary/50',
  upcoming: 'bg-transparent border-t border-dashed border-base-content/30',
  none: 'bg-transparent',
}

export function dayStatusText(status: CalDayStatus): string {
  return t(`calendar.status.${status}`)
}

export function joinParts(parts: string[]): string {
  return parts.reduce((a, b) => t('common.join', { a, b }))
}

export function dayLabel(day: CalendarDay): string {
  const parts = [longDate(day.date)]
  if (day.day_type === 'holiday') parts.push(day.holiday_name ? t('calendar.day.holidayNamed', { name: day.holiday_name }) : t('calendar.day.holiday'))
  if (day.day_type === 'workday') parts.push(t('calendar.day.makeupWorkday'))
  parts.push(dayStatusText(day.status))
  if (day.due_count > 0) parts.push(t('calendar.day.doneOf', { done: day.done_count, due: day.due_count }))
  if (day.has_overdue) parts.push(t('calendar.day.overdueItem'))
  return joinParts(parts)
}

/** Local calendar dates (YYYY-MM-DD) an external event touches. */
export function eventDates(e: ExternalEvent): string[] {
  const first = e.start.slice(0, 10)
  let last = e.end.slice(0, 10)
  // A timed event ending exactly at midnight does not occupy that last day.
  if (!e.all_day && last > first && /T00:00:00/.test(e.end)) last = addDays(last, -1)
  if (last < first) last = first
  const out: string[] = []
  for (let d = first; d <= last && out.length < 62; d = addDays(d, 1)) out.push(d)
  return out
}

export function eventTime(e: ExternalEvent): string {
  return e.all_day ? t('calendar.allDay') : e.start.slice(11, 16)
}

/** Whether a day can take a retroactive check-in (not in the future). */
export function canMakeUp(date: string, today: string): boolean {
  return date <= today
}

export function isRealDay(date: string): boolean {
  return /^\d{4}-\d{2}-\d{2}$/.test(date) && !Number.isNaN(parseDate(date).getTime())
}
