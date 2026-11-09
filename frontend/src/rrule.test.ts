import { describe, expect, it } from 'vitest'
import { buildRRule, type Preset } from './rrule'

describe('buildRRule', () => {
  const ok: [string, Preset, string][] = [
    ['every day', { kind: 'days', interval: 1 }, 'FREQ=DAILY'],
    ['every 3 days', { kind: 'days', interval: 3 }, 'FREQ=DAILY;INTERVAL=3'],
    ['mon/wed/fri', { kind: 'weekly', days: ['MO', 'WE', 'FR'] }, 'FREQ=WEEKLY;BYDAY=MO,WE,FR'],
    [
      'unordered input is sorted',
      { kind: 'weekly', days: ['SU', 'MO'] },
      'FREQ=WEEKLY;BYDAY=MO,SU',
    ],
  ]
  it.each(ok)('%s', (_n, preset, want) => {
    expect(buildRRule(preset)).toBe(want)
  })

  const bad: [string, Preset, string][] = [
    ['no weekday', { kind: 'weekly', days: [] }, 'at least one weekday'],
    ['zero interval', { kind: 'days', interval: 0 }, 'at least 1'],
    ['fractional interval', { kind: 'days', interval: 1.5 }, 'whole number'],
  ]
  it.each(bad)('rejects %s', (_n, preset, msg) => {
    expect(() => buildRRule(preset)).toThrow(msg)
  })
})
