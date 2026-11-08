import { describe, expect, it } from 'vitest'
import { formatLongDate, formatMonthYear, monthName, weekdayName } from './format'
import { setLocale } from './index'
import { describeRRule } from '../rrule'
import { describeRule } from '../items/logic'

describe('format helpers (en)', () => {
  it('names weekdays from Sunday = 0', () => {
    expect(weekdayName(0)).toBe('Sunday')
    expect(weekdayName(1, 'short')).toBe('Mon')
    expect(weekdayName(6)).toBe('Saturday')
    expect(weekdayName(7)).toBe('Sunday')
  })

  it('names months from January = 1', () => {
    expect(monthName(1)).toBe('January')
    expect(monthName(12, 'short')).toBe('Dec')
  })

  it('formats date-only strings without a timezone shift', () => {
    expect(formatLongDate('2026-11-08')).toBe('Sunday, November 8, 2026')
    expect(formatMonthYear('2026-11-08')).toBe('November 2026')
  })
})

describe('format helpers (zh-CN)', () => {
  it('follows the active locale', async () => {
    await setLocale('zh-CN')
    try {
      expect(weekdayName(0)).toBe('星期日')
      expect(weekdayName(1, 'short')).toBe('周一')
      expect(monthName(3)).toBe('三月')
      expect(formatLongDate('2026-11-08')).toContain('2026年11月8日')
      expect(formatMonthYear('2026-11-08')).toBe('2026年11月')
    } finally {
      await setLocale('en')
    }
  })

  it('describes RRULEs and rules in the active locale', async () => {
    await setLocale('zh-CN')
    try {
      expect(describeRRule('FREQ=DAILY')).toBe('每天')
      expect(describeRRule('FREQ=WEEKLY;BYDAY=MO,FR')).toBe('每周星期一、星期五')
      expect(describeRRule('FREQ=YEARLY;BYMONTH=3;BYMONTHDAY=8')).toBe('每年三月8日')
      expect(describeRule({ schedule_type: 'rolling', rolling_interval: 7, rolling_unit: 'day' })).toBe('每 7 天')
    } finally {
      await setLocale('en')
    }
  })
})
