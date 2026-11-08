import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import Heatmap from './Heatmap.vue'
import { buildGrid, cellLabel, levelBucket, monthLabels, weekdayNames, type HeatmapCell } from '../items/heatmap'

const day = (date: string, level = 0, extra: Partial<HeatmapCell> = {}): HeatmapCell => ({ date, level, value: 0, ...extra })

describe('heatmap logic', () => {
  it('pads the first column by week start', () => {
    // 2026-11-04 is a Wednesday
    const sun = buildGrid([day('2026-11-04')], 0)
    expect(sun[0].filter((c) => c === null)).toHaveLength(3)
    const mon = buildGrid([day('2026-11-04')], 1)
    expect(mon[0].filter((c) => c === null)).toHaveLength(2)
    expect(buildGrid([], 1)).toEqual([])
  })

  it('fills missing days and sorts', () => {
    const g = buildGrid([day('2026-11-09', 1), day('2026-11-02')], 1).flat()
    const dates = g.filter((c) => c).map((c) => c!.date)
    expect(dates).toHaveLength(8)
    expect(dates[0]).toBe('2026-11-02')
    expect(dates[7]).toBe('2026-11-09')
    expect(g.find((c) => c?.date === '2026-11-05')?.level).toBe(0)
  })

  it('computes level buckets', () => {
    expect(levelBucket(0)).toBe(0)
    expect(levelBucket(NaN)).toBe(0)
    expect(levelBucket(0.01)).toBe(1)
    expect(levelBucket(0.5)).toBe(2)
    expect(levelBucket(0.75)).toBe(3)
    expect(levelBucket(0.99)).toBe(3)
    expect(levelBucket(1)).toBe(4)
    expect(levelBucket(2)).toBe(4)
  })

  it('names weekdays and month labels', () => {
    expect(weekdayNames(1)).toEqual(['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'])
    expect(weekdayNames(0)[0]).toBe('Sun')
    const days = Array.from({ length: 70 }, (_, i) => day(new Date(Date.UTC(2026, 8, 1 + i)).toISOString().slice(0, 10)))
    const labels = monthLabels(buildGrid(days, 1)).filter(Boolean)
    expect(labels).toEqual(['Sep', 'Oct', 'Nov'])
  })

  it('builds labels without names and honours redaction', () => {
    const c = day('2026-11-05', 0.5, { value: 1, options_met: 1, options_total: 2 })
    expect(cellLabel(c)).toBe('2026-11-05: partly completed, 1 of 2 options')
    expect(cellLabel(c, { redact: true })).toBe('2026-11-05: partly completed')
    expect(cellLabel(day('2026-11-06'))).toBe('2026-11-06: no check-in')
    expect(cellLabel(day('2026-11-07', 1, { value: 1, options_met: 1, options_total: 1 }))).toBe('2026-11-07: completed')
    const neg = day('2026-11-08', 1, { value: 3 })
    expect(cellLabel(neg, { negative: true })).toBe('2026-11-08: occurred 3 times')
    expect(cellLabel(neg, { negative: true, redact: true })).toBe('2026-11-08: occurred')
    expect(cellLabel(day('2026-11-09'), { negative: true })).toBe('2026-11-09: none')
  })
})

describe('Heatmap component', () => {
  const days = [day('2026-11-05', 1, { options_met: 1, options_total: 1 }), day('2026-11-06', 0.5, { options_met: 1, options_total: 2 }), day('2026-11-07')]

  it('renders cells with aria labels, one tab stop and levels', () => {
    const w = mount(Heatmap, { props: { days, weekStart: 1 } })
    const cells = w.findAll('[data-date]')
    expect(cells).toHaveLength(3)
    expect(cells[0].attributes('aria-label')).toBe('2026-11-05: completed')
    expect(cells[0].attributes('data-level')).toBe('4')
    expect(cells[1].attributes('data-level')).toBe('2')
    expect(w.findAll('[tabindex="0"]')).toHaveLength(1)
    expect(w.find('[data-date="2026-11-07"]').attributes('tabindex')).toBe('0')
    expect(w.find('[data-testid=heatmap-legend]').text()).toContain('All')
  })

  it('shows tooltip on hover and moves focus with arrow keys', async () => {
    const w = mount(Heatmap, { props: { days }, attachTo: document.body })
    await w.find('[data-date="2026-11-06"]').trigger('mouseenter')
    expect(w.find('[data-testid=heatmap-tip]').text()).toContain('1 of 2 options')
    await w.find('[data-date="2026-11-06"]').trigger('keydown', { key: 'ArrowDown' })
    await new Promise((r) => setTimeout(r))
    expect(document.activeElement?.getAttribute('data-date')).toBe('2026-11-07')
    w.unmount()
  })

  it('redacts counts and uses negative colours', async () => {
    const w = mount(Heatmap, { props: { days, redact: true } })
    expect(w.find('[data-date="2026-11-06"]').attributes('aria-label')).not.toContain('options')
    const n = mount(Heatmap, { props: { days: [day('2026-11-05', 1, { value: 2 })], negative: true } })
    expect(n.find('[data-date]').classes()).toContain('bg-error')
  })
})
