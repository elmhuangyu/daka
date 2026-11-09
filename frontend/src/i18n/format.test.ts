import { describe, expect, it } from 'vitest'
import {
  currentDateFormat,
  dateFormatPlaceholder,
  formatCustomDate,
  formatLongDate,
  formatMonthYear,
  monthName,
  parseCustomDate,
  setDateFormat,
  weekdayName,
} from './format'
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
      expect(
        describeRule({ schedule_type: 'rolling', rolling_interval: 7, rolling_unit: 'day' }),
      ).toBe('每 7 天')
    } finally {
      await setLocale('en')
    }
  })
})

describe('date format settings and custom parsing/formatting', () => {
  it('formats custom dates according to format', () => {
    expect(formatCustomDate('2026-11-09', 'YYYY-MM-DD')).toBe('2026-11-09')
    expect(formatCustomDate('2026-11-09', 'YYYY/MM/DD')).toBe('2026/11/09')
    expect(formatCustomDate('2026-11-09', 'MM/DD/YYYY')).toBe('11/09/2026')
    expect(formatCustomDate('2026-11-09', 'DD/MM/YYYY')).toBe('09/11/2026')
    expect(formatCustomDate('')).toBe('')
    expect(formatCustomDate('invalid')).toBe('invalid')
  })

  it('updates reactive activeDateFormat when setDateFormat is called', () => {
    try {
      setDateFormat('YYYY/MM/DD')
      expect(currentDateFormat()).toBe('YYYY/MM/DD')
      expect(formatCustomDate('2026-11-09')).toBe('2026/11/09')
      expect(dateFormatPlaceholder()).toBe('YYYY/MM/DD')

      setDateFormat('MM/DD/YYYY')
      expect(currentDateFormat()).toBe('MM/DD/YYYY')
      expect(formatCustomDate('2026-11-09')).toBe('11/09/2026')
      expect(dateFormatPlaceholder()).toBe('MM/DD/YYYY')

      setDateFormat('DD/MM/YYYY')
      expect(currentDateFormat()).toBe('DD/MM/YYYY')
      expect(formatCustomDate('2026-11-09')).toBe('09/11/2026')
      expect(dateFormatPlaceholder()).toBe('DD/MM/YYYY')
    } finally {
      setDateFormat('YYYY-MM-DD')
    }
  })

  it('parses custom date inputs accurately', () => {
    // Year-first inputs
    expect(parseCustomDate('2026-11-09')).toBe('2026-11-09')
    expect(parseCustomDate('2026/11/09')).toBe('2026-11-09')
    expect(parseCustomDate('2026.11.09')).toBe('2026-11-09')

    // MM/DD/YYYY
    expect(parseCustomDate('11/09/2026', 'MM/DD/YYYY')).toBe('2026-11-09')
    expect(parseCustomDate('11-09-2026', 'MM/DD/YYYY')).toBe('2026-11-09')

    // DD/MM/YYYY
    expect(parseCustomDate('09/11/2026', 'DD/MM/YYYY')).toBe('2026-11-09')
    expect(parseCustomDate('09-11-2026', 'DD/MM/YYYY')).toBe('2026-11-09')

    // Disambiguation: 25/11/2026 must be DD/MM/YYYY even if format was MM/DD/YYYY
    expect(parseCustomDate('25/11/2026', 'MM/DD/YYYY')).toBe('2026-11-25')
    // Disambiguation: 11/25/2026 must be MM/DD/YYYY even if format was DD/MM/YYYY
    expect(parseCustomDate('11/25/2026', 'DD/MM/YYYY')).toBe('2026-11-25')

    // Invalid dates
    expect(parseCustomDate('')).toBeNull()
    expect(parseCustomDate('random')).toBeNull()
    expect(parseCustomDate('2026-02-29')).toBeNull() // non leap-year
    expect(parseCustomDate('2024-02-29')).toBe('2024-02-29') // leap-year
    expect(parseCustomDate('2026-13-01')).toBeNull()
    expect(parseCustomDate('2026-04-31')).toBeNull()
  })
})
