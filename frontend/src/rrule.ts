export const WEEKDAYS = ['MO', 'TU', 'WE', 'TH', 'FR', 'SA', 'SU'] as const
export type Weekday = (typeof WEEKDAYS)[number]

export type Preset = { kind: 'days'; interval: number } | { kind: 'weekly'; days: Weekday[] }

/** Builds an RRULE string from a preset; throws an Error with a UI-ready message when invalid. */
export function buildRRule(p: Preset): string {
  if (p.kind === 'days') {
    if (!Number.isInteger(p.interval) || p.interval < 1) {
      throw new Error('Interval must be a whole number of at least 1')
    }
    return p.interval === 1 ? 'FREQ=DAILY' : `FREQ=DAILY;INTERVAL=${p.interval}`
  }
  if (p.days.length === 0) throw new Error('Select at least one weekday')
  const sorted = WEEKDAYS.filter((d) => p.days.includes(d))
  return `FREQ=WEEKLY;BYDAY=${sorted.join(',')}`
}
