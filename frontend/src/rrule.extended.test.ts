import { describe, expect, it } from 'vitest'
import { buildRRule, describeRRule, nextOccurrences, presetFromRRule, type Preset } from './rrule'

describe('buildRRule extended presets', () => {
  it('builds monthly, yearly and interval weekly rules', () => {
    expect(buildRRule({ kind: 'monthly', day: 15 })).toBe('FREQ=MONTHLY;BYMONTHDAY=15')
    expect(buildRRule({ kind: 'monthly', day: -1, interval: 2 })).toBe(
      'FREQ=MONTHLY;INTERVAL=2;BYMONTHDAY=-1',
    )
    expect(buildRRule({ kind: 'yearly', month: 3, day: 8 })).toBe(
      'FREQ=YEARLY;BYMONTH=3;BYMONTHDAY=8',
    )
    expect(buildRRule({ kind: 'weekly', days: ['FR'], interval: 2 })).toBe(
      'FREQ=WEEKLY;INTERVAL=2;BYDAY=FR',
    )
  })
  it('rejects bad monthly/yearly/custom input', () => {
    expect(() => buildRRule({ kind: 'monthly', day: 0 })).toThrow('Day of month')
    expect(() => buildRRule({ kind: 'yearly', month: 2, day: 30 })).toThrow('not valid')
    expect(() => buildRRule({ kind: 'custom', rrule: 'BYHOUR=3;FREQ=DAILY' })).toThrow(
      'Unsupported',
    )
    expect(() => buildRRule({ kind: 'custom', rrule: 'INTERVAL=2' })).toThrow('requires FREQ')
    expect(() => buildRRule({ kind: 'custom', rrule: 'FREQ=HOURLY' })).toThrow('FREQ must be')
  })
  it('normalizes a custom rule', () => {
    expect(buildRRule({ kind: 'custom', rrule: ' RRULE:FREQ=DAILY;COUNT=3 ' })).toBe(
      'FREQ=DAILY;COUNT=3',
    )
  })
})

describe('nextOccurrences', () => {
  it('daily interval keeps phase from dtstart', () => {
    expect(nextOccurrences('FREQ=DAILY;INTERVAL=3', '2026-11-01', '2026-11-08', 3)).toEqual([
      '2026-11-10',
      '2026-11-13',
      '2026-11-16',
    ])
  })
  it('weekly weekdays', () => {
    // 2026-11-08 is a Sunday
    expect(nextOccurrences('FREQ=WEEKLY;BYDAY=MO,WE', '2026-11-08', '2026-11-08', 3)).toEqual([
      '2026-11-09',
      '2026-11-11',
      '2026-11-16',
    ])
  })
  it('biweekly respects week phase', () => {
    expect(
      nextOccurrences('FREQ=WEEKLY;INTERVAL=2;BYDAY=MO', '2026-11-02', '2026-11-03', 2),
    ).toEqual(['2026-11-16', '2026-11-30'])
  })
  it('monthly skips months without the day and supports last day', () => {
    expect(nextOccurrences('FREQ=MONTHLY;BYMONTHDAY=31', '2026-01-31', '2026-01-31', 4)).toEqual([
      '2026-01-31',
      '2026-03-31',
      '2026-05-31',
      '2026-07-31',
    ])
    expect(nextOccurrences('FREQ=MONTHLY;BYMONTHDAY=-1', '2026-01-01', '2026-02-01', 2)).toEqual([
      '2026-02-28',
      '2026-03-31',
    ])
  })
  it('yearly and COUNT/UNTIL', () => {
    expect(
      nextOccurrences('FREQ=YEARLY;BYMONTH=3;BYMONTHDAY=8', '2026-11-08', '2026-11-08', 2),
    ).toEqual(['2027-03-08', '2028-03-08'])
    expect(nextOccurrences('FREQ=DAILY;COUNT=2', '2026-11-08', '2026-11-08', 5)).toEqual([
      '2026-11-08',
      '2026-11-09',
    ])
    expect(nextOccurrences('FREQ=DAILY;UNTIL=20261109', '2026-11-08', '2026-11-08', 5)).toEqual([
      '2026-11-08',
      '2026-11-09',
    ])
  })
  it('returns null for rules it cannot evaluate', () => {
    expect(
      nextOccurrences('FREQ=MONTHLY;BYDAY=MO;BYSETPOS=1', '2026-11-08', '2026-11-08'),
    ).toBeNull()
  })
})

describe('describeRRule / presetFromRRule', () => {
  it('describes builder shapes', () => {
    expect(describeRRule('FREQ=DAILY')).toBe('Every day')
    expect(describeRRule('FREQ=WEEKLY;BYDAY=MO,FR')).toBe('Every week on Monday, Friday')
    expect(describeRRule('FREQ=MONTHLY;BYMONTHDAY=22')).toBe('Every month on the 22nd')
    expect(describeRRule('FREQ=YEARLY;BYMONTH=3;BYMONTHDAY=8')).toBe(
      'Every year in March on the 8th',
    )
  })
  it('round-trips presets', () => {
    const presets: Preset[] = [
      { kind: 'days', interval: 4 },
      { kind: 'weekly', days: ['MO', 'TH'], interval: 1 },
      { kind: 'monthly', day: -1, interval: 1 },
      { kind: 'yearly', month: 12, day: 25 },
    ]
    for (const p of presets) expect(presetFromRRule(buildRRule(p))).toEqual(p)
    expect(presetFromRRule('FREQ=MONTHLY;BYDAY=MO')).toBeNull()
  })
})
