import { currentLocale } from './index'

// Date-only values are YYYY-MM-DD strings. They are formatted in UTC from a
// UTC midnight Date so the viewer's timezone can never shift the day.

const cache = new Map<string, Intl.DateTimeFormat>()

function fmt(opts: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  const loc = currentLocale()
  const key = `${loc}|${JSON.stringify(opts)}`
  let f = cache.get(key)
  if (!f) {
    try {
      f = new Intl.DateTimeFormat(loc, { timeZone: 'UTC', ...opts })
    } catch {
      f = new Intl.DateTimeFormat('en', { timeZone: 'UTC', ...opts })
    }
    cache.set(key, f)
  }
  return f
}

export type Width = 'long' | 'short' | 'narrow'

function isoToDate(iso: string): Date {
  const [y, m, d] = iso.split('-').map(Number)
  return new Date(Date.UTC(y!, m! - 1, d))
}

/** Weekday name; 0 = Sunday .. 6 = Saturday. */
export function weekdayName(weekday: number, width: Width = 'long'): string {
  // 2023-01-01 was a Sunday.
  return fmt({ weekday: width }).format(new Date(Date.UTC(2023, 0, 1 + (((weekday % 7) + 7) % 7))))
}

/** Month name; month is 1..12. */
export function monthName(month: number, width: Width = 'long'): string {
  return fmt({ month: width }).format(new Date(Date.UTC(2023, month - 1, 1)))
}

/** For example "Monday, March 3, 2025". */
export function formatLongDate(iso: string): string {
  return fmt({ weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' }).format(isoToDate(iso))
}

/** For example "March 2025". */
export function formatMonthYear(iso: string): string {
  return fmt({ year: 'numeric', month: 'long' }).format(isoToDate(iso))
}

