import { currentLocale, t, tn, type PluralBase } from './i18n'
import { monthName, weekdayName } from './i18n/format'

export const WEEKDAYS = ['MO', 'TU', 'WE', 'TH', 'FR', 'SA', 'SU'] as const
export type Weekday = (typeof WEEKDAYS)[number]

export type Preset =
  | { kind: 'days'; interval: number }
  | { kind: 'weekly'; days: Weekday[]; interval?: number }
  /** day is 1..31, or -1 for the last day of the month. */
  | { kind: 'monthly'; day: number; interval?: number }
  | { kind: 'yearly'; month: number; day: number }
  | { kind: 'custom'; rrule: string }

export const MAX_RRULE_LEN = 500
const MAX_INTERVAL = 1000

function checkInterval(n: number): void {
  if (!Number.isInteger(n) || n < 1) throw new Error(t('rrule.err.interval'))
  if (n > MAX_INTERVAL) throw new Error(t('rrule.err.intervalMax', { max: MAX_INTERVAL }))
}

const DAYS_IN_MONTH = [31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
// 0 = Sunday .. 6 = Saturday, the numbering used by the format helpers.
const WEEKDAY_INDEX: Record<Weekday, number> = { SU: 0, MO: 1, TU: 2, WE: 3, TH: 4, FR: 5, SA: 6 }

/** Builds an RRULE string from a preset; throws an Error with a UI-ready message when invalid. */
export function buildRRule(p: Preset): string {
  switch (p.kind) {
    case 'days':
      checkInterval(p.interval)
      return p.interval === 1 ? 'FREQ=DAILY' : `FREQ=DAILY;INTERVAL=${p.interval}`
    case 'weekly': {
      if (p.days.length === 0) throw new Error(t('rrule.err.weekday'))
      const sorted = WEEKDAYS.filter((d) => p.days.includes(d))
      const n = p.interval ?? 1
      checkInterval(n)
      return `FREQ=WEEKLY${n > 1 ? `;INTERVAL=${n}` : ''};BYDAY=${sorted.join(',')}`
    }
    case 'monthly': {
      if (!Number.isInteger(p.day) || !((p.day >= 1 && p.day <= 31) || p.day === -1)) {
        throw new Error(t('rrule.err.monthDay'))
      }
      const n = p.interval ?? 1
      checkInterval(n)
      return `FREQ=MONTHLY${n > 1 ? `;INTERVAL=${n}` : ''};BYMONTHDAY=${p.day}`
    }
    case 'yearly': {
      if (!Number.isInteger(p.month) || p.month < 1 || p.month > 12) throw new Error(t('rrule.err.month'))
      if (!Number.isInteger(p.day) || p.day < 1 || p.day > DAYS_IN_MONTH[p.month - 1]) {
        throw new Error(t('rrule.err.dayInMonth'))
      }
      return `FREQ=YEARLY;BYMONTH=${p.month};BYMONTHDAY=${p.day}`
    }
    case 'custom':
      return normalizeCustom(p.rrule)
  }
}

// Parts and frequencies the backend accepts (backend/lib/schedule/rrule.go).
const ALLOWED_PARTS = new Set([
  'FREQ', 'INTERVAL', 'COUNT', 'UNTIL', 'WKST', 'BYDAY', 'BYMONTHDAY', 'BYMONTH', 'BYYEARDAY', 'BYWEEKNO', 'BYSETPOS',
])
const FREQS = ['DAILY', 'WEEKLY', 'MONTHLY', 'YEARLY'] as const
type Freq = (typeof FREQS)[number]

function splitParts(s: string): Map<string, string> {
  const parts = new Map<string, string>()
  for (const raw of s.split(';')) {
    const i = raw.indexOf('=')
    const k = (i < 0 ? raw : raw.slice(0, i)).trim().toUpperCase()
    if (i < 0 || !ALLOWED_PARTS.has(k)) throw new Error(t('rrule.err.unsupportedPart', { part: k }))
    if (parts.has(k)) throw new Error(t('rrule.err.duplicatePart', { part: k }))
    parts.set(k, raw.slice(i + 1).trim())
  }
  return parts
}

/** Trims, drops an "RRULE:" prefix and mirrors the server's syntax checks; returns the body. */
export function normalizeCustom(input: string): string {
  let s = input.trim()
  if (/^rrule:/i.test(s)) s = s.slice(6).trim()
  if (s === '') throw new Error(t('rrule.err.empty'))
  if (s.length > MAX_RRULE_LEN) throw new Error(t('rrule.err.tooLong', { max: MAX_RRULE_LEN }))
  const parts = splitParts(s)
  const freq = parts.get('FREQ')?.toUpperCase()
  if (!freq) throw new Error(t('rrule.err.freq'))
  if (!(FREQS as readonly string[]).includes(freq)) throw new Error(t('rrule.err.freqValue'))
  const iv = parts.get('INTERVAL')
  if (iv !== undefined) checkInterval(Number(iv))
  const count = parts.get('COUNT')
  if (count !== undefined && !(Number.isInteger(Number(count)) && Number(count) >= 1)) {
    throw new Error(t('rrule.err.count'))
  }
  const until = parts.get('UNTIL')
  if (until !== undefined && !/^\d{8}/.test(until)) throw new Error(t('rrule.err.until'))
  return s
}

export interface ParsedRule {
  freq: Freq
  interval: number
  byday: Weekday[]
  bymonthday: number[]
  bymonth: number[]
  count?: number
  /** Inclusive, YYYY-MM-DD. */
  until?: string
}

/**
 * Parses the subset of RRULE the client can expand. Returns null for rules it
 * cannot evaluate (ordinal BYDAY, BYSETPOS, BYYEARDAY, BYWEEKNO, WKST other
 * than MO); the server still accepts those.
 */
export function parseRRule(s: string): ParsedRule | null {
  let parts: Map<string, string>
  try {
    parts = splitParts(normalizeCustom(s))
  } catch {
    return null
  }
  if (['BYYEARDAY', 'BYWEEKNO', 'BYSETPOS'].some((k) => parts.has(k))) return null
  const wkst = parts.get('WKST')
  if (wkst && wkst.toUpperCase() !== 'MO') return null
  const nums = (v: string | undefined) => (v ? v.split(',').map((x) => Number(x.trim())) : [])
  const byday: Weekday[] = []
  for (const d of (parts.get('BYDAY') ?? '').split(',').filter(Boolean)) {
    const u = d.trim().toUpperCase() as Weekday
    if (!WEEKDAYS.includes(u)) return null
    byday.push(u)
  }
  const bymonthday = nums(parts.get('BYMONTHDAY'))
  const bymonth = nums(parts.get('BYMONTH'))
  if ([...bymonthday, ...bymonth].some((n) => !Number.isInteger(n) || n === 0)) return null
  const until = parts.get('UNTIL')
  return {
    freq: parts.get('FREQ')!.toUpperCase() as Freq,
    interval: parts.has('INTERVAL') ? Number(parts.get('INTERVAL')) : 1,
    byday,
    bymonthday,
    bymonth,
    count: parts.has('COUNT') ? Number(parts.get('COUNT')) : undefined,
    until: until ? `${until.slice(0, 4)}-${until.slice(4, 6)}-${until.slice(6, 8)}` : undefined,
  }
}

// ---- date helpers (UTC day numbers, so no DST or timezone effects) ----

const DAY_MS = 86_400_000

export function isoToDay(iso: string): number {
  const [y, m, d] = iso.split('-').map(Number)
  return Math.floor(Date.UTC(y, m - 1, d) / DAY_MS)
}

export function dayToIso(day: number): string {
  return new Date(day * DAY_MS).toISOString().slice(0, 10)
}

/** Local calendar date of a JS Date as YYYY-MM-DD. */
export function localIso(d: Date = new Date()): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

function dim(y: number, m: number): number {
  return new Date(Date.UTC(y, m, 0)).getUTCDate() // m is 1-based
}

/** 0 = Monday .. 6 = Sunday for a UTC day number (1970-01-01 was a Thursday). */
function weekdayOf(day: number): number {
  return (((day + 3) % 7) + 7) % 7
}

/**
 * Next occurrences (YYYY-MM-DD) of rule on or after `from`, anchored at
 * `dtstart` (which sets the INTERVAL phase). Holiday policy is not applied.
 * Returns null when the rule cannot be evaluated client-side.
 */
export function nextOccurrences(rrule: string, dtstart: string, from: string, limit = 5): string[] | null {
  const r = parseRRule(rrule)
  if (!r) return null
  const start = isoToDay(dtstart)
  const lo = Math.max(start, isoToDay(from))
  const [sy, sm, sd] = dtstart.split('-').map(Number)
  const startWd = weekdayOf(start)
  const hasDay = r.bymonthday.length > 0 || r.byday.length > 0
  const untilDay = r.until ? isoToDay(r.until) : Infinity
  const out: string[] = []
  let seen = 0
  for (let day = start; day <= lo + 366 * 6 && out.length < limit && day <= untilDay; day++) {
    const dt = new Date(day * DAY_MS)
    const y = dt.getUTCFullYear()
    const m = dt.getUTCMonth() + 1
    const d = dt.getUTCDate()
    const wd = weekdayOf(day)
    let period = 0
    if (r.freq === 'DAILY') period = day - start
    else if (r.freq === 'WEEKLY') period = Math.floor((day - wd - (start - startWd)) / 7)
    else if (r.freq === 'MONTHLY') period = (y - sy) * 12 + (m - sm)
    else period = y - sy
    if (period % r.interval !== 0) continue
    if (r.bymonth.length > 0 && !r.bymonth.includes(m)) continue
    if (r.freq === 'YEARLY' && r.bymonth.length === 0 && !hasDay && m !== sm) continue
    if (r.bymonthday.length > 0) {
      const last = dim(y, m)
      if (!r.bymonthday.some((x) => (x > 0 ? x === d : last + x + 1 === d))) continue
    }
    if (r.byday.length > 0 && !r.byday.includes(WEEKDAYS[wd])) continue
    if (!hasDay) {
      if (r.freq === 'WEEKLY' && wd !== startWd) continue
      if ((r.freq === 'MONTHLY' || r.freq === 'YEARLY') && d !== sd) continue
    }
    seen++
    if (r.count !== undefined && seen > r.count) break
    if (day >= lo) out.push(dayToIso(day))
  }
  return out
}

// ---- human-readable description ----

/** "the 1st", "the 22nd" in English (ordinal plural rules); a plain day number elsewhere. */
function nth(n: number): string {
  let cat = 'other'
  try {
    cat = new Intl.PluralRules(currentLocale(), { type: 'ordinal' }).select(n)
  } catch {
    // Unknown locale tag: use the generic form.
  }
  const key = cat === 'one' ? 'rrule.nth.one' : cat === 'two' ? 'rrule.nth.two' : cat === 'few' ? 'rrule.nth.few' : 'rrule.nth.other'
  return t(key, { n })
}

const list = (xs: string[]) => (xs.length ? xs.reduce((a, b) => t('rrule.list', { a, b })) : '')

const EVERY: Record<Freq, PluralBase> = {
  DAILY: 'rrule.every.day',
  WEEKLY: 'rrule.every.week',
  MONTHLY: 'rrule.every.month',
  YEARLY: 'rrule.every.year',
}

/**
 * One sentence for an RRULE, built from per-locale fragment templates (the
 * RRULE text itself stays language-independent); a generic label when it
 * cannot be parsed.
 */
export function describeRRule(rrule: string): string {
  const r = parseRRule(rrule)
  if (!r) return t('rrule.custom')
  const days = list(r.byday.map((d) => weekdayName(WEEKDAY_INDEX[d])))
  const md = list(r.bymonthday.map((x) => (x === -1 ? t('rrule.lastDay') : nth(x))))
  let s = tn(EVERY[r.freq], r.interval)
  if (r.freq === 'WEEKLY') {
    if (r.byday.length) s = t('rrule.on', { head: s, what: days })
  } else if (r.freq === 'MONTHLY') {
    if (r.bymonthday.length) s = t('rrule.on', { head: s, what: md })
    if (r.byday.length) s = t('rrule.on', { head: s, what: days })
  } else if (r.freq === 'YEARLY') {
    if (r.bymonth.length) s = t('rrule.in', { head: s, what: list(r.bymonth.map((x) => monthName(x))) })
    if (r.bymonthday.length) s = t('rrule.on', { head: s, what: md })
  }
  if (r.count !== undefined) s = tn('rrule.count', r.count, { head: s })
  if (r.until) s = t('rrule.until', { head: s, date: r.until })
  return s.charAt(0).toUpperCase() + s.slice(1)
}

/** Reverse of buildRRule for the shapes the builder produces; null for anything else. */
export function presetFromRRule(rrule: string): Preset | null {
  const r = parseRRule(rrule)
  if (!r || r.count !== undefined || r.until !== undefined) return null
  const keys = [...splitParts(normalizeCustom(rrule)).keys()].filter((k) => k !== 'FREQ' && k !== 'INTERVAL')
  const only = (...allowed: string[]) => keys.every((k) => allowed.includes(k))
  if (r.freq === 'DAILY' && keys.length === 0) return { kind: 'days', interval: r.interval }
  if (r.freq === 'WEEKLY' && only('BYDAY') && r.byday.length > 0) return { kind: 'weekly', days: r.byday, interval: r.interval }
  if (r.freq === 'MONTHLY' && only('BYMONTHDAY') && r.bymonthday.length === 1) {
    return { kind: 'monthly', day: r.bymonthday[0], interval: r.interval }
  }
  if (r.freq === 'YEARLY' && r.interval === 1 && only('BYMONTH', 'BYMONTHDAY') && r.bymonth.length === 1 && r.bymonthday.length === 1) {
    return { kind: 'yearly', month: r.bymonth[0], day: r.bymonthday[0] }
  }
  return null
}
