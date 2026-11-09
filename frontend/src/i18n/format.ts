import { ref } from 'vue'
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
  return fmt({ weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' }).format(
    isoToDate(iso),
  )
}

/** For example "March 2025". */
export function formatMonthYear(iso: string): string {
  return fmt({ year: 'numeric', month: 'long' }).format(isoToDate(iso))
}

export type DateFormat = 'YYYY-MM-DD' | 'YYYY/MM/DD' | 'MM/DD/YYYY' | 'DD/MM/YYYY'

export const DATE_FORMAT_OPTIONS: { value: DateFormat; label: string; sample: string }[] = [
  { value: 'YYYY-MM-DD', label: 'YYYY-MM-DD', sample: '2026-11-09' },
  { value: 'YYYY/MM/DD', label: 'YYYY/MM/DD', sample: '2026/11/09' },
  { value: 'MM/DD/YYYY', label: 'MM/DD/YYYY', sample: '11/09/2026' },
  { value: 'DD/MM/YYYY', label: 'DD/MM/YYYY', sample: '09/11/2026' },
]

function readSavedDateFormat(): DateFormat {
  try {
    if (typeof localStorage !== 'undefined') {
      const saved = localStorage.getItem('daka:date_format')
      if (saved === 'YYYY/MM/DD' || saved === 'MM/DD/YYYY' || saved === 'DD/MM/YYYY') {
        return saved
      }
    }
  } catch {
    // storage unavailable
  }
  return 'YYYY-MM-DD'
}

export const activeDateFormat = ref<DateFormat>(readSavedDateFormat())

export function currentDateFormat(): DateFormat {
  return activeDateFormat.value
}

export function setDateFormat(df: DateFormat) {
  activeDateFormat.value = df
  try {
    if (typeof localStorage !== 'undefined') {
      localStorage.setItem('daka:date_format', df)
    }
  } catch {
    // storage unavailable
  }
}

export function dateFormatPlaceholder(format?: DateFormat): string {
  const f = format || currentDateFormat()
  switch (f) {
    case 'YYYY/MM/DD':
      return 'YYYY/MM/DD'
    case 'MM/DD/YYYY':
      return 'MM/DD/YYYY'
    case 'DD/MM/YYYY':
      return 'DD/MM/YYYY'
    case 'YYYY-MM-DD':
    default:
      return 'YYYY-MM-DD'
  }
}

function isValidYMD(y: number, m: number, d: number): boolean {
  if (m < 1 || m > 12 || d < 1 || d > 31 || y < 1000 || y > 9999) return false
  const dt = new Date(Date.UTC(y, m - 1, d))
  return dt.getUTCFullYear() === y && dt.getUTCMonth() === m - 1 && dt.getUTCDate() === d
}

function toIsoString(y: number, m: number, d: number): string {
  return `${String(y).padStart(4, '0')}-${String(m).padStart(2, '0')}-${String(d).padStart(2, '0')}`
}

export function parseCustomDate(input: string, format?: DateFormat): string | null {
  const trimmed = input.trim()
  if (!trimmed) return null

  // 1. Try standard ISO format YYYY-MM-DD or YYYY/MM/DD (year first)
  const yFirst = /^(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})$/.exec(trimmed)
  if (yFirst) {
    const y = Number(yFirst[1])
    const m = Number(yFirst[2])
    const d = Number(yFirst[3])
    if (isValidYMD(y, m, d)) return toIsoString(y, m, d)
    return null
  }

  // 2. Year at end, e.g. MM/DD/YYYY or DD/MM/YYYY
  const yEnd = /^(\d{1,2})[-/.](\d{1,2})[-/.](\d{4})$/.exec(trimmed)
  if (yEnd) {
    const p1 = Number(yEnd[1])
    const p2 = Number(yEnd[2])
    const y = Number(yEnd[3])
    const f = format || currentDateFormat()

    let m = f === 'DD/MM/YYYY' ? p2 : p1
    let d = f === 'DD/MM/YYYY' ? p1 : p2

    // Disambiguation if one of the parts is > 12
    if (p1 > 12 && p2 <= 12) {
      d = p1
      m = p2
    } else if (p2 > 12 && p1 <= 12) {
      m = p1
      d = p2
    }

    if (isValidYMD(y, m, d)) return toIsoString(y, m, d)
    return null
  }

  return null
}

export function formatCustomDate(iso: string, format?: DateFormat): string {
  if (!iso || !iso.includes('-')) return iso
  const parts = iso.split('-')
  if (parts.length !== 3) return iso
  const [y, m, d] = parts
  const f = format || currentDateFormat()
  switch (f) {
    case 'YYYY/MM/DD':
      return `${y}/${m}/${d}`
    case 'MM/DD/YYYY':
      return `${m}/${d}/${y}`
    case 'DD/MM/YYYY':
      return `${d}/${m}/${y}`
    case 'YYYY-MM-DD':
    default:
      return `${y}-${m}-${d}`
  }
}
